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
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	corev1 "k8s.io/api/core/v1"

	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

func cleanupResource(resource, name, namespace string) {
	args := []string{"delete", resource}
	if name != "" {
		args = append(args, name)
	} else {
		args = append(args, "--all")
	}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	args = append(args, "--timeout=60s", "--ignore-not-found=true")
	_, _ = utils.Run(exec.Command("kubectl", args...))
}

func envOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

var _ = Describe("OLM Upgrade", Ordered, func() {
	var (
		baseCatalogImage   string
		targetCatalogImage string
		channelName        string
		packageName        string
		baseCSV            string
	)

	const (
		catalogName       = "model-validation-test-catalog"
		operatorGroupName = "model-validation-og"
		subscriptionName  = "model-validation-operator"
		testModelName     = "olm-upgrade-test-model"
	)

	BeforeAll(func() {
		baseCatalogImage = os.Getenv("TEST_BASE_CATALOG")
		Expect(baseCatalogImage).NotTo(BeEmpty(), "TEST_BASE_CATALOG env var must be set")

		targetCatalogImage = os.Getenv("TEST_TARGET_CATALOG")
		Expect(targetCatalogImage).NotTo(BeEmpty(), "TEST_TARGET_CATALOG env var must be set")

		channelName = envOrDefault("OLM_CHANNEL", "tech-preview")
		packageName = envOrDefault("OLM_PACKAGE", "model-validation-operator")

		_, _ = fmt.Fprintf(GinkgoWriter, "OLM Upgrade Test Configuration:\n")
		_, _ = fmt.Fprintf(GinkgoWriter, "  Base catalog: %s\n", baseCatalogImage)
		_, _ = fmt.Fprintf(GinkgoWriter, "  Target catalog: %s\n", targetCatalogImage)
		_, _ = fmt.Fprintf(GinkgoWriter, "  Channel: %s\n", channelName)
		_, _ = fmt.Fprintf(GinkgoWriter, "  Package: %s\n", packageName)

		DeferCleanup(func() {
			By("cleaning up OLM test resources")
			cleanupResource("pods", "", webhookTestNamespace)
			cleanupResource("modelvalidations", "", webhookTestNamespace)
			cleanupResource("subscription", subscriptionName, operatorNamespace)
			cleanupResource("csv", "", operatorNamespace)
			cleanupResource("operatorgroup", operatorGroupName, operatorNamespace)
			cleanupResource("catalogsource", catalogName, olmNamespace)
		})
	})

	It("should install the initial version via OLM", func() {
		By("creating a CatalogSource with the base catalog image")
		err := utils.KubectlApply(catalogSourceTemplate, utils.CatalogSourceTemplateData{
			Name:      catalogName,
			Namespace: olmNamespace,
			Image:     baseCatalogImage,
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create CatalogSource")

		By("waiting for CatalogSource to be ready")
		Eventually(func(g Gomega) {
			status, err := utils.KubectlGet("catalogsource", catalogName, olmNamespace,
				"jsonpath={.status.connectionState.lastObservedState}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(status).To(Equal("READY"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed(), "CatalogSource should become READY")

		By("creating an OperatorGroup for AllNamespaces mode")
		err = utils.KubectlApply(operatorGroupTemplate, utils.OperatorGroupTemplateData{
			Name:      operatorGroupName,
			Namespace: operatorNamespace,
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create OperatorGroup")

		By("creating a Subscription to trigger installation")
		err = utils.KubectlApply(subscriptionTemplate, utils.SubscriptionTemplateData{
			Name:                   subscriptionName,
			Namespace:              operatorNamespace,
			Channel:                channelName,
			PackageName:            packageName,
			CatalogSourceName:      catalogName,
			CatalogSourceNamespace: olmNamespace,
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create Subscription")

		By("waiting for subscription to report an installed CSV")
		Eventually(func(g Gomega) {
			csv, err := utils.KubectlGet("subscription", subscriptionName, operatorNamespace,
				"jsonpath={.status.installedCSV}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(csv).NotTo(BeEmpty())
			baseCSV = csv
		}, 5*time.Minute, 5*time.Second).Should(Succeed(),
			"Subscription should report an installed CSV")

		_, _ = fmt.Fprintf(GinkgoWriter, "  Installed base CSV: %s\n", baseCSV)

		By(fmt.Sprintf("waiting for base CSV %s to reach Succeeded phase", baseCSV))
		Eventually(func(g Gomega) {
			phase, err := utils.KubectlGet("csv", baseCSV, operatorNamespace,
				"jsonpath={.status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(phase).To(Equal("Succeeded"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed(),
			"Base CSV should reach Succeeded phase")

		By("waiting for controller deployment to be available")
		Eventually(func() error {
			return utils.KubectlWait("deployment", "model-validation-controller-manager",
				operatorNamespace, "condition=Available", "30s")
		}, 3*time.Minute, 10*time.Second).Should(Succeed(),
			"Controller deployment should be available")

		By("verifying controller pod is running")
		Eventually(func(g Gomega) {
			output, err := utils.KubectlGet("pods", "", operatorNamespace,
				"jsonpath={.items[?(@.metadata.labels.control-plane==\"controller-manager\")].status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("Running"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"Controller pod should be running")
	})

	It("should have a functioning webhook after OLM installation", func() {
		By("deploying a ModelValidation CR")
		err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
			ModelName: testModelName,
			Namespace: webhookTestNamespace,
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to apply ModelValidation CR")

		By("deploying a test pod with model validation label")
		err = utils.KubectlApply(podTemplate, utils.DefaultPodData(
			"olm-test-pod-initial", webhookTestNamespace, testModelName, "olm-upgrade"))
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy test pod")

		By("verifying the init container is injected by the webhook")
		Eventually(func(g Gomega) {
			var pod corev1.Pod
			err := utils.KubectlGetJSON("pod", "olm-test-pod-initial", webhookTestNamespace, &pod)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pod.Spec.InitContainers).ToNot(BeEmpty(),
				"Pod should have init containers after webhook injection")
			g.Expect(pod.Spec.InitContainers[0].Name).To(
				Equal(constants.ModelValidationInitContainerName))
		}, 1*time.Minute, 5*time.Second).Should(Succeed(),
			"Init container should be injected")
	})

	It("should upgrade when the catalog is updated", func() {
		By(fmt.Sprintf("patching CatalogSource to use target catalog: %s", targetCatalogImage))
		err := utils.KubectlPatch("catalogsource", catalogName, olmNamespace, "merge",
			fmt.Sprintf(`{"spec":{"image":"%s"}}`, targetCatalogImage))
		Expect(err).NotTo(HaveOccurred(), "Failed to patch CatalogSource")

		By("waiting for CatalogSource to reconnect with updated catalog")
		Eventually(func(g Gomega) {
			status, err := utils.KubectlGet("catalogsource", catalogName, olmNamespace,
				"jsonpath={.status.connectionState.lastObservedState}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(status).To(Equal("READY"))
		}, 3*time.Minute, 5*time.Second).Should(Succeed(),
			"CatalogSource should reconnect after update")

		By("waiting for subscription to report a new installed CSV")
		Eventually(func(g Gomega) {
			csv, err := utils.KubectlGet("subscription", subscriptionName, operatorNamespace,
				"jsonpath={.status.installedCSV}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(csv).NotTo(BeEmpty())
			g.Expect(csv).NotTo(Equal(baseCSV), "installed CSV should change after upgrade")
		}, 5*time.Minute, 10*time.Second).Should(Succeed(),
			"Subscription should report a new installed CSV after upgrade")

		var upgradeCSV string
		upgradeCSV, err = utils.KubectlGet("subscription", subscriptionName, operatorNamespace,
			"jsonpath={.status.installedCSV}")
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "  Upgraded CSV: %s (was: %s)\n", upgradeCSV, baseCSV)

		By(fmt.Sprintf("waiting for upgrade CSV %s to reach Succeeded phase", upgradeCSV))
		Eventually(func(g Gomega) {
			phase, err := utils.KubectlGet("csv", upgradeCSV, operatorNamespace,
				"jsonpath={.status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(phase).To(Equal("Succeeded"))
		}, 5*time.Minute, 10*time.Second).Should(Succeed(),
			"Upgrade CSV should reach Succeeded phase")

		By("waiting for controller deployment to be available after upgrade")
		Eventually(func() error {
			return utils.KubectlWait("deployment", "model-validation-controller-manager",
				operatorNamespace, "condition=Available", "30s")
		}, 3*time.Minute, 10*time.Second).Should(Succeed(),
			"Controller deployment should be available after upgrade")
	})

	It("should maintain functionality after upgrade", func() {
		By("verifying existing ModelValidation CR survived the upgrade")
		Expect(utils.KubectlResourceExists("modelvalidation", testModelName,
			webhookTestNamespace)).To(BeTrue(),
			"Existing ModelValidation CR should persist through upgrade")

		By("verifying controller pod is running after upgrade")
		Eventually(func(g Gomega) {
			output, err := utils.KubectlGet("pods", "", operatorNamespace,
				"jsonpath={.items[?(@.metadata.labels.control-plane==\"controller-manager\")].status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("Running"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"Controller pod should be running after upgrade")

		By("deploying a new test pod to verify webhook still works after upgrade")
		Eventually(func() error {
			return utils.KubectlApply(podTemplate, utils.DefaultPodData(
				"olm-test-pod-upgrade", webhookTestNamespace, testModelName, "olm-upgrade"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"Post-upgrade test pod should be accepted by webhook")

		By("verifying the init container is injected on the new pod")
		Eventually(func(g Gomega) {
			var pod corev1.Pod
			err := utils.KubectlGetJSON("pod", "olm-test-pod-upgrade", webhookTestNamespace, &pod)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pod.Spec.InitContainers).ToNot(BeEmpty(),
				"Post-upgrade pod should have init containers")
			g.Expect(pod.Spec.InitContainers[0].Name).To(
				Equal(constants.ModelValidationInitContainerName))
		}, 1*time.Minute, 5*time.Second).Should(Succeed(),
			"Init container should be injected after upgrade")
	})

})
