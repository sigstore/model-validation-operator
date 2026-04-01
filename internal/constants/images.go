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

// Package constants provides shared constants used throughout the model validation operator
package constants

const (
	// ModelValidationInitContainerName is the name of the init container injected for model validation
	ModelValidationInitContainerName = "model-validation"

	// ModelValidationSidecarContainerName is the name of the regular sidecar container
	// used for continuous validation on clusters that don't support native sidecars (pre-1.28)
	ModelValidationSidecarContainerName = "model-validation-sidecar"
)

var (
	// ModelValidationAgentImage is the image for the validation agent.
	// It contains the validation-agent binary which natively validates models
	// using the model-transparency-go library. Used for both one-shot and
	// continuous validation modes.
	// This can be overridden at build time via ldflags:
	//   go build -ldflags="-X github.com/sigstore/model-validation-operator/internal/constants.ModelValidationAgentImage=myimage:tag"
	ModelValidationAgentImage = "ghcr.io/sigstore/model-validation-agent:v0.1.0"
)
