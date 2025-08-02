package tracker

import (
	"context"
	"sync"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// MockStatusTracker is a mock implementation of StatusTracker for testing
type MockStatusTracker struct {
	mu sync.RWMutex

	// Track method calls for verification
	AddModelValidationCalls    []*v1alpha1.ModelValidation
	RemoveModelValidationCalls []types.NamespacedName
	ProcessPodEventCalls       []ProcessPodEventCall
	RemovePodEventCalls        []types.UID
	RemovePodByNameCalls       []types.NamespacedName
	StopCalls                  int

	// Optional error responses for testing error scenarios
	ProcessPodEventError error
	RemovePodEventError  error
	RemovePodByNameError error

	// Track ModelValidations that are considered "tracked"
	TrackedModelValidations map[types.NamespacedName]bool
}

// ProcessPodEventCall captures the parameters of a ProcessPodEvent call
type ProcessPodEventCall struct {
	Ctx context.Context
	Pod *corev1.Pod
}

// NewMockStatusTracker creates a new mock status tracker
func NewMockStatusTracker() *MockStatusTracker {
	return &MockStatusTracker{
		TrackedModelValidations: make(map[types.NamespacedName]bool),
	}
}

// AddModelValidation implements StatusTracker interface
func (m *MockStatusTracker) AddModelValidation(_ context.Context, mv *v1alpha1.ModelValidation) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.AddModelValidationCalls = append(m.AddModelValidationCalls, mv)
	mvKey := types.NamespacedName{Name: mv.Name, Namespace: mv.Namespace}
	m.TrackedModelValidations[mvKey] = true
}

// RemoveModelValidation implements StatusTracker interface
func (m *MockStatusTracker) RemoveModelValidation(mvKey types.NamespacedName) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RemoveModelValidationCalls = append(m.RemoveModelValidationCalls, mvKey)
	delete(m.TrackedModelValidations, mvKey)
}

// ProcessPodEvent implements StatusTracker interface
func (m *MockStatusTracker) ProcessPodEvent(ctx context.Context, pod *corev1.Pod) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ProcessPodEventCalls = append(m.ProcessPodEventCalls, ProcessPodEventCall{
		Ctx: ctx,
		Pod: pod,
	})

	return m.ProcessPodEventError
}

// RemovePodEvent implements StatusTracker interface
func (m *MockStatusTracker) RemovePodEvent(_ context.Context, podUID types.UID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RemovePodEventCalls = append(m.RemovePodEventCalls, podUID)
	return m.RemovePodEventError
}

// RemovePodByName implements StatusTracker interface
func (m *MockStatusTracker) RemovePodByName(_ context.Context, podName types.NamespacedName) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RemovePodByNameCalls = append(m.RemovePodByNameCalls, podName)
	return m.RemovePodByNameError
}

// Stop implements StatusTracker interface
func (m *MockStatusTracker) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.StopCalls++
}

// Reset clears all recorded calls
func (m *MockStatusTracker) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.AddModelValidationCalls = nil
	m.RemoveModelValidationCalls = nil
	m.ProcessPodEventCalls = nil
	m.RemovePodEventCalls = nil
	m.RemovePodByNameCalls = nil
	m.StopCalls = 0
	m.ProcessPodEventError = nil
	m.RemovePodEventError = nil
	m.RemovePodByNameError = nil
	m.TrackedModelValidations = make(map[types.NamespacedName]bool)
}

// GetAddModelValidationCalls returns a copy of the AddModelValidation calls
func (m *MockStatusTracker) GetAddModelValidationCalls() []*v1alpha1.ModelValidation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	calls := make([]*v1alpha1.ModelValidation, len(m.AddModelValidationCalls))
	copy(calls, m.AddModelValidationCalls)
	return calls
}

// GetRemoveModelValidationCalls returns a copy of the RemoveModelValidation calls
func (m *MockStatusTracker) GetRemoveModelValidationCalls() []types.NamespacedName {
	m.mu.RLock()
	defer m.mu.RUnlock()

	calls := make([]types.NamespacedName, len(m.RemoveModelValidationCalls))
	copy(calls, m.RemoveModelValidationCalls)
	return calls
}

// GetProcessPodEventCalls returns a copy of the ProcessPodEvent calls
func (m *MockStatusTracker) GetProcessPodEventCalls() []ProcessPodEventCall {
	m.mu.RLock()
	defer m.mu.RUnlock()

	calls := make([]ProcessPodEventCall, len(m.ProcessPodEventCalls))
	copy(calls, m.ProcessPodEventCalls)
	return calls
}

// GetRemovePodEventCalls returns a copy of the RemovePodEvent calls
func (m *MockStatusTracker) GetRemovePodEventCalls() []types.UID {
	m.mu.RLock()
	defer m.mu.RUnlock()

	calls := make([]types.UID, len(m.RemovePodEventCalls))
	copy(calls, m.RemovePodEventCalls)
	return calls
}

// GetRemovePodByNameCalls returns a copy of the RemovePodByName calls
func (m *MockStatusTracker) GetRemovePodByNameCalls() []types.NamespacedName {
	m.mu.RLock()
	defer m.mu.RUnlock()

	calls := make([]types.NamespacedName, len(m.RemovePodByNameCalls))
	copy(calls, m.RemovePodByNameCalls)
	return calls
}

// GetStopCalls returns the number of Stop calls
func (m *MockStatusTracker) GetStopCalls() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.StopCalls
}

// IsModelValidationTracked returns whether a ModelValidation is being tracked
func (m *MockStatusTracker) IsModelValidationTracked(mvKey types.NamespacedName) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.TrackedModelValidations[mvKey]
}
