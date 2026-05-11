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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TracingConfig defines the OpenTelemetry tracing configuration to inject
// into validation agent containers.
type TracingConfig struct {
	// Enabled controls whether tracing env vars are injected into matched containers.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Endpoint is the OTLP collector endpoint (sets OTEL_EXPORTER_OTLP_ENDPOINT).
	// For example, "http://otel-collector.monitoring:4317".
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Insecure controls whether the gRPC connection to the collector uses TLS.
	// Defaults to true, which is typical for in-cluster collectors.
	// Sets OTEL_EXPORTER_OTLP_INSECURE.
	// +kubebuilder:default=true
	// +kubebuilder:validation:Optional
	Insecure *bool `json:"insecure,omitempty"`

	// Sampler specifies the trace sampler to use (sets OTEL_TRACES_SAMPLER).
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=always_on;always_off;traceidratio;parentbased_always_on;parentbased_always_off;parentbased_traceidratio
	Sampler string `json:"sampler,omitempty"`

	// SamplerArg is the argument for the sampler (sets OTEL_TRACES_SAMPLER_ARG).
	// For example, "0.1" for a 10% sampling ratio with traceidratio.
	// +kubebuilder:validation:Optional
	SamplerArg string `json:"samplerArg,omitempty"`

	// Env specifies additional environment variables to inject.
	// This allows setting any OTEL_* variable without requiring CRD changes.
	// +kubebuilder:validation:Optional
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// TelemetryConfigSpec defines the desired state of TelemetryConfig.
type TelemetryConfigSpec struct {
	// NamespaceSelector selects namespaces this config applies to.
	// An empty or nil selector matches all namespaces.
	// +kubebuilder:validation:Optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// Selector selects which ModelValidation CRs this config applies to.
	// An empty or nil selector matches all ModelValidation CRs.
	// +kubebuilder:validation:Optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Tracing defines the OpenTelemetry tracing configuration.
	Tracing TracingConfig `json:"tracing"`
}

// TelemetryConfigStatus defines the observed state of TelemetryConfig.
type TelemetryConfigStatus struct {
	// MatchedNamespaceCount is the number of namespaces matched by namespaceSelector.
	MatchedNamespaceCount int32 `json:"matchedNamespaceCount"`

	// MatchedModelValidationCount is the number of ModelValidation CRs matched by selector.
	MatchedModelValidationCount int32 `json:"matchedModelValidationCount"`

	// LastApplied is the timestamp when this config was last evaluated.
	LastApplied metav1.Time `json:"lastApplied,omitempty"`

	// Conditions represent the latest available observations of the TelemetryConfig's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TelemetryConfig is the Schema for the telemetryconfigs API.
// It defines OpenTelemetry configuration that the operator injects into
// validation agent containers via environment variables.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=tc
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.spec.tracing.endpoint`
// +kubebuilder:printcolumn:name="Sampler",type=string,JSONPath=`.spec.tracing.sampler`
// +kubebuilder:printcolumn:name="Matched NS",type=integer,JSONPath=`.status.matchedNamespaceCount`
// +kubebuilder:printcolumn:name="Matched MV",type=integer,JSONPath=`.status.matchedModelValidationCount`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type TelemetryConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TelemetryConfigSpec   `json:"spec,omitempty"`
	Status TelemetryConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TelemetryConfigList contains a list of TelemetryConfig.
type TelemetryConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TelemetryConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TelemetryConfig{}, &TelemetryConfigList{})
}
