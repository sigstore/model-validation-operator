// Copyright 2025 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package webhooks

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/constants"
	"github.com/sigstore/model-validation-operator/internal/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Pod webhook", func() {
	Context("Pod webhook test", func() {
		Name := "test"
		var Namespace string

		ctx := context.Background()

		var typeNamespaceName types.NamespacedName

		BeforeEach(func() {
			Namespace = fmt.Sprintf("test-ns-%d", time.Now().UnixNano())
			typeNamespaceName = testutil.CreateTestNamespacedName(Name, Namespace)

			By("Creating the Namespace to perform the tests")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: Namespace,
				},
			}
			err := k8sClient.Create(ctx, namespace)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation resource")
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           Name,
				Namespace:      Namespace,
				ConfigType:     "sigstore",
				CertIdentity:   "test@example.com",
				CertOidcIssuer: "https://accounts.google.com",
			})
			err = k8sClient.Create(ctx, mv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, mv)
		})

		AfterEach(func() {
			// TODO(user): Attention if you improve this code by adding other context test you MUST
			// be aware of the current delete namespace limitations.
			// More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations

			By("Deleting the ModelValidation resource")
			_ = k8sClient.Delete(ctx, &v1alpha1.ModelValidation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      Name,
					Namespace: Namespace,
				},
			})

			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: Namespace}})
		})

		It("Should create sidecar container and add finalizer", func() {
			By("create labeled pod")
			instance := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      Name,
				Namespace: Namespace,
				Labels:    map[string]string{constants.ModelValidationLabel: Name},
			})
			err := k8sClient.Create(ctx, instance)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar was created")
			found := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) error {
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, 5*time.Second).Should(Succeed())

			Eventually(ctx,
				func(g Gomega, ctx context.Context) []corev1.Container {
					g.Expect(k8sClient.Get(ctx, typeNamespaceName, found)).To(Succeed())
					return found.Spec.InitContainers
				}, 5*time.Second,
			).Should(And(
				WithTransform(func(containers []corev1.Container) int { return len(containers) }, Equal(1)),
				WithTransform(
					func(containers []corev1.Container) string { return containers[0].Image },
					Equal(constants.ModelTransparencyCliImage)),
			))

			By("Checking that finalizer was added")
			Expect(found.Finalizers).To(ContainElement(constants.ModelValidationFinalizer))
		})

		It("Should track pod in ModelValidation status", func() {
			By("create labeled pod")
			instance := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "tracked-pod",
				Namespace: Namespace,
				Labels:    map[string]string{constants.ModelValidationLabel: Name},
			})
			err := k8sClient.Create(ctx, instance)
			Expect(err).To(Not(HaveOccurred()))

			By("Waiting for pod to be injected")
			found := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: "tracked-pod", Namespace: Namespace}, found)
				return found.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			err = statusTracker.ProcessPodEvent(ctx, found)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking ModelValidation status was updated")
			mv := &v1alpha1.ModelValidation{}
			Eventually(ctx, func(ctx context.Context) int32 {
				_ = k8sClient.Get(ctx, typeNamespaceName, mv)
				return mv.Status.InjectedPodCount
			}, 5*time.Second).Should(BeNumerically(">", 0))

			Expect(mv.Status.AuthMethod).To(Equal("sigstore")) // Sigstore auth method configured in test
			Expect(mv.Status.InjectedPods).ToNot(BeEmpty())

			foundTrackedPod := false
			for _, tp := range mv.Status.InjectedPods {
				if tp.Name == "tracked-pod" {
					foundTrackedPod = true
					break
				}
			}
			Expect(foundTrackedPod).To(BeTrue(), "Pod should be tracked in status")
		})
	})
})
