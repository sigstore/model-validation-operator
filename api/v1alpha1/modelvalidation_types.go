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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Model defines the details of the model to validate.
type Model struct {
	Path          string `json:"path"`
	SignaturePath string `json:"signaturePath"`
}

// SigstoreConfig defines the Sigstore verification configuration
// for validating model signatures using certificate identity and OIDC issuer
type SigstoreConfig struct {
	CertificateIdentity   string `json:"certificateIdentity,omitempty"`
	CertificateOidcIssuer string `json:"certificateOidcIssuer,omitempty"`
}

// PkiConfig defines the PKI-based verification configuration
// using a certificate authority for validating model signatures
type PkiConfig struct {
	// Path to the certificate authority for PKI.
	CertificateAuthority string `json:"certificateAuthority,omitempty"`
}

// PrivateKeyConfig defines the private key verification configuration
// for validating model signatures using a local private key
type PrivateKeyConfig struct {
	// Path to the private key.
	KeyPath string `json:"keyPath,omitempty"`
}

// ValidationConfig defines the various methods available for validating model signatures.
// At least one validation method must be specified.
type ValidationConfig struct {
	SigstoreConfig   *SigstoreConfig   `json:"sigstoreConfig,omitempty"`
	PkiConfig        *PkiConfig        `json:"pkiConfig,omitempty"`
	PrivateKeyConfig *PrivateKeyConfig `json:"privateKeyConfig,omitempty"`
}

// ModelValidationSpec defines the desired state of ModelValidation
type ModelValidationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Model details.
	Model Model `json:"model"`
	// Configuration for validation methods.
	Config ValidationConfig `json:"config"`
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
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Auth Method",type=string,JSONPath=`.status.authMethod`
// +kubebuilder:printcolumn:name="Injected Pods",type=integer,JSONPath=`.status.injectedPodCount`
// +kubebuilder:printcolumn:name="Uninjected Pods",type=integer,JSONPath=`.status.uninjectedPodCount`
// +kubebuilder:printcolumn:name="Orphaned Pods",type=integer,JSONPath=`.status.orphanedPodCount`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ModelValidation is the Schema for the modelvalidations API
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
	} else if mv.Spec.Config.PrivateKeyConfig != nil {
		return "private-key"
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
	} else if vc.PrivateKeyConfig != nil {
		hasher.Write([]byte("privatekey"))
		hasher.Write([]byte(vc.PrivateKeyConfig.KeyPath))
	}

	return fmt.Sprintf("%x", hasher.Sum(nil))[:16] // Use first 16 chars for brevity
}

func init() {
	SchemeBuilder.Register(&ModelValidation{}, &ModelValidationList{})
}
