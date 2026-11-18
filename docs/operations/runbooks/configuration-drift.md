# Runbook: Configuration Drift

## What is configuration drift?

Configuration drift is when a `ModelValidation` custom resource's current
configuration no longer matches the configuration that was in effect when its
tracked pods were admitted.

When a pod is admitted, the webhook injects a validation init container based on
the `ModelValidation` spec at that moment, and the operator records the
configuration it used (a config hash and an auth method). Those pods are validated
once, under that configuration — they are **not** retroactively revalidated. If
the CR later changes, the running pods keep their old validation configuration
while the CR declares a new one. That mismatch is drift.

The operator detects drift during status reconciliation. When the tracked config
hash or auth method differs from the CR's current values it:

- increments `model_validation_operator_configuration_drift_events_total`
  (label `drift_type` = `config_hash` and/or `auth_method`), and
- **fails that status update** for the CR.

## Why does it happen?

Drift is almost always the result of an intentional or accidental change to a
`ModelValidation` CR that already has pods running against it:

- The validation spec changed — model/signature paths, keys, or any field that
  feeds the config hash (`drift_type="config_hash"`).
- The auth method changed — e.g. switching between public key, PKI, and Sigstore
  (`drift_type="auth_method"`).

A brief drift signal is **expected and benign** right after such a change: the old
pods still exist until they are recreated under the new config. Drift becomes a
problem when those pods are never recreated, so the CR permanently disagrees with
its workloads and its status updates keep failing.

## How to detect it

Drift surfaces through the metric, the CR status, and the operator logs.

**1. The drift metric is non-zero.**

```promql
# Drift events per second over the last 5m, by CR and drift type
sum by (namespace, model_validation, drift_type) (
  rate(model_validation_operator_configuration_drift_events_total[5m])
)
```

```promql
# CRs with ongoing drift (rate > 0 over the last 10m)
sum by (namespace, model_validation) (
  rate(model_validation_operator_configuration_drift_events_total[10m])
) > 0
```

A brief spike that returns to zero is a normal reconfiguration. A **sustained**
rate above zero for a CR means drift is unresolved.

**2. Status updates for the CR are failing**, because each drift event fails the
update:

```promql
sum by (namespace, model_validation) (
  rate(model_validation_operator_status_updates_total{result="failure"}[5m])
)
```

**3. Orphaned pods appear**, a common symptom of drifted workloads:

```promql
model_validation_operator_modelvalidation_pod_count{pod_state="orphaned"} > 0
```

## How to confirm a drift

Confirm that a specific CR has really drifted (versus a one-off reconfiguration
that already resolved):

1. **Inspect the CR and its status:**
   ```bash
   kubectl -n <namespace> get modelvalidation <name> -o yaml
   kubectl -n <namespace> describe modelvalidation <name>
   ```
   Check the injected / uninjected / orphaned pod counts in the status.

2. **Read the operator logs** — on drift the operator logs the old vs. new values,
   which confirms both the drift and what changed:
   ```bash
   kubectl -n <operator-namespace> logs deploy/model-validation-operator-controller-manager \
     | grep -i "configuration drift"
   ```
   The log line includes `oldConfigHash`/`newConfigHash` and
   `oldAuthMethod`/`newAuthMethod`.

3. **Cross-check the metric** with the "ongoing drift" query above. If the rate is
   still positive and the logs show a current hash/auth-method mismatch, the drift
   is real and unresolved.

## How to resolve it

Drifted pods cannot be fixed in place — they must be re-admitted so the webhook
injects validation based on the current CR configuration.

1. **Recreate the affected workloads** so their pods are re-admitted under the
   current config, for example:
   ```bash
   # For a Deployment / StatefulSet / DaemonSet backing the pods
   kubectl -n <namespace> rollout restart deploy/<workload>
   ```
   For pods reported as orphaned, deleting them lets the owning controller recreate
   them under the current configuration.

2. **Verify recovery:**
   - The drift rate returns to zero:
     ```promql
     sum by (namespace, model_validation) (
       rate(model_validation_operator_configuration_drift_events_total[10m])
     )
     ```
   - Status updates for the CR succeed again (failure rate drops to zero).
   - The orphaned pod count returns to zero.

3. **If drift keeps recurring**, something is repeatedly changing the CR. Identify
   the source of the edits (GitOps/controller reconciling an old spec, a CI job, or
   a human) and reconcile the intended configuration before recreating workloads.

## Reference

- Metric: `model_validation_operator_configuration_drift_events_total` (CounterVec;
  labels `namespace`, `model_validation`, `drift_type` ∈ {`config_hash`,
  `auth_method`}), defined in
  [`internal/metrics/metrics.go`](../../../internal/metrics/metrics.go).
- Detection logic:
  [`internal/tracker/status_tracker.go`](../../../internal/tracker/status_tracker.go).
- Related incident scenario: see **Configuration drift** in the
  [incident response playbook](../incident-response.md) (SECURESIGN-5170).
- Alert: `HighConfigurationDriftRate` (to be defined in SECURESIGN-5171).
