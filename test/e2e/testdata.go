/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import _ "embed"

// ModelValidation CR template used by multiple tests
//
//go:embed testdata/modelvalidation_template.yaml
var modelValidationTemplate []byte

// Pod template
//
//go:embed testdata/pod_template.yaml
var podTemplate []byte

//go:embed testdata/curl_metrics_pod_template.yaml
var curlPodTemplate []byte

//go:embed testdata/clusterrolebinding_template.yaml
var clusterRoleBindingTemplate []byte
