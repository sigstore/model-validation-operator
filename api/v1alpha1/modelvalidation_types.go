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

package v1alpha1

import (
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Model defines the details of the model to validate, including its path and
// the path to its corresponding signature file.
type Model struct {
	// Path to the model artifact. This could be a file path on a shared volume
	// or a URI to an object store.
	// +kubebuilder:validation:Pattern=`^(/|s3://|gs://|https?://)`
	Path string `json:"path"`
	// SignaturePath is the path to the cryptographic signature bundle for the model.
	// This is used by the various validation methods to verify the model's integrity.
	// +kubebuilder:validation:Pattern=`^(/|s3://|gs://|https?://)`
	SignaturePath string `json:"signaturePath"`
	// IgnorePaths is a list of file paths to ignore when verifying the model.
	// These paths will be excluded from the verification process.
	// +kubebuilder:validation:Optional
	IgnorePaths []string `json:"ignorePaths,omitempty"`
	// IgnoreGitPaths specifies whether to ignore git-related files during verification.
	// When set to true, git files (e.g., .git/, .gitignore) will be excluded.
	// +kubebuilder:validation:Optional
	IgnoreGitPaths *bool `json:"ignoreGitPaths,omitempty"`
	// IgnoreUnsignedFiles specifies whether to ignore files that were not originally signed.
	// When set to true, unsigned files will not cause verification to fail.
	// +kubebuilder:validation:Optional
	IgnoreUnsignedFiles *bool `json:"ignoreUnsignedFiles,omitempty"`
	// AllowSymlinks specifies whether to follow symbolic links during verification.
	// When set to true, symlinks will be followed and their targets verified.
	// +kubebuilder:validation:Optional
	AllowSymlinks *bool `json:"allowSymlinks,omitempty"`
}

// SigstoreConfig defines the configuration for validating model signatures using
// Sigstore's certificate-based method, which requires a specific certificate
// identity and OIDC issuer.
type SigstoreConfig struct {
	// CertificateIdentity is the expected identity of the signing certificate.
	// For example, "email:jane.doe@example.com".
	// +kubebuilder:validation:Required
	CertificateIdentity string `json:"certificateIdentity,omitempty"`
	// CertificateOidcIssuer is the URL of the OIDC issuer that issued the signing certificate.
	// For example, "https://accounts.google.com".
	// +kubebuilder:validation:Required
	CertificateOidcIssuer string `json:"certificateOidcIssuer,omitempty"`
}

// PkiConfig defines the configuration for PKI-based verification.
// This method validates the signature using a trusted certificate authority (CA).
type PkiConfig struct {
	// CertificateAuthorityPath is the path to the trusted certificate authority (CA) file.
	// The signature's chain of trust will be verified against this CA.
	// +kubebuilder:validation:Required
	CertificateAuthority string `json:"certificateAuthority,omitempty"`
}

// PublicKeyConfig defines the configuration for public key-based verification.
// This method validates the signature directly against a public key.
type PublicKeyConfig struct {
	// KeyPath is the file path to the public key used for signature verification.
	// This should be the public key corresponding to the private key used for signing.
	// +kubebuilder:validation:Required
	KeyPath string `json:"keyPath,omitempty"`
}

// ClientTrustConfig defines the configuration for client trust settings,
// used when working with private rekor/fulcio instances.
type ClientTrustConfig struct {
	// TrustConfigPath is the path to the trust configuration file.
	// This specifies the trust configuration needed for using private rekor/fulcio instances
	// and should conform to the ClientTrustConfig message.
	// +kubebuilder:validation:Required
	TrustConfigPath string `json:"trustConfigPath,omitempty"`
}

// ValidationConfig defines the various methods available for validating model signatures.
// Only one validation method should be specified. The controller will use the first
// non-nil method it finds.
// +kubebuilder:validation:XValidation:rule="[has(self.sigstoreConfig), has(self.pkiConfig), has(self.publicKeyConfig)].filter(x, x).size() == 1", message="exactly one validation method must be specified"
type ValidationConfig struct {
	// SigstoreConfig is the configuration for Sigstore-based signature verification.
	// +kubebuilder:validation:Optional
	SigstoreConfig *SigstoreConfig `json:"sigstoreConfig,omitempty"`
	// +kubebuilder:validation:Optional
	// PkiConfig is the configuration for traditional PKI-based signature verification.
	PkiConfig *PkiConfig `json:"pkiConfig,omitempty"`
	// +kubebuilder:validation:Optional
	// PublicKeyConfig is the configuration for public key-based signature verification.
	PublicKeyConfig *PublicKeyConfig `json:"publicKeyConfig,omitempty"`
	// +kubebuilder:validation:Optional
	// ClientTrustConfig is the configuration for client trust settings.
	ClientTrustConfig *ClientTrustConfig `json:"clientTrustConfig,omitempty"`
}

// ModelValidationSpec defines the desired state of ModelValidation
type ModelValidationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Model details.
	Model Model `json:"model"`
	// Configuration for validation methods.
	// Exactly one validation method must be specified.
	Config ValidationConfig `json:"config"`
	// ImagePullPolicy defines the pull policy for the init container.
	// Defaults to Always if not specified.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=Always
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
}

// PodTrackingInfo contains information about a tracked pod
type PodTrackingInfo struct {
	// Name is the name of the pod
	Name string `json:"name"`
	// UID is the unique identifier of the pod
	UID string `json:"uid"`
	// InjectedAt is when the pod was injected
	InjectedAt metav1.Time `json:"injectedAt"`
}

// ModelValidationStatus defines the observed state of ModelValidation
type ModelValidationStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// InjectedPodCount is the number of pods that have been injected with validation
	InjectedPodCount int32 `json:"injectedPodCount"`

	// UninjectedPodCount is the number of pods that have the label but were not injected
	UninjectedPodCount int32 `json:"uninjectedPodCount"`

	// OrphanedPodCount is the number of injected pods that reference this CR but are inconsistent
	OrphanedPodCount int32 `json:"orphanedPodCount"`

	// AuthMethod indicates which authentication method is being used
	AuthMethod string `json:"authMethod,omitempty"`

	// InjectedPods contains detailed information about injected pods
	InjectedPods []PodTrackingInfo `json:"injectedPods,omitempty"`

	// UninjectedPods contains detailed information about pods that should have been injected but weren't
	UninjectedPods []PodTrackingInfo `json:"uninjectedPods,omitempty"`

	// OrphanedPods contains detailed information about pods that are injected but inconsistent
	OrphanedPods []PodTrackingInfo `json:"orphanedPods,omitempty"`

	// LastUpdated is the timestamp of the last status update
	LastUpdated metav1.Time `json:"lastUpdated,omitempty"`
}

// ModelValidation is the Schema for the modelvalidations API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mv
// +kubebuilder:printcolumn:name="Auth Method",type=string,JSONPath=`.status.authMethod`
// +kubebuilder:printcolumn:name="Injected Pods",type=integer,JSONPath=`.status.injectedPodCount`
// +kubebuilder:printcolumn:name="Uninjected Pods",type=integer,JSONPath=`.status.uninjectedPodCount`
// +kubebuilder:printcolumn:name="Orphaned Pods",type=integer,JSONPath=`.status.orphanedPodCount`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Model Path",type="string",JSONPath=".spec.model.path"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ModelValidation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelValidationSpec   `json:"spec,omitempty"`
	Status ModelValidationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModelValidationList contains a list of ModelValidation
type ModelValidationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelValidation `json:"items"`
}

// GetAuthMethod returns the authentication method being used
func (mv *ModelValidation) GetAuthMethod() string {
	if mv.Spec.Config.SigstoreConfig != nil {
		return "sigstore"
	} else if mv.Spec.Config.PkiConfig != nil {
		return "pki"
	} else if mv.Spec.Config.PublicKeyConfig != nil {
		return "public-key"
	}
	return "unknown"
}

// GetConfigHash returns a hash of the validation configuration for drift detection
func (mv *ModelValidation) GetConfigHash() string {
	return mv.Spec.Config.GetConfigHash()
}

// GetConfigHash returns a hash of the validation configuration for drift detection
func (vc *ValidationConfig) GetConfigHash() string {
	hasher := sha256.New()

	if vc.SigstoreConfig != nil {
		hasher.Write([]byte("sigstore"))
		hasher.Write([]byte(vc.SigstoreConfig.CertificateIdentity))
		hasher.Write([]byte(vc.SigstoreConfig.CertificateOidcIssuer))
	} else if vc.PkiConfig != nil {
		hasher.Write([]byte("pki"))
		hasher.Write([]byte(vc.PkiConfig.CertificateAuthority))
	} else if vc.PublicKeyConfig != nil {
		hasher.Write([]byte("publickey"))
		hasher.Write([]byte(vc.PublicKeyConfig.KeyPath))
	}

	if vc.ClientTrustConfig != nil {
		hasher.Write([]byte("clienttrust"))
		hasher.Write([]byte(vc.ClientTrustConfig.TrustConfigPath))
	}

	return fmt.Sprintf("%x", hasher.Sum(nil))[:16] // Use first 16 chars for brevity
}

func init() {
	SchemeBuilder.Register(&ModelValidation{}, &ModelValidationList{})
}
