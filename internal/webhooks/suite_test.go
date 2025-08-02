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

// Package webhooks provides mutating webhooks for model validation
package webhooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/tracker"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/test"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg           *rest.Config
	k8sClient     client.Client // You'll be using this client in your tests.
	testEnv       *envtest.Environment
	ctx           context.Context
	cancel        context.CancelFunc
	statusTracker tracker.StatusTracker
)

// findBinaryAssetsDirectory locates the kubernetes binaries directory
// in the bin/k8s directory and returns the path to use with envtest.
// Expects exactly one directory matching the platform suffix.
func findBinaryAssetsDirectory() (string, error) {
	binDir := filepath.Join("..", "..", "bin", "k8s")

	// Check if the directory exists
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		return "", fmt.Errorf("bin/k8s directory does not exist")
	}

	entries, err := os.ReadDir(binDir)
	if err != nil {
		return "", fmt.Errorf("failed to read bin/k8s directory: %w", err)
	}

	var matchingDirs []string
	expectedSuffix := fmt.Sprintf("-%s-%s", runtime.GOOS, runtime.GOARCH)

	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), expectedSuffix) {
			matchingDirs = append(matchingDirs, entry.Name())
		}
	}

	if len(matchingDirs) == 0 {
		return "", fmt.Errorf("no directories found with suffix %s", expectedSuffix)
	}

	if len(matchingDirs) > 1 {
		return "", fmt.Errorf("multiple directories found with suffix %s: %v", expectedSuffix, matchingDirs)
	}

	return filepath.Join(binDir, matchingDirs[0]), nil
}

func TestAPIs(t *testing.T) {
	fs := test.InitKlog(t)
	_ = fs.Set("v", "5")
	klog.SetOutput(GinkgoWriter)
	ctrl.SetLogger(klog.NewKlogr())

	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")

	binaryAssetsDir, err := findBinaryAssetsDirectory()
	if err != nil {
		Fail(fmt.Sprintf("Failed to locate Kubernetes binary assets: %v\n"+
			"Please run 'make setup-envtest' to download the required binaries.", err))
	}

	By(fmt.Sprintf("Using binary assets directory: %s", binaryAssetsDir))

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,

		BinaryAssetsDirectory: binaryAssetsDir,
		WebhookInstallOptions: envtest.WebhookInstallOptions{Paths: []string{filepath.Join("..", "..", "config", "webhook")}},
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	webhookServer := webhook.NewServer(webhook.Options{
		Host:    testEnv.WebhookInstallOptions.LocalServingHost,
		Port:    testEnv.WebhookInstallOptions.LocalServingPort,
		CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
	})

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:        scheme.Scheme,
		WebhookServer: webhookServer,
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Create a decoder for your webhook
	decoder := admission.NewDecoder(scheme.Scheme)
	statusTracker = tracker.NewStatusTracker(mgr.GetClient(), tracker.StatusTrackerConfig{
		DebounceDuration:    50 * time.Millisecond, // Faster for tests
		RetryBaseDelay:      10 * time.Millisecond, // Faster retries for tests
		RetryMaxDelay:       1 * time.Second,       // Lower max delay for tests
		RateLimitQPS:        100,                   // Higher QPS for tests
		RateLimitBurst:      1000,                  // Higher burst for tests
		StatusUpdateTimeout: 5 * time.Second,       // Shorter timeout for tests
	})
	podWebhookHandler := NewPodInterceptor(mgr.GetClient(), decoder)
	mgr.GetWebhookServer().Register("/mutate-v1-pod", &admission.Webhook{
		Handler: podWebhookHandler,
	})

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	// Wait for webhook server to be ready by checking if it's serving
	Eventually(func() error {
		return mgr.GetWebhookServer().StartedChecker()(nil)
	}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	statusTracker.Stop()
	cancel()

	// Give manager time to shutdown gracefully
	time.Sleep(100 * time.Millisecond)

	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
