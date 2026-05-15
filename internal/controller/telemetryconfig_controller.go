// Copyright 2025 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"time"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TelemetryConfigReconciler reconciles TelemetryConfig objects.
type TelemetryConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=telemetryconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=telemetryconfigs/status,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=modelvalidations,verbs=get;list;watch

// Reconcile evaluates selectors and updates matched counts in status.
func (r *TelemetryConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	tc := &v1alpha1.TelemetryConfig{}
	if err := r.Get(ctx, req.NamespacedName, tc); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if !tc.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	nsCount, err := r.countMatchedNamespaces(ctx, tc)
	if err != nil {
		logger.Error(err, "failed to count matched namespaces")
		return reconcile.Result{}, err
	}

	mvCount, err := r.countMatchedModelValidations(ctx, tc)
	if err != nil {
		logger.Error(err, "failed to count matched ModelValidations")
		return reconcile.Result{}, err
	}

	tc.Status.MatchedNamespaceCount = nsCount
	tc.Status.MatchedModelValidationCount = mvCount
	tc.Status.LastApplied = metav1.NewTime(time.Now())

	readyCond := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: tc.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
		Reason:             "SelectorsEvaluated",
		Message:            "Selectors evaluated successfully",
	}
	setCondition(&tc.Status.Conditions, readyCond)

	if err := r.Status().Update(ctx, tc); err != nil {
		logger.Error(err, "failed to update TelemetryConfig status")
		return reconcile.Result{}, err
	}

	logger.Info("TelemetryConfig reconciled",
		"matchedNamespaces", nsCount,
		"matchedModelValidations", mvCount,
	)
	return reconcile.Result{}, nil
}

func (r *TelemetryConfigReconciler) countMatchedNamespaces(ctx context.Context, tc *v1alpha1.TelemetryConfig) (int32, error) {
	var nsList corev1.NamespaceList
	if err := r.List(ctx, &nsList); err != nil {
		return 0, err
	}

	if tc.Spec.NamespaceSelector == nil {
		return int32(len(nsList.Items)), nil
	}

	sel, err := metav1.LabelSelectorAsSelector(tc.Spec.NamespaceSelector)
	if err != nil {
		return 0, err
	}

	var count int32
	for i := range nsList.Items {
		if sel.Matches(labels.Set(nsList.Items[i].Labels)) {
			count++
		}
	}
	return count, nil
}

func (r *TelemetryConfigReconciler) countMatchedModelValidations(ctx context.Context, tc *v1alpha1.TelemetryConfig) (int32, error) {
	var mvList v1alpha1.ModelValidationList
	if err := r.List(ctx, &mvList); err != nil {
		return 0, err
	}

	if tc.Spec.Selector == nil {
		return int32(len(mvList.Items)), nil
	}

	sel, err := metav1.LabelSelectorAsSelector(tc.Spec.Selector)
	if err != nil {
		return 0, err
	}

	var count int32
	for i := range mvList.Items {
		if sel.Matches(labels.Set(mvList.Items[i].Labels)) {
			count++
		}
	}
	return count, nil
}

// setCondition updates or appends a condition in the slice.
func setCondition(conditions *[]metav1.Condition, cond metav1.Condition) {
	for i, existing := range *conditions {
		if existing.Type == cond.Type {
			(*conditions)[i] = cond
			return
		}
	}
	*conditions = append(*conditions, cond)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TelemetryConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TelemetryConfig{}).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.mapNamespaceToTelemetryConfigs)).
		Watches(&v1alpha1.ModelValidation{}, handler.EnqueueRequestsFromMapFunc(r.mapModelValidationToTelemetryConfigs)).
		Complete(r)
}

// mapNamespaceToTelemetryConfigs re-enqueues all TelemetryConfigs when a namespace changes,
// since namespace label changes could affect selector matching.
func (r *TelemetryConfigReconciler) mapNamespaceToTelemetryConfigs(ctx context.Context, _ client.Object) []reconcile.Request {
	var tcList v1alpha1.TelemetryConfigList
	if err := r.List(ctx, &tcList); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, len(tcList.Items))
	for i, tc := range tcList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&tc),
		}
	}
	return requests
}

// mapModelValidationToTelemetryConfigs re-enqueues all TelemetryConfigs when a ModelValidation changes,
// since MV label changes could affect selector matching.
func (r *TelemetryConfigReconciler) mapModelValidationToTelemetryConfigs(ctx context.Context, _ client.Object) []reconcile.Request {
	var tcList v1alpha1.TelemetryConfigList
	if err := r.List(ctx, &tcList); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, len(tcList.Items))
	for i, tc := range tcList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&tc),
		}
	}
	return requests
}
