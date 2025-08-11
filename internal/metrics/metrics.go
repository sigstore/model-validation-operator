// Package metrics provides Prometheus metrics for the model validation operator.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	// Metric label names
	labelNamespace          = "namespace"
	labelModelValidation    = "model_validation"
	labelPodState           = "pod_state"
	labelStatusUpdateResult = "result"
	labelDriftType          = "drift_type"

	// PodStateInjected represents pods with model validation finalizers
	PodStateInjected = "injected"
	// PodStateUninjected represents pods without model validation finalizers
	PodStateUninjected = "uninjected"
	// PodStateOrphaned represents pods with configuration drift
	PodStateOrphaned = "orphaned"

	// StatusUpdateSuccess indicates a successful status update
	StatusUpdateSuccess = "success"
	// StatusUpdateFailure indicates a failed status update
	StatusUpdateFailure = "failure"
)

var (
	// ModelValidationPodCounts tracks the current number of pods in each state per ModelValidation
	ModelValidationPodCounts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "model_validation_operator",
			Name:      "modelvalidation_pod_count",
			Help:      "Current number of pods tracked per ModelValidation by state",
		},
		[]string{labelNamespace, labelModelValidation, labelPodState},
	)

	// PodStateTransitionsTotal tracks pod state transitions
	PodStateTransitionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "model_validation_operator",
			Name:      "pod_state_transitions_total",
			Help:      "Total number of pod state transitions",
		},
		[]string{labelNamespace, labelModelValidation, "from_state", "to_state"},
	)

	// StatusUpdatesTotal tracks ModelValidation status updates
	StatusUpdatesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "model_validation_operator",
			Name:      "status_updates_total",
			Help:      "Total number of ModelValidation status updates",
		},
		[]string{labelNamespace, labelModelValidation, labelStatusUpdateResult},
	)

	// ConfigurationDriftEventsTotal tracks configuration drift events
	ConfigurationDriftEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "model_validation_operator",
			Name:      "configuration_drift_events_total",
			Help:      "Total number of configuration drift events detected",
		},
		[]string{labelNamespace, labelModelValidation, labelDriftType},
	)

	// ModelValidationCRsTotal tracks total number of ModelValidation CRs per namespace
	// Does not include authMethod for namespace-level tracking
	ModelValidationCRsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "model_validation_operator",
			Name:      "modelvalidation_crs_total",
			Help:      "Total number of ModelValidation CRs being tracked per namespace",
		},
		[]string{labelNamespace},
	)

	// StatusUpdateDuration tracks the duration of status update operations
	StatusUpdateDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "model_validation_operator",
			Name:      "status_update_duration_seconds",
			Help:      "Duration of ModelValidation status update operations",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{labelNamespace, labelModelValidation, labelStatusUpdateResult},
	)

	// QueueSize tracks the current size of the status update queue
	QueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "model_validation_operator",
			Name:      "status_update_queue_size",
			Help:      "Current size of the status update queue",
		},
	)

	// RetryAttemptsTotal tracks retry attempts for status updates
	RetryAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "model_validation_operator",
			Name:      "status_update_retry_attempts_total",
			Help:      "Total number of status update retry attempts",
		},
		[]string{labelNamespace, labelModelValidation},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ModelValidationPodCounts,
		PodStateTransitionsTotal,
		StatusUpdatesTotal,
		ConfigurationDriftEventsTotal,
		ModelValidationCRsTotal,
		StatusUpdateDuration,
		QueueSize,
		RetryAttemptsTotal,
	)
}

// RecordPodCount records the current pod count for a ModelValidation
func RecordPodCount(namespace, modelValidation, podState string, count float64) {
	ModelValidationPodCounts.WithLabelValues(namespace, modelValidation, podState).Set(count)
}

// RecordPodStateTransition records a pod state transition
func RecordPodStateTransition(namespace, modelValidation, fromState, toState string) {
	PodStateTransitionsTotal.WithLabelValues(namespace, modelValidation, fromState, toState).Inc()
}

// RecordStatusUpdate records a status update result
func RecordStatusUpdate(namespace, modelValidation, result string) {
	StatusUpdatesTotal.WithLabelValues(namespace, modelValidation, result).Inc()
}

// RecordConfigurationDrift records a configuration drift event
func RecordConfigurationDrift(namespace, modelValidation, driftType string) {
	ConfigurationDriftEventsTotal.WithLabelValues(namespace, modelValidation, driftType).Inc()
}

// RecordModelValidationCR records the current number of ModelValidation CRs per namespace
func RecordModelValidationCR(namespace string, count float64) {
	ModelValidationCRsTotal.WithLabelValues(namespace).Set(count)
}

// RecordStatusUpdateDuration records the duration of a status update
func RecordStatusUpdateDuration(namespace, modelValidation, result string, duration float64) {
	StatusUpdateDuration.WithLabelValues(namespace, modelValidation, result).Observe(duration)
}

// SetQueueSize sets the current queue size
func SetQueueSize(size float64) {
	QueueSize.Set(size)
}

// RecordRetryAttempt records a retry attempt
func RecordRetryAttempt(namespace, modelValidation string) {
	RetryAttemptsTotal.WithLabelValues(namespace, modelValidation).Inc()
}

// RecordMultiplePodStateTransitions records multiple identical pod state transitions
func RecordMultiplePodStateTransitions(namespace, modelValidation, fromState, toState string, count int) {
	if count > 0 {
		PodStateTransitionsTotal.WithLabelValues(namespace, modelValidation, fromState, toState).Add(float64(count))
	}
}
