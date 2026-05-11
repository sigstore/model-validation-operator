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
	"context"
	"fmt"
	"sort"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// findMatchingTelemetryConfig finds the best matching TelemetryConfig for a
// given namespace and ModelValidation. It lists all TelemetryConfigs across the
// cluster, filters by namespace and MV selectors, then picks the most specific
// match. Returns nil if no TelemetryConfig matches.
func findMatchingTelemetryConfig(
	ctx context.Context,
	c client.Client,
	namespace *corev1.Namespace,
	mv *v1alpha1.ModelValidation,
) (*v1alpha1.TelemetryConfig, error) {
	logger := log.FromContext(ctx)

	var tcList v1alpha1.TelemetryConfigList
	if err := c.List(ctx, &tcList); err != nil {
		return nil, fmt.Errorf("listing TelemetryConfigs: %w", err)
	}

	if len(tcList.Items) == 0 {
		return nil, nil
	}

	type candidate struct {
		tc          *v1alpha1.TelemetryConfig
		specificity int
	}

	var candidates []candidate
	for i := range tcList.Items {
		tc := &tcList.Items[i]

		if !tc.Spec.Tracing.Enabled {
			continue
		}

		// Check namespace selector
		if tc.Spec.NamespaceSelector != nil {
			sel, err := metav1.LabelSelectorAsSelector(tc.Spec.NamespaceSelector)
			if err != nil {
				logger.Error(err, "invalid namespaceSelector", "telemetryconfig", tc.Name, "namespace", tc.Namespace)
				continue
			}
			if !sel.Matches(labels.Set(namespace.Labels)) {
				continue
			}
		}

		// Check ModelValidation selector
		if tc.Spec.Selector != nil {
			sel, err := metav1.LabelSelectorAsSelector(tc.Spec.Selector)
			if err != nil {
				logger.Error(err, "invalid selector", "telemetryconfig", tc.Name, "namespace", tc.Namespace)
				continue
			}
			if !sel.Matches(labels.Set(mv.Labels)) {
				continue
			}
		}

		// Calculate specificity: count of selector terms set
		spec := selectorSpecificity(tc.Spec.NamespaceSelector) + selectorSpecificity(tc.Spec.Selector)
		candidates = append(candidates, candidate{tc: tc, specificity: spec})
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Sort: most specific first, then same-namespace preferred, then alphabetical
	targetNS := namespace.Name
	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]
		if ci.specificity != cj.specificity {
			return ci.specificity > cj.specificity
		}
		iSameNS := ci.tc.Namespace == targetNS
		jSameNS := cj.tc.Namespace == targetNS
		if iSameNS != jSameNS {
			return iSameNS
		}
		iKey := ci.tc.Namespace + "/" + ci.tc.Name
		jKey := cj.tc.Namespace + "/" + cj.tc.Name
		return iKey < jKey
	})

	winner := candidates[0].tc
	logger.Info("Matched TelemetryConfig",
		"telemetryconfig", winner.Name,
		"namespace", winner.Namespace,
		"specificity", candidates[0].specificity,
	)
	return winner, nil
}

// selectorSpecificity returns the number of match terms in a label selector.
// A nil or empty selector has specificity 0 (matches everything).
func selectorSpecificity(sel *metav1.LabelSelector) int {
	if sel == nil {
		return 0
	}
	return len(sel.MatchLabels) + len(sel.MatchExpressions)
}

// telemetryEnvVars builds the list of environment variables from a TelemetryConfig.
func telemetryEnvVars(tc *v1alpha1.TelemetryConfig) []corev1.EnvVar {
	if tc == nil || !tc.Spec.Tracing.Enabled {
		return nil
	}

	tracing := tc.Spec.Tracing
	envs := []corev1.EnvVar{
		{Name: "OTEL_SERVICE_NAME", Value: "validation-agent"},
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: tracing.Endpoint},
	}

	if tracing.Insecure != nil {
		envs = append(envs, corev1.EnvVar{
			Name:  "OTEL_EXPORTER_OTLP_INSECURE",
			Value: fmt.Sprintf("%t", *tracing.Insecure),
		})
	}

	if tracing.Sampler != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "OTEL_TRACES_SAMPLER",
			Value: tracing.Sampler,
		})
	}

	if tracing.SamplerArg != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "OTEL_TRACES_SAMPLER_ARG",
			Value: tracing.SamplerArg,
		})
	}

	envs = append(envs, tracing.Env...)

	return envs
}
