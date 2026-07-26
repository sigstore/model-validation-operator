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

package metrics

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var testShutdown func(context.Context) error

func ensureOTelSetup(t *testing.T) {
	t.Helper()
	if webhookMutations != nil {
		return
	}
	var err error
	testShutdown, err = SetupOTelMetrics()
	require.NoError(t, err)
	require.NotNil(t, testShutdown)
}

func TestSetupOTelMetrics(t *testing.T) {
	ensureOTelSetup(t)

	assert.NotNil(t, webhookMutations)
	assert.NotNil(t, webhookDuration)
	assert.NotNil(t, reconcileCount)
	assert.NotNil(t, reconcileDuration)
}

func TestRecordWebhookMutation(t *testing.T) {
	ensureOTelSetup(t)

	ctx := context.Background()
	RecordWebhookMutation(ctx, "default", "success", 50*time.Millisecond)
	RecordWebhookMutation(ctx, "default", "error", 100*time.Millisecond)

	families := gatherMetrics(t, crmetrics.Registry)

	counterFamily := findMetricFamily(t, families, "webhook.mutations")
	require.NotNil(t, counterFamily, "expected a metric family containing 'webhook.mutations'")
	assert.NotEmpty(t, counterFamily.GetMetric())

	histFamily := findMetricFamily(t, families, "webhook.mutation.duration")
	require.NotNil(t, histFamily, "expected a metric family containing 'webhook.mutation.duration'")
	assert.NotEmpty(t, histFamily.GetMetric())
}

func TestRecordReconcile(t *testing.T) {
	ensureOTelSetup(t)

	ctx := context.Background()
	RecordReconcile(ctx, "pod", "success", 10*time.Millisecond)
	RecordReconcile(ctx, "modelvalidation", "error", 20*time.Millisecond)

	families := gatherMetrics(t, crmetrics.Registry)

	counterFamily := findMetricFamily(t, families, "reconcile.count")
	require.NotNil(t, counterFamily, "expected a metric family containing 'reconcile.count'")
	assert.NotEmpty(t, counterFamily.GetMetric())

	histFamily := findMetricFamily(t, families, "reconcile.duration")
	require.NotNil(t, histFamily, "expected a metric family containing 'reconcile.duration'")
	assert.NotEmpty(t, histFamily.GetMetric())
}

func TestRecordWithoutSetup(t *testing.T) {
	saved := webhookMutations
	webhookMutations = nil
	savedRc := reconcileCount
	reconcileCount = nil
	defer func() {
		webhookMutations = saved
		reconcileCount = savedRc
	}()

	ctx := context.Background()
	assert.NotPanics(t, func() {
		RecordWebhookMutation(ctx, "default", "success", time.Millisecond)
	})
	assert.NotPanics(t, func() {
		RecordReconcile(ctx, "pod", "success", time.Millisecond)
	})
}

func gatherMetrics(t *testing.T, gatherer prometheus.Gatherer) []*io_prometheus_client.MetricFamily {
	t.Helper()
	families, err := gatherer.Gather()
	require.NoError(t, err)
	return families
}

func findMetricFamily(
	t *testing.T, families []*io_prometheus_client.MetricFamily, substring string,
) *io_prometheus_client.MetricFamily {
	t.Helper()
	for _, f := range families {
		if strings.Contains(f.GetName(), substring) {
			return f
		}
	}
	return nil
}
