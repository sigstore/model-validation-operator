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

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	corev1 "k8s.io/api/core/v1"

	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

const legacySidecarTestNamespace = "e2e-webhook-test-ns"

// defaultLegacyPodData creates a standard pod configuration for legacy sidecar tests
func defaultLegacyPodData(podName, namespace, modelName string) utils.PodTemplateData {
	return utils.DefaultPodData(podName, namespace, modelName, "legacy-sidecar")
}

var _ = Describe("Legacy Sidecar Continuous Validation", Ordered, func() {
	Context("When operator is running in legacy sidecar mode", func() {
		BeforeAll(func() {
			By("enabling force-legacy-sidecar on the operator deployment")
			cmd := exec.Command("kubectl", "set", "env",
				"deployment/model-validation-controller-manager",
				"-n", operatorNamespace,
				"FORCE_LEGACY_SIDECAR=true")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to set FORCE_LEGACY_SIDECAR env var")

			By("waiting for operator rollout to complete")
			cmd = exec.Command("kubectl", "rollout", "status",
				"deployment/model-validation-controller-manager",
				"-n", operatorNamespace,
				"--timeout=120s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Operator rollout did not complete")

			By("waiting for new controller pod to be ready")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "wait", "pod",
					"--selector=control-plane=controller-manager",
					"-n", operatorNamespace,
					"--for=condition=Ready",
					"--timeout=60s")
				_, err := utils.Run(cmd)
				return err
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		AfterAll(func() {
			By("removing force-legacy-sidecar from the operator deployment")
			cmd := exec.Command("kubectl", "set", "env",
				"deployment/model-validation-controller-manager",
				"-n", operatorNamespace,
				"FORCE_LEGACY_SIDECAR-")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to remove FORCE_LEGACY_SIDECAR env var")

			By("waiting for operator rollout to complete")
			cmd = exec.Command("kubectl", "rollout", "status",
				"deployment/model-validation-controller-manager",
				"-n", operatorNamespace,
				"--timeout=120s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Operator rollout did not complete")
		})

		It("should inject init container and sidecar for continuous validation", func() {
			modelName := "legacy-continuous-test"
			podName := "legacy-sidecar-pod"

			By("deploying a ModelValidation CR with continuous validation enabled")
			err := utils.KubectlApply(modelValidationContinuousTemplate, utils.CRTemplateData{
				ModelName: modelName,
				Namespace: legacySidecarTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			By("deploying a pod with the validation label")
			err = utils.KubectlApply(podTemplate,
				defaultLegacyPodData(podName, legacySidecarTestNamespace, modelName))
			Expect(err).NotTo(HaveOccurred())

			By("verifying the init container was injected (one-shot validation)")
			Eventually(func(g Gomega) {
				var pod corev1.Pod
				err := utils.KubectlGetJSON("pod", podName, legacySidecarTestNamespace, &pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Spec.InitContainers).NotTo(BeEmpty())
				g.Expect(utils.HasValidationContainer(&pod)).To(BeTrue(),
					"Pod should have validation init container")
			}, 30*time.Second, 1*time.Second).Should(Succeed())

			By("verifying the legacy sidecar container was injected")
			Eventually(func(g Gomega) {
				var pod corev1.Pod
				err := utils.KubectlGetJSON("pod", podName, legacySidecarTestNamespace, &pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(utils.HasValidationSidecar(&pod)).To(BeTrue(),
					"Pod should have legacy validation sidecar container")
			}, 30*time.Second, 1*time.Second).Should(Succeed())

			By("verifying init container does NOT have restartPolicy (one-shot mode)")
			var pod corev1.Pod
			err = utils.KubectlGetJSON("pod", podName, legacySidecarTestNamespace, &pod)
			Expect(err).NotTo(HaveOccurred())

			for _, c := range pod.Spec.InitContainers {
				if c.Name == constants.ModelValidationInitContainerName {
					Expect(c.RestartPolicy).To(BeNil(),
						"Init container should not have restartPolicy in legacy mode")
					break
				}
			}

			By("verifying sidecar has --skip-initial and --interval flags")
			for _, c := range pod.Spec.Containers {
				if c.Name == constants.ModelValidationSidecarContainerName {
					Expect(c.Args).To(ContainElement("--skip-initial"),
						"Sidecar should have --skip-initial flag")
					Expect(c.Args).To(ContainElement(ContainSubstring("--interval=")),
						"Sidecar should have --interval flag")
					Expect(c.ReadinessProbe).NotTo(BeNil(),
						"Sidecar should have readiness probe")
					Expect(c.LivenessProbe).NotTo(BeNil(),
						"Sidecar should have liveness probe")
					break
				}
			}

			By("verifying continuous validation annotation is set")
			Expect(pod.Annotations[constants.ContinuousValidationAnnotationKey]).To(Equal("true"),
				"Pod should have continuous validation annotation")

			By("verifying the init container completes successfully (pod reaches Running)")
			Eventually(func(g Gomega) {
				var p corev1.Pod
				err := utils.KubectlGetJSON("pod", podName, legacySidecarTestNamespace, &p)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(p.Status.Phase).To(Equal(corev1.PodRunning),
					"Pod should be running after init container passes validation")
			}, 60*time.Second, 5*time.Second).Should(Succeed())

			By("verifying the init container logs show successful validation")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", podName, "-n",
					legacySidecarTestNamespace, "-c", constants.ModelValidationInitContainerName)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Verification succeeded"))
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("verifying the sidecar container is running")
			Eventually(func(g Gomega) {
				var p corev1.Pod
				err := utils.KubectlGetJSON("pod", podName, legacySidecarTestNamespace, &p)
				g.Expect(err).NotTo(HaveOccurred())

				for _, cs := range p.Status.ContainerStatuses {
					if cs.Name == constants.ModelValidationSidecarContainerName {
						g.Expect(cs.State.Running).NotTo(BeNil(),
							"Sidecar container should be running")
						g.Expect(cs.Ready).To(BeTrue(),
							"Sidecar container should be ready (skip-initial marks ready immediately)")
						return
					}
				}
				g.Expect(false).To(BeTrue(), "Sidecar container status not found")
			}, 60*time.Second, 5*time.Second).Should(Succeed())

			By("cleaning up test resources")
			podToDelete := fmt.Sprintf(
				"apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\n  namespace: %s",
				podName, legacySidecarTestNamespace)
			_ = utils.KubectlDelete([]byte(podToDelete),
				&utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
			crToDelete := fmt.Sprintf(
				"apiVersion: ml.sigstore.dev/v1alpha1\nkind: ModelValidation\nmetadata:\n  name: %s\n  namespace: %s",
				modelName, legacySidecarTestNamespace)
			_ = utils.KubectlDelete([]byte(crToDelete),
				&utils.KubectlDeleteOptions{Timeout: "30s", IgnoreNotFound: true})
		})
	})
})
