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

// Package github provides a GitHub commit status reporter.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const apiTimeout = 10 * time.Second

// commitStatusPayload is the request body for the GitHub commit status API.
type commitStatusPayload struct {
	State       string `json:"state"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context"`
}

// PostCommitStatus posts a GitHub commit status for the given SHA.
// It reads GITHUB_TOKEN and GITHUB_REPO (format "owner/repo") from the environment.
// If sha is empty, the call is skipped with a warning log.
func PostCommitStatus(ctx context.Context, sha, contextName, state, description string) error {
	if sha == "" {
		// Log is handled by caller; no-op here.
		return nil
	}

	token := os.Getenv("GITHUB_TOKEN")
	repo := os.Getenv("GITHUB_REPO")
	if token == "" || repo == "" {
		return fmt.Errorf("GITHUB_TOKEN and GITHUB_REPO must be set")
	}

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("GITHUB_REPO must be in owner/repo format, got: %q", repo)
	}
	owner, repoName := parts[0], parts[1]

	payload := commitStatusPayload{
		State:       state,
		Description: description,
		Context:     contextName,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling commit status payload: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/statuses/%s", owner, repoName, sha)

	reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building GitHub request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting GitHub commit status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
