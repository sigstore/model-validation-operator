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

package e2e

import (
	"os/exec"

	_ "embed"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	corev1 "k8s.io/api/core/v1"

	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "model-validation-operator-metrics-binding"

// curlMetricsPodName is the name of the pod used to access the metrics endpoint
const curlMetricsPodName = "curl-metrics"

const e2eTestPodName = "e2e-test-pod"

var _ = Describe("Manager", Ordered, func() {
	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", operatorNamespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", operatorNamespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			err := utils.KubectlApply(clusterRoleBindingTemplate, utils.ClusterRoleBindingTemplateData{
				Name:               metricsRoleBindingName,
				ServiceAccountName: utils.ServiceAccountName,
				Namespace:          operatorNamespace,
				ClusterRoleName:    "model-validation-metrics-reader",
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to apply ClusterRoleBinding")

			By("validating that the metrics service is available")
			exists := utils.KubectlResourceExists("service", utils.MetricsServiceName, operatorNamespace)
			Expect(exists).To(BeTrue(), "Metrics service should exist")

			By("getting the service account token")
			token, err := utils.CreateServiceAccountToken(utils.ServiceAccountName, operatorNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				output, err := utils.KubectlGet("endpoints", utils.MetricsServiceName, operatorNamespace, "")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", operatorNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("getting the metrics from the persistent curl pod")
			metricsOutput := utils.GetMetricsOutput(operatorNamespace, curlMetricsPodName)
			Expect(metricsOutput).To(ContainSubstring(
				"controller_runtime_webhook_requests_total",
			))
		})

		It("should inject the model validation init container", func() {
			By("deploying a ModelValidation CR")
			err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
				ModelName: "e2e-test-model",
				Namespace: webhookTestNamespace,
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to apply ModelValidation CR")

			By("deploying a pod with the model validation label")
			err = utils.KubectlApply(podTemplate, utils.PodTemplateData{
				PodName:   "e2e-test-pod",
				Namespace: webhookTestNamespace,
				ModelName: "e2e-test-model",
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to apply test pod")

			By("verifying the init container is injected")
			verifyInitContainerInjection := func(g Gomega) {
				var pod corev1.Pod
				err := utils.KubectlGetJSON("pod", e2eTestPodName, webhookTestNamespace, &pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Spec.InitContainers).To(HaveLen(1))
				g.Expect(pod.Spec.InitContainers[0].Name).To(Equal(constants.ModelValidationInitContainerName))
			}
			Eventually(verifyInitContainerInjection).Should(Succeed())
		})

		AfterAll(func() {
			By("cleaning up ClusterRoleBinding")
			_ = utils.KubectlDelete(clusterRoleBindingTemplate, &utils.KubectlDeleteOptions{
				Timeout:        "30s",
				IgnoreNotFound: true,
				TemplateData: utils.ClusterRoleBindingTemplateData{
					Name:               metricsRoleBindingName,
					ServiceAccountName: utils.ServiceAccountName,
					Namespace:          operatorNamespace,
					ClusterRoleName:    "model-validation-metrics-reader",
				},
			})
		})
	})
})
