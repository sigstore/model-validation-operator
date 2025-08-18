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
	"github.com/sigstore/model-validation-operator/internal/metrics"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

const integrationTestModelName = "integration-test-model"
const integrationTestNamespace = "e2e-webhook-test-ns"

// defaultIntegrationPodData creates a standard integration test pod configuration
func defaultIntegrationPodData(podName, namespace, modelName string) utils.PodTemplateData {
	return utils.DefaultPodData(podName, namespace, modelName, "integration")
}

var _ = Describe("ModelValidation Integration Tests", Ordered, func() {
	Context("End-to-End Status and Metrics Integration", func() {

		It("should demonstrate full lifecycle with status and metrics consistency", func() {
			By("deploying a ModelValidation CR with signed model configuration")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: integrationTestModelName,
				Namespace: integrationTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to apply ModelValidation CR")

			By("waiting for ModelValidation CR to be ready")
			Eventually(func() error {
				return utils.KubectlGetJSON("modelvalidation", integrationTestModelName,
					integrationTestNamespace, &v1alpha1.ModelValidation{})
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "ModelValidation CR should be available")

			By("verifying initial status is correctly set")
			Eventually(func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", integrationTestModelName, integrationTestNamespace, &mv)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mv.Status.AuthMethod).To(Equal("public-key"))
				g.Expect(mv.Status.InjectedPodCount).To(Equal(int32(0)))
				g.Expect(mv.Status.LastUpdated).NotTo(BeZero())
			}, 30*time.Second).Should(Succeed())

			By("verifying initial metrics reflect the new CR")
			Eventually(func(g Gomega) {
				metricsOutput := utils.GetMetricsOutput(operatorNamespace, curlMetricsPodName)
				g.Expect(metricsOutput).To(ContainSubstring("model_validation_operator_modelvalidation_crs_total"))
				g.Expect(metricsOutput).To(ContainSubstring("model_validation_operator_status_updates_total"))
			}, 30*time.Second).Should(Succeed())

			By("deploying pods with realistic model volumes that will be injected")
			podNames := []string{"integration-pod-1", "integration-pod-2", "integration-pod-3"}
			for _, podName := range podNames {
				err = utils.KubectlApply(podTemplate,
					defaultIntegrationPodData(podName, integrationTestNamespace, integrationTestModelName))
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to deploy pod %s", podName))
			}

			By("verifying all pods get the init container injected")
			for _, podName := range podNames {
				Eventually(func(g Gomega) {
					var pod corev1.Pod
					err := utils.KubectlGetJSON("pod", podName, integrationTestNamespace, &pod)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(pod.Spec.InitContainers).ToNot(BeEmpty())
					g.Expect(utils.HasValidationContainer(&pod)).To(BeTrue())
				}, 30*time.Second).Should(Succeed())
			}

			By("verifying status reflects all injected pods")
			Eventually(func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", integrationTestModelName, integrationTestNamespace, &mv)
				g.Expect(err).NotTo(HaveOccurred())
				status := mv.Status
				g.Expect(status.InjectedPodCount).To(Equal(int32(3)))
				g.Expect(status.UninjectedPodCount).To(Equal(int32(0)))
				g.Expect(status.OrphanedPodCount).To(Equal(int32(0)))
				g.Expect(status.InjectedPods).To(HaveLen(3))

				// Verify all expected pod names are present
				podNamesInStatus := make(map[string]bool)
				for _, pod := range status.InjectedPods {
					podNamesInStatus[pod.Name] = true
					g.Expect(pod.UID).NotTo(BeEmpty())
					g.Expect(pod.InjectedAt).NotTo(BeZero())
				}
				for _, expectedName := range podNames {
					g.Expect(podNamesInStatus[expectedName]).To(BeTrue())
				}
			}, 60*time.Second, 1*time.Second).Should(Succeed())

			By("verifying metrics match the status counts")
			Eventually(func(g Gomega) {
				metricsOutput := utils.GetMetricsOutput(operatorNamespace, curlMetricsPodName)

				expectedInjectedMetric := fmt.Sprintf(
					`model_validation_operator_modelvalidation_pod_count{model_validation="%s",namespace="%s",pod_state="%s"} 3`,
					integrationTestModelName, integrationTestNamespace, metrics.PodStateInjected)
				g.Expect(metricsOutput).To(ContainSubstring(expectedInjectedMetric))

				expectedUninjectedMetric := fmt.Sprintf(
					`model_validation_operator_modelvalidation_pod_count{model_validation="%s",`+
						`namespace="%s",pod_state="%s"} 0`,
					integrationTestModelName, integrationTestNamespace, metrics.PodStateUninjected)
				g.Expect(metricsOutput).To(ContainSubstring(expectedUninjectedMetric))

				g.Expect(metricsOutput).To(ContainSubstring("model_validation_operator_status_updates_total"))
			}, 45*time.Second).Should(Succeed())

			By("simulating pod deletion and verifying status/metrics updates")
			err = utils.KubectlDelete(podTemplate, &utils.KubectlDeleteOptions{
				TemplateData: defaultIntegrationPodData(podNames[0], integrationTestNamespace, integrationTestModelName),
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying status reflects pod deletion")
			Eventually(func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", integrationTestModelName, integrationTestNamespace, &mv)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mv.Status.InjectedPodCount).To(Equal(int32(2)))
			}, 30*time.Second).Should(Succeed())

			By("verifying metrics reflect the pod deletion")
			Eventually(func(g Gomega) {
				metricsOutput := utils.GetMetricsOutput(operatorNamespace, curlMetricsPodName)
				expectedMetric := fmt.Sprintf(
					`model_validation_operator_modelvalidation_pod_count{model_validation="%s",namespace="%s",pod_state="%s"} 2`,
					integrationTestModelName, integrationTestNamespace, metrics.PodStateInjected)
				g.Expect(metricsOutput).To(ContainSubstring(expectedMetric))
			}, 30*time.Second).Should(Succeed())

			By("cleaning up all test resources")
			for _, podName := range podNames[1:] {
				_ = utils.KubectlDelete(podTemplate, &utils.KubectlDeleteOptions{
					IgnoreNotFound: true,
					TemplateData:   defaultIntegrationPodData(podName, integrationTestNamespace, integrationTestModelName),
				})
			}

			_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{
				IgnoreNotFound: true,
				TemplateData: utils.CRTemplateData{
					ModelName: integrationTestModelName,
					Namespace: integrationTestNamespace,
				},
			})

			By("verifying cleanup is reflected in status and metrics")
			Eventually(func(g Gomega) {
				var mv v1alpha1.ModelValidation
				err := utils.KubectlGetJSON("modelvalidation", integrationTestModelName, integrationTestNamespace, &mv)
				g.Expect(err).To(HaveOccurred()) // Should fail because resource is deleted
			}, 30*time.Second).Should(Succeed())
		})

	})
})
