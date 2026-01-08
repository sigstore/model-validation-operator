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
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/go-logr/logr"
	"github.com/sigstore/model-validation-operator/api/v1alpha1"
)

// NewPodInterceptor creates a new pod mutating webhook to be registered
func NewPodInterceptor(c client.Client, decoder admission.Decoder) webhook.AdmissionHandler {
	return &podInterceptor{
		client:  c,
		decoder: decoder,
	}
}

//nolint:lll
// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=fail,groups="",resources=pods,sideEffects=None,verbs=create;update,versions=v1,name=pods.validation.ml.sigstore.dev,admissionReviewVersions=v1

// +kubebuilder:rbac:groups=ml.sigstore.dev,resources=modelvalidations,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// podInterceptor extends pods with Model Validation Init-Container if annotation is specified.
type podInterceptor struct {
	client  client.Client
	decoder admission.Decoder
}

// Handle extends pods with Model Validation Init-Container if annotation is specified.
func (p *podInterceptor) Handle(ctx context.Context, req admission.Request) admission.Response {
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

	logger.Info("Checking pod labels", "labels", pod.Labels)
	modelValidationName, ok := pod.Labels[constants.ModelValidationLabel]
	if !ok || modelValidationName == "" {
		logger.Info("ModelValidation label not found or empty, skipping injection")
		return admission.Allowed("no ModelValidation label found, no action needed")
	}
	logger.Info("ModelValidation label found, proceeding with injection", "modelValidationName", modelValidationName)

	logger.Info("Search associated Model Validation CR", "pod", pod.Name, "namespace", pod.Namespace,
		"modelValidationName", modelValidationName)
	mv := &v1alpha1.ModelValidation{}
	err := p.client.Get(ctx, client.ObjectKey{Name: modelValidationName, Namespace: pod.Namespace}, mv)
	if err != nil {
		msg := fmt.Sprintf("failed to get the ModelValidation CR %s/%s", pod.Namespace, modelValidationName)
		logger.Error(err, msg)
		return admission.Errored(http.StatusBadRequest, err) // Fail deployment if CR not found
	}
	// NOTE: check if validation sidecar is already injected. Then no action needed.
	for _, c := range pod.Spec.InitContainers {
		if c.Name == constants.ModelValidationInitContainerName {
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

	vm := []corev1.VolumeMount{}
	for _, c := range pod.Spec.Containers {
		vm = append(vm, c.VolumeMounts...)
	}
	pp.Spec.InitContainers = append(pp.Spec.InitContainers, corev1.Container{
		Name:            constants.ModelValidationInitContainerName,
		ImagePullPolicy: corev1.PullAlways,
		Image:           constants.ModelTransparencyCliImage,
		Command:         []string{"/usr/local/bin/model_signing"},
		Args:            args,
		VolumeMounts:    vm,
	})
	marshaledPod, err := json.Marshal(pp)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func validationConfigToArgs(logger logr.Logger, cfg v1alpha1.ValidationConfig, model v1alpha1.Model) []string {
	logger.Info("construct args")
	res := []string{}
	if cfg.SigstoreConfig != nil {
		logger.Info("found sigstore config")
		res = append(res,
			"sigstore",
			fmt.Sprintf("--signature=%s", model.SignaturePath),
			"--identity", cfg.SigstoreConfig.CertificateIdentity,
			"--identity_provider", cfg.SigstoreConfig.CertificateOidcIssuer,
		)
	} else if cfg.PublicKeyConfig != nil {
		logger.Info("found public-key config")
		res = append(res,
			"key",
			fmt.Sprintf("--signature=%s", model.SignaturePath),
			"--public_key", cfg.PublicKeyConfig.KeyPath,
		)
	} else if cfg.PkiConfig != nil {
		logger.Info("found pki config")
		res = append(res,
			"certificate",
			fmt.Sprintf("--signature=%s", model.SignaturePath),
			"--certificate_chain", cfg.PkiConfig.CertificateAuthority,
		)
	} else {
		logger.Info("missing validation config")
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
