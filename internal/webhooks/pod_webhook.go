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

package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sigstore/model-validation-operator/internal/constants"
	"github.com/sigstore/model-validation-operator/internal/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/go-logr/logr"
	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// NewPodInterceptor creates a new pod mutating webhook to be registered.
// nativeSidecarSupport indicates whether the cluster supports native sidecars
// (init containers with restartPolicy: Always, available in Kubernetes 1.28+).
// When false, continuous validation falls back to injecting a traditional sidecar
// container alongside a one-shot init container.
func NewPodInterceptor(c client.Client, decoder admission.Decoder, nativeSidecarSupport bool) webhook.AdmissionHandler {
	return &podInterceptor{
		client:               c,
		decoder:              decoder,
		nativeSidecarSupport: nativeSidecarSupport,
	}
}

//nolint:lll
// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=fail,groups="",resources=pods,sideEffects=None,verbs=create;update,versions=v1,name=pods.validation.ml.sigstore.dev,admissionReviewVersions=v1

// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=modelvalidations,verbs=get;list;watch
// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=telemetryconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// podInterceptor extends pods with Model Validation Init-Container if annotation is specified.
type podInterceptor struct {
	client               client.Client
	decoder              admission.Decoder
	nativeSidecarSupport bool
}

// Handle extends pods with Model Validation Init-Container if annotation is specified.
func (p *podInterceptor) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	start := time.Now()
	defer func() {
		metrics.RecordWebhookMutation(ctx, req.Namespace, webhookResult(resp), time.Since(start))
	}()

	logger := log.FromContext(ctx)
	logger.Info("Execute webhook")
	pod := &corev1.Pod{}

	if err := p.decoder.Decode(req, pod); err != nil {
		logger.Error(err, "failed to decode pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if namespace should be ignored
	ns := &corev1.Namespace{}
	if err := p.client.Get(ctx, client.ObjectKey{Name: req.Namespace}, ns); err != nil {
		logger.Error(err, "failed to get namespace")
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if ns.Labels[constants.IgnoreNamespaceLabel] == constants.IgnoreNamespaceValue {
		logger.Info("Namespace has ignore label, skipping", "namespace", req.Namespace)
		return admission.Allowed("namespace ignored")
	}

	logger.Info("Checking pod labels", "matchLabel", pod.Labels[constants.ModelValidationLabel])
	modelValidationName, ok := pod.Labels[constants.ModelValidationLabel]
	if !ok || modelValidationName == "" {
		logger.Info("ModelValidation label not found or empty, skipping injection")
		return admission.Allowed("no ModelValidation label found, no action needed")
	}

	mv := &v1alpha1.ModelValidation{}
	err := p.client.Get(ctx, client.ObjectKey{Name: modelValidationName, Namespace: pod.Namespace}, mv)
	if err != nil {
		logger.Error(err, "failed to get ModelValidation CR", "namespace", pod.Namespace, "modelValidation", modelValidationName)
		return admission.Errored(http.StatusBadRequest, err) // Fail deployment if CR not found
	}
	// NOTE: check if validation sidecar is already injected. Then no action needed.
	for _, c := range pod.Spec.InitContainers {
		if c.Name == constants.ModelValidationInitContainerName {
			logger.V(1).Info("Validation init container already exists, skipping", "pod", pod.Name, "namespace", req.Namespace)
			return admission.Allowed("validation exists, no action needed")
		}
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == constants.ModelValidationSidecarContainerName {
			logger.V(1).Info("Validation sidecar already exists, skipping", "pod", pod.Name, "namespace", req.Namespace)
			return admission.Allowed("validation exists, no action needed")
		}
	}

	mergedModel := mergeModelWithAnnotations(logger, mv.Spec.Model, pod.Annotations)

	args := []string{"verify"}
	args = append(args, validationConfigToArgs(logger, mv.Spec.Config, mergedModel)...)
	args = append(args, mergedModel.Path)

	pp := pod.DeepCopy()

	controllerutil.AddFinalizer(pp, constants.ModelValidationFinalizer)
	if pp.Annotations == nil {
		pp.Annotations = make(map[string]string)
	}
	pp.Annotations[constants.InjectedAnnotationKey] = time.Now().Format(time.RFC3339)
	pp.Annotations[constants.AuthMethodAnnotationKey] = mv.GetAuthMethod()
	pp.Annotations[constants.ConfigHashAnnotationKey] = mv.GetConfigHash()

	tc, err := findMatchingTelemetryConfig(ctx, p.client, ns, mv)
	if err != nil {
		logger.Error(err, "failed to find TelemetryConfig, proceeding without telemetry")
	}

	neededPaths := collectNeededPaths(mergedModel, mv.Spec.Config)
	vm := filterVolumeMounts(pod.Spec.Containers, neededPaths)

	continuousEnabled := mv.Spec.ContinuousValidation != nil && mv.Spec.ContinuousValidation.Enabled
	useLegacySidecar := continuousEnabled && !p.nativeSidecarSupport

	if useLegacySidecar {
		logger.Info("Using legacy sidecar for continuous validation (native sidecars not supported)")
	}

	if mv.Spec.Config.SigstoreConfig != nil {
		const tufVolName = "sigstore-tuf-cache"
		pp.Spec.Volumes = append(pp.Spec.Volumes, corev1.Volume{
			Name: tufVolName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
				SizeLimit: ptr.To(resource.MustParse("10Mi")),
			}},
		})
		vm = append(vm, corev1.VolumeMount{Name: tufVolName, MountPath: "/.sigstore"})
	}

	container := buildValidationContainer(mv, args, vm, pp, tc, p.nativeSidecarSupport)
	pp.Spec.InitContainers = append(pp.Spec.InitContainers, container)

	// On pre-1.28 clusters with continuous validation, inject a traditional sidecar
	// container alongside the init container for periodic re-validation.
	if useLegacySidecar {
		sidecar := buildLegacySidecarContainer(mv, args, vm, pp)
		pp.Spec.Containers = append(pp.Spec.Containers, sidecar)
	}

	logger.Info("Injected validation container",
		"pod", pod.Name,
		"namespace", req.Namespace,
		"modelValidation", modelValidationName,
		"authMethod", mv.GetAuthMethod(),
		"continuous", continuousEnabled,
		"legacySidecar", useLegacySidecar,
	)

	marshaledPod, err := json.Marshal(pp)
	if err != nil {
		logger.Error(err, "failed to marshal mutated pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func buildValidationContainer(
	mv *v1alpha1.ModelValidation, args []string, vm []corev1.VolumeMount, pp *corev1.Pod,
	tc *v1alpha1.TelemetryConfig, nativeSidecarSupport bool,
) corev1.Container {
	// Determine image pull policy
	imagePullPolicy := corev1.PullAlways
	if mv.Spec.ImagePullPolicy != "" {
		imagePullPolicy = mv.Spec.ImagePullPolicy
	}

	container := corev1.Container{
		Name:            constants.ModelValidationInitContainerName,
		Image:           constants.ModelValidationAgentImage,
		ImagePullPolicy: imagePullPolicy,
		Command:         []string{"/usr/local/bin/validation-agent"},
		Args:            args,
		VolumeMounts:    vm,
	}

	// Add continuous validation configuration if enabled AND native sidecars are supported
	continuousEnabled := mv.Spec.ContinuousValidation != nil && mv.Spec.ContinuousValidation.Enabled
	if continuousEnabled && nativeSidecarSupport {
		interval := "5m"
		if mv.Spec.ContinuousValidation.Interval != "" {
			interval = mv.Spec.ContinuousValidation.Interval
		}

		// Make it a native sidecar with restartPolicy: Always
		container.RestartPolicy = ptr.To(corev1.ContainerRestartPolicyAlways)

		// Prepend interval flag to args
		container.Args = append([]string{"--interval=" + interval}, args...)

		// Add readiness probe (ready after first successful validation)
		container.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		}

		// Add liveness probe (healthy while process is running)
		container.LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       30,
		}
	}

	// Track continuous validation in annotations regardless of sidecar mode
	if continuousEnabled {
		if pp.Annotations == nil {
			pp.Annotations = make(map[string]string)
		}
		pp.Annotations[constants.ContinuousValidationAnnotationKey] = "true"
	}

	// Apply resource requirements (for both one-shot and continuous modes)
	if mv.Spec.Resources != nil {
		container.Resources = *mv.Spec.Resources
	} else {
		container.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}
	}

	// Inject telemetry env vars if a TelemetryConfig matched
	if envs := telemetryEnvVars(tc); len(envs) > 0 {
		container.Env = append(container.Env, envs...)
	}

	container.SecurityContext = restrictedSecurityContext()

	return container
}

// buildLegacySidecarContainer constructs a traditional sidecar container for
// continuous validation on clusters that don't support native sidecars (pre-1.28).
// This container runs alongside the application containers and periodically
// re-validates the model. The init container (built by buildValidationContainer)
// handles the initial one-shot validation to block pod startup.
func buildLegacySidecarContainer(
	mv *v1alpha1.ModelValidation, args []string, vm []corev1.VolumeMount, _ *corev1.Pod,
) corev1.Container {
	imagePullPolicy := corev1.PullAlways
	if mv.Spec.ImagePullPolicy != "" {
		imagePullPolicy = mv.Spec.ImagePullPolicy
	}

	interval := "5m"
	if mv.Spec.ContinuousValidation != nil && mv.Spec.ContinuousValidation.Interval != "" {
		interval = mv.Spec.ContinuousValidation.Interval
	}

	// Prepend interval flag and --skip-initial to args.
	// --skip-initial tells the agent to skip initial validation since the
	// init container already performed it.
	sidecarArgs := append([]string{"--interval=" + interval, "--skip-initial"}, args...)

	container := corev1.Container{
		Name:            constants.ModelValidationSidecarContainerName,
		Image:           constants.ModelValidationAgentImage,
		ImagePullPolicy: imagePullPolicy,
		Command:         []string{"/usr/local/bin/validation-agent"},
		Args:            sidecarArgs,
		VolumeMounts:    vm,
		// Add readiness probe (ready immediately since init container already validated)
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		// Add liveness probe
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       30,
		},
	}

	// Apply resource requirements
	if mv.Spec.Resources != nil {
		container.Resources = *mv.Spec.Resources
	} else {
		container.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}
	}

	container.SecurityContext = restrictedSecurityContext()

	return container
}

func restrictedSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot:             ptr.To(true),
		ReadOnlyRootFilesystem:   ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

func validationConfigToArgs(logger logr.Logger, cfg v1alpha1.ValidationConfig, model v1alpha1.Model) []string {
	res := []string{}
	if cfg.SigstoreConfig != nil {
		res = append(res,
			"sigstore",
			fmt.Sprintf("--signature=%s", model.SignaturePath),
			"--identity", cfg.SigstoreConfig.CertificateIdentity,
			"--identity_provider", cfg.SigstoreConfig.CertificateOidcIssuer,
		)
	} else if cfg.PublicKeyConfig != nil {
		res = append(res,
			"key",
			fmt.Sprintf("--signature=%s", model.SignaturePath),
			"--public_key", cfg.PublicKeyConfig.KeyPath,
		)
	} else if cfg.PkiConfig != nil {
		res = append(res,
			"certificate",
			fmt.Sprintf("--signature=%s", model.SignaturePath),
			"--certificate_chain", cfg.PkiConfig.CertificateAuthority,
		)
	} else {
		logger.Error(nil, "missing validation config")
		return []string{}
	}

	if cfg.ClientTrustConfig != nil {
		res = append(res, "--trust_config", cfg.ClientTrustConfig.TrustConfigPath)
	}

	for _, ignorePath := range model.IgnorePaths {
		res = append(res, "--ignore-paths", ignorePath)
	}

	if model.IgnoreGitPaths != nil {
		if *model.IgnoreGitPaths {
			res = append(res, "--ignore-git-paths")
		} else {
			res = append(res, "--no-ignore-git-paths")
		}
	}

	if model.IgnoreUnsignedFiles != nil {
		if *model.IgnoreUnsignedFiles {
			res = append(res, "--ignore_unsigned_files")
		} else {
			res = append(res, "--no-ignore_unsigned_files")
		}
	}

	if model.AllowSymlinks != nil && *model.AllowSymlinks {
		res = append(res, "--allow_symlinks")
	}

	return res
}

// parseBoolAnnotation is a helper function to parse boolean annotations.
// It returns the parsed value and true if the annotation exists and was successfully parsed,
// otherwise returns nil, false.
func parseBoolAnnotation(logger logr.Logger, annotations map[string]string, key, name string) (*bool, bool) {
	if raw, ok := annotations[key]; ok {
		valStr := strings.TrimSpace(raw)
		val, err := strconv.ParseBool(valStr)
		if err == nil {
			return &val, true
		}
		logger.Error(err, "Failed to parse "+name+" annotation", "value", valStr)
	}
	return nil, false
}

// mergeModelWithAnnotations merges Model settings from ModelValidation CR with pod annotations.
// Pod annotations take precedence over CR settings.
func mergeModelWithAnnotations(logger logr.Logger, model v1alpha1.Model, annotations map[string]string) v1alpha1.Model {
	merged := model.DeepCopy()

	if ignorePathsStr, ok := annotations[constants.IgnorePathsAnnotationKey]; ok && ignorePathsStr != "" {
		logger.Info("Found ignore-paths annotation", "value", ignorePathsStr)
		paths := strings.Split(ignorePathsStr, ",")
		validPaths := make([]string, 0, len(paths))
		for _, path := range paths {
			trimmed := strings.TrimSpace(path)
			if trimmed != "" {
				validPaths = append(validPaths, trimmed)
			}
		}
		if len(validPaths) > 0 {
			merged.IgnorePaths = validPaths
		} else {
			logger.Info("No valid paths found in ignore-paths annotation after filtering empty entries")
		}
	}

	if val, ok := parseBoolAnnotation(logger, annotations, constants.IgnoreGitPathsAnnotationKey, "ignore-git-paths"); ok {
		merged.IgnoreGitPaths = val
	}

	val, ok := parseBoolAnnotation(
		logger, annotations, constants.IgnoreUnsignedFilesAnnotationKey, "ignore-unsigned-files")
	if ok {
		merged.IgnoreUnsignedFiles = val
	}

	if val, ok := parseBoolAnnotation(logger, annotations, constants.AllowSymlinksAnnotationKey, "allow-symlinks"); ok {
		merged.AllowSymlinks = val
	}

	return *merged
}

// collectNeededPaths returns file paths the validation agent needs access to.
func collectNeededPaths(model v1alpha1.Model, cfg v1alpha1.ValidationConfig) []string {
	paths := []string{model.Path}
	if model.SignaturePath != "" {
		paths = append(paths, model.SignaturePath)
	}
	if cfg.PkiConfig != nil && cfg.PkiConfig.CertificateAuthority != "" {
		paths = append(paths, cfg.PkiConfig.CertificateAuthority)
	}
	if cfg.PublicKeyConfig != nil && cfg.PublicKeyConfig.KeyPath != "" {
		paths = append(paths, cfg.PublicKeyConfig.KeyPath)
	}
	if cfg.ClientTrustConfig != nil && cfg.ClientTrustConfig.TrustConfigPath != "" {
		paths = append(paths, cfg.ClientTrustConfig.TrustConfigPath)
	}
	return paths
}

// filterVolumeMounts returns only the mounts whose mountPath is a proper directory
// prefix of a needed path, all forced read-only. Mounts at "/" are excluded to
// prevent leaking the entire root filesystem into the validation container.
func filterVolumeMounts(containers []corev1.Container, neededPaths []string) []corev1.VolumeMount {
	seen := make(map[string]bool)
	var out []corev1.VolumeMount
	for _, c := range containers {
		for _, m := range c.VolumeMounts {
			if seen[m.MountPath] || m.MountPath == "/" {
				continue
			}
			prefix := m.MountPath
			if !strings.HasSuffix(prefix, "/") {
				prefix += "/"
			}
			for _, p := range neededPaths {
				if strings.HasPrefix(p, prefix) || p == m.MountPath {
					m.ReadOnly = true
					out = append(out, m)
					seen[m.MountPath] = true
					break
				}
			}
		}
	}
	return out
}

func webhookResult(resp admission.Response) string {
	if resp.Result == nil {
		return "success"
	}
	code := resp.Result.Code
	if code >= 200 && code < 300 {
		if resp.PatchType != nil {
			return "success"
		}
		return "skipped"
	}
	return "error"
}
