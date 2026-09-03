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

package e2eolm

import (
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive

	utils "github.com/sigstore/model-validation-operator/test/utils"
)

const (
	operatorNamespace    = "model-validation-operator-system"
	webhookTestNamespace = "e2e-webhook-test-ns"
)

var olmNamespace string

func detectOLMNamespace() string {
	if utils.KubectlResourceExists("ns", "openshift-operator-lifecycle-manager", "") {
		return "openshift-operator-lifecycle-manager"
	}
	return "olm"
}

func TestOLMUpgrade(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting OLM upgrade test suite\n")

	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	RunSpecs(t, "OLM Upgrade Suite")
}

var _ = BeforeSuite(func() {
	By("detecting OLM namespace")
	olmNamespace = detectOLMNamespace()
	_, _ = fmt.Fprintf(GinkgoWriter, "Using OLM namespace: %s\n", olmNamespace)

	By("verifying OLM is installed")
	Expect(utils.KubectlResourceExists("ns", olmNamespace, "")).To(BeTrue(),
		"OLM namespace should exist - run 'make e2e-install-olm' first")

	By("verifying OLM operators are running")
	Eventually(func() error {
		output, err := utils.KubectlGet("pods", "", olmNamespace,
			"jsonpath={range .items[*]}{.status.phase}{\"\\n\"}{end}")
		if err != nil {
			return err
		}
		phases := utils.GetNonEmptyLines(output)
		if len(phases) == 0 {
			return fmt.Errorf("no OLM pods found in %s namespace", olmNamespace)
		}
		for _, phase := range phases {
			if phase != "Running" && phase != "Succeeded" {
				return fmt.Errorf("OLM pod not ready, phase: %s", phase)
			}
		}
		return nil
	}, 2*time.Minute, 5*time.Second).Should(Succeed(), "OLM operators should be running")

	By("verifying operator namespace exists")
	Expect(utils.KubectlResourceExists("ns", operatorNamespace, "")).To(BeTrue(),
		"Operator namespace should exist - run 'make e2e-setup-namespaces' first")

	By("verifying test namespace exists")
	Expect(utils.KubectlResourceExists("ns", webhookTestNamespace, "")).To(BeTrue(),
		"Test namespace should exist - run 'make e2e-setup-namespaces' first")
})

var _ = AfterSuite(func() {
	_, _ = fmt.Fprintf(GinkgoWriter, "OLM upgrade test suite cleanup complete\n")
})

var _ = JustAfterEach(func() {
	if !CurrentSpecReport().Failed() {
		return
	}

	_, _ = fmt.Fprintf(GinkgoWriter, "----------------------- Dumping operator resources -----------------------\n")
	if csvOutput, err := utils.KubectlGet("csv", "", operatorNamespace, "wide"); err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "CSVs: %s\n", csvOutput)
	}
	if podOutput, err := utils.KubectlGet("pods", "", operatorNamespace, "wide"); err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "Pods: %s\n", podOutput)
	}
})
