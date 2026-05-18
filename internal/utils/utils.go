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

// Package utils provides utility functions for the model validation operator
package utils //nolint:revive

import (
	"flag"
	"os"
)

// StringFlagOrEnv defines a string flag which can be set by an environment variable.
// Precedence: flag > env var > default value.
func StringFlagOrEnv(p *string, name string, envName string, defaultValue string, usage string) {
	envValue := os.Getenv(envName)
	if envValue != "" {
		defaultValue = envValue
	}
	flag.StringVar(p, name, defaultValue, usage)
}

// BoolFlagOrEnv defines a bool flag which can be set by an environment variable.
// Precedence: flag > env var > default value.
// The env var is considered true if set to "true", "1", or "yes" (case-insensitive).
func BoolFlagOrEnv(p *bool, name string, envName string, defaultValue bool, usage string) {
	envValue := os.Getenv(envName)
	if envValue == "true" || envValue == "1" || envValue == "yes" {
		defaultValue = true
	}
	flag.BoolVar(p, name, defaultValue, usage)
}
