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
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const meterName = "model-validation-operator"

var (
	webhookMutations  metric.Int64Counter
	webhookDuration   metric.Float64Histogram
	reconcileCount    metric.Int64Counter
	reconcileDuration metric.Float64Histogram
)

// SetupOTelMetrics initialises the OTel SDK with a Prometheus exporter
// registered against controller-runtime's metrics.Registry so that OTel
// instruments appear on the existing /metrics endpoint.
func SetupOTelMetrics() (shutdown func(context.Context) error, err error) {
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(crmetrics.Registry),
	)
	if err != nil {
		return nil, err
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
	)
	otel.SetMeterProvider(provider)

	meter := provider.Meter(meterName)

	webhookMutations, err = meter.Int64Counter("webhook.mutations",
		metric.WithDescription("Total webhook mutation requests"),
	)
	if err != nil {
		return nil, err
	}

	webhookDuration, err = meter.Float64Histogram("webhook.mutation.duration",
		metric.WithDescription("Duration of webhook mutation handling"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	reconcileCount, err = meter.Int64Counter("reconcile.count",
		metric.WithDescription("Total controller reconcile operations"),
	)
	if err != nil {
		return nil, err
	}

	reconcileDuration, err = meter.Float64Histogram("reconcile.duration",
		metric.WithDescription("Duration of controller reconcile operations"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	return provider.Shutdown, nil
}

// RecordWebhookMutation records a webhook mutation event with its outcome and duration.
func RecordWebhookMutation(ctx context.Context, namespace, result string, duration time.Duration) {
	if webhookMutations == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("namespace", namespace),
		attribute.String("result", result),
	)
	webhookMutations.Add(ctx, 1, attrs)
	webhookDuration.Record(ctx, duration.Seconds(), attrs)
}

// RecordReconcile records a controller reconcile event with its outcome and duration.
func RecordReconcile(ctx context.Context, controller, result string, duration time.Duration) {
	if reconcileCount == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("controller", controller),
		attribute.String("result", result),
	)
	reconcileCount.Add(ctx, 1, attrs)
	reconcileDuration.Record(ctx, duration.Seconds(), attrs)
}
