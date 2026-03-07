/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostCommitStatusEmptySHA(t *testing.T) {
	// When SHA is empty, PostCommitStatus should be a no-op (no error).
	err := PostCommitStatus(context.Background(), "", "ctx", "success", "ok")
	if err != nil {
		t.Errorf("expected no error for empty SHA, got: %v", err)
	}
}

func TestPostCommitStatusSuccess(t *testing.T) {
	var received commitStatusPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	// Patch the API call to use our test server by temporarily overriding the
	// URL-building logic via environment. We do this by constructing a custom
	// request directly; since PostCommitStatus is a thin wrapper, we test the
	// full path by redirecting http.DefaultClient via a transport shim.
	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteTransport{base: http.DefaultTransport, target: srv.URL}
	defer func() { http.DefaultTransport = origTransport }()

	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GITHUB_REPO", "owner/repo")

	err := PostCommitStatus(context.Background(), "abc123", "helm-release-tests/mytest", "success", "passed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received.State != "success" {
		t.Errorf("expected state=success, got %q", received.State)
	}
	if received.Context != "helm-release-tests/mytest" {
		t.Errorf("expected context=helm-release-tests/mytest, got %q", received.Context)
	}
}

// rewriteTransport rewrites the host of all requests to the given target URL.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Host = rt.target[len("http://"):]
	req2.URL.Scheme = "http"
	return rt.base.RoundTrip(req2)
}
