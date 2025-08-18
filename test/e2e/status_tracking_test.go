/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"time"

	_ "embed"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	corev1 "k8s.io/api/core/v1"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

const statusTestPodName = "status-test-pod"
const statusTestModelName = "status-test-model"

// defaultStatusPodData creates a standard status test pod configuration
func defaultStatusPodData(podName string) utils.PodTemplateData {
	return utils.DefaultPodData(podName, webhookTestNamespace, statusTestModelName, "status")
}

var _ = Describe("ModelValidation Status Tracking", Ordered, func() {
	Context("Status Field Updates", func() {
		It("should track injected pod count correctly", func() {
			By("deploying a ModelValidation CR with signed model")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: statusTestModelName,
				Namespace: webhookTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to apply signed ModelValidation CR")

			By("verifying initial status shows zero counts")
			verifyInitialStatus := func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", statusTestModelName, webhookTestNamespace, &mv)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mv.Status.AuthMethod).To(Equal("public-key"))
				g.Expect(mv.Status.InjectedPodCount).To(Equal(int32(0)))
				g.Expect(mv.Status.UninjectedPodCount).To(Equal(int32(0)))
				g.Expect(mv.Status.OrphanedPodCount).To(Equal(int32(0)))
			}
			Eventually(verifyInitialStatus).Should(Succeed())

			By("deploying a pod with the model validation label and signed model volume")
			err = utils.KubectlApply(podTemplate, defaultStatusPodData(statusTestPodName))
			Expect(err).NotTo(HaveOccurred(), "Failed to apply signed model pod")

			By("waiting for the init container to be injected")
			verifyInitContainerInjection := func(g Gomega) {
				var pod corev1.Pod
				err := utils.KubectlGetJSON("pod", statusTestPodName, webhookTestNamespace, &pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Spec.InitContainers).To(HaveLen(1))
				g.Expect(pod.Spec.InitContainers[0].Name).To(Equal(constants.ModelValidationInitContainerName))
			}
			Eventually(verifyInitContainerInjection).Should(Succeed())

			By("verifying status shows injected pod count increment")
			verifyInjectedStatus := func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", statusTestModelName, webhookTestNamespace, &mv)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mv.Status.AuthMethod).To(Equal("public-key"))
				g.Expect(mv.Status.InjectedPodCount).To(Equal(int32(1)))
				g.Expect(mv.Status.UninjectedPodCount).To(Equal(int32(0)))
				g.Expect(mv.Status.OrphanedPodCount).To(Equal(int32(0)))
				g.Expect(mv.Status.InjectedPods).To(HaveLen(1))
				g.Expect(mv.Status.InjectedPods[0].Name).To(Equal(statusTestPodName))
				g.Expect(mv.Status.InjectedPods[0].UID).NotTo(BeEmpty())
				g.Expect(mv.Status.InjectedPods[0].InjectedAt).NotTo(BeZero())
				g.Expect(mv.Status.LastUpdated).NotTo(BeZero())
			}
			Eventually(verifyInjectedStatus, 30*time.Second).Should(Succeed())

			By("cleaning up test resources")
			_ = utils.KubectlDelete(podTemplate, &utils.KubectlDeleteOptions{
				Timeout:        "30s",
				IgnoreNotFound: true,
				TemplateData:   defaultStatusPodData(statusTestPodName),
			})
			_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
		})

		It("should handle multiple pods correctly", func() {
			By("deploying a ModelValidation CR")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: statusTestModelName,
				Namespace: webhookTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			By("deploying multiple pods with the same validation label")
			for i := 1; i <= 3; i++ {
				podName := fmt.Sprintf("status-test-pod-%d", i)
				err = utils.KubectlApply(podTemplate, defaultStatusPodData(podName))
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to apply pod %d", i))
			}

			By("verifying status shows correct pod count")
			verifyMultiplePodStatus := func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", statusTestModelName, webhookTestNamespace, &mv)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mv.Status.InjectedPodCount).To(Equal(int32(3)))
				g.Expect(mv.Status.InjectedPods).To(HaveLen(3))
			}
			Eventually(verifyMultiplePodStatus, 60*time.Second).Should(Succeed())

			By("cleaning up test resources")
			for i := 1; i <= 3; i++ {
				podToDelete := fmt.Sprintf(
					"apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\n  namespace: %s",
					fmt.Sprintf("status-test-pod-%d", i), webhookTestNamespace)
				_ = utils.KubectlDelete([]byte(podToDelete), &utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
			}
			_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
		})

		It("should track pod deletion and update counts", func() {
			By("deploying a ModelValidation CR and pod")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: statusTestModelName,
				Namespace: webhookTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			err = utils.KubectlApply(podTemplate, defaultStatusPodData(statusTestPodName))
			Expect(err).NotTo(HaveOccurred())

			By("waiting for pod injection and status update")
			Eventually(func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", statusTestModelName, webhookTestNamespace, &mv)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mv.Status.InjectedPodCount).To(Equal(int32(1)))
			}, 30*time.Second).Should(Succeed())

			By("deleting the pod")
			err = utils.KubectlDelete(podTemplate, &utils.KubectlDeleteOptions{
				TemplateData: defaultStatusPodData(statusTestPodName),
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying status reflects pod deletion")
			verifyPodDeletion := func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", statusTestModelName, webhookTestNamespace, &mv)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mv.Status.InjectedPodCount).To(Equal(int32(0)))
				g.Expect(mv.Status.InjectedPods).To(BeEmpty())
			}
			Eventually(verifyPodDeletion, 30*time.Second).Should(Succeed())

			By("cleaning up ModelValidation CR")
			_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
		})
	})
})
