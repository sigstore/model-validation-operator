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

package webhooks

import (
	"fmt"
	"strconv"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// NativeSidecarSupport checks whether the Kubernetes cluster supports native
// sidecar containers (init containers with restartPolicy: Always).
// Native sidecars require Kubernetes 1.28+ (with SidecarContainers feature gate)
// or 1.29+ (where the feature gate is enabled by default).
func NativeSidecarSupport(cfg *rest.Config) (bool, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return false, fmt.Errorf("failed to create discovery client: %w", err)
	}

	serverVersion, err := dc.ServerVersion()
	if err != nil {
		return false, fmt.Errorf("failed to get server version: %w", err)
	}

	major, err := strconv.Atoi(serverVersion.Major)
	if err != nil {
		return false, fmt.Errorf("failed to parse major version %q: %w", serverVersion.Major, err)
	}

	// Minor version may contain "+" suffix (e.g. "28+")
	minorStr := serverVersion.Minor
	for i, c := range minorStr {
		if c < '0' || c > '9' {
			minorStr = minorStr[:i]
			break
		}
	}
	minor, err := strconv.Atoi(minorStr)
	if err != nil {
		return false, fmt.Errorf("failed to parse minor version %q: %w", serverVersion.Minor, err)
	}

	// Native sidecars available in 1.28+ (feature gate), enabled by default in 1.29+
	return major > 1 || (major == 1 && minor >= 28), nil
}
