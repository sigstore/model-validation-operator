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

// Package e2e contains end-to-end tests for the model validation operator
package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive

	utils "github.com/sigstore/model-validation-operator/test/utils"
)

var (
	// controllerPodName stores the name of the controller pod for debugging
	controllerPodName string
)

const (
	// operatorNamespace is the namespace where the project is deployed in
	operatorNamespace = "model-validation-operator-system"

	// webhookTestNamespace is the namespace for webhook tests
	webhookTestNamespace = "e2e-webhook-test-ns"
)

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purposed to be used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, and installs
// CertManager.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting model-validation-operator integration test suite\n")

	// Set test timeouts
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("verifying operator namespace exists")
	Expect(utils.KubectlResourceExists("ns", operatorNamespace, "")).To(BeTrue(), "Operator namespace should exist")

	By("verifying test namespace exists")
	Expect(utils.KubectlResourceExists("ns", webhookTestNamespace, "")).To(BeTrue(), "Test namespace should exist")

	By("verifying controller is running")
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "pods", "-l", "control-plane=controller-manager",
			"-n", operatorNamespace, "-o", "jsonpath={.items[0].metadata.name}")
		output, err := utils.Run(cmd)
		if err != nil {
			return err
		}
		controllerPodName = output

		// Verify the pod is ready
		phase, err := utils.KubectlGet("pod", controllerPodName, operatorNamespace, "jsonpath={.status.phase}")
		if err != nil {
			return err
		}
		if phase != "Running" {
			return fmt.Errorf("controller pod is not running: %s", phase)
		}
		return nil
	}, 1*time.Minute, 5*time.Second).Should(Succeed(), "Controller pod should be running")

	By("setting up persistent metrics pod")
	templateData := utils.CurlPodTemplateData{
		PodName:        curlMetricsPodName,
		Namespace:      operatorNamespace,
		ServiceAccount: utils.MetricsServiceAccountName,
	}
	err := utils.KubectlApply(curlPodTemplate, templateData)
	Expect(err).NotTo(HaveOccurred(), "Failed to create persistent metrics pod")

	err = utils.KubectlWait("pod", curlMetricsPodName, operatorNamespace, "condition=Ready", "30s")
	Expect(err).NotTo(HaveOccurred(), "Failed to wait for persistent metrics pod to be ready")
})

var _ = AfterSuite(func() {
	By("cleaning up test resources")

	// Clean up any test resources that may have been created during tests
	By("cleaning up test pods and CRs")
	cmd := exec.Command("kubectl", "delete", "pods", "--all", "-n", webhookTestNamespace,
		"--timeout=30s", "--ignore-not-found=true")
	_, _ = utils.Run(cmd)

	cmd = exec.Command("kubectl", "delete", "modelvalidations", "--all", "-n", webhookTestNamespace,
		"--timeout=30s", "--ignore-not-found=true")
	_, _ = utils.Run(cmd)

	By("cleaning up persistent metrics pod")
	cleanupTemplateData := utils.CurlPodTemplateData{
		PodName:        curlMetricsPodName,
		Namespace:      operatorNamespace,
		ServiceAccount: utils.MetricsServiceAccountName,
	}
	if err := utils.KubectlDelete(curlPodTemplate, &utils.KubectlDeleteOptions{
		IgnoreNotFound: true,
		Timeout:        "30s",
		TemplateData:   cleanupTemplateData,
	}); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to cleanup metrics pod: %s\n", err)
	}

	_, _ = fmt.Fprintf(GinkgoWriter, "Test cleanup complete.\n")
})

var _ = AfterEach(func() {
	if !CurrentSpecReport().Failed() {
		return
	}

	By("Capturing comprehensive pod logs and status for failed test")

	// Capture all pods in test namespace with logs and descriptions
	By("Fetching all pods in test namespace")
	if testPodsOutput, err := utils.KubectlGet("pods", "", webhookTestNamespace, "wide"); err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "Test namespace pods:\n%s\n", testPodsOutput)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get test namespace pods: %s\n", err)
	}

	// Get all pod names in test namespace
	podNamesOutput, err := utils.KubectlGet("pods", "", webhookTestNamespace, "jsonpath={.items[*].metadata.name}")
	if err == nil && podNamesOutput != "" {
		podNames := strings.Fields(podNamesOutput)
		for _, podName := range podNames {
			By(fmt.Sprintf("Capturing logs and description for pod: %s", podName))

			// Pod description
			if podDesc, err := utils.KubectlDescribe("pod", podName, webhookTestNamespace); err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "=== Pod %s description ===\n%s\n", podName, podDesc)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to describe pod %s: %s\n", podName, err)
			}

			// Main container logs
			if podLogs, err := utils.Run(exec.Command("kubectl", "logs", podName, "-n",
				webhookTestNamespace, "--all-containers=true")); err == nil && podLogs != "" {
				_, _ = fmt.Fprintf(GinkgoWriter, "=== Pod %s logs ===\n%s\n", podName, podLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "No logs available for pod %s or error: %v\n", podName, err)
			}

			// Previous container logs (if restarted)
			if prevLogs, err := utils.Run(exec.Command("kubectl", "logs", podName, "-n",
				webhookTestNamespace, "--all-containers=true", "--previous=true")); err == nil && prevLogs != "" {
				_, _ = fmt.Fprintf(GinkgoWriter, "=== Pod %s previous logs ===\n%s\n", podName, prevLogs)
			}

			// Init container logs specifically
			if initLogs, err := utils.Run(exec.Command("kubectl", "logs", podName, "-n",
				webhookTestNamespace, "-c", "model-transparency-init")); err == nil && initLogs != "" {
				_, _ = fmt.Fprintf(GinkgoWriter, "=== Pod %s init container logs ===\n%s\n", podName, initLogs)
			}
		}
	}

	// Capture controller logs
	By("Fetching controller manager pod logs")
	if controllerLogs, err := utils.Run(exec.Command("kubectl", "logs", controllerPodName,
		"-n", operatorNamespace)); err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== Controller logs ===\n%s\n", controllerLogs)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s\n", err)
	}

	// Capture events from both namespaces
	By("Fetching test namespace events")
	if testEventsOutput, err := utils.KubectlGet("events", "", webhookTestNamespace, ""); err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== Test namespace events ===\n%s\n", testEventsOutput)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get test namespace events: %s\n", err)
	}

	By("Fetching operator namespace events")
	if eventsOutput, err := utils.KubectlGet("events", "", operatorNamespace, ""); err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== Operator namespace events ===\n%s\n", eventsOutput)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get operator namespace events: %s\n", err)
	}

	// Capture ModelValidation resources for debugging
	By("Fetching all ModelValidation resources")
	mvOutput, err := utils.KubectlGet("modelvalidations", "", webhookTestNamespace, "wide")
	if err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== ModelValidation resources ===\n%s\n", mvOutput)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get ModelValidation resources: %s\n", err)
	}

	// Get detailed YAML output for ModelValidation resources
	mvYamlOutput, err := utils.KubectlGet("modelvalidations", "", webhookTestNamespace, "yaml")
	if err == nil && mvYamlOutput != "" {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== ModelValidation resources (YAML) ===\n%s\n", mvYamlOutput)
	} else if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get ModelValidation YAML: %s\n", err)
	}
})
