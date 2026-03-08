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

package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	testingv1alpha1 "github.com/networkengineer-cloud/testrun-operator/api/v1alpha1"
)

func hmacSign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestValidateSignature(t *testing.T) {
	secret := []byte("mysecret")
	body := []byte(`{"reason":"upgrade succeeded"}`)

	h := &Handler{HMACSecret: secret}

	t.Run("valid signature", func(t *testing.T) {
		if !h.validateSignature(hmacSign(secret, body), body) {
			t.Error("expected valid signature to pass")
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		sig := hmacSign([]byte("wrong"), body)
		if h.validateSignature(sig, body) {
			t.Error("expected wrong-secret signature to fail")
		}
	})

	t.Run("missing prefix", func(t *testing.T) {
		mac := hmac.New(sha256.New, secret)
		mac.Write(body)
		if h.validateSignature(hex.EncodeToString(mac.Sum(nil)), body) {
			t.Error("expected missing prefix to fail")
		}
	})

	t.Run("empty header with no secret configured", func(t *testing.T) {
		h2 := &Handler{}
		if !h2.validateSignature("", body) {
			t.Error("expected empty secret to always pass")
		}
	})
}

func TestHandlerDedup(t *testing.T) {
	secret := []byte("sec")
	scheme := runtime.NewScheme()
	_ = testingv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	hrt := &testingv1alpha1.HelmReleaseTest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-hrt",
			Namespace: "default",
		},
		Spec: testingv1alpha1.HelmReleaseTestSpec{
			HelmReleaseRef:   testingv1alpha1.ObjectReference{Name: "my-release", Namespace: "default"},
			KustomizationRef: testingv1alpha1.ObjectReference{Name: "my-kust", Namespace: "default"},
			CronJobRef:       testingv1alpha1.ObjectReference{Name: "my-cron", Namespace: "default"},
		},
	}

	// A recent Job with the source label — should trigger dedup.
	recentJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-hrt-abcde",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
			Labels: map[string]string{
				"app":                        "helm-release-test",
				"testing.platform.io/source": "my-hrt",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(hrt, recentJob).Build()
	handler := &Handler{Client: fakeClient, HMACSecret: secret}

	if !handler.isDuplicate(t.Context(), hrt) {
		t.Error("expected isDuplicate to return true when recent job exists")
	}
}

func TestHandlerIgnoresNonUpgradeEvents(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = testingv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	handler := &Handler{Client: fakeClient}
	mux := NewMux(fakeClient, nil)

	payload := FluxEvent{
		Reason:         "upgrade failed",
		InvolvedObject: FluxEventObject{Kind: "HelmRelease", Name: "foo", Namespace: "default"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/trigger", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	_ = handler
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rr.Code)
	}
}
