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
)

var (
	// ModelTransparencyCliImage is the default image for the model transparency CLI
	// used as an init container to validate model signatures
	ModelTransparencyCliImage = "ghcr.io/sigstore/model-transparency-cli:v1.0.1"
)
