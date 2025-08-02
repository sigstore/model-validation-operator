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

package constants

const (
	// ModelValidationDomain is the domain used for model validation labels
	ModelValidationDomain = "validation.ml.sigstore.dev"

	// ModelValidationLabel is the label used to enable model validation for a pod
	ModelValidationLabel = ModelValidationDomain + "/ml"

	// IgnoreNamespaceLabel is the label used to ignore a namespace for model validation
	IgnoreNamespaceLabel = ModelValidationDomain + "/ignore"

	// ModelValidationFinalizer is the finalizer used to track model validation pods
	ModelValidationFinalizer = ModelValidationDomain + "/finalizer"

	// InjectedAnnotationKey is the annotation key used to track injected pods
	InjectedAnnotationKey = ModelValidationDomain + "/injected-at"

	// AuthMethodAnnotationKey is the annotation key used to track the auth method used during injection
	AuthMethodAnnotationKey = ModelValidationDomain + "/auth-method"

	// ConfigHashAnnotationKey is the annotation key used to track the configuration hash during injection
	ConfigHashAnnotationKey = ModelValidationDomain + "/config-hash"

	// IgnoreNamespaceValue is the value for the ignore namespace label
	IgnoreNamespaceValue = "true"
)
