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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObjectReference is a reference to a Kubernetes object by name and optional namespace.
type ObjectReference struct {
	// name is the name of the referenced object.
	// +required
	Name string `json:"name"`

	// namespace is the namespace of the referenced object.
	// Defaults to the HelmReleaseTest's namespace if omitted.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// HelmReleaseTestSpec defines the desired state of HelmReleaseTest
type HelmReleaseTestSpec struct {
	// helmReleaseRef is a reference to the HelmRelease to watch for successful upgrades.
	// +required
	HelmReleaseRef ObjectReference `json:"helmReleaseRef"`

	// kustomizationRef is a reference to the Kustomization used for commit SHA resolution.
	// +required
	KustomizationRef ObjectReference `json:"kustomizationRef"`

	// cronJobRef is a reference to a suspended CronJob whose jobTemplate will be copied
	// to create test Jobs on each successful HelmRelease upgrade.
	// +required
	CronJobRef ObjectReference `json:"cronJobRef"`
}

// HelmReleaseTestStatus defines the observed state of HelmReleaseTest.
type HelmReleaseTestStatus struct {
	// conditions represent the current state of the HelmReleaseTest resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// lastRunJob is the name of the most recently created test Job.
	// +optional
	LastRunJob string `json:"lastRunJob,omitempty"`

	// lastRunTime is the time the most recent test Job completed.
	// +optional
	LastRunTime *metav1.Time `json:"lastRunTime,omitempty"`

	// lastRunResult is the result of the most recent test Job ("passed" or "failed").
	// +optional
	LastRunResult string `json:"lastRunResult,omitempty"`

	// lastCommitSHA is the commit SHA resolved from the Kustomization at the time of the last test run.
	// +optional
	LastCommitSHA string `json:"lastCommitSHA,omitempty"`

	// lastTestedRevision is the HelmRelease revision that was last tested.
	// +optional
	LastTestedRevision string `json:"lastTestedRevision,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="HelmRelease",type="string",JSONPath=".spec.helmReleaseRef.name"
// +kubebuilder:printcolumn:name="Last Run",type="date",JSONPath=".status.lastRunTime"
// +kubebuilder:printcolumn:name="Result",type="string",JSONPath=".status.lastRunResult"
// +kubebuilder:printcolumn:name="SHA",type="string",JSONPath=".status.lastCommitSHA"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// HelmReleaseTest is the Schema for the helmreleasetests API
type HelmReleaseTest struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of HelmReleaseTest
	// +required
	Spec HelmReleaseTestSpec `json:"spec"`

	// status defines the observed state of HelmReleaseTest
	// +optional
	Status HelmReleaseTestStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// HelmReleaseTestList contains a list of HelmReleaseTest
type HelmReleaseTestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []HelmReleaseTest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HelmReleaseTest{}, &HelmReleaseTestList{})
}
