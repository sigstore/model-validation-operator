// Package testutil provides Ginkgo-specific test utilities
package testutil

import (
	"context"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/onsi/gomega"
)

// ExpectModelValidationTracking verifies a ModelValidation's tracking state
func ExpectModelValidationTracking(
	statusTracker ModelValidationTracker,
	namespacedName types.NamespacedName,
	shouldBeTracked bool,
) {
	isTracked := statusTracker.IsModelValidationTracked(namespacedName)
	gomega.Expect(isTracked).To(gomega.Equal(shouldBeTracked),
		"Expected ModelValidation %s tracking state to be %t", namespacedName, shouldBeTracked)
}

// GetModelValidationFromClientExpected retrieves a ModelValidation from the client with Ginkgo expectations
func GetModelValidationFromClientExpected(
	ctx context.Context,
	client client.Client,
	namespacedName types.NamespacedName,
) *v1alpha1.ModelValidation {
	mv, err := GetModelValidationFromClient(ctx, client, namespacedName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return mv
}

// ExpectAttemptCount waits for and verifies expected attempt counts in retry testing
func ExpectAttemptCount(fakeClient *FailingClient, expectedAttempts int) {
	gomega.Eventually(func() int {
		return fakeClient.GetAttemptCount()
	}, "2s", "10ms").Should(gomega.Equal(expectedAttempts))
}
