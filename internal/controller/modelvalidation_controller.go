// Package controller provides controllers for managing ModelValidation resources
package controller

import (
	"context"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/tracker"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ModelValidationReconciler reconciles ModelValidation objects
type ModelValidationReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Tracker tracker.StatusTracker
}

// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=modelvalidations,verbs=get;list;watch
// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=modelvalidations/status,verbs=update

// Reconcile handles ModelValidation events to track creation/updates/deletion
func (r *ModelValidationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	mv := &v1alpha1.ModelValidation{}
	if err := r.Get(ctx, req.NamespacedName, mv); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ModelValidation deleted, removing from tracking", "modelvalidation", req.NamespacedName)
			r.Tracker.RemoveModelValidation(req.NamespacedName)
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to get ModelValidation")
		return reconcile.Result{}, err
	}

	if !mv.DeletionTimestamp.IsZero() {
		logger.Info("ModelValidation being deleted, removing from tracking", "modelvalidation", req.NamespacedName)
		r.Tracker.RemoveModelValidation(req.NamespacedName)
		return reconcile.Result{}, nil
	}

	logger.Info("ModelValidation created/updated, adding to tracking", "modelvalidation", req.NamespacedName)
	r.Tracker.AddModelValidation(ctx, mv)

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ModelValidationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ModelValidation{}).
		Complete(r)
}
