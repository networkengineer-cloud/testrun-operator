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

package kustomization

import (
	"testing"
)

func TestParseSHA(t *testing.T) {
	cases := []struct {
		revision string
		want     string
	}{
		{"main@sha1:abc123def456", "abc123def456"},
		{"main@sha1:a1b2c3", "a1b2c3"},
		{"sha1:deadbeef", "deadbeef"},
		{"nocolon", ""},
		{"trailing:", ""},
		{"", ""},
		{"main@sha1:abc:extra", "extra"},
	}
	for _, tc := range cases {
		got := parseSHA(tc.revision)
		if got != tc.want {
			t.Errorf("parseSHA(%q) = %q, want %q", tc.revision, got, tc.want)
		}
	}
}
