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

package servicemonitor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCreator_Defaults(t *testing.T) {
	creator := NewCreator(nil, nil, Options{})
	assert.Equal(t, "/metrics", creator.options.Path)
	assert.Equal(t, "https", creator.options.PortName)
	assert.NotEmpty(t, creator.options.Namespace)
}

func TestNewCreator_CustomOptions(t *testing.T) {
	opts := Options{
		Namespace: "custom-ns",
		PortName:  "http",
		Path:      "/custom-metrics",
		Secure:    true,
	}
	creator := NewCreator(nil, nil, opts)
	assert.Equal(t, "custom-ns", creator.options.Namespace)
	assert.Equal(t, "http", creator.options.PortName)
	assert.Equal(t, "/custom-metrics", creator.options.Path)
	assert.True(t, creator.options.Secure)
}

func TestBuildServiceMonitor(t *testing.T) {
	creator := NewCreator(nil, nil, Options{
		Namespace: "test-ns",
		PortName:  "https",
		Path:      "/metrics",
	})

	sm := creator.buildServiceMonitor()
	require.NotNil(t, sm)

	assert.Equal(t, "controller-manager-metrics-monitor", sm.Name)
	assert.Equal(t, "test-ns", sm.Namespace)
	assert.Equal(t, "controller-manager", sm.Labels["control-plane"])
	assert.Equal(t, "model-validation-operator", sm.Labels["app.kubernetes.io/name"])
	assert.Equal(t, "operator", sm.Labels["app.kubernetes.io/managed-by"])
}

func TestApplySpec_Secure(t *testing.T) {
	creator := NewCreator(nil, nil, Options{
		Namespace: "test-ns",
		PortName:  "https",
		Path:      "/metrics",
		Secure:    true,
	})

	sm := creator.buildServiceMonitor()
	creator.applySpec(sm)

	require.Len(t, sm.Spec.Endpoints, 1)
	ep := sm.Spec.Endpoints[0]
	assert.Equal(t, "https", ep.Scheme)
	assert.Equal(t, "https", ep.Port)
	assert.Equal(t, "/metrics", ep.Path)
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/token", ep.BearerTokenFile) //nolint:staticcheck
	require.NotNil(t, ep.TLSConfig)
	require.NotNil(t, ep.TLSConfig.InsecureSkipVerify)
	assert.True(t, *ep.TLSConfig.InsecureSkipVerify)

	assert.Equal(t, "controller-manager", sm.Spec.Selector.MatchLabels["control-plane"])
	assert.Equal(t, "model-validation-operator", sm.Spec.Selector.MatchLabels["app.kubernetes.io/name"])
}

func TestApplySpec_Insecure(t *testing.T) {
	creator := NewCreator(nil, nil, Options{
		Namespace: "test-ns",
		PortName:  "http",
		Path:      "/metrics",
		Secure:    false,
	})

	sm := creator.buildServiceMonitor()
	creator.applySpec(sm)

	require.Len(t, sm.Spec.Endpoints, 1)
	assert.Equal(t, "http", sm.Spec.Endpoints[0].Scheme)
}

func TestDetectNamespace_Default(t *testing.T) {
	ns := detectNamespace()
	assert.NotEmpty(t, ns)
}
