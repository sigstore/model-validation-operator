package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive

	"github.com/sigstore/model-validation-operator/internal/constants"
	"github.com/sigstore/model-validation-operator/internal/testutil"
	"github.com/sigstore/model-validation-operator/internal/tracker"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("PodReconciler", func() {
	var (
		ctx         context.Context
		reconciler  *PodReconciler
		mockTracker *tracker.MockStatusTracker
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockTracker = tracker.NewMockStatusTracker()
	})

	Context("when reconciling Pod resources", func() {
		It("should try to remove pods without ModelValidation label if previously tracked", func() {
			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "test-pod",
				Namespace: "default",
			})

			fakeClient := testutil.SetupFakeClientWithObjects(pod)

			reconciler = &PodReconciler{
				Client:  fakeClient,
				Scheme:  runtime.NewScheme(),
				Tracker: mockTracker,
			}

			req := testutil.CreateReconcileRequest(pod.Namespace, pod.Name)

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			processEvents := mockTracker.GetProcessPodEventCalls()
			Expect(processEvents).To(BeEmpty())

			// Should try to remove the pod in case it was previously tracked
			removeByNameCalls := mockTracker.GetRemovePodByNameCalls()
			Expect(removeByNameCalls).To(HaveLen(1))
			Expect(removeByNameCalls[0].Name).To(Equal("test-pod"))
			Expect(removeByNameCalls[0].Namespace).To(Equal("default"))
		})

		It("should process pods with finalizer but not being deleted", func() {
			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:       "test-pod",
				Namespace:  "default",
				Labels:     map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers: []string{constants.ModelValidationFinalizer},
			})

			fakeClient := testutil.SetupFakeClientWithObjects(pod)

			reconciler = &PodReconciler{
				Client:  fakeClient,
				Scheme:  runtime.NewScheme(),
				Tracker: mockTracker,
			}

			req := testutil.CreateReconcileRequest(pod.Namespace, pod.Name)

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			processEvents := mockTracker.GetProcessPodEventCalls()
			Expect(processEvents).To(HaveLen(1))
			Expect(processEvents[0].Pod.Name).To(Equal(pod.Name))
			Expect(processEvents[0].Pod.Namespace).To(Equal(pod.Namespace))
		})

		It("should handle pod deletion by removing finalizer", func() {
			now := metav1.Now()
			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:              "test-pod",
				Namespace:         "default",
				UID:               types.UID("test-uid"),
				Labels:            map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers:        []string{constants.ModelValidationFinalizer},
				DeletionTimestamp: &now,
			})

			fakeClient := testutil.SetupFakeClientWithObjects(pod)

			reconciler = &PodReconciler{
				Client:  fakeClient,
				Scheme:  runtime.NewScheme(),
				Tracker: mockTracker,
			}

			req := testutil.CreateReconcileRequest(pod.Namespace, pod.Name)

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			removeEvents := mockTracker.GetRemovePodEventCalls()
			Expect(removeEvents).To(HaveLen(1))
			Expect(removeEvents[0]).To(Equal(pod.UID))

			Eventually(ctx, func(ctx context.Context) []string {
				updatedPod := &corev1.Pod{}
				err := fakeClient.Get(ctx, req.NamespacedName, updatedPod)
				if err != nil {
					return []string{}
				}
				return updatedPod.Finalizers
			}, "2s", "100ms").ShouldNot(ContainElement(constants.ModelValidationFinalizer))
		})

		It("should handle pod with multiple finalizers being deleted", func() {
			now := metav1.Now()
			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:              "test-pod",
				Namespace:         "default",
				UID:               types.UID("test-uid"),
				Labels:            map[string]string{constants.ModelValidationLabel: "test-mv"},
				Finalizers:        []string{"other-finalizer", constants.ModelValidationFinalizer, "another-finalizer"},
				DeletionTimestamp: &now,
			})

			fakeClient := testutil.SetupFakeClientWithObjects(pod)

			reconciler = &PodReconciler{
				Client:  fakeClient,
				Scheme:  runtime.NewScheme(),
				Tracker: mockTracker,
			}

			req := testutil.CreateReconcileRequest(pod.Namespace, pod.Name)

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			removeEvents := mockTracker.GetRemovePodEventCalls()
			Expect(removeEvents).To(HaveLen(1))
			Expect(removeEvents[0]).To(Equal(pod.UID))

			updatedPod := &corev1.Pod{}
			err = fakeClient.Get(ctx, req.NamespacedName, updatedPod)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedPod.Finalizers).NotTo(ContainElement(constants.ModelValidationFinalizer))
			Expect(updatedPod.Finalizers).To(ContainElement("other-finalizer"))
			Expect(updatedPod.Finalizers).To(ContainElement("another-finalizer"))
		})

		It("should handle reconciling deleted pods gracefully", func() {
			fakeClient := testutil.SetupFakeClientWithObjects()

			reconciler = &PodReconciler{
				Client:  fakeClient,
				Scheme:  runtime.NewScheme(),
				Tracker: mockTracker,
			}

			req := testutil.CreateReconcileRequest("default", "deleted-pod")

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			removeByNameCalls := mockTracker.GetRemovePodByNameCalls()
			Expect(removeByNameCalls).To(HaveLen(1))
			Expect(removeByNameCalls[0].Name).To(Equal("deleted-pod"))
			Expect(removeByNameCalls[0].Namespace).To(Equal("default"))
		})
	})
})
