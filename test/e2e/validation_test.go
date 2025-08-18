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
	"os/exec"
	"time"

	_ "embed"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	corev1 "k8s.io/api/core/v1"

	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

const validationTestNamespace = "e2e-webhook-test-ns"
const validationTestModelName = "validation-test-model"

// defaultValidationPodData creates a standard validation test pod configuration
func defaultValidationPodData(podName, namespace, modelName string) utils.PodTemplateData {
	return utils.DefaultPodData(podName, namespace, modelName, "validation")
}

var _ = Describe("ModelValidation Success/Failure Scenarios", Ordered, func() {
	Context("Validation Success and Failure", func() {
		It("should fail validation with invalid signature (current test model)", func() {
			podName := "failure-test-pod"

			By("deploying a ModelValidation CR for failure test")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: validationTestModelName,
				Namespace: validationTestNamespace,
				KeyPath:   "/keys/test_invalid_public_key.pub",
			})
			Expect(err).NotTo(HaveOccurred())

			By("deploying a pod that should fail validation due to invalid signature")
			err = utils.KubectlApply(podTemplate,
				defaultValidationPodData(podName, validationTestNamespace, validationTestModelName))
			Expect(err).NotTo(HaveOccurred())

			By("verifying the init container was injected")
			Eventually(func(g Gomega) {
				var pod corev1.Pod
				err := utils.KubectlGetJSON("pod", podName, validationTestNamespace, &pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Spec.InitContainers).ToNot(BeEmpty())
				g.Expect(utils.HasValidationContainer(&pod)).To(BeTrue())
			}, 30*time.Second, 1*time.Second).Should(Succeed())

			By("verifying the init container fails due to invalid signature")
			Eventually(func(g Gomega) {
				var pod corev1.Pod
				err := utils.KubectlGetJSON("pod", podName, validationTestNamespace, &pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Status.Phase).To(Equal(corev1.PodFailed))

				g.Expect(pod.Status.InitContainerStatuses).ToNot(BeEmpty())
				initStatus := pod.Status.InitContainerStatuses[0]
				g.Expect(initStatus.State.Terminated).NotTo(BeNil())
				g.Expect(initStatus.State.Terminated.ExitCode).To(Equal(int32(1))) // Should fail with exit code 1
			}, 60*time.Second, 5*time.Second).Should(Succeed())

			By("verifying the init container logs show validation failure")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", podName, "-n", validationTestNamespace, "-c",
					constants.ModelValidationInitContainerName)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(output).To(ContainSubstring("Verification failed with error"))
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("validation failure test completed successfully - webhook injection and " +
				"validation failure both working as expected")

			By("cleaning up failure test resources")
			podToDelete := fmt.Sprintf(
				"apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\n  namespace: %s",
				podName, validationTestNamespace)
			_ = utils.KubectlDelete([]byte(podToDelete), &utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
		})

		It("should successfully validate with public key signature", func() {
			modelName := "public-key-success-test"
			podName := "success-test-pod"

			By("deploying a ModelValidation CR with public key configuration")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: modelName,
				Namespace: validationTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			By("deploying a pod that should pass validation with public key signature")
			err = utils.KubectlApply(podTemplate, defaultValidationPodData(podName, validationTestNamespace, modelName))
			Expect(err).NotTo(HaveOccurred())

			By("verifying the init container was injected")
			Eventually(func(g Gomega) {
				var pod corev1.Pod
				err := utils.KubectlGetJSON("pod", podName, validationTestNamespace, &pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Spec.InitContainers).ToNot(BeEmpty())

				g.Expect(utils.HasValidationContainer(&pod)).To(BeTrue())
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying the init container succeeds and pod reaches Running state")
			Eventually(func(g Gomega) {
				var pod corev1.Pod
				err := utils.KubectlGetJSON("pod", podName, validationTestNamespace, &pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))

				g.Expect(pod.Status.InitContainerStatuses).ToNot(BeEmpty())
				initStatus := pod.Status.InitContainerStatuses[0]
				g.Expect(initStatus.State.Terminated).NotTo(BeNil())
				g.Expect(initStatus.State.Terminated.ExitCode).To(Equal(int32(0))) // Should succeed with exit code 0
			}, 60*time.Second, 5*time.Second).Should(Succeed())

			By("verifying the init container logs show successful validation")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", podName, "-n", validationTestNamespace, "-c",
					constants.ModelValidationInitContainerName)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(output).To(ContainSubstring("Verification succeeded"))
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("cleaning up success test resources")
			podToDelete := fmt.Sprintf(
				"apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\n  namespace: %s",
				podName, validationTestNamespace)
			_ = utils.KubectlDelete([]byte(podToDelete), &utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
			crToDelete := fmt.Sprintf(
				"apiVersion: ml.sigstore.dev/v1alpha1\nkind: ModelValidation\nmetadata:\n  name: %s\n  namespace: %s",
				modelName, validationTestNamespace)
			_ = utils.KubectlDelete([]byte(crToDelete), &utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
		})
	})
})
