// Package controller provides controllers for managing ModelValidation resources
package controller

import (
	"context"

	"github.com/sigstore/model-validation-operator/internal/constants"
	"github.com/sigstore/model-validation-operator/internal/tracker"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// PodReconciler reconciles Pod objects to track injected pods
type PodReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Tracker tracker.StatusTracker
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update
// +kubebuilder:rbac:groups="",resources=pods/finalizers,verbs=update
// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=modelvalidations,verbs=get;list
// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=modelvalidations/status,verbs=update

// Reconcile handles pod events to update ModelValidation status
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("Pod deleted, removing from tracking", "pod", req.NamespacedName)
			if err := r.Tracker.RemovePodByName(ctx, req.NamespacedName); err != nil {
				logger.Error(err, "Failed to remove deleted pod from tracking", "pod", req.NamespacedName)
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if !pod.DeletionTimestamp.IsZero() {
		logger.Info("Handling pod deletion", "pod", req.NamespacedName)

		if err := r.Tracker.RemovePodEvent(ctx, pod.UID); err != nil {
			logger.Error(err, "Failed to remove pod from tracking")
			return reconcile.Result{}, err
		}

		if controllerutil.ContainsFinalizer(pod, constants.ModelValidationFinalizer) {
			controllerutil.RemoveFinalizer(pod, constants.ModelValidationFinalizer)
			if err := r.Update(ctx, pod); err != nil {
				logger.Error(err, "Failed to remove finalizer from pod")
				return reconcile.Result{}, err
			}
		}

		logger.Info("Successfully handled pod deletion", "pod", req.NamespacedName)
		return reconcile.Result{}, nil
	}

	modelValidationName, ok := pod.Labels[constants.ModelValidationLabel]
	if !ok || modelValidationName == "" {
		// Try to remove the pod in case it was previously tracked but label was removed
		if err := r.Tracker.RemovePodByName(ctx, req.NamespacedName); err != nil {
			logger.Error(err, "Failed to remove pod without label from tracking", "pod", req.NamespacedName)
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}
	if err := r.Tracker.ProcessPodEvent(ctx, pod); err != nil {
		logger.Error(err, "Failed to process pod event")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}
