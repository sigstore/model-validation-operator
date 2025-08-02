package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive

	"github.com/sigstore/model-validation-operator/internal/testutil"
	"github.com/sigstore/model-validation-operator/internal/tracker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = Describe("ModelValidationReconciler", func() {
	var (
		ctx         context.Context
		reconciler  *ModelValidationReconciler
		mockTracker *tracker.MockStatusTracker
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockTracker = tracker.NewMockStatusTracker()
	})

	Context("when reconciling ModelValidation resources", func() {
		It("should call tracker for each reconcile", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:      "test-mv",
				Namespace: "default",
			})

			fakeClient := testutil.SetupFakeClientWithObjects(mv)

			reconciler = &ModelValidationReconciler{
				Client:  fakeClient,
				Scheme:  runtime.NewScheme(),
				Tracker: mockTracker,
			}

			namespace := mv.Namespace
			name := mv.Name
			req := testutil.CreateReconcileRequest(namespace, name)

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			addCalls := mockTracker.GetAddModelValidationCalls()
			Expect(addCalls).To(HaveLen(1))
			Expect(addCalls[0].Name).To(Equal(name))
			Expect(addCalls[0].Namespace).To(Equal(namespace))

			// Second reconcile should also call AddModelValidation
			// Controller is not idempotent - it calls tracker each time
			result, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			addCalls = mockTracker.GetAddModelValidationCalls()
			Expect(addCalls).To(HaveLen(2))
			Expect(addCalls[1].Name).To(Equal(name))
			Expect(addCalls[1].Namespace).To(Equal(namespace))
		})

		It("should remove ModelValidation with DeletionTimestamp from tracking", func() {
			now := metav1.Now()
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:              "test-mv-deleting",
				Namespace:         "default",
				DeletionTimestamp: &now,
				Finalizers:        []string{"test-finalizer"},
			})

			fakeClient := testutil.SetupFakeClientWithObjects(mv)

			reconciler = &ModelValidationReconciler{
				Client:  fakeClient,
				Scheme:  runtime.NewScheme(),
				Tracker: mockTracker,
			}

			namespace := mv.Namespace
			name := mv.Name
			req := testutil.CreateReconcileRequest(namespace, name)

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			removeCalls := mockTracker.GetRemoveModelValidationCalls()
			Expect(removeCalls).To(HaveLen(1))
			Expect(removeCalls[0].Namespace).To(Equal(namespace))
			Expect(removeCalls[0].Name).To(Equal(name))
		})

		It("should remove deleted ModelValidation from tracking", func() {
			fakeClient := testutil.SetupFakeClientWithObjects()

			reconciler = &ModelValidationReconciler{
				Client:  fakeClient,
				Scheme:  runtime.NewScheme(),
				Tracker: mockTracker,
			}

			namespace := "default"
			name := "deleted-mv"
			req := testutil.CreateReconcileRequest(namespace, name)

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			removeCalls := mockTracker.GetRemoveModelValidationCalls()
			Expect(removeCalls).To(HaveLen(1))
			Expect(removeCalls[0].Namespace).To(Equal(namespace))
			Expect(removeCalls[0].Name).To(Equal(name))
		})
	})
})
