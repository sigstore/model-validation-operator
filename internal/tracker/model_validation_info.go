// Package tracker provides status tracking functionality for ModelValidation resources
package tracker

import (
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
func NewModelValidationInfo(configHash, authMethod string, observedGeneration int64) *ModelValidationInfo {
	return &ModelValidationInfo{
		ConfigHash:         configHash,
		AuthMethod:         authMethod,
		ObservedGeneration: observedGeneration,
		InjectedPods:       make(map[types.UID]*PodInfo),
		UninjectedPods:     make(map[types.UID]*PodInfo),
		OrphanedPods:       make(map[types.UID]*PodInfo),
	}
}

// AddInjectedPod adds a pod with finalizer, automatically determining if it's injected or orphaned
func (mvi *ModelValidationInfo) AddInjectedPod(podInfo *PodInfo) {
	mvi.RemovePod(podInfo.UID)

	// Determine if pod is orphaned based on configuration drift
	isOrphaned := (podInfo.ConfigHash != "" && podInfo.ConfigHash != mvi.ConfigHash) ||
		(podInfo.AuthMethod != "" && podInfo.AuthMethod != mvi.AuthMethod)

	if isOrphaned {
		mvi.OrphanedPods[podInfo.UID] = podInfo
	} else {
		mvi.InjectedPods[podInfo.UID] = podInfo
	}
}

// AddUninjectedPod adds a pod to the uninjected category
func (mvi *ModelValidationInfo) AddUninjectedPod(podInfo *PodInfo) {
	mvi.RemovePod(podInfo.UID)
	mvi.UninjectedPods[podInfo.UID] = podInfo
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
