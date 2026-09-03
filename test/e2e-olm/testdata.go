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

// Package e2eolm contains e2e tests for OLM upgrade lifecycle.
package e2eolm

import _ "embed"

// OLM resource templates

//go:embed testdata/catalogsource_template.yaml
var catalogSourceTemplate []byte

//go:embed testdata/operatorgroup_template.yaml
var operatorGroupTemplate []byte

//go:embed testdata/subscription_template.yaml
var subscriptionTemplate []byte

// Shared resource templates (copied from test/e2e/testdata)

//go:embed testdata/modelvalidation_template.yaml
var modelValidationTemplate []byte

//go:embed testdata/pod_template.yaml
var podTemplate []byte
