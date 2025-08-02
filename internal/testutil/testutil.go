// Package testutil provides shared test utilities for the model validation operator
package testutil

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DefaultNamespace is the default namespace used in tests
const DefaultNamespace = "default"

// TestModelValidationOptions holds configuration for creating test ModelValidation resources
type TestModelValidationOptions struct {
	Name              string
	Namespace         string
	DeletionTimestamp *metav1.Time
	Finalizers        []string
	ConfigType        string
	CertificateCA     string
	CertIdentity      string
	CertOidcIssuer    string
}

// TestPodOptions holds configuration for creating test Pod resources
type TestPodOptions struct {
	Name              string
	Namespace         string
	UID               types.UID
	Labels            map[string]string
	Annotations       map[string]string
	Finalizers        []string
	DeletionTimestamp *metav1.Time
}

// CreateTestModelValidation creates a test ModelValidation resource with the given options
func CreateTestModelValidation(opts TestModelValidationOptions) *v1alpha1.ModelValidation {
	if opts.Name == "" {
		opts.Name = fmt.Sprintf("test-mv-%d", time.Now().UnixNano())
	}
	if opts.Namespace == "" {
		opts.Namespace = DefaultNamespace
	}
	if opts.ConfigType == "" {
		opts.ConfigType = "sigstore"
	}

	mv := &v1alpha1.ModelValidation{
		ObjectMeta: metav1.ObjectMeta{
			Name:              opts.Name,
			Namespace:         opts.Namespace,
			DeletionTimestamp: opts.DeletionTimestamp,
			Finalizers:        opts.Finalizers,
		},
		Spec: v1alpha1.ModelValidationSpec{
			Model: v1alpha1.Model{
				Path:          "test-model",
				SignaturePath: "test-signature",
			},
		},
	}

	// Configure auth method
	switch opts.ConfigType {
	case "pki":
		certCA := opts.CertificateCA
		if certCA == "" {
			certCA = "test-ca"
		}
		mv.Spec.Config = v1alpha1.ValidationConfig{
			PkiConfig: &v1alpha1.PkiConfig{
				CertificateAuthority: certCA,
			},
		}
	case "sigstore":
		fallthrough
	default:
		certIdentity := opts.CertIdentity
		if certIdentity == "" {
			certIdentity = "test@example.com"
		}
		certOidcIssuer := opts.CertOidcIssuer
		if certOidcIssuer == "" {
			certOidcIssuer = "https://accounts.google.com"
		}
		mv.Spec.Config = v1alpha1.ValidationConfig{
			SigstoreConfig: &v1alpha1.SigstoreConfig{
				CertificateIdentity:   certIdentity,
				CertificateOidcIssuer: certOidcIssuer,
			},
		}
	}

	return mv
}

// CreateTestPod creates a test Pod resource with the given options
func CreateTestPod(opts TestPodOptions) *corev1.Pod {
	if opts.Name == "" {
		opts.Name = fmt.Sprintf("test-pod-%d", time.Now().UnixNano())
	}
	if opts.Namespace == "" {
		opts.Namespace = DefaultNamespace
	}
	if opts.UID == types.UID("") {
		opts.UID = types.UID(fmt.Sprintf("uid-%d", time.Now().UnixNano()))
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              opts.Name,
			Namespace:         opts.Namespace,
			UID:               opts.UID,
			Labels:            opts.Labels,
			Annotations:       opts.Annotations,
			Finalizers:        opts.Finalizers,
			DeletionTimestamp: opts.DeletionTimestamp,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}

	return pod
}

// CreateTestNamespacedName creates a NamespacedName for testing
func CreateTestNamespacedName(name, namespace string) types.NamespacedName {
	if name == "" {
		name = fmt.Sprintf("test-name-%d", time.Now().UnixNano())
	}
	if namespace == "" {
		namespace = DefaultNamespace
	}
	return types.NamespacedName{Name: name, Namespace: namespace}
}

// SetupFakeClientWithObjects creates a fake Kubernetes client with the given objects
func SetupFakeClientWithObjects(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err) // This should not happen in tests
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		panic(err) // This should not happen in tests
	}

	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
		// Add status subresource for ModelValidation objects
		for _, obj := range objects {
			if _, ok := obj.(*v1alpha1.ModelValidation); ok {
				builder = builder.WithStatusSubresource(obj)
			}
		}
	}

	return builder.Build()
}

// CreateReconcileRequest creates a reconcile request for testing
func CreateReconcileRequest(namespace, name string) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}}
}

// ModelValidationTracker interface for tracking functionality
type ModelValidationTracker interface {
	IsModelValidationTracked(namespacedName types.NamespacedName) bool
}

// GetModelValidationFromClient retrieves a ModelValidation from the client
func GetModelValidationFromClient(
	ctx context.Context,
	client client.Client,
	namespacedName types.NamespacedName,
) (*v1alpha1.ModelValidation, error) {
	mv := &v1alpha1.ModelValidation{}
	err := client.Get(ctx, namespacedName, mv)
	return mv, err
}

// FailingClient is a mock client that fails status updates for testing retry behavior
type FailingClient struct {
	client.Client
	FailureCount int
	MaxFailures  int
	AttemptCount int
	mu           sync.Mutex
}

// Status returns a failing sub-resource writer for testing
func (f *FailingClient) Status() client.SubResourceWriter {
	return &FailingSubResourceWriter{
		SubResourceWriter: f.Client.Status(),
		parent:            f,
	}
}

// FailingSubResourceWriter wraps a SubResourceWriter to simulate failures
type FailingSubResourceWriter struct {
	client.SubResourceWriter
	parent *FailingClient
}

// Update simulates failures for the first MaxFailures attempts, then succeeds
func (f *FailingSubResourceWriter) Update(
	ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption,
) error {
	f.parent.mu.Lock()
	defer f.parent.mu.Unlock()

	f.parent.AttemptCount++
	if f.parent.FailureCount < f.parent.MaxFailures {
		f.parent.FailureCount++
		return errors.New("simulated status update failure")
	}

	// After max failures, delegate to real client
	return f.SubResourceWriter.Update(ctx, obj, opts...)
}

// GetAttemptCount returns the current attempt count (thread-safe)
func (f *FailingClient) GetAttemptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.AttemptCount
}

// GetFailureCount returns the current failure count (thread-safe)
func (f *FailingClient) GetFailureCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.FailureCount
}

// CreateFailingClientWithObjects creates a FailingClient with the given objects
// that fails the first maxFailures attempts
func CreateFailingClientWithObjects(maxFailures int, objects ...client.Object) *FailingClient {
	fakeClient := SetupFakeClientWithObjects(objects...)
	return &FailingClient{
		Client:       fakeClient,
		MaxFailures:  maxFailures,
		FailureCount: 0,
		AttemptCount: 0,
	}
}
