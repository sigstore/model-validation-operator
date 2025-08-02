package tracker

import (
	"context"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// StatusTracker defines the interface for tracking ModelValidation and Pod events
type StatusTracker interface {
	// ModelValidation tracking methods
	AddModelValidation(ctx context.Context, mv *v1alpha1.ModelValidation)
	RemoveModelValidation(mvKey types.NamespacedName)

	// Pod tracking methods
	ProcessPodEvent(ctx context.Context, pod *corev1.Pod) error
	RemovePodEvent(ctx context.Context, podUID types.UID) error
	RemovePodByName(ctx context.Context, podName types.NamespacedName) error

	// Lifecycle methods
	Stop()
}
