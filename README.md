# Model Validation Controller

This project is a proof of concept based on the [sigstore/model-transperency-cli](https://github.com/sigstore/model-transparency). It offers a Kubernetes/OpenShift operator designed to validate AI models before they are picked up by actual workload. This project provides a webhook that adds an initcontainer to perform model validation. The operator uses a custom resource to define how the models should be validated, such as utilizing [Sigstore](https://www.sigstore.dev/) or public keys.

### Features

- Model Validation: Ensures AI models are validated before they are used by workloads.
- Webhook Integration: A webhook automatically injects an initcontainer into pods to perform the validation step.
- Custom Resource: Configurable `ModelValidation` custom resource to specify how models should be validated.
    - Supports methods like [Sigstore](https://www.sigstore.dev/), pki or public key validation.
- Continuous Validation: Optional periodic re-validation of models using Kubernetes native sidecars (requires Kubernetes 1.28+).

### Prerequisites

- Kubernetes 1.29+ or OpenShift 4.16+ (Kubernetes 1.28+ for continuous validation)
- Proper configuration for model validation (e.g., Sigstore, public keys)
- A signed model (e.g. check the `testdata` or `examples` folder)

### Installation

The operator can be installed via [kustomize](https://kustomize.io/) using different deployment overlays.

#### Production Deployment
For production environments with cert-manager integration:
```bash
kubectl apply -k https://raw.githubusercontent.com/sigstore/model-validation-operator/main/config/overlays/production
# or local
kubectl apply -k config/overlays/production
```

#### Testing Deployment
For testing environments with manual certificate management:
```bash
kubectl apply -k https://raw.githubusercontent.com/sigstore/model-validation-operator/main/config/overlays/testing
# or local
kubectl apply -k config/overlays/testing
```

#### Development Deployment
For development environments, deploying the operator without the webhook integration:
```bash
kubectl apply -k https://raw.githubusercontent.com/sigstore/model-validation-operator/main/config/overlays/development
# or local
kubectl apply -k config/overlays/development
```

#### OLM Deployment
For OpenShift/OLM environments:
```bash
kubectl apply -k https://raw.githubusercontent.com/sigstore/model-validation-operator/main/config/overlays/olm
# or local
kubectl apply -k config/overlays/olm
```

#### Uninstall
To uninstall the operator, use the same overlay you used for installation:
```bash
kubectl delete -k config/overlays/production
```

### Configuration Structure

The operator uses a kustomize based, overlay configuration structure, aiming to separate generated content from environment specific content:

```
config/
├── crd/                      # Custom Resource Definitions
├── rbac/                     # RBAC permissions
├── webhook/                  # Webhook configuration
├── manager/                  # Controller manager deployment
├── manifests/                # OLM manifests
├── components/               # Reusable components
│   ├── webhook/              # Webhook service component
│   ├── certmanager/          # Certificate manager component
│   ├── manual-tls/           # Manual TLS configuration
│   ├── metrics-port/         # Metrics configuration
│   └── webhook-replacements/ # Webhook configuration replacements
└── overlays/                 # Environment-specific overlays
    ├── production/           # Production (cert-manager)
    ├── development/          # Development (operator only, no webhooks)
    ├── testing/              # Testing (manual, self-signed certs)
    └── olm/                  # OpenShift/OLM
```

#### Certificate Management

The operator supports different certificate management approaches:

1. **Production**: Uses cert-manager for automatic certificate management
   - **⚠️ Important**: The default cert-manager configuration uses self-signed certificates
   - For production environments, you should configure cert-manager with a proper CA issuer
2. **Development**: Does not use certificates, there are no webhook configurations in this overlay
3. **Testing**: Uses manual, self-signed certificate management for testing scenarios
4. **OLM**: Uses OLM's built-in certificate management for OpenShift deployments

#### Running the Webhook Server Locally

The webhook server requires TLS certificates. When you run the operator locally, certificates will be generated automatically:

```bash
make run
```

This command will start the webhook server on https://localhost:9443, using the generated certs.

### Known limitations

The project is at an early stage and therefore has some limitations.

- There is no validation or defaulting for the custom resource.
- The validation is namespace scoped and cannot be used across multiple namespaces.

- There are no status fields for the custom resource.
- The model and signature path must be specified, there is no auto discovery.
- TLS certificates used by the webhook are self generated.

### Usage

First, a ModelValidation CR must be created as follows:
```yaml
apiVersion: ml.sigstore.dev/v1alpha1
kind: ModelValidation
metadata:
  name: demo
spec:
  config:
    sigstoreConfig:
      certificateIdentity: "https://github.com/sigstore/model-validation-operator/.github/workflows/sign-model.yaml@refs/tags/v0.0.2"
      certificateOidcIssuer: "https://token.actions.githubusercontent.com"
  model:
    path: /data/tensorflow_saved_model
    signaturePath: /data/tensorflow_saved_model/model.sig
```

Pods in the namespace that have the label `validation.ml.sigstore.dev/ml: "<modelvalidation-cr-name>"` will be validated using the specified ModelValidation CR.
It should be noted that this does not apply to subsequently labeled pods.

```diff
apiVersion: v1
kind: Pod
metadata:
  name: whatever-workload
+  labels:
+    validation.ml.sigstore.dev/ml: "demo"
spec:
  restartPolicy: Never
  containers:
  - name: whatever-workload
    image: nginx
    ports:
    - containerPort: 80
    volumeMounts:
    - name: model-storage
      mountPath: /data
  volumes:
  - name: model-storage
    persistentVolumeClaim:
      claimName: models
```

### Continuous Model Validation

The operator supports continuous validation, which periodically re-validates models after the initial validation. This feature uses Kubernetes 1.28+ native sidecars with `restartPolicy: Always`.

#### How It Works

When continuous validation is enabled:
1. The validation container runs as a native sidecar (not just an init container)
2. After the initial validation succeeds, the container becomes ready
3. The validation repeats at the specified interval
4. On validation failure, the error is logged but the container continues running
5. The readiness probe reflects the validation state

#### Configuration

Add the `continuousValidation` field to your ModelValidation CR:

```yaml
apiVersion: ml.sigstore.dev/v1alpha1
kind: ModelValidation
metadata:
  name: demo-continuous
spec:
  config:
    sigstoreConfig:
      certificateIdentity: "user@example.com"
      certificateOidcIssuer: "https://token.actions.githubusercontent.com"
  model:
    path: /data/tensorflow_saved_model
    signaturePath: /data/tensorflow_saved_model/model.sig
  continuousValidation:
    enabled: true
    interval: "10m"  # Supports s, m, h units (e.g., "30s", "5m", "1h")
```

#### Requirements

- Kubernetes 1.28 or later (for native sidecar support with `restartPolicy: Always`)
- The validation container will consume resources continuously (CPU/memory)
- Consider longer intervals (e.g., 10m, 1h) for production workloads

### Examples

The example folder contains example files for testing the operator.

#### Example Continuous Validation

See `examples/continuous-validation.yaml` for a complete example.

#### Prerequisites for Examples

Before running the examples, create a namespace for testing (separate from the operator namespace):

```bash
kubectl create namespace testing
```

**Important**: Do not deploy examples in the operator namespace (e.g., `model-validation-operator-system`). The operator namespace has the label `validation.ml.sigstore.dev/ignore: "true"` which prevents the webhook from processing pods in that namespace.

#### Example Files

- **prepare.yaml**: Contains a persistent volume claim and a job that downloads a signed test model.
```bash
kubectl apply -f https://raw.githubusercontent.com/sigstore/model-validation-operator/main/examples/prepare.yaml -n testing
# or local
kubectl apply -f examples/prepare.yaml -n testing
```

- **verify.yaml**: Contains a model validation manifest for the validation of this model and a demo pod, which is provided with the appropriate label for validation.
```bash
kubectl apply -f https://raw.githubusercontent.com/sigstore/model-validation-operator/main/examples/verify.yaml -n testing
# or local
kubectl apply -f examples/verify.yaml -n testing
```

- **unsigned.yaml**: Contains an example of a pod that would fail validation (for testing purposes).
```bash
kubectl apply -f https://raw.githubusercontent.com/sigstore/model-validation-operator/main/examples/unsigned.yaml -n testing
# or local
kubectl apply -f examples/unsigned.yaml -n testing
```

After the example installation, the logs of the generated job should show a successful download:
```bash
$ kubectl logs -n testing job/download-extract-model 
Connecting to github.com (140.82.121.3:443)
Connecting to objects.githubusercontent.com (185.199.108.133:443)
saving to '/data/tensorflow_saved_model.tar.gz'
tensorflow_saved_mod  44% |**************                  | 3983k  0:00:01 ETA
tensorflow_saved_mod 100% |********************************| 8952k  0:00:00 ETA
'/data/tensorflow_saved_model.tar.gz' saved
./
./model.sig
./variables/
./variables/variables.data-00000-of-00001
./variables/variables.index
./saved_model.pb
./fingerprint.pb
```

The operator logs should show that a pod has been modified:
```bash
$ kubectl logs -n model-validation-operator-system deploy/model-validation-controller-manager
time=2025-01-20T22:13:05.051Z level=INFO msg="Starting webhook server on :9443"
time=2025-01-20T22:13:47.556Z level=INFO msg="new request, path: /mutate-v1-pod"
time=2025-01-20T22:13:47.557Z level=INFO msg="Execute webhook"
time=2025-01-20T22:13:47.560Z level=INFO msg="Search associated Model Validation CR" pod=whatever-workload namespace=testing
time=2025-01-20T22:13:47.591Z level=INFO msg="construct args"
time=2025-01-20T22:13:47.591Z level=INFO msg="found sigstore config"
```

Finally, the test pod should be running and the injected initcontainer should have been successfully validated.
```bash
$ kubectl logs -n testing whatever-workload model-validation
INFO:__main__:Creating verifier for sigstore
INFO:tuf.api._payload:No signature for keyid f5312f542c21273d9485a49394386c4575804770667f2ddb59b3bf0669fddd2f
INFO:tuf.api._payload:No signature for keyid ff51e17fcf253119b7033f6f57512631da4a0969442afcf9fc8b141c7f2be99c
INFO:tuf.api._payload:No signature for keyid ff51e17fcf253119b7033f6f57512631da4a0969442afcf9fc8b141c7f2be99c
INFO:tuf.api._payload:No signature for keyid ff51e17fcf253119b7033f6f57512631da4a0969442afcf9fc8b141c7f2be99c
INFO:tuf.api._payload:No signature for keyid ff51e17fcf253119b7033f6f57512631da4a0969442afcf9fc8b141c7f2be99c
INFO:__main__:Verifying model signature from /data/model.sig
INFO:__main__:all checks passed
```
In case the workload is modified, is not executed:
```bash
ERROR:__main__:verification failed: the manifests do not match
```

#### Ignore Options

The `model` section of the ModelValidation CR supports additional options to control which files are included during verification:

| Field | Type | Description |
|-------|------|-------------|
| `ignorePaths` | `[]string` | List of file paths to exclude from verification |
| `ignoreGitPaths` | `bool` | When `true`, excludes git-related files (e.g., `.git/`, `.gitignore`) |
| `ignoreUnsignedFiles` | `bool` | When `true`, unsigned files will not cause verification to fail |
| `allowSymlinks` | `bool` | When `true`, symbolic links will be followed and their targets verified |

Example with ignore options:
```yaml
apiVersion: ml.sigstore.dev/v1alpha1
kind: ModelValidation
metadata:
  name: demo
spec:
  config:
    sigstoreConfig:
      certificateIdentity: "https://github.com/sigstore/model-validation-operator/.github/workflows/sign-model.yaml@refs/tags/v0.0.2"
      certificateOidcIssuer: "https://token.actions.githubusercontent.com"
  model:
    path: /data/tensorflow_saved_model
    signaturePath: /data/tensorflow_saved_model/model.sig
    ignorePaths:
      - /data/tensorflow_saved_model/cache
      - /data/tensorflow_saved_model/tmp
    ignoreGitPaths: true
    allowSymlinks: true
```

#### Pod Annotations

Ignore options can also be specified or overridden on individual pods using annotations. Pod annotations take precedence over the ModelValidation CR settings.

| Annotation | Value | Description |
|------------|-------|-------------|
| `validation.ml.sigstore.dev/ignore-paths` | Comma-separated paths | Paths to exclude from verification |
| `validation.ml.sigstore.dev/ignore-git-paths` | `"true"` or `"false"` | Exclude git-related files |
| `validation.ml.sigstore.dev/ignore-unsigned-files` | `"true"` or `"false"` | Allow unsigned files |
| `validation.ml.sigstore.dev/allow-symlinks` | `"true"` or `"false"` | Follow symbolic links |

Example pod with annotation overrides:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: whatever-workload
  labels:
    validation.ml.sigstore.dev/ml: "demo"
  annotations:
    validation.ml.sigstore.dev/ignore-paths: "/data/tensorflow_saved_model/logs,/data/tensorflow_saved_model/tmp"
    validation.ml.sigstore.dev/ignore-git-paths: "true"
spec:
  # ... rest of pod spec
```
