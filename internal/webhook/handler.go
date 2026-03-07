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

// Package webhook provides an HTTP handler for Flux notification events.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	testingv1alpha1 "github.com/networkengineer-cloud/testrun-operator/api/v1alpha1"
)

const (
	labelApp    = "app"
	labelSource = "testing.platform.io/source"
	labelAppVal = "helm-release-test"

	annotationHelmRelease       = "testing.platform.io/helmrelease"
	annotationHelmReleaseTestNS = "testing.platform.io/helmreleasetest-namespace"

	dedupWindow = 5 * time.Minute
)

// FluxEvent is the subset of a Flux notification event payload we care about.
type FluxEvent struct {
	Reason         string          `json:"reason"`
	InvolvedObject FluxEventObject `json:"involvedObject"`
}

// FluxEventObject describes the Kubernetes object the event is about.
type FluxEventObject struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Handler handles incoming Flux webhook events and creates test Jobs.
type Handler struct {
	Client     client.Client
	HMACSecret []byte
}

// ServeHTTP implements http.Handler for the /trigger endpoint.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := log.FromContext(r.Context())

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error(err, "Failed to read request body")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if !h.validateSignature(r.Header.Get("X-Signature"), body) {
		logger.Info("Invalid HMAC signature, ignoring request")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var event FluxEvent
	if err := json.Unmarshal(body, &event); err != nil {
		logger.Error(err, "Failed to parse Flux event JSON")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if !strings.EqualFold(event.Reason, "upgrade succeeded") ||
		event.InvolvedObject.Kind != "HelmRelease" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	logger.Info("Received HelmRelease upgrade succeeded event",
		"helmrelease", event.InvolvedObject.Name,
		"namespace", event.InvolvedObject.Namespace)

	if err := h.handleUpgrade(r.Context(), event); err != nil {
		logger.Error(err, "Error handling upgrade event")
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) validateSignature(header string, body []byte) bool {
	if len(h.HMACSecret) == 0 {
		return true
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	gotHex := strings.TrimPrefix(header, prefix)
	gotBytes, err := hex.DecodeString(gotHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, h.HMACSecret)
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(expected, gotBytes)
}

func (h *Handler) handleUpgrade(ctx context.Context, event FluxEvent) error {
	logger := log.FromContext(ctx)

	var testList testingv1alpha1.HelmReleaseTestList
	if err := h.Client.List(ctx, &testList); err != nil {
		return fmt.Errorf("listing HelmReleaseTests: %w", err)
	}

	for i := range testList.Items {
		hrt := &testList.Items[i]
		ref := hrt.Spec.HelmReleaseRef
		refNS := ref.Namespace
		if refNS == "" {
			refNS = hrt.Namespace
		}
		if ref.Name != event.InvolvedObject.Name || refNS != event.InvolvedObject.Namespace {
			continue
		}

		if h.isDuplicate(ctx, hrt) {
			logger.Info("Duplicate detected, skipping Job creation",
				"helmreleasetest", hrt.Name,
				"namespace", hrt.Namespace)
			continue
		}

		if err := h.createJob(ctx, hrt); err != nil {
			logger.Error(err, "Failed to create test Job",
				"helmreleasetest", hrt.Name,
				"namespace", hrt.Namespace)
		}
	}
	return nil
}

func (h *Handler) isDuplicate(ctx context.Context, hrt *testingv1alpha1.HelmReleaseTest) bool {
	logger := log.FromContext(ctx)
	sel := labels.SelectorFromSet(labels.Set{labelSource: hrt.Name})
	var jobList batchv1.JobList
	if err := h.Client.List(ctx, &jobList,
		client.InNamespace(hrt.Namespace),
		client.MatchingLabelsSelector{Selector: sel},
	); err != nil {
		logger.Error(err, "Failed to list Jobs for dedup check")
		return false
	}

	cutoff := time.Now().Add(-dedupWindow)
	for _, job := range jobList.Items {
		if job.CreationTimestamp.After(cutoff) {
			return true
		}
	}
	return false
}

func (h *Handler) createJob(ctx context.Context, hrt *testingv1alpha1.HelmReleaseTest) error {
	cronJobRef := hrt.Spec.CronJobRef
	cronJobNS := cronJobRef.Namespace
	if cronJobNS == "" {
		cronJobNS = hrt.Namespace
	}

	var cronJob batchv1.CronJob
	if err := h.Client.Get(ctx, client.ObjectKey{Name: cronJobRef.Name, Namespace: cronJobNS}, &cronJob); err != nil {
		return fmt.Errorf("fetching CronJob %s/%s: %w", cronJobNS, cronJobRef.Name, err)
	}

	backoffLimit := int32(0)
	ttl := int32(3600)

	jobTemplate := cronJob.Spec.JobTemplate.Spec.DeepCopy()
	jobTemplate.BackoffLimit = &backoffLimit
	jobTemplate.TTLSecondsAfterFinished = &ttl

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: hrt.Name + "-",
			Namespace:    hrt.Namespace,
			Labels: map[string]string{
				labelApp:    labelAppVal,
				labelSource: hrt.Name,
			},
			Annotations: map[string]string{
				annotationHelmRelease:       hrt.Spec.HelmReleaseRef.Name,
				annotationHelmReleaseTestNS: hrt.Namespace,
			},
		},
		Spec: *jobTemplate,
	}

	if err := h.Client.Create(ctx, job); err != nil {
		return fmt.Errorf("creating Job: %w", err)
	}

	log.FromContext(ctx).Info("Created test Job",
		"job", job.Name,
		"helmreleasetest", hrt.Name,
		"namespace", hrt.Namespace)
	return nil
}

// NewMux returns an http.ServeMux with the /trigger handler registered.
func NewMux(c client.Client, hmacSecret []byte) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/trigger", &Handler{Client: c, HMACSecret: hmacSecret})
	return mux
}
