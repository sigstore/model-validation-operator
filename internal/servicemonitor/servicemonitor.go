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

// Package servicemonitor handles automatic Prometheus ServiceMonitor creation.
package servicemonitor

import (
	"context"
	"fmt"
	"os"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;delete

// Options configures the ServiceMonitor to be created.
type Options struct {
	Namespace   string
	ServiceName string
	PortName    string
	Path        string
	Secure      bool
}

// Creator implements manager.Runnable and creates a Prometheus ServiceMonitor
// for the operator's metrics endpoint on startup.
type Creator struct {
	client  client.Client
	config  *rest.Config
	options Options
}

// NewCreator returns a new ServiceMonitor Creator.
func NewCreator(c client.Client, cfg *rest.Config, opts Options) *Creator {
	if opts.Namespace == "" {
		opts.Namespace = detectNamespace()
	}
	if opts.Path == "" {
		opts.Path = "/metrics"
	}
	if opts.PortName == "" {
		opts.PortName = "https"
	}
	return &Creator{
		client:  c,
		config:  cfg,
		options: opts,
	}
}

// Start creates the ServiceMonitor and blocks until the context is cancelled.
func (c *Creator) Start(ctx context.Context) error {
	log := ctrl.Log.WithName("servicemonitor")

	available, err := c.isServiceMonitorCRDAvailable()
	if err != nil {
		log.Error(err, "Failed to check for ServiceMonitor CRD availability")
		return nil
	}
	if !available {
		log.Info("ServiceMonitor CRD not found on cluster, skipping auto-creation")
		return nil
	}

	sm := c.buildServiceMonitor()
	result, err := controllerutil.CreateOrUpdate(ctx, c.client, sm, func() error {
		c.applySpec(sm)
		return nil
	})
	if err != nil {
		log.Error(err, "Failed to create or update ServiceMonitor")
		return nil
	}

	log.Info("ServiceMonitor reconciled", "result", result, "name", sm.Name, "namespace", sm.Namespace)

	<-ctx.Done()
	return nil
}

func (c *Creator) isServiceMonitorCRDAvailable() (bool, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(c.config)
	if err != nil {
		return false, fmt.Errorf("creating discovery client: %w", err)
	}

	resources, err := dc.ServerResourcesForGroupVersion("monitoring.coreos.com/v1")
	if err != nil {
		return false, nil //nolint:nilerr // API group not found means CRD not installed
	}

	for _, r := range resources.APIResources {
		if r.Kind == "ServiceMonitor" {
			return true, nil
		}
	}
	return false, nil
}

func (c *Creator) buildServiceMonitor() *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller-manager-metrics-monitor",
			Namespace: c.options.Namespace,
			Labels: map[string]string{
				"control-plane":                "controller-manager",
				"app.kubernetes.io/name":       "model-validation-operator",
				"app.kubernetes.io/managed-by": "operator",
			},
		},
	}
}

func (c *Creator) applySpec(sm *monitoringv1.ServiceMonitor) {
	scheme := "http"
	if c.options.Secure {
		scheme = "https"
	}

	sm.Labels = map[string]string{
		"control-plane":                "controller-manager",
		"app.kubernetes.io/name":       "model-validation-operator",
		"app.kubernetes.io/managed-by": "operator",
	}

	sm.Spec = monitoringv1.ServiceMonitorSpec{
		Endpoints: []monitoringv1.Endpoint{
			{
				Path:            c.options.Path,
				Port:            c.options.PortName,
				Scheme:          scheme,
				BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token", //nolint:staticcheck
				TLSConfig: &monitoringv1.TLSConfig{
					SafeTLSConfig: monitoringv1.SafeTLSConfig{
						InsecureSkipVerify: ptr.To(true),
					},
				},
			},
		},
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "model-validation-operator",
			},
		},
	}
}

func detectNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		return string(data)
	}
	return "model-validation-operator-system"
}
