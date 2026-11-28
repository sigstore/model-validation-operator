# Runbooks

Operational runbooks for the Model Validation Operator. Every alert defined in
[`config/prometheus/alerts.yaml`](../../../config/prometheus/alerts.yaml) carries a
`runbook_url` annotation pointing at a section of this page, or at a dedicated
page where one exists.

## Service level objectives

| SLO | Objective | Window | Error budget |
| --- | --- | --- | --- |
| Status update availability | 99% of ModelValidation status updates succeed | 30d rolling | 1% |
| Status update latency | 99% of ModelValidation status updates complete within 1s | 30d rolling | 1% |

Both SLOs are measured from `model_validation_operator_status_updates_total` and
`model_validation_operator_status_update_duration_seconds`. The burn-rate alerts use
the multiwindow, multi-burn-rate method: a **fast burn** (14.4x, 1h and 5m windows)
is critical, a **slow burn** (6x, 6h and 30m windows) is a warning. Both windows must
agree before an alert fires, which filters out single-scrape spikes.

Periods with no status updates produce `NaN` rather than `0`, so an idle operator
neither fires alerts nor consumes error budget.

### A note on metric labels

The operator labels its metrics with `namespace`, meaning the namespace of the watched
ModelValidation. Because the ServiceMonitor does not set `honorLabels: true`, Prometheus
keeps the scrape target's own `namespace` label and renames the metric's to
`exported_namespace`. In queries and alert output, **`exported_namespace` is the watched
namespace** and `namespace` is where the operator itself runs.

---

## MVOStatusUpdateErrorBudgetBurnFast

**Severity:** critical — the availability error budget is being consumed 14.4x faster
than the SLO allows; 2% of the 30-day budget goes in an hour.

ModelValidation resources are not reflecting the true state of their pods. Anything
reading CR status — dashboards, admission decisions, operators downstream — is working
from stale data.

1. Confirm the scope and find which CRs are affected:
   ```promql
   sum by (exported_namespace, model_validation) (
     rate(model_validation_operator_status_updates_total{result="failure"}[5m])
   )
   ```
2. Check operator logs for the write failures:
   `kubectl logs -n model-validation-operator-system deploy/model-validation-controller-manager`
3. Distinguish the usual causes:
   - **Conflict errors** — competing writers on the CR; expect `MVOHighRetryAttempts` too.
   - **Forbidden** — RBAC regression on the `modelvalidations/status` subresource.
   - **Timeout / connection refused** — API server pressure; check the queue depth and
     `MVOStatusUpdateLatencyBudgetBurnFast`.
4. If the API server is healthy and failures persist, capture logs and roll the operator
   deployment: `kubectl rollout restart -n model-validation-operator-system deploy/model-validation-controller-manager`

## MVOStatusUpdateErrorBudgetBurnSlow

**Severity:** warning — failures are sustained at 6x the SLO rate. Left alone the 30-day
budget is exhausted in roughly five days.

Same investigation as the fast burn above, but it is not an emergency. Work it during
business hours. The most common cause of a slow burn is a single misbehaving CR rather
than a cluster-wide problem, so start by grouping failures per CR and look for one
outlier.

## MVOStatusUpdateLatencyBudgetBurnFast

**Severity:** critical — more than 14.4% of status updates exceed the 1s objective.

1. Look at the latency distribution rather than the quantile alone:
   ```promql
   histogram_quantile(0.99, sum by (le) (
     rate(model_validation_operator_status_update_duration_seconds_bucket[5m])
   ))
   ```
2. Check whether the queue is also backing up
   (`model_validation_operator_status_update_queue_size`). Queue growth alongside latency
   points at the operator being throttled; latency without queue growth points at the
   API server.
3. Check API server health and any client-side rate limiting in the operator logs
   (look for `client-side throttling`).

## MVOStatusUpdateLatencyBudgetBurnSlow

**Severity:** warning — sustained latency above the objective at 6x burn.

Same checks as the fast latency burn. Commonly a gradual effect of cluster growth: more
tracked pods means more status writes. Compare `model_validation_operator_modelvalidation_crs_total`
and the pod counts against the last known-good baseline before treating it as a regression.

## MVOStatusUpdateFailureRate

**Severity:** warning — more than 5% of status updates failed over 5 minutes.

This is the early symptom alert for the availability SLO. It deliberately overlaps with
`MVOStatusUpdateErrorBudgetBurnFast`: this one catches a short, shallow failure spike that
is not yet burning budget fast enough to page. If both fire, treat the burn-rate alert as
the authoritative one and follow its runbook.

## MVOHighRetryAttempts

**Severity:** warning — a ModelValidation is retrying status updates more than once per
second for 15 minutes.

Retries are normal in bursts; sustained retries are not. Usual causes:

1. **Write conflicts.** Another controller, or a second operator replica without working
   leader election, is writing the same CR. Verify only one manager pod is active:
   `kubectl get lease -n model-validation-operator-system`
2. **API server pressure.** Check whether retries correlate with latency alerts.
3. **A CR stuck in a reconcile loop.** Identify it with the per-CR query, then inspect its
   events: `kubectl describe modelvalidation -n <exported_namespace> <model_validation>`

## MVOStatusUpdateQueueBacklog

**Severity:** warning — the queue has held more than 50 items for 10 minutes.

The operator is receiving pod events faster than it can write status. Reported state is
increasingly stale, though no data is lost.

1. Check whether this is a transient burst (a large deployment rolling out) or sustained
   growth. A burst drains on its own; sustained growth does not.
2. If sustained, check latency — a slow API server turns a manageable event rate into a
   growing queue.
3. Consider whether the tracked workload has genuinely grown:
   ```promql
   sum(model_validation_operator_modelvalidation_pod_count)
   ```

## MVOStatusUpdateLatencyHigh

**Severity:** warning — p99 status update duration is above 5s.

Note the histogram's largest explicit bucket is 10s (client_golang defaults), so a p99
reported beyond that is interpolated from the `+Inf` bucket. Read it as "very slow"
rather than as an exact figure.

Follow the latency burn-rate runbook above; this alert is a plain threshold on the same
underlying signal.

## MVOHighConfigurationDriftRate

See the dedicated page: [configuration-drift.md](configuration-drift.md).
