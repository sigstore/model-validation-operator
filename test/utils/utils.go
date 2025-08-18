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

// Package utils_test provides test utilities for e2e testing
package utils_test //nolint:revive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	corev1 "k8s.io/api/core/v1"
)

const (
	// ServiceAccountName is the name of the service account used by the model validation operator
	ServiceAccountName = "model-validation-controller-manager"

	// MetricsServiceAccountName is the name of the service account used for metrics access in tests
	MetricsServiceAccountName = "e2e-metrics-reader"

	// MetricsServiceName is the name of the metrics service exposed by the model validation operator
	MetricsServiceName = "model-validation-controller-manager-metrics-service"
)

// PodTemplateData represents template data for e2e pod tests
type PodTemplateData struct {
	PodName      string
	Namespace    string
	ModelName    string
	TestBatch    string
	VolumeMounts []VolumeMount
	Volumes      []Volume
}

// VolumeMount represents a volume mount configuration
type VolumeMount struct {
	Name      string
	MountPath string
	ReadOnly  bool
}

// Volume represents a volume configuration
type Volume struct {
	Name     string
	HostPath *HostPathVolume
}

// HostPathVolume represents a host path volume configuration
type HostPathVolume struct {
	Path string
	Type string
}

// CRTemplateData represents template data for custom resource tests
type CRTemplateData struct {
	ModelName string
	Namespace string
	KeyPath   string
}

// CurlPodTemplateData represents template data for curl pod tests
type CurlPodTemplateData struct {
	PodName        string
	Namespace      string
	Token          string
	ServiceName    string
	ServiceAccount string
}

// ClusterRoleBindingTemplateData represents template data for cluster role binding tests
type ClusterRoleBindingTemplateData struct {
	Name               string
	ServiceAccountName string
	Namespace          string
	ClusterRoleName    string
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %s\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s failed with error: (%v) %s",
			command, err, string(output))
	}

	return string(output), nil
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")
	return wd, nil
}

// KubectlApply applies a YAML resource from embedded bytes data or template
// If templateData is nil, yamlData is used directly. If templateData is provided,
// yamlData is treated as a template.
func KubectlApply(yamlData []byte, templateData any) error {
	var finalData []byte
	var err error

	if templateData != nil {
		finalData, err = executeTemplate(yamlData, templateData)
		if err != nil {
			return err
		}
	} else {
		finalData = yamlData
	}

	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(finalData)
	_, err = Run(cmd)
	return err
}

// KubectlDeleteOptions contains options for kubectl delete operations
type KubectlDeleteOptions struct {
	Timeout        string // e.g. "30s", "5m"
	IgnoreNotFound bool   // Use --ignore-not-found flag
	TemplateData   any    // Optional template data for Go template processing
}

// KubectlDelete deletes a YAML resource from embedded bytes data or template
// with optional settings. If opts is nil, yamlData is used directly with default
// settings. If opts.TemplateData is provided, yamlData is treated as a template.
func KubectlDelete(yamlData []byte, opts *KubectlDeleteOptions) error {
	args := []string{"delete", "-f", "-"}
	var finalData []byte
	var err error

	if opts != nil {
		// Handle template processing if template data is provided
		if opts.TemplateData != nil {
			finalData, err = executeTemplate(yamlData, opts.TemplateData)
			if err != nil {
				return err
			}
		} else {
			finalData = yamlData
		}

		if opts.Timeout != "" {
			args = append(args, "--timeout="+opts.Timeout)
		}
		if opts.IgnoreNotFound {
			args = append(args, "--ignore-not-found=true")
		}
	} else {
		finalData = yamlData
	}

	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = bytes.NewReader(finalData)
	_, err = Run(cmd)
	return err
}

// KubectlGet retrieves a Kubernetes resource and returns the output
// If name is empty, retrieves all resources of the specified type
func KubectlGet(resource, name, namespace string, outputFormat string) (string, error) {
	args := []string{"get", resource}
	if name != "" {
		args = append(args, name)
	}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	if outputFormat != "" {
		args = append(args, "-o", outputFormat)
	}

	cmd := exec.Command("kubectl", args...)
	return Run(cmd)
}

// KubectlGetJSON retrieves a Kubernetes resource as JSON and unmarshals it
func KubectlGetJSON(resource, name, namespace string, result any) error {
	output, err := KubectlGet(resource, name, namespace, "json")
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(output), result)
}

// KubectlWait waits for a condition on a Kubernetes resource
func KubectlWait(resource, name, namespace, condition string, timeout string) error {
	args := []string{"wait", resource, name}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	args = append(args, "--for", condition, "--timeout", timeout)

	cmd := exec.Command("kubectl", args...)
	_, err := Run(cmd)
	return err
}

// executeTemplate processes a Go template with the given data and returns the result
func executeTemplate(templateData []byte, data any) ([]byte, error) {
	tmpl, err := template.New("kubectl").Parse(string(templateData))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.Bytes(), nil
}

// DefaultPodData creates a pod template with common defaults
func DefaultPodData(podName, namespace, modelName, testBatch string) PodTemplateData {
	modelDataVolume := Volume{
		Name: "model-data",
		HostPath: &HostPathVolume{
			Path: "/tmp/e2e-model-data",
			Type: "Directory",
		},
	}
	keysDataVolume := Volume{
		Name: "keys-data",
		HostPath: &HostPathVolume{
			Path: "/tmp/e2e-keys-data",
			Type: "Directory",
		},
	}
	modelDataMount := VolumeMount{
		Name:      "model-data",
		MountPath: "/data",
		ReadOnly:  true,
	}
	keysDataMount := VolumeMount{
		Name:      "keys-data",
		MountPath: "/keys",
		ReadOnly:  true,
	}

	data := PodTemplateData{
		PodName:      podName,
		Namespace:    namespace,
		ModelName:    modelName,
		TestBatch:    testBatch,
		VolumeMounts: []VolumeMount{modelDataMount, keysDataMount},
		Volumes:      []Volume{modelDataVolume, keysDataVolume},
	}

	return data
}

// GetMetricsOutput retrieves fresh metrics by executing curl in the persistent pod
func GetMetricsOutput(namespace, podName string) string {
	token, err := CreateServiceAccountToken(MetricsServiceAccountName, namespace)
	if err != nil {
		return ""
	}

	curlCommand := fmt.Sprintf(
		"curl -s -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics 2>/dev/null",
		token, MetricsServiceName, namespace)
	execCmd := exec.Command("kubectl", "exec", podName, "-n", namespace,
		"--", "/bin/sh", "-c", curlCommand)

	output, err := Run(execCmd)
	if err != nil {
		return ""
	}
	return output
}

// KubectlDescribe describes a Kubernetes resource
func KubectlDescribe(resource, name, namespace string) (string, error) {
	args := []string{"describe", resource, name}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	cmd := exec.Command("kubectl", args...)
	return Run(cmd)
}

// KubectlResourceExists checks if a Kubernetes resource exists
func KubectlResourceExists(resource, name, namespace string) bool {
	_, err := KubectlGet(resource, name, namespace, "")
	return err == nil
}

// HasValidationContainer checks if a pod has the model-validation init container
func HasValidationContainer(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.InitContainers {
		if strings.Contains(container.Name, "model-validation") {
			return true
		}
	}
	return false
}

// CreateServiceAccountToken creates a token for the specified service account in the given namespace
func CreateServiceAccountToken(serviceAccountName, namespace string) (string, error) {
	tokenCmd := exec.Command("kubectl", "create", "token", serviceAccountName, "-n", namespace)
	token, err := Run(tokenCmd)
	if err != nil {
		return "", fmt.Errorf(
			"failed to create token for service account %s in namespace %s: %w",
			serviceAccountName, namespace, err)
	}
	return strings.TrimSpace(token), nil
}

// ExtractMetricValue extracts a specific metric value from Prometheus output
func ExtractMetricValue(metricsOutput, metricName string, labels map[string]string) int {
	parser := expfmt.TextParser{}
	metricFamilies, err := parser.TextToMetricFamilies(strings.NewReader(metricsOutput))
	if err != nil {
		return 0
	}

	metricFamily, exists := metricFamilies[metricName]
	if !exists {
		return 0
	}

	for _, metric := range metricFamily.GetMetric() {
		if labelsMatch(metric.GetLabel(), labels) {
			// Extract the metric value based on type
			switch metricFamily.GetType() {
			case dto.MetricType_GAUGE:
				if gauge := metric.GetGauge(); gauge != nil {
					return int(gauge.GetValue())
				}
			case dto.MetricType_COUNTER:
				if counter := metric.GetCounter(); counter != nil {
					return int(counter.GetValue())
				}
			case dto.MetricType_HISTOGRAM:
				if histogram := metric.GetHistogram(); histogram != nil {
					return int(histogram.GetSampleCount())
				}
			}
		}
	}

	return 0
}

// labelsMatch checks if all expected labels are present and match in the actual Prometheus labels
func labelsMatch(actualLabels []*dto.LabelPair, expectedLabels map[string]string) bool {
	actualLabelMap := make(map[string]string)
	for _, labelPair := range actualLabels {
		actualLabelMap[labelPair.GetName()] = labelPair.GetValue()
	}

	for key, expectedValue := range expectedLabels {
		actualValue, exists := actualLabelMap[key]
		if !exists || actualValue != expectedValue {
			return false
		}
	}
	return true
}

// GetMetricValue gets the current value of a specific metric
func GetMetricValue(metricName string, labels map[string]string, namespace, podName string) int {
	metrics := GetMetricsOutput(namespace, podName)
	return ExtractMetricValue(metrics, metricName, labels)
}
