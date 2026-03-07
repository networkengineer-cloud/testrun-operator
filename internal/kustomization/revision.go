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

// Package kustomization provides helpers for reading Flux Kustomization status.
package kustomization

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var kustomizationGVK = schema.GroupVersionKind{
	Group:   "kustomize.toolkit.fluxcd.io",
	Version: "v1",
	Kind:    "Kustomization",
}

// ResolveCommitSHA fetches the Flux Kustomization identified by name/namespace and
// extracts the commit SHA from status.lastAppliedRevision (format: "main@sha1:<sha>").
// Returns an empty string (with a warning log) if the SHA cannot be resolved.
func ResolveCommitSHA(ctx context.Context, c client.Client, name, namespace string) (string, error) {
	logger := log.FromContext(ctx)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(kustomizationGVK)

	if err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, obj); err != nil {
		return "", fmt.Errorf("getting Kustomization %s/%s: %w", namespace, name, err)
	}

	revision, found, err := unstructured.NestedString(obj.Object, "status", "lastAppliedRevision")
	if err != nil || !found || revision == "" {
		logger.Info("Kustomization lastAppliedRevision not available, SHA will be empty",
			"kustomization", name, "namespace", namespace)
		return "", nil
	}

	sha := parseSHA(revision)
	if sha == "" {
		logger.Info("Could not parse SHA from revision string",
			"revision", revision, "kustomization", name)
	}
	return sha, nil
}

// parseSHA extracts the hex SHA from a revision string like "main@sha1:abc123".
// Returns everything after the last colon, or empty string if no colon is present.
func parseSHA(revision string) string {
	idx := strings.LastIndex(revision, ":")
	if idx < 0 || idx == len(revision)-1 {
		return ""
	}
	return revision[idx+1:]
}
