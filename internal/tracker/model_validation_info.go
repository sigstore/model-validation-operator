// Package tracker provides status tracking functionality for ModelValidation resources
package tracker

import (
	"github.com/sigstore/model-validation-operator/internal/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// PodInfo stores information about a tracked pod
type PodInfo struct {
	Name       string
	Namespace  string
	UID        types.UID
	Timestamp  metav1.Time
	ConfigHash string
	AuthMethod string
}

// ModelValidationInfo consolidates all tracking information for a ModelValidation resource
type ModelValidationInfo struct {
	// Name of the ModelValidation CR
	Name string
	// Current configuration hash for drift detection
	ConfigHash string
	// Current authentication method
	AuthMethod string
	// Observed generation for detecting spec changes
	ObservedGeneration int64
	// Pods with finalizer and matching configuration
	InjectedPods map[types.UID]*PodInfo
	// Pods with label but no finalizer
	UninjectedPods map[types.UID]*PodInfo
	// Pods with finalizer but configuration drift
	OrphanedPods map[types.UID]*PodInfo
}

// NewModelValidationInfo creates a new ModelValidationInfo with initialized maps
func NewModelValidationInfo(name, configHash, authMethod string, observedGeneration int64) *ModelValidationInfo {
	return &ModelValidationInfo{
		Name:               name,
		ConfigHash:         configHash,
		AuthMethod:         authMethod,
		ObservedGeneration: observedGeneration,
		InjectedPods:       make(map[types.UID]*PodInfo),
		UninjectedPods:     make(map[types.UID]*PodInfo),
		OrphanedPods:       make(map[types.UID]*PodInfo),
	}
}

// getPodState returns the current state of a pod, or empty string if not found
func (mvi *ModelValidationInfo) getPodState(podUID types.UID) string {
	if _, exists := mvi.InjectedPods[podUID]; exists {
		return metrics.PodStateInjected
	}
	if _, exists := mvi.OrphanedPods[podUID]; exists {
		return metrics.PodStateOrphaned
	}
	if _, exists := mvi.UninjectedPods[podUID]; exists {
		return metrics.PodStateUninjected
	}
	return ""
}

// movePodToState removes pod from current state, assigns to new state, and records transition
func (mvi *ModelValidationInfo) movePodToState(podInfo *PodInfo, newState string, targetMap map[types.UID]*PodInfo) {
	prevState := mvi.getPodState(podInfo.UID)
	mvi.RemovePod(podInfo.UID)

	targetMap[podInfo.UID] = podInfo

	if prevState != "" && prevState != newState {
		metrics.RecordPodStateTransition(podInfo.Namespace, mvi.Name, prevState, newState)
	}
}

// AddInjectedPod adds a pod with finalizer, automatically determining if it's injected or orphaned
func (mvi *ModelValidationInfo) AddInjectedPod(podInfo *PodInfo) {
	// Determine if pod is orphaned based on configuration drift
	isOrphaned := (podInfo.ConfigHash != "" && podInfo.ConfigHash != mvi.ConfigHash) ||
		(podInfo.AuthMethod != "" && podInfo.AuthMethod != mvi.AuthMethod)

	if isOrphaned {
		mvi.movePodToState(podInfo, metrics.PodStateOrphaned, mvi.OrphanedPods)
	} else {
		mvi.movePodToState(podInfo, metrics.PodStateInjected, mvi.InjectedPods)
	}
}

// AddUninjectedPod adds a pod to the uninjected category
func (mvi *ModelValidationInfo) AddUninjectedPod(podInfo *PodInfo) {
	mvi.movePodToState(podInfo, metrics.PodStateUninjected, mvi.UninjectedPods)
}

// RemovePod removes a pod from all categories
func (mvi *ModelValidationInfo) RemovePod(podUID types.UID) bool {
	// A pod should only exist in one of these maps, so we can stop after first match
	// Check in order of likelihood: injected, orphaned, uninjected
	if _, exists := mvi.InjectedPods[podUID]; exists {
		delete(mvi.InjectedPods, podUID)
		return true
	}
	if _, exists := mvi.OrphanedPods[podUID]; exists {
		delete(mvi.OrphanedPods, podUID)
		return true
	}
	if _, exists := mvi.UninjectedPods[podUID]; exists {
		delete(mvi.UninjectedPods, podUID)
		return true
	}
	return false
}

// UpdateConfig updates the configuration information and returns any drifted pods
// This safely handles configuration changes by detecting pods that are now orphaned
func (mvi *ModelValidationInfo) UpdateConfig(configHash, authMethod string, observedGeneration int64) []*PodInfo {
	// If config hasn't changed, no drift possible
	if mvi.ConfigHash == configHash && mvi.AuthMethod == authMethod {
		// still update observed generation for tracking
		mvi.ObservedGeneration = observedGeneration
		return nil
	}

	// Get drifted pods before updating config using new parameters
	var driftedPods []*PodInfo
	for _, podInfo := range mvi.InjectedPods {
		if (podInfo.ConfigHash != "" && podInfo.ConfigHash != configHash) ||
			(podInfo.AuthMethod != "" && podInfo.AuthMethod != authMethod) {
			driftedPods = append(driftedPods, podInfo)
		}
	}

	// Update the config and observed generation
	mvi.ConfigHash = configHash
	mvi.AuthMethod = authMethod
	mvi.ObservedGeneration = observedGeneration

	// Move drifted pods to orphaned
	for _, podInfo := range driftedPods {
		delete(mvi.InjectedPods, podInfo.UID)
		mvi.OrphanedPods[podInfo.UID] = podInfo
	}

	return driftedPods
}

// GetAllPods returns all pods from all categories for cleanup
func (mvi *ModelValidationInfo) GetAllPods() []*PodInfo {
	totalPods := len(mvi.InjectedPods) + len(mvi.UninjectedPods) + len(mvi.OrphanedPods)
	allPods := make([]*PodInfo, 0, totalPods)
	for _, podInfo := range mvi.InjectedPods {
		allPods = append(allPods, podInfo)
	}
	for _, podInfo := range mvi.UninjectedPods {
		allPods = append(allPods, podInfo)
	}
	for _, podInfo := range mvi.OrphanedPods {
		allPods = append(allPods, podInfo)
	}
	return allPods
}
