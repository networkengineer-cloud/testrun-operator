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

package controller

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	testingv1alpha1 "github.com/networkengineer-cloud/testrun-operator/api/v1alpha1"
	"github.com/networkengineer-cloud/testrun-operator/internal/github"
	"github.com/networkengineer-cloud/testrun-operator/internal/kustomization"
)

const (
	labelSource             = "testing.platform.io/source"
	annotationHelmReleaseNS = "testing.platform.io/helmreleasetest-namespace"

	conditionTypeTestPassed = "TestPassed"

	resultPassed = "passed"
	resultFailed = "failed"
)

// HelmReleaseTestReconciler watches batch/v1 Jobs created by the webhook and updates
// the parent HelmReleaseTest status on completion.
type HelmReleaseTestReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	GitHubPoster *github.Poster
}

// +kubebuilder:rbac:groups=testing.testing.platform.io,resources=helmreleasetests,verbs=get;list;watch
// +kubebuilder:rbac:groups=testing.testing.platform.io,resources=helmreleasetests/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=create;get;list;watch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list
// +kubebuilder:rbac:groups=kustomize.toolkit.fluxcd.io,resources=kustomizations,verbs=get;list
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list

// Reconcile processes completed test Jobs and updates HelmReleaseTest status.
func (r *HelmReleaseTestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	logger.V(1).Info("Reconciling Job", "job", req.NamespacedName)

	var job batchv1.Job
	if err := r.Get(ctx, req.NamespacedName, &job); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip Jobs that haven't completed yet.
	if job.Status.CompletionTime == nil {
		logger.V(1).Info("Job not yet completed, skipping", "job", job.Name)
		return ctrl.Result{}, nil
	}

	hrtName, ok := job.Labels[labelSource]
	if !ok {
		logger.Info("Job missing source label, skipping", "job", job.Name)
		return ctrl.Result{}, nil
	}

	hrtNamespace := job.Annotations[annotationHelmReleaseNS]
	if hrtNamespace == "" {
		hrtNamespace = job.Namespace
	}

	var hrt testingv1alpha1.HelmReleaseTest
	if err := r.Get(ctx, client.ObjectKey{Name: hrtName, Namespace: hrtNamespace}, &hrt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Already processed this Job — avoid redundant status patches and duplicate GitHub API calls.
	if hrt.Status.LastRunJob == job.Name {
		return ctrl.Result{}, nil
	}

	passed := job.Status.Succeeded > 0
	result := resultFailed
	if passed {
		result = resultPassed
	}

	// Lazy SHA resolution from Kustomization.
	kRef := hrt.Spec.KustomizationRef
	kNS := kRef.Namespace
	if kNS == "" {
		kNS = hrt.Namespace
	}
	sha, err := kustomization.ResolveCommitSHA(ctx, r.Client, kRef.Name, kNS)
	if err != nil {
		logger.Error(err, "Failed to resolve commit SHA from Kustomization")
		// Non-fatal: continue to update status and skip GitHub report.
	}

	// Update HelmReleaseTest status.
	now := metav1.Now()
	patch := client.MergeFrom(hrt.DeepCopy())
	hrt.Status.LastRunJob = job.Name
	hrt.Status.LastRunTime = &now
	hrt.Status.LastRunResult = result
	hrt.Status.LastCommitSHA = sha

	condStatus := metav1.ConditionTrue
	condReason := "TestPassed"
	condMsg := fmt.Sprintf("Job %s passed", job.Name)
	if !passed {
		condStatus = metav1.ConditionFalse
		condReason = "TestFailed"
		condMsg = fmt.Sprintf("Job %s failed", job.Name)
	}
	apimeta.SetStatusCondition(&hrt.Status.Conditions, metav1.Condition{
		Type:               conditionTypeTestPassed,
		Status:             condStatus,
		Reason:             condReason,
		Message:            condMsg,
		ObservedGeneration: hrt.Generation,
	})

	if err := r.Status().Patch(ctx, &hrt, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching HelmReleaseTest status: %w", err)
	}

	// Post GitHub commit status.
	if sha == "" {
		logger.Info("Commit SHA unavailable, skipping GitHub status post",
			"helmreleasetest", hrt.Name)
	} else {
		ghState := "failure"
		if passed {
			ghState = "success"
		}
		ghCtx := fmt.Sprintf("helm-release-tests/%s", hrt.Name)
		if err := r.GitHubPoster.PostCommitStatus(ctx, sha, ghCtx, ghState, condMsg); err != nil {
			logger.Error(err, "Failed to post GitHub commit status")
			// Non-fatal: status already patched.
		}
	}

	logger.Info("Processed test Job",
		"job", job.Name,
		"helmreleasetest", hrt.Name,
		"result", result,
		"sha", sha)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller to watch batch/v1 Jobs filtered by
// the app=helm-release-test label.
func (r *HelmReleaseTestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	labelSelector := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetLabels()["app"] == "helm-release-test"
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1.Job{}, builder.WithPredicates(labelSelector)).
		Named("helmreleasetest").
		Complete(r)
}
