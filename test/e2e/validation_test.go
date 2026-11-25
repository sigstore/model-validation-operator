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
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"text/template"
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
		It("should remove a terminating pod after its ModelValidation is deleted", func(ctx SpecContext) {
			const modelName = "cleanup-after-cr-deletion"
			const podName = "cleanup-after-cr-deletion-pod"
			modelData := utils.CRTemplateData{ModelName: modelName, Namespace: validationTestNamespace}
			podData := defaultValidationPodData(podName, validationTestNamespace, modelName)
			kubectl := func(ctx context.Context, input string, args ...string) (string, error) {
				commandCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
				defer cancel()
				args = append([]string{"--request-timeout=10s", "-n", validationTestNamespace}, args...)
				cmd := exec.CommandContext(commandCtx, "kubectl", args...)
				cmd.Stdin = strings.NewReader(input)
				return utils.Run(cmd)
			}
			apply := func(ctx context.Context, source []byte, data any) {
				var manifest strings.Builder
				tmpl, err := template.New("cleanup").Parse(string(source))
				Expect(err).NotTo(HaveOccurred())
				Expect(tmpl.Execute(&manifest, data)).To(Succeed())
				_, err = kubectl(ctx, manifest.String(), "apply", "-f", "-")
				Expect(err).NotTo(HaveOccurred())
			}
			DeferCleanup(func(cleanupCtx SpecContext) {
				// Restore the CR so finalizer cleanup also works when this regression fails.
				apply(cleanupCtx, modelValidationTemplate, modelData)
				_, err := kubectl(cleanupCtx, "", "delete", "pod", podName, "--ignore-not-found=true", "--timeout=30s")
				Expect(err).NotTo(HaveOccurred())
				_, err = kubectl(cleanupCtx, "", "delete", "modelvalidation", modelName,
					"--ignore-not-found=true", "--timeout=30s")
				Expect(err).NotTo(HaveOccurred())
			}, NodeTimeout(90*time.Second))

			By("creating a ModelValidation and a Pod referencing it")
			apply(ctx, modelValidationTemplate, modelData)
			apply(ctx, podTemplate, podData)

			By("verifying the webhook injected validation and added its finalizer")
			Eventually(ctx, func(g Gomega) {
				output, err := kubectl(ctx, "", "get", "pod", podName, "-o", "json")
				g.Expect(err).NotTo(HaveOccurred())
				var pod corev1.Pod
				g.Expect(json.Unmarshal([]byte(output), &pod)).To(Succeed())
				g.Expect(utils.HasValidationContainer(&pod)).To(BeTrue())
				g.Expect(pod.Finalizers).To(ContainElement(constants.ModelValidationFinalizer))
			}, 30*time.Second, time.Second).Should(Succeed())

			By("deleting the ModelValidation before its Pod")
			_, err := kubectl(ctx, "", "delete", "modelvalidation", modelName, "--wait=true", "--timeout=30s")
			Expect(err).NotTo(HaveOccurred())

			By("requesting Pod deletion while the referenced configuration is absent")
			_, err = kubectl(ctx, "", "delete", "pod", podName, "--wait=false")
			Expect(err).NotTo(HaveOccurred())

			By("waiting for the controller to remove the finalizer and the Pod to disappear")
			Eventually(ctx, func(g Gomega) {
				output, err := kubectl(ctx, "", "get", "pod", podName, "--ignore-not-found=true", "-o", "name")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(BeEmpty())
			}, time.Minute, time.Second).Should(Succeed())
		}, SpecTimeout(2*time.Minute))

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

				g.Expect(output).To(ContainSubstring("verification failed"))
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
