package tracker

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/constants"
	"github.com/sigstore/model-validation-operator/internal/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewStatusTrackerForTesting creates a status tracker with fast test-friendly defaults
func NewStatusTrackerForTesting(client client.Client) StatusTracker {
	return NewStatusTracker(client, StatusTrackerConfig{
		DebounceDuration:    50 * time.Millisecond, // Faster for tests
		RetryBaseDelay:      10 * time.Millisecond, // Faster retries for tests
		RetryMaxDelay:       1 * time.Second,       // Lower max delay for tests
		RateLimitQPS:        100,                   // Higher QPS for tests
		RateLimitBurst:      1000,                  // Higher burst for tests
		StatusUpdateTimeout: 5 * time.Second,       // Shorter timeout for tests
	})
}

// setupTestEnvironment creates a common test setup with ModelValidation and StatusTracker
func setupTestEnvironment(mv *v1alpha1.ModelValidation) (*StatusTrackerImpl, client.Client, types.NamespacedName) {
	fakeClient := testutil.SetupFakeClientWithObjects(mv)
	statusTracker := NewStatusTrackerForTesting(fakeClient).(*StatusTrackerImpl)
	mvKey := types.NamespacedName{Name: mv.Name, Namespace: mv.Namespace}
	return statusTracker, fakeClient, mvKey
}

// expectPodCount waits for and verifies expected pod counts in ModelValidation status
func expectPodCount(
	ctx context.Context,
	fakeClient client.Client,
	mvKey types.NamespacedName,
	injected, uninjected, orphaned int32,
) {
	Eventually(func() bool {
		updatedMV := testutil.GetModelValidationFromClientExpected(ctx, fakeClient, mvKey)
		return updatedMV.Status.InjectedPodCount == injected &&
			updatedMV.Status.UninjectedPodCount == uninjected &&
			updatedMV.Status.OrphanedPodCount == orphaned
	}, "2s", "50ms").Should(BeTrue())
}

// getNamespaceCount returns the count of ModelValidations in the given namespace
func getNamespaceCount(tracker *StatusTrackerImpl, namespace string) int {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	return tracker.mvNamespaces[namespace]
}

// namespaceExists checks if a namespace exists in the tracker
func namespaceExists(tracker *StatusTrackerImpl, namespace string) bool {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	_, exists := tracker.mvNamespaces[namespace]
	return exists
}

var _ = Describe("StatusTracker", func() {
	var (
		ctx           context.Context
		statusTracker *StatusTrackerImpl
		fakeClient    client.Client
		mvKey         types.NamespacedName
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	AfterEach(func() {
		if statusTracker != nil {
			statusTracker.Stop()
		}
	})

	Context("when tracking pod injections", func() {
		It("should track injected pods and update ModelValidation status", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})

			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:       "test-pod",
				Namespace:  "default",
				UID:        types.UID("test-uid"),
				Labels:     map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers: []string{constants.ModelValidationFinalizer},
			})

			err := statusTracker.ProcessPodEvent(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 1, 0, 0)

			updatedMV := testutil.GetModelValidationFromClientExpected(ctx, fakeClient, mvKey)
			Expect(updatedMV.Status.InjectedPods).To(HaveLen(1))
			Expect(updatedMV.Status.InjectedPods[0].Name).To(Equal("test-pod"))
		})

		It("should categorize pods correctly based on finalizer and CR existence", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})

			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			injectedPod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:       "injected-pod",
				Namespace:  "default",
				UID:        types.UID("uid-1"),
				Labels:     map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers: []string{constants.ModelValidationFinalizer},
			})

			uninjectedPod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "uninjected-pod",
				Namespace: "default",
				UID:       types.UID("uid-2"),
				Labels:    map[string]string{constants.ModelValidationLabel: "test-mv"},
			})

			err := statusTracker.ProcessPodEvent(ctx, injectedPod)
			Expect(err).NotTo(HaveOccurred())

			err = statusTracker.ProcessPodEvent(ctx, uninjectedPod)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 1, 1, 0)

			updatedMV := testutil.GetModelValidationFromClientExpected(ctx, fakeClient, mvKey)
			Expect(updatedMV.Status.InjectedPods).To(HaveLen(1))
			Expect(updatedMV.Status.UninjectedPods).To(HaveLen(1))
		})
	})

	Context("when removing pods", func() {
		It("should remove pods from tracking and update status", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})

			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			pod1 := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:       "test-pod-1",
				Namespace:  "default",
				UID:        types.UID("test-uid-1"),
				Labels:     map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers: []string{constants.ModelValidationFinalizer},
			})
			pod2 := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:       "test-pod-2",
				Namespace:  "default",
				UID:        types.UID("test-uid-2"),
				Labels:     map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers: []string{constants.ModelValidationFinalizer},
			})

			err := statusTracker.ProcessPodEvent(ctx, pod1)
			Expect(err).NotTo(HaveOccurred())
			err = statusTracker.ProcessPodEvent(ctx, pod2)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 2, 0, 0)

			err = statusTracker.RemovePodEvent(ctx, pod1.UID)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 1, 0, 0)

			updatedMV := testutil.GetModelValidationFromClientExpected(ctx, fakeClient, mvKey)
			Expect(updatedMV.Status.InjectedPods).To(HaveLen(1))
			Expect(updatedMV.Status.InjectedPods[0].Name).To(Equal("test-pod-2"))
		})
	})

	Context("when managing ModelValidation tracking", func() {
		It("should add and remove ModelValidations from tracking", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})
			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)

			Expect(statusTracker.IsModelValidationTracked(mvKey)).To(BeFalse())

			statusTracker.AddModelValidation(ctx, mv)
			Expect(statusTracker.IsModelValidationTracked(mvKey)).To(BeTrue())

			statusTracker.RemoveModelValidation(mvKey)
			Expect(statusTracker.IsModelValidationTracked(mvKey)).To(BeFalse())
		})

		It("should handle AddModelValidation idempotently", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})
			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
			mvKey = testutil.CreateTestNamespacedName("test-mv", "default")

			Expect(statusTracker.IsModelValidationTracked(mvKey)).To(BeFalse())

			initialCount := getNamespaceCount(statusTracker, "default")

			statusTracker.AddModelValidation(ctx, mv)
			Expect(statusTracker.IsModelValidationTracked(mvKey)).To(BeTrue())

			firstCount := getNamespaceCount(statusTracker, "default")
			Expect(firstCount).To(Equal(initialCount + 1))

			statusTracker.AddModelValidation(ctx, mv)
			Expect(statusTracker.IsModelValidationTracked(mvKey)).To(BeTrue())

			secondCount := getNamespaceCount(statusTracker, "default")
			Expect(secondCount).To(Equal(firstCount), "Second AddModelValidation should not increment namespace counter")

			statusTracker.AddModelValidation(ctx, mv)
			thirdCount := getNamespaceCount(statusTracker, "default")
			Expect(thirdCount).To(Equal(firstCount), "Third AddModelValidation should not increment namespace counter")
		})

		It("should skip AddModelValidation when generation hasn't changed", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv-generation",
				Namespace: "default",
			})
			statusTracker, _, mvKey = setupTestEnvironment(mv)

			// Set initial generation
			mv.Generation = 1
			statusTracker.AddModelValidation(ctx, mv)
			Expect(statusTracker.IsModelValidationTracked(mvKey)).To(BeTrue())

			initialObservedGen, tracked := statusTracker.GetObservedGeneration(mvKey)
			Expect(tracked).To(BeTrue(), "ModelValidation should be tracked")
			Expect(initialObservedGen).To(Equal(int64(1)))

			// Call AddModelValidation again with same generation - should be skipped
			statusTracker.AddModelValidation(ctx, mv)

			currentObservedGen, tracked := statusTracker.GetObservedGeneration(mvKey)
			Expect(tracked).To(BeTrue(), "ModelValidation should still be tracked")
			Expect(currentObservedGen).To(Equal(int64(1)),
				"Generation should remain unchanged when same generation is processed")

			// Updated generation should trigger processing
			mv.Generation = 2
			statusTracker.AddModelValidation(ctx, mv)

			updatedObservedGen, tracked := statusTracker.GetObservedGeneration(mvKey)
			Expect(tracked).To(BeTrue(), "ModelValidation should still be tracked")
			Expect(updatedObservedGen).To(Equal(int64(2)), "Generation should be updated when new generation is processed")
		})

		It("should return false for untracked ModelValidation in GetObservedGeneration", func() {
			untrackedKey := types.NamespacedName{Name: "untracked-mv", Namespace: "default"}

			// Create a statusTracker but don't add any ModelValidation
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "tracked-mv",
				Namespace: "default",
			})
			statusTracker, _, _ = setupTestEnvironment(mv)

			_, tracked := statusTracker.GetObservedGeneration(untrackedKey)
			Expect(tracked).To(BeFalse(), "Untracked ModelValidation should return false")
		})

		It("should handle RemoveModelValidation idempotently and manage namespace counts correctly", func() {
			mv1 := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv-1",
				Namespace: "default",
			})
			mv2 := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv-2",
				Namespace: "default",
			})
			mv3 := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv-3",
				Namespace: "other-namespace",
			})
			fakeClient = testutil.SetupFakeClientWithObjects(mv1, mv2, mv3)
			statusTracker = NewStatusTrackerForTesting(fakeClient).(*StatusTrackerImpl)

			mvKey1 := testutil.CreateTestNamespacedName("test-mv-1", "default")
			mvKey2 := testutil.CreateTestNamespacedName("test-mv-2", "default")
			mvKey3 := testutil.CreateTestNamespacedName("test-mv-3", "other-namespace")

			statusTracker.AddModelValidation(ctx, mv1)
			statusTracker.AddModelValidation(ctx, mv2)
			statusTracker.AddModelValidation(ctx, mv3)

			defaultCount := getNamespaceCount(statusTracker, "default")
			otherCount := getNamespaceCount(statusTracker, "other-namespace")
			Expect(defaultCount).To(Equal(2))
			Expect(otherCount).To(Equal(1))

			statusTracker.RemoveModelValidation(mvKey1)
			Expect(statusTracker.IsModelValidationTracked(mvKey1)).To(BeFalse())

			defaultCountAfterFirst := getNamespaceCount(statusTracker, "default")
			Expect(defaultCountAfterFirst).To(Equal(1))

			statusTracker.RemoveModelValidation(mvKey1)
			defaultCountAfterSecond := getNamespaceCount(statusTracker, "default")
			Expect(defaultCountAfterSecond).To(Equal(1), "Second RemoveModelValidation should not decrement namespace counter")

			statusTracker.RemoveModelValidation(mvKey2)
			defaultExists := namespaceExists(statusTracker, "default")
			otherCountFinal := getNamespaceCount(statusTracker, "other-namespace")
			Expect(defaultExists).To(BeFalse(), "Namespace should be deleted when count reaches zero")
			Expect(otherCountFinal).To(Equal(1), "Other namespace should be unaffected")

			statusTracker.RemoveModelValidation(mvKey3)
			otherExists := namespaceExists(statusTracker, "other-namespace")
			Expect(otherExists).To(BeFalse(), "Other namespace should also be deleted when count reaches zero")
		})

		DescribeTable("should handle configuration drift detection",
			func(
				mvConfig testutil.TestModelValidationOptions,
				podAuth string,
				podConfigHash string,
				expectedInjected, expectedOrphaned int32,
				expectedPodName string,
			) {
				mv := testutil.CreateTestModelValidation(mvConfig)
				statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
				statusTracker.AddModelValidation(ctx, mv)

				podAnnotations := map[string]string{
					constants.AuthMethodAnnotationKey: podAuth,
				}
				if podConfigHash != "" {
					podAnnotations[constants.ConfigHashAnnotationKey] = podConfigHash
				}

				pod := testutil.CreateTestPod(testutil.TestPodOptions{
					Name:        expectedPodName,
					Namespace:   "default",
					UID:         types.UID("uid-" + expectedPodName),
					Labels:      map[string]string{constants.ModelValidationLabel: "test-mv"},
					Finalizers:  []string{constants.ModelValidationFinalizer},
					Annotations: podAnnotations,
				})

				err := statusTracker.ProcessPodEvent(ctx, pod)
				Expect(err).NotTo(HaveOccurred())

				statusTracker.WaitForUpdates()
				expectPodCount(ctx, fakeClient, mvKey, expectedInjected, 0, expectedOrphaned)

				updatedMV := testutil.GetModelValidationFromClientExpected(ctx, fakeClient, mvKey)
				if expectedInjected > 0 {
					Expect(updatedMV.Status.InjectedPods).To(HaveLen(int(expectedInjected)))
					Expect(updatedMV.Status.InjectedPods[0].Name).To(Equal(expectedPodName))
				}
				if expectedOrphaned > 0 {
					Expect(updatedMV.Status.OrphanedPods).To(HaveLen(int(expectedOrphaned)))
					Expect(updatedMV.Status.OrphanedPods[0].Name).To(Equal(expectedPodName))
				}
			},
			Entry("auth method mismatch - sigstore MV with pki pod",
				testutil.TestModelValidationOptions{Name: "test-mv", Namespace: "default", ConfigType: "sigstore"},
				"pki", "", int32(0), int32(1), "orphaned-pod"),
			Entry("matching pki configuration",
				testutil.TestModelValidationOptions{Name: "test-mv", Namespace: "default", ConfigType: "pki"},
				"pki", "", int32(1), int32(0), "matching-pod"),
			Entry("sigstore config hash drift",
				testutil.TestModelValidationOptions{
					Name: "test-mv", Namespace: "default", ConfigType: "sigstore",
					CertIdentity: "user@example.com", CertOidcIssuer: "https://accounts.google.com",
				},
				"sigstore", func() string {
					oldMV := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
						Name: "test-mv", Namespace: "default", ConfigType: "sigstore",
						CertIdentity: "different@example.com", CertOidcIssuer: "https://accounts.google.com",
					})
					return oldMV.GetConfigHash()
				}(), int32(0), int32(1), "drift-pod"),
			Entry("pki config hash drift",
				testutil.TestModelValidationOptions{
					Name: "test-mv", Namespace: "default", ConfigType: "pki", CertificateCA: "/path/to/ca.crt",
				},
				"pki", func() string {
					oldMV := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
						Name: "test-mv", Namespace: "default", ConfigType: "pki", CertificateCA: "/different/path/ca.crt",
					})
					return oldMV.GetConfigHash()
				}(), int32(0), int32(1), "pki-drift-pod"),
		)

		It("should handle pod deletion by name", func() {
			// Create ModelValidation with Sigstore config
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:       "test-mv",
				Namespace:  "default",
				ConfigType: "sigstore",
			})

			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:       "deleted-pod",
				Namespace:  "default",
				UID:        types.UID("uid-deleted"),
				Labels:     map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers: []string{constants.ModelValidationFinalizer},
			})

			err := statusTracker.ProcessPodEvent(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 1, 0, 0)

			// Now simulate pod deletion by removing it by name
			podName := types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}
			err = statusTracker.RemovePodByName(ctx, podName)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 0, 0, 0)

			updatedMV := testutil.GetModelValidationFromClientExpected(ctx, fakeClient, mvKey)
			Expect(updatedMV.Status.InjectedPods).To(BeEmpty())
		})

		It("should re-evaluate existing pods when ModelValidation is updated", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:       "test-mv",
				Namespace:  "default",
				ConfigType: "sigstore",
			})

			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:       "test-pod",
				Namespace:  "default",
				UID:        types.UID("test-uid"),
				Labels:     map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers: []string{constants.ModelValidationFinalizer},
				Annotations: map[string]string{
					constants.AuthMethodAnnotationKey: "sigstore",
				},
			})

			fakeClient = testutil.SetupFakeClientWithObjects(mv, pod)
			statusTracker = NewStatusTrackerForTesting(fakeClient).(*StatusTrackerImpl)

			mvKey := types.NamespacedName{Name: mv.Name, Namespace: mv.Namespace}

			statusTracker.AddModelValidation(ctx, mv)

			err := statusTracker.ProcessPodEvent(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 1, 0, 0)

			// This should trigger re-evaluation of existing pods
			statusTracker.AddModelValidation(ctx, mv)

			// The pod should still be tracked (no configuration drift in this case)
			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 1, 0, 0)

			updatedMV := testutil.GetModelValidationFromClientExpected(ctx, fakeClient, mvKey)
			Expect(updatedMV.Status.InjectedPods).To(HaveLen(1))
			Expect(updatedMV.Status.InjectedPods[0].Name).To(Equal("test-pod"))
		})
	})

	Context("when handling error conditions and edge cases", func() {
		It("should handle pod without required labels", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})
			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "unlabeled-pod",
				Namespace: "default",
				UID:       types.UID("unlabeled-uid"),
				Labels:    map[string]string{}, // No ModelValidation label
			})

			err := statusTracker.ProcessPodEvent(ctx, pod)
			Expect(err).NotTo(HaveOccurred()) // Should not error, just ignore the pod

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 0, 0, 0) // No pods should be tracked
		})

		It("should handle pod with unknown ModelValidation", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})
			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "unknown-mv-pod",
				Namespace: "default",
				UID:       types.UID("unknown-uid"),
				Labels:    map[string]string{constants.ModelValidationLabel: "unknown-mv"},
			})

			err := statusTracker.ProcessPodEvent(ctx, pod)
			Expect(err).NotTo(HaveOccurred()) // Should not error, just ignore the pod

			statusTracker.WaitForUpdates()
			expectPodCount(ctx, fakeClient, mvKey, 0, 0, 0) // No pods should be tracked
		})

		It("should handle removal of non-existent pod by UID", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})
			statusTracker, _, _ = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			err := statusTracker.RemovePodEvent(ctx, types.UID("non-existent-uid"))
			Expect(err).NotTo(HaveOccurred()) // Should not error for non-existent pod
		})

		It("should handle removal of non-existent pod by name", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})
			statusTracker, _, _ = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			podName := types.NamespacedName{Name: "non-existent-pod", Namespace: "default"}
			err := statusTracker.RemovePodByName(ctx, podName)
			Expect(err).NotTo(HaveOccurred()) // Should not error for non-existent pod
		})

		It("should handle pod with invalid annotations", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:       "test-mv",
				Namespace:  "default",
				ConfigType: "sigstore",
			})
			statusTracker, fakeClient, mvKey = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:       "invalid-pod",
				Namespace:  "default",
				UID:        types.UID("invalid-uid"),
				Labels:     map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers: []string{constants.ModelValidationFinalizer},
				Annotations: map[string]string{
					constants.AuthMethodAnnotationKey: "invalid-auth-method",
					constants.ConfigHashAnnotationKey: "invalid-hash",
				},
			})

			err := statusTracker.ProcessPodEvent(ctx, pod)
			Expect(err).NotTo(HaveOccurred()) // Should handle gracefully

			statusTracker.WaitForUpdates()
			// Should be classified as orphaned due to invalid auth method
			expectPodCount(ctx, fakeClient, mvKey, 0, 0, 1)
		})

		It("should handle empty UID gracefully", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})
			statusTracker, _, _ = setupTestEnvironment(mv)
			statusTracker.AddModelValidation(ctx, mv)

			err := statusTracker.RemovePodEvent(ctx, types.UID(""))
			Expect(err).NotTo(HaveOccurred()) // Should handle empty UID gracefully
		})
	})
})

// NewStatusTrackerForRetryTesting creates a status tracker with fast test-friendly config for retry testing
func NewStatusTrackerForRetryTesting(client client.Client) StatusTracker {
	return NewStatusTracker(client, StatusTrackerConfig{
		DebounceDuration:    20 * time.Millisecond,  // Very fast for testing
		RetryBaseDelay:      10 * time.Millisecond,  // Fast retries
		RetryMaxDelay:       100 * time.Millisecond, // Low max delay
		RateLimitQPS:        1000,                   // High QPS to avoid interference
		RateLimitBurst:      10000,                  // High burst to avoid interference
		StatusUpdateTimeout: 5 * time.Second,
	})
}

var _ = Describe("StatusTracker retry and debounce integration", func() {
	var (
		ctx           context.Context
		cancel        context.CancelFunc
		fakeClient    *testutil.FailingClient
		statusTracker *StatusTrackerImpl
		mvKey         types.NamespacedName
		mv            *v1alpha1.ModelValidation
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		mv = testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
			Name:      "test-mv",
			Namespace: "default",
		})
		// Setup failing environment with 2 failures before success
		fakeClient = testutil.CreateFailingClientWithObjects(2, mv)
		statusTracker = NewStatusTrackerForRetryTesting(fakeClient).(*StatusTrackerImpl)
		mvKey = types.NamespacedName{Name: mv.Name, Namespace: mv.Namespace}
	})

	AfterEach(func() {
		if statusTracker != nil {
			statusTracker.Stop()
		}
		if cancel != nil {
			cancel()
		}
	})

	It("should retry failed status updates with exponential backoff while respecting debouncing", func() {
		statusTracker.AddModelValidation(ctx, mv)

		pod := testutil.CreateTestPod(testutil.TestPodOptions{
			Name:      "test-pod-1",
			Namespace: mv.Namespace,
			Labels: map[string]string{
				constants.ModelValidationLabel: mv.Name,
			},
			Annotations: map[string]string{
				constants.ConfigHashAnnotationKey: mv.GetConfigHash(),
				constants.AuthMethodAnnotationKey: mv.GetAuthMethod(),
			},
			Finalizers: []string{constants.ModelValidationFinalizer},
		})
		err := statusTracker.ProcessPodEvent(ctx, pod)
		Expect(err).NotTo(HaveOccurred())

		// Trigger multiple rapid updates to test debouncing
		for i := 0; i < 5; i++ {
			statusTracker.debouncedQueue.Add(mvKey)
			time.Sleep(5 * time.Millisecond) // Shorter than debounce duration
		}

		// Wait for debouncing and retries to complete (2 failures + 1 success)
		testutil.ExpectAttemptCount(fakeClient, 3)

		// Verify that despite multiple debounce calls, we only had one series of retries
		totalAttempts := fakeClient.GetAttemptCount()
		actualFailures := fakeClient.GetFailureCount()

		Expect(totalAttempts).To(Equal(3), "Should have exactly 3 attempts: 2 failures + 1 success")
		Expect(actualFailures).To(Equal(2), "Should have exactly 2 failures before success")

		expectPodCount(ctx, fakeClient, mvKey, 1, 0, 0)
	})

	It("should handle concurrent debounced updates with retries correctly", func() {
		statusTracker.AddModelValidation(ctx, mv)

		// Trigger concurrent updates from multiple goroutines
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				statusTracker.debouncedQueue.Add(mvKey)
			}()
		}
		wg.Wait()

		// Wait for processing to complete (2 failures + 1 success)
		testutil.ExpectAttemptCount(fakeClient, 3)

		totalAttempts := fakeClient.GetAttemptCount()

		Expect(totalAttempts).To(Equal(3), "Concurrent updates should be debounced into single retry sequence")
	})
})

var _ = Describe("StatusTracker utility functions", func() {
	Context("when comparing ModelValidation statuses", func() {
		It("should correctly identify equal statuses", func() {
			status1 := v1alpha1.ModelValidationStatus{
				InjectedPodCount: 2,
				AuthMethod:       "sigstore",
				InjectedPods: []v1alpha1.PodTrackingInfo{
					{Name: "pod1", UID: "uid1"},
					{Name: "pod2", UID: "uid2"},
				},
			}
			status2 := v1alpha1.ModelValidationStatus{
				InjectedPodCount: 2,
				AuthMethod:       "sigstore",
				InjectedPods: []v1alpha1.PodTrackingInfo{
					{Name: "pod1", UID: "uid1"},
					{Name: "pod2", UID: "uid2"},
				},
			}

			Expect(statusEqual(status1, status2)).To(BeTrue())
		})

		It("should correctly identify different statuses", func() {
			status1 := v1alpha1.ModelValidationStatus{
				InjectedPodCount: 2,
			}
			status2 := v1alpha1.ModelValidationStatus{
				InjectedPodCount: 3,
			}

			Expect(statusEqual(status1, status2)).To(BeFalse())
		})

		It("should ignore LastUpdated differences", func() {
			status1 := v1alpha1.ModelValidationStatus{
				InjectedPodCount: 1,
				LastUpdated:      metav1.Time{Time: time.Now()},
			}
			status2 := v1alpha1.ModelValidationStatus{
				InjectedPodCount: 1,
				LastUpdated:      metav1.Time{Time: time.Now().Add(1 * time.Hour)},
			}

			Expect(statusEqual(status1, status2)).To(BeTrue())
		})
	})

})
