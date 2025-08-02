// Package tracker provides status tracking functionality for ModelValidation resources
package tracker

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// StatusTrackerImpl tracks injected pods and namespaces for ModelValidation resources
type StatusTrackerImpl struct {
	client client.Client
	mu     sync.RWMutex
	// Consolidated tracking information for each ModelValidation
	modelValidations map[types.NamespacedName]*ModelValidationInfo
	// Count of ModelValidation resources per namespace
	mvNamespaces map[string]int
	// Pod name/UID bidirectional mapping
	podMapping *PodMapping

	// Configuration
	statusUpdateTimeout time.Duration

	// Async update components
	debouncedQueue DebouncedQueue
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

// StatusTrackerConfig holds configuration options for the status tracker
type StatusTrackerConfig struct {
	DebounceDuration    time.Duration
	RetryBaseDelay      time.Duration
	RetryMaxDelay       time.Duration
	RateLimitQPS        float64
	RateLimitBurst      int
	StatusUpdateTimeout time.Duration
}

// NewStatusTracker creates a new status tracker with explicit configuration
func NewStatusTracker(client client.Client, config StatusTrackerConfig) StatusTracker {
	debouncedQueue := NewDebouncedQueue(DebouncedQueueConfig{
		DebounceDuration: config.DebounceDuration,
		RetryBaseDelay:   config.RetryBaseDelay,
		RetryMaxDelay:    config.RetryMaxDelay,
		RateLimitQPS:     config.RateLimitQPS,
		RateLimitBurst:   config.RateLimitBurst,
	})

	st := &StatusTrackerImpl{
		client:              client,
		modelValidations:    make(map[types.NamespacedName]*ModelValidationInfo),
		mvNamespaces:        make(map[string]int),
		podMapping:          NewPodMapping(),
		statusUpdateTimeout: config.StatusUpdateTimeout,
		debouncedQueue:      debouncedQueue,
		stopCh:              make(chan struct{}),
	}

	st.wg.Add(1)
	go st.asyncUpdateWorker()

	return st
}

// collectPodTrackingInfo is a helper to convert PodInfo to PodTrackingInfo
func collectPodTrackingInfo(pods map[types.UID]*PodInfo, result *[]v1alpha1.PodTrackingInfo) {
	if pods == nil {
		return
	}
	for _, podInfo := range pods {
		*result = append(*result, v1alpha1.PodTrackingInfo{
			Name:       podInfo.Name,
			UID:        string(podInfo.UID),
			InjectedAt: podInfo.Timestamp,
		})
	}
}

// collectPodsForCleanup is a helper to collect pod names for cleanup
func collectPodsForCleanup(allPods []*PodInfo, result *[]types.NamespacedName) {
	if allPods == nil {
		return
	}
	for _, podInfo := range allPods {
		*result = append(*result, types.NamespacedName{
			Name:      podInfo.Name,
			Namespace: podInfo.Namespace,
		})
	}
}

// Stop stops the async update worker
func (st *StatusTrackerImpl) Stop() {
	// Wait for pending updates before stopping
	st.debouncedQueue.WaitForUpdates()

	// Stop components
	close(st.stopCh)
	st.debouncedQueue.ShutDown()
	st.wg.Wait()
}

// asyncUpdateWorker processes status updates asynchronously
func (st *StatusTrackerImpl) asyncUpdateWorker() {
	defer st.wg.Done()

	for {
		select {
		case <-st.stopCh:
			return
		default:
			st.processNextUpdate()
		}
	}
}

// processNextUpdate processes the next update from the queue
func (st *StatusTrackerImpl) processNextUpdate() {
	mvKey, shutdown := st.debouncedQueue.Get()
	if shutdown {
		return
	}
	defer st.debouncedQueue.Done(mvKey)

	ctx, cancel := context.WithTimeout(context.Background(), st.statusUpdateTimeout)
	defer cancel()

	if err := st.doUpdateStatus(ctx, mvKey); err != nil {
		logger := log.FromContext(ctx)
		logger.V(1).Info("Status update failed, will retry",
			"modelvalidation", mvKey,
			"attempts", st.debouncedQueue.GetRetryCount(mvKey)+1,
			"error", err)
		st.debouncedQueue.AddWithRetry(mvKey)
	} else {
		st.debouncedQueue.ForgetRetries(mvKey)
	}
}

// WaitForUpdates waits for all pending status updates to complete
// This is useful in tests to ensure async operations finish before assertions
func (st *StatusTrackerImpl) WaitForUpdates() {
	st.debouncedQueue.WaitForCompletion()
}

// doUpdateStatus updates the ModelValidation status with current metrics
// This is called asynchronously from the update worker
func (st *StatusTrackerImpl) doUpdateStatus(ctx context.Context, mvKey types.NamespacedName) error {
	logger := log.FromContext(ctx)

	mv := &v1alpha1.ModelValidation{}
	if err := st.client.Get(ctx, mvKey, mv); err != nil {
		if errors.IsNotFound(err) {
			st.mu.Lock()
			delete(st.modelValidations, mvKey)
			st.mu.Unlock()
			return nil
		}
		return err
	}

	trackedPods := []v1alpha1.PodTrackingInfo{}
	uninjectedPods := []v1alpha1.PodTrackingInfo{}
	orphanedPods := []v1alpha1.PodTrackingInfo{}
	var trackedConfigHash, trackedAuthMethod string

	st.mu.RLock()
	mvInfo := st.modelValidations[mvKey]
	if mvInfo != nil {
		collectPodTrackingInfo(mvInfo.InjectedPods, &trackedPods)
		collectPodTrackingInfo(mvInfo.UninjectedPods, &uninjectedPods)
		collectPodTrackingInfo(mvInfo.OrphanedPods, &orphanedPods)
		trackedConfigHash = mvInfo.ConfigHash
		trackedAuthMethod = mvInfo.AuthMethod
	}
	st.mu.RUnlock()
	if mvInfo == nil {
		return nil
	}
	sort.Slice(trackedPods, func(i, j int) bool {
		return trackedPods[i].Name < trackedPods[j].Name
	})
	sort.Slice(uninjectedPods, func(i, j int) bool {
		return uninjectedPods[i].Name < uninjectedPods[j].Name
	})
	sort.Slice(orphanedPods, func(i, j int) bool {
		return orphanedPods[i].Name < orphanedPods[j].Name
	})

	// Check if ModelValidation parameters have changed since tracking began
	currentConfigHash := mv.GetConfigHash()
	currentAuthMethod := mv.GetAuthMethod()

	// Check for configuration drift between tracked and current state
	if trackedConfigHash != currentConfigHash || trackedAuthMethod != currentAuthMethod {
		logger.Error(fmt.Errorf("configuration drift detected"), "ModelValidation config drift during status update",
			"modelvalidation", mvKey,
			"oldConfigHash", trackedConfigHash,
			"newConfigHash", currentConfigHash,
			"oldAuthMethod", trackedAuthMethod,
			"newAuthMethod", currentAuthMethod)
		return fmt.Errorf(
			"configuration drift detected for ModelValidation %s: hash changed from %s to %s, auth method changed from %s to %s",
			mvKey, trackedConfigHash, currentConfigHash, trackedAuthMethod, currentAuthMethod)
	}

	newStatus := v1alpha1.ModelValidationStatus{
		Conditions:         mv.Status.Conditions,
		InjectedPodCount:   int32(len(trackedPods)),
		UninjectedPodCount: int32(len(uninjectedPods)),
		OrphanedPodCount:   int32(len(orphanedPods)),
		AuthMethod:         trackedAuthMethod,
		InjectedPods:       trackedPods,
		UninjectedPods:     uninjectedPods,
		OrphanedPods:       orphanedPods,
		LastUpdated:        metav1.Now(),
	}

	if statusEqual(mv.Status, newStatus) {
		logger.V(2).Info("Status unchanged, skipping update", "modelvalidation", mvKey)
		return nil
	}

	mv.Status = newStatus
	if err := st.client.Status().Update(ctx, mv); err != nil {
		logger.Error(err, "Failed to update ModelValidation status", "modelvalidation", mvKey)
		return err
	}

	logger.Info("Updated ModelValidation status",
		"modelvalidation", mvKey,
		"injectedPods", mv.Status.InjectedPodCount,
		"uninjectedPods", mv.Status.UninjectedPodCount,
		"orphanedPods", mv.Status.OrphanedPodCount,
		"authMethod", mv.Status.AuthMethod)

	return nil
}

// comparePodSlices compares two slices of PodTrackingInfo for equality
func comparePodSlices(a, b []v1alpha1.PodTrackingInfo) bool {
	return slices.EqualFunc(a, b, func(x, y v1alpha1.PodTrackingInfo) bool {
		return x.Name == y.Name && x.UID == y.UID
	})
}

// statusEqual compares two ModelValidationStatus objects for equality
// ignoring LastUpdated timestamp.
func statusEqual(a, b v1alpha1.ModelValidationStatus) bool {
	if a.InjectedPodCount != b.InjectedPodCount ||
		a.UninjectedPodCount != b.UninjectedPodCount ||
		a.OrphanedPodCount != b.OrphanedPodCount ||
		a.AuthMethod != b.AuthMethod {
		return false
	}

	return comparePodSlices(a.InjectedPods, b.InjectedPods) &&
		comparePodSlices(a.UninjectedPods, b.UninjectedPods) &&
		comparePodSlices(a.OrphanedPods, b.OrphanedPods)
}

// AddModelValidation adds a ModelValidation to tracking using the provided ModelValidation
func (st *StatusTrackerImpl) AddModelValidation(ctx context.Context, mv *v1alpha1.ModelValidation) {
	mvKey := types.NamespacedName{Name: mv.Name, Namespace: mv.Namespace}

	st.mu.Lock()
	mvi, alreadyTracking := st.modelValidations[mvKey]
	if !alreadyTracking {
		mvInfo := NewModelValidationInfo(mv.GetConfigHash(), mv.GetAuthMethod(), mv.Generation)
		st.modelValidations[mvKey] = mvInfo
		st.mvNamespaces[mvKey.Namespace]++
	} else if mv.Generation != mvi.ObservedGeneration {
		// Update existing tracking with current config and handle drift
		driftedPods := st.modelValidations[mvKey].UpdateConfig(mv.GetConfigHash(), mv.GetAuthMethod(), mv.Generation)

		if len(driftedPods) > 0 {
			logger := log.FromContext(ctx)
			logger.Info("Detected configuration drift, moved pods to orphaned status",
				"modelvalidation", mvKey, "driftedPods", len(driftedPods))
		}
	}
	st.mu.Unlock()

	if !alreadyTracking {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), st.statusUpdateTimeout)
			defer cancel()

			if err := st.seedExistingPods(ctx, mvKey); err != nil {
				logger := log.FromContext(ctx)
				logger.Error(err, "Failed to seed existing pods", "modelvalidation", mvKey)
			}
		}()
	}

	st.debouncedQueue.Add(mvKey)
}

// RemoveModelValidation removes a ModelValidation from tracking
func (st *StatusTrackerImpl) RemoveModelValidation(mvKey types.NamespacedName) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Only decrement namespace counter if ModelValidation was actually tracked
	mvInfo, wasTracked := st.modelValidations[mvKey]
	if wasTracked {
		delete(st.modelValidations, mvKey)
		st.mvNamespaces[mvKey.Namespace]--
		if st.mvNamespaces[mvKey.Namespace] <= 0 {
			delete(st.mvNamespaces, mvKey.Namespace)
		}
		allPods := mvInfo.GetAllPods()
		var podsToCleanup []types.NamespacedName
		collectPodsForCleanup(allPods, &podsToCleanup)

		st.podMapping.RemovePodsByName(podsToCleanup...)
	}
}

// IsModelValidationTracked checks if a ModelValidation is being tracked
func (st *StatusTrackerImpl) IsModelValidationTracked(mvKey types.NamespacedName) bool {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return st.modelValidations[mvKey] != nil
}

// GetObservedGeneration returns the observed generation for a tracked ModelValidation
// Returns the generation and a boolean indicating whether the ModelValidation is tracked
func (st *StatusTrackerImpl) GetObservedGeneration(mvKey types.NamespacedName) (int64, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	if mvInfo := st.modelValidations[mvKey]; mvInfo != nil {
		return mvInfo.ObservedGeneration, true
	}
	return 0, false
}

// seedExistingPods processes existing pods using the provided ModelValidation
func (st *StatusTrackerImpl) seedExistingPods(ctx context.Context, mvKey types.NamespacedName) error {
	logger := log.FromContext(ctx)

	// List all pods in the namespace with the ModelValidation label
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(mvKey.Namespace),
		client.MatchingLabels{constants.ModelValidationLabel: mvKey.Name},
	}

	if err := st.client.List(ctx, podList, listOpts...); err != nil {
		logger.Error(err, "Failed to list existing pods for seeding", "modelvalidation", mvKey)
		return err
	}

	logger.Info("Seeding existing pods", "modelvalidation", mvKey, "podCount", len(podList.Items))

	if err := st.processSeedPods(ctx, podList.Items, mvKey); err != nil {
		logger.Error(err, "Failed to process some pods during seeding", "modelvalidation", mvKey)
	}

	return nil
}

// processSeedPods processes seed pods using the provided ModelValidation
func (st *StatusTrackerImpl) processSeedPods(ctx context.Context, pods []corev1.Pod, mvKey types.NamespacedName) error {
	logger := log.FromContext(ctx)
	var errors []error

	for i := range pods {
		pod := &pods[i]
		if err := st.processSeedPodEvent(pod, mvKey); err != nil {
			logger.Error(err, "Failed to process existing pod during batch seeding", "pod", pod.Name, "modelvalidation", mvKey)
			errors = append(errors, err)
		}
	}

	// Return an aggregate error if any individual pod processing failed
	if len(errors) > 0 {
		return fmt.Errorf("failed to process %d out of %d pods", len(errors), len(pods))
	}

	return nil
}

// processSeedPodEvent processes a pod event with a provided ModelValidation
func (st *StatusTrackerImpl) processSeedPodEvent(pod *corev1.Pod, mvKey types.NamespacedName) error {
	modelValidationName, hasLabel := pod.Labels[constants.ModelValidationLabel]
	if !hasLabel || modelValidationName == "" {
		return nil
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	mvInfo := st.modelValidations[mvKey]
	if mvInfo != nil {
		st.processPodEventCommon(pod, mvKey, mvInfo)
	}

	return nil
}

// processPodEventCommon contains the common logic for processing pod events
func (st *StatusTrackerImpl) processPodEventCommon(
	pod *corev1.Pod,
	mvKey types.NamespacedName,
	mvInfo *ModelValidationInfo,
) {
	hasOurFinalizer := controllerutil.ContainsFinalizer(pod, constants.ModelValidationFinalizer)

	podInfo := &PodInfo{
		Name:       pod.Name,
		Namespace:  pod.Namespace,
		UID:        pod.UID,
		Timestamp:  pod.CreationTimestamp,
		ConfigHash: pod.Annotations[constants.ConfigHashAnnotationKey],
		AuthMethod: pod.Annotations[constants.AuthMethodAnnotationKey],
	}

	podNamespacedName := types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}
	st.podMapping.AddPod(podNamespacedName, pod.UID)

	if hasOurFinalizer {
		mvInfo.AddInjectedPod(podInfo)
	} else {
		mvInfo.AddUninjectedPod(podInfo)
	}

	st.debouncedQueue.Add(mvKey)
}

// ProcessPodEvent processes a pod event from controllers
// This determines how to categorize the pod based on its state and configuration consistency
func (st *StatusTrackerImpl) ProcessPodEvent(_ context.Context, pod *corev1.Pod) error {
	if pod == nil {
		return fmt.Errorf("pod cannot be nil")
	}

	modelValidationName, hasLabel := pod.Labels[constants.ModelValidationLabel]
	if !hasLabel || modelValidationName == "" {
		return nil
	}

	mvKey := types.NamespacedName{
		Name:      modelValidationName,
		Namespace: pod.Namespace,
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	mvInfo := st.modelValidations[mvKey]
	if mvInfo != nil {
		st.processPodEventCommon(pod, mvKey, mvInfo)
	}
	return nil
}

// RemovePodEvent removes a pod from tracking when it's deleted
func (st *StatusTrackerImpl) RemovePodEvent(_ context.Context, podUID types.UID) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	updatedMVs := st.removePodFromTrackingMapsUnsafe(podUID)

	for mvKey := range updatedMVs {
		st.debouncedQueue.Add(mvKey)
	}

	return nil
}

// RemovePodByName removes a pod from tracking by its NamespacedName
// This is useful when we know a pod was deleted but don't have its UID
func (st *StatusTrackerImpl) RemovePodByName(_ context.Context, podName types.NamespacedName) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	podUID, exists := st.podMapping.GetUIDByName(podName)
	if !exists {
		return nil
	}

	updatedMVs := st.removePodFromTrackingMapsUnsafe(podUID)
	st.podMapping.RemovePodsByName(podName)

	for mvKey := range updatedMVs {
		st.debouncedQueue.Add(mvKey)
	}

	return nil
}

// removePodFromTrackingMapsUnsafe removes a pod from tracking maps and returns updated MVs
func (st *StatusTrackerImpl) removePodFromTrackingMapsUnsafe(podUID types.UID) map[types.NamespacedName]bool {
	updatedMVs := make(map[types.NamespacedName]bool)

	for mvKey, mvInfo := range st.modelValidations {
		if mvInfo.RemovePod(podUID) {
			updatedMVs[mvKey] = true
		}
	}

	return updatedMVs
}
