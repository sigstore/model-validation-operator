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

package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

// SetupTracing initializes the OpenTelemetry trace SDK and registers it
// globally. The returned shutdown function flushes pending spans and releases
// resources; callers must invoke it before process exit.
func SetupTracing(ctx context.Context, opts ...Option) (shutdown func(context.Context) error, err error) {
	cfg := defaultOptions()
	for _, o := range opts {
		o(cfg)
	}

	res, err := buildResource(cfg)
	if err != nil {
		return nil, fmt.Errorf("building otel resource: %w", err)
	}

	exporter, err := buildExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating otel exporter: %w", err)
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	}
	if cfg.sampler != nil {
		tpOpts = append(tpOpts, sdktrace.WithSampler(cfg.sampler))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func buildResource(cfg *options) (*resource.Resource, error) {
	attrs := []resource.Option{
		resource.WithAttributes(semconv.ServiceName(cfg.serviceName)),
	}
	if cfg.serviceVersion != "" {
		attrs = append(attrs, resource.WithAttributes(semconv.ServiceVersion(cfg.serviceVersion)))
	}
	return resource.New(context.Background(), attrs...)
}

func buildExporter(ctx context.Context, cfg *options) (sdktrace.SpanExporter, error) {
	if cfg.useStdout {
		return stdouttrace.New()
	}

	grpcOpts := []otlptracegrpc.Option{}
	if cfg.endpoint != "" {
		grpcOpts = append(grpcOpts, otlptracegrpc.WithEndpoint(cfg.endpoint))
	}
	if cfg.insecure {
		grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, grpcOpts...)
}

// Tracer returns a named tracer from the global TracerProvider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
