package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	testNamespace       = "test-namespace"
	testModelValidation = "test-mv"
)

func TestMetricsDefinition(t *testing.T) {
	// Test that all metric variables are defined
	assert.NotNil(t, ModelValidationPodCounts)
	assert.NotNil(t, PodStateTransitionsTotal)
	assert.NotNil(t, StatusUpdatesTotal)
	assert.NotNil(t, ConfigurationDriftEventsTotal)
	assert.NotNil(t, ModelValidationCRsTotal)
	assert.NotNil(t, StatusUpdateDuration)
	assert.NotNil(t, QueueSize)
	assert.NotNil(t, RetryAttemptsTotal)
}

// Test helper to verify gauge metrics
func verifyGaugeMetric(t *testing.T, gauge *prometheus.GaugeVec, labels []string, expectedValue float64) {
	metric := gauge.WithLabelValues(labels...)
	metricDto := &dto.Metric{}
	err := metric.Write(metricDto)
	assert.NoError(t, err)
	assert.Equal(t, expectedValue, metricDto.GetGauge().GetValue())
}

// Test helper to verify counter metrics
func verifyCounterIncrement(t *testing.T, counter *prometheus.CounterVec, labels []string) float64 {
	metric := counter.WithLabelValues(labels...)
	metricDto := &dto.Metric{}
	err := metric.Write(metricDto)
	assert.NoError(t, err)
	return metricDto.GetCounter().GetValue()
}

func TestRecordPodCount(t *testing.T) {
	podState := PodStateInjected
	count := float64(5)

	RecordPodCount(testNamespace, testModelValidation, podState, count)

	verifyGaugeMetric(t, ModelValidationPodCounts, []string{testNamespace, testModelValidation, podState}, count)
}

func TestRecordPodStateTransition(t *testing.T) {
	fromState := PodStateUninjected
	toState := PodStateInjected

	labels := []string{testNamespace, testModelValidation, fromState, toState}
	initialValue := verifyCounterIncrement(t, PodStateTransitionsTotal, labels)
	RecordPodStateTransition(testNamespace, testModelValidation, fromState, toState)
	finalValue := verifyCounterIncrement(t, PodStateTransitionsTotal, labels)

	assert.Equal(t, initialValue+1, finalValue)
}

func TestRecordStatusUpdate(t *testing.T) {
	result := StatusUpdateSuccess

	labels := []string{testNamespace, testModelValidation, result}
	initialValue := verifyCounterIncrement(t, StatusUpdatesTotal, labels)
	RecordStatusUpdate(testNamespace, testModelValidation, result)
	finalValue := verifyCounterIncrement(t, StatusUpdatesTotal, labels)

	assert.Equal(t, initialValue+1, finalValue)
}

func TestRecordConfigurationDrift(t *testing.T) {
	driftType := "config_hash"

	labels := []string{testNamespace, testModelValidation, driftType}
	initialValue := verifyCounterIncrement(t, ConfigurationDriftEventsTotal, labels)
	RecordConfigurationDrift(testNamespace, testModelValidation, driftType)
	finalValue := verifyCounterIncrement(t, ConfigurationDriftEventsTotal, labels)

	assert.Equal(t, initialValue+1, finalValue)
}

func TestRecordModelValidationCR(t *testing.T) {
	count := float64(3)

	RecordModelValidationCR(testNamespace, count)

	verifyGaugeMetric(t, ModelValidationCRsTotal, []string{testNamespace}, count)
}

func TestSetQueueSize(t *testing.T) {
	size := float64(10)

	SetQueueSize(size)

	metricDto := &dto.Metric{}
	err := QueueSize.Write(metricDto)
	assert.NoError(t, err)
	assert.Equal(t, size, metricDto.GetGauge().GetValue())
}

func TestRecordStatusUpdateDuration(t *testing.T) {
	result := StatusUpdateSuccess
	duration := 0.5 // 500ms

	RecordStatusUpdateDuration(testNamespace, testModelValidation, result, duration)

	// Verify the histogram was recorded by checking the metric family
	metricFamilies, err := metrics.Registry.Gather()
	assert.NoError(t, err)

	var found bool
	for _, mf := range metricFamilies {
		if mf.GetName() == "model_validation_operator_status_update_duration_seconds" {
			for _, metric := range mf.GetMetric() {
				if metric.GetHistogram().GetSampleCount() > 0 {
					found = true
					break
				}
			}
		}
	}
	assert.True(t, found, "Expected histogram metric to be recorded")
}

func TestRecordMultiplePodStateTransitions(t *testing.T) {
	fromState := PodStateUninjected
	toState := PodStateInjected
	count := 3

	labels := []string{testNamespace, testModelValidation, fromState, toState}
	initialValue := verifyCounterIncrement(t, PodStateTransitionsTotal, labels)
	RecordMultiplePodStateTransitions(testNamespace, testModelValidation, fromState, toState, count)
	finalValue := verifyCounterIncrement(t, PodStateTransitionsTotal, labels)

	assert.Equal(t, initialValue+float64(count), finalValue)
}
