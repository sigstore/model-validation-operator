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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Option configures the tracing setup.
type Option func(*options)

type options struct {
	serviceName    string
	serviceVersion string
	endpoint       string
	insecure       bool
	useStdout      bool
	sampler        sdktrace.Sampler
}

func defaultOptions() *options {
	return &options{
		serviceName: "unknown-service",
		insecure:    true,
	}
}

// WithServiceName sets the service.name resource attribute.
func WithServiceName(name string) Option {
	return func(o *options) {
		o.serviceName = name
	}
}

// WithServiceVersion sets the service.version resource attribute.
func WithServiceVersion(version string) Option {
	return func(o *options) {
		o.serviceVersion = version
	}
}

// WithEndpoint sets the OTLP gRPC collector endpoint.
// If empty, the SDK reads OTEL_EXPORTER_OTLP_ENDPOINT (default: localhost:4317).
func WithEndpoint(endpoint string) Option {
	return func(o *options) {
		o.endpoint = endpoint
	}
}

// WithInsecure controls whether the gRPC connection uses TLS.
// Defaults to true (no TLS), which is typical for in-cluster collectors.
func WithInsecure(insecure bool) Option {
	return func(o *options) {
		o.insecure = insecure
	}
}

// WithStdoutExporter uses a stdout exporter instead of OTLP gRPC.
// Useful for local development and debugging.
func WithStdoutExporter() Option {
	return func(o *options) {
		o.useStdout = true
	}
}

// WithSampler overrides the default sampler (ParentBased(AlwaysSample)).
func WithSampler(s sdktrace.Sampler) Option {
	return func(o *options) {
		o.sampler = s
	}
}
