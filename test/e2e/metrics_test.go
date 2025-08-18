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

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/metrics"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

const metricsTestPodName = "metrics-test-pod"
const metricsTestModelName = "metrics-test-model"

// defaultMetricsPodData creates a standard metrics test pod configuration
func defaultMetricsPodData(podName string) utils.PodTemplateData {
	return utils.DefaultPodData(podName, webhookTestNamespace, metricsTestModelName, "metrics")
}

var _ = Describe("ModelValidation Metrics", Ordered, func() {
	Context("Prometheus Metrics Collection", func() {

		It("should expose pod count metrics", func() {
			By("deploying a ModelValidation CR")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: metricsTestModelName,
				Namespace: webhookTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for ModelValidation CR to be ready")
			Eventually(func() error {
				return utils.KubectlGetJSON("modelvalidation", metricsTestModelName,
					webhookTestNamespace, &v1alpha1.ModelValidation{})
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "ModelValidation CR should be available")

			By("getting baseline metrics before pod creation")
			injectedLabels := map[string]string{
				"namespace":        webhookTestNamespace,
				"model_validation": metricsTestModelName,
				"pod_state":        metrics.PodStateInjected,
			}
			baselineInjectedCount := utils.GetMetricValue(
				"model_validation_operator_modelvalidation_pod_count", injectedLabels,
				operatorNamespace, curlMetricsPodName)

			By("deploying a pod to trigger metrics updates")
			err = utils.KubectlApply(podTemplate, defaultMetricsPodData(metricsTestPodName))
			Expect(err).NotTo(HaveOccurred())

			By("verifying metrics increment after pod injection")
			Eventually(func() int {
				return utils.GetMetricValue("model_validation_operator_modelvalidation_pod_count",
					injectedLabels, operatorNamespace, curlMetricsPodName)
			}, 30*time.Second, 5*time.Second).Should(Equal(baselineInjectedCount + 1))

			By("cleaning up test resources - deleting pod first")
			_ = utils.KubectlDelete(podTemplate, &utils.KubectlDeleteOptions{
				Timeout:        "30s",
				IgnoreNotFound: true,
				TemplateData:   defaultMetricsPodData(metricsTestPodName),
			})

			By("verifying metrics decrement after pod deletion")
			Eventually(func() int {
				return utils.GetMetricValue("model_validation_operator_modelvalidation_pod_count",
					injectedLabels, operatorNamespace, curlMetricsPodName)
			}, 30*time.Second, 5*time.Second).Should(Equal(baselineInjectedCount))

			_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{
				Timeout:        "30s",
				IgnoreNotFound: true,
				TemplateData: utils.CRTemplateData{
					ModelName: metricsTestModelName,
					Namespace: webhookTestNamespace,
				},
			})
		})

		It("should track status update metrics", func() {
			By("getting baseline status update metrics")
			statusLabels := map[string]string{
				"namespace":        webhookTestNamespace,
				"model_validation": metricsTestModelName,
				"result":           "success",
			}
			baselineSuccessCount := utils.GetMetricValue(
				"model_validation_operator_status_updates_total", statusLabels,
				operatorNamespace, curlMetricsPodName)

			By("deploying a ModelValidation CR")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: metricsTestModelName,
				Namespace: webhookTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying status update metrics increment")
			Eventually(func() int {
				return utils.GetMetricValue("model_validation_operator_status_updates_total",
					statusLabels, operatorNamespace, curlMetricsPodName)
			}, 30*time.Second, 5*time.Second).Should(BeNumerically(">", baselineSuccessCount))

			By("cleaning up test resources")
			_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{
				Timeout:        "30s",
				IgnoreNotFound: true,
				TemplateData: utils.CRTemplateData{
					ModelName: metricsTestModelName,
					Namespace: webhookTestNamespace,
				},
			})
		})

		It("should track pod state transitions", func() {
			By("deploying a ModelValidation CR")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: metricsTestModelName,
				Namespace: webhookTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			By("deploying a pod (this creates '' -> 'injected' transition, not tracked)")
			err = utils.KubectlApply(podTemplate, defaultMetricsPodData(fmt.Sprintf("%s-transition", metricsTestPodName)))
			Expect(err).NotTo(HaveOccurred())

			By("getting baseline transition metrics for injected -> orphaned")
			transitionLabels := map[string]string{
				"from_state":       metrics.PodStateInjected,
				"to_state":         metrics.PodStateOrphaned,
				"namespace":        webhookTestNamespace,
				"model_validation": metricsTestModelName,
			}
			baselineTransitionCount := utils.GetMetricValue(
				"model_validation_operator_pod_state_transitions_total", transitionLabels,
				operatorNamespace, curlMetricsPodName)

			By("updating the ModelValidation CR keyPath to trigger injected -> orphaned transition")
			err = utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: metricsTestModelName,
				Namespace: webhookTestNamespace,
				KeyPath:   "/keys/different_key.pub",
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying transition metrics increment after MV update")
			Eventually(func() int {
				return utils.GetMetricValue("model_validation_operator_pod_state_transitions_total",
					transitionLabels, operatorNamespace, curlMetricsPodName)
			}, 30*time.Second, 5*time.Second).Should(Equal(baselineTransitionCount + 1))

			By("cleaning up test resources")
			podData := defaultMetricsPodData(fmt.Sprintf("%s-transition", metricsTestPodName))
			_ = utils.KubectlDelete(podTemplate, &utils.KubectlDeleteOptions{
				Timeout:        "30s",
				IgnoreNotFound: true,
				TemplateData:   podData,
			})

			_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{
				Timeout:        "30s",
				IgnoreNotFound: true,
				TemplateData: utils.CRTemplateData{
					ModelName: metricsTestModelName,
					Namespace: webhookTestNamespace,
				},
			})
		})

		It("should track ModelValidation CR count per namespace", func() {
			By("getting baseline transition metrics for namespace count")
			namespaceLabels := map[string]string{
				"namespace": webhookTestNamespace,
			}
			namespaceCount := utils.GetMetricValue(
				"model_validation_operator_modelvalidation_crs_total", namespaceLabels,
				operatorNamespace, curlMetricsPodName)
			By("deploying multiple ModelValidation CRs in the test namespace")
			for i := 1; i <= 2; i++ {
				err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
					ModelName: fmt.Sprintf("metrics-test-model-%d", i),
					Namespace: webhookTestNamespace,
				})
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to apply ModelValidation CR %d", i))
			}

			By("verifying CR count after ModelValidation deployments")
			Eventually(func() int {
				return utils.GetMetricValue("model_validation_operator_modelvalidation_crs_total",
					namespaceLabels, operatorNamespace, curlMetricsPodName)
			}, 30*time.Second, 5*time.Second).Should(Equal(namespaceCount + 2))

			By("cleaning up test resources")
			for i := 1; i <= 2; i++ {
				_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{
					Timeout:        "30s",
					IgnoreNotFound: true,
				})
			}
		})

		It("should expose queue size metrics", func() {
			By("checking metrics for queue information")
			metricsOutput := utils.GetMetricsOutput(operatorNamespace, curlMetricsPodName)

			Expect(metricsOutput).To(ContainSubstring("model_validation_operator_status_update_queue_size"))

			Expect(metricsOutput).To(ContainSubstring("model_validation_operator_status_update_duration_seconds"))
		})
	})

	Context("Metrics Integration", func() {
		It("should show consistent metrics across pod lifecycle", func() {
			By("deploying a ModelValidation CR")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: metricsTestModelName,
				Namespace: webhookTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			By("getting baseline metrics")
			baselineMetrics := utils.GetMetricsOutput(operatorNamespace, curlMetricsPodName)
			baselineInjectedCount := utils.ExtractMetricValue(baselineMetrics,
				"model_validation_operator_modelvalidation_pod_count",
				map[string]string{
					"namespace":        webhookTestNamespace,
					"model_validation": metricsTestModelName,
					"pod_state":        metrics.PodStateInjected,
				})

			By("deploying a pod")
			err = utils.KubectlApply(podTemplate, defaultMetricsPodData(fmt.Sprintf("%s-lifecycle", metricsTestPodName)))
			Expect(err).NotTo(HaveOccurred())

			By("verifying metrics increment after pod injection")
			Eventually(func(g Gomega) {
				currentMetrics := utils.GetMetricsOutput(operatorNamespace, curlMetricsPodName)
				currentInjectedCount := utils.ExtractMetricValue(currentMetrics,
					"model_validation_operator_modelvalidation_pod_count",
					map[string]string{
						"namespace":        webhookTestNamespace,
						"model_validation": metricsTestModelName,
						"pod_state":        metrics.PodStateInjected,
					})
				g.Expect(currentInjectedCount).To(Equal(baselineInjectedCount + 1))
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("deleting the pod")
			podToDelete := fmt.Sprintf(
				"apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\n  namespace: %s",
				fmt.Sprintf("%s-lifecycle", metricsTestPodName), webhookTestNamespace)
			err = utils.KubectlDelete([]byte(podToDelete), nil)
			Expect(err).NotTo(HaveOccurred())

			By("verifying metrics decrement after pod deletion")
			Eventually(func(g Gomega) {
				currentMetrics := utils.GetMetricsOutput(operatorNamespace, curlMetricsPodName)
				currentInjectedCount := utils.ExtractMetricValue(currentMetrics,
					"model_validation_operator_modelvalidation_pod_count",
					map[string]string{
						"namespace":        webhookTestNamespace,
						"model_validation": metricsTestModelName,
						"pod_state":        metrics.PodStateInjected,
					})
				g.Expect(currentInjectedCount).To(Equal(baselineInjectedCount))
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("cleaning up test resources")
			_ = utils.KubectlDelete(modelValidationTemplate, &utils.KubectlDeleteOptions{
				Timeout:        "30s",
				IgnoreNotFound: true,
				TemplateData: utils.CRTemplateData{
					ModelName: metricsTestModelName,
					Namespace: webhookTestNamespace,
				},
			})
		})
	})
})
