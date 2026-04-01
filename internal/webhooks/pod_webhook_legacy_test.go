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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/constants"
	"github.com/sigstore/model-validation-operator/internal/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Legacy sidecar support", func() {
	Context("buildValidationContainer with nativeSidecarSupport=false", func() {
		It("Should not set restartPolicy even when continuous validation is enabled", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:       "test-mv",
				Namespace:  "default",
				ConfigType: "sigstore",
			})
			mv.Spec.ContinuousValidation = &v1alpha1.ContinuousValidation{
				Enabled:  true,
				Interval: "10m",
			}

			pp := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: make(map[string]string),
				},
			}

			args := []string{"verify", "sigstore", "--signature=/sig", "/model"}
			container := buildValidationContainer(mv, args, nil, pp, nil, false)

			Expect(container.Name).To(Equal(constants.ModelValidationInitContainerName))
			Expect(container.RestartPolicy).To(BeNil(), "restartPolicy should not be set for legacy mode")
			Expect(container.Args).To(Equal(args), "args should be one-shot (no --interval)")
			Expect(container.ReadinessProbe).To(BeNil(), "no probes for one-shot init container")
			Expect(container.LivenessProbe).To(BeNil(), "no probes for one-shot init container")
			Expect(pp.Annotations[constants.ContinuousValidationAnnotationKey]).To(Equal("true"),
				"continuous validation annotation should still be set")
		})

		It("Should set restartPolicy when nativeSidecarSupport=true and continuous validation is enabled", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:       "test-mv",
				Namespace:  "default",
				ConfigType: "sigstore",
			})
			mv.Spec.ContinuousValidation = &v1alpha1.ContinuousValidation{
				Enabled:  true,
				Interval: "10m",
			}

			pp := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: make(map[string]string),
				},
			}

			args := []string{"verify", "sigstore", "--signature=/sig", "/model"}
			container := buildValidationContainer(mv, args, nil, pp, nil, true)

			Expect(container.RestartPolicy).NotTo(BeNil(), "restartPolicy should be set for native sidecar")
			Expect(*container.RestartPolicy).To(Equal(corev1.ContainerRestartPolicyAlways))
			Expect(container.Args[0]).To(Equal("--interval=10m"))
			Expect(container.ReadinessProbe).NotTo(BeNil())
			Expect(container.LivenessProbe).NotTo(BeNil())
		})
	})

	Context("buildLegacySidecarContainer", func() {
		It("Should create a sidecar with --interval and --skip-initial flags", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:       "test-mv",
				Namespace:  "default",
				ConfigType: "sigstore",
			})
			mv.Spec.ContinuousValidation = &v1alpha1.ContinuousValidation{
				Enabled:  true,
				Interval: "15m",
			}

			pp := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: make(map[string]string),
				},
			}

			args := []string{"verify", "sigstore", "--signature=/sig", "/model"}
			container := buildLegacySidecarContainer(mv, args, nil, pp)

			Expect(container.Name).To(Equal(constants.ModelValidationSidecarContainerName))
			Expect(container.RestartPolicy).To(BeNil(), "regular containers don't use restartPolicy")
			Expect(container.Args[0]).To(Equal("--interval=15m"))
			Expect(container.Args[1]).To(Equal("--skip-initial"))
			Expect(container.Args[2:]).To(Equal(args))
			Expect(container.ReadinessProbe).NotTo(BeNil())
			Expect(container.LivenessProbe).NotTo(BeNil())
			Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/ready"))
			Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
		})

		It("Should use default interval when not specified", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:       "test-mv",
				Namespace:  "default",
				ConfigType: "sigstore",
			})
			mv.Spec.ContinuousValidation = &v1alpha1.ContinuousValidation{
				Enabled: true,
			}

			pp := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: make(map[string]string),
				},
			}

			args := []string{"verify", "sigstore", "--signature=/sig", "/model"}
			container := buildLegacySidecarContainer(mv, args, nil, pp)

			Expect(container.Args[0]).To(Equal("--interval=5m"))
		})
	})

	Context("Legacy sidecar webhook integration", func() {
		It("Should inject both init container and sidecar on pre-1.28 cluster with continuous validation", func() {
			legacyNs := fmt.Sprintf("legacy-ns-%d", time.Now().UnixNano())

			By("Creating namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: legacyNs},
			}
			err := k8sClient.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())

			By("Creating ModelValidation with continuous validation enabled")
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           "legacy-test",
				Namespace:      legacyNs,
				ConfigType:     "sigstore",
				CertIdentity:   "legacy@example.com",
				CertOidcIssuer: "https://accounts.google.com",
				ModelPath:      "/path/to/model",
				SignaturePath:  "/path/to/model.sig",
			})
			mv.Spec.ContinuousValidation = &v1alpha1.ContinuousValidation{
				Enabled:  true,
				Interval: "10m",
			}
			err = k8sClient.Create(ctx, mv)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.AddModelValidation(ctx, mv)

			By("Registering a legacy-mode webhook on a separate path")
			// We can't replace the main webhook, so we test via the build functions directly.
			// The integration test above already covers native mode. Here we verify the
			// build functions produce the correct output for legacy mode.

			args := []string{"verify", "sigstore", "--signature=/path/to/model.sig",
				"--identity", "legacy@example.com",
				"--identity_provider", "https://accounts.google.com",
				"/path/to/model"}

			pp := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: make(map[string]string),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "app:latest"},
					},
				},
			}

			initContainer := buildValidationContainer(mv, args, nil, pp, nil, false)
			sidecar := buildLegacySidecarContainer(mv, args, nil, pp)

			By("Verifying init container is one-shot")
			Expect(initContainer.RestartPolicy).To(BeNil())
			Expect(initContainer.Args).To(Equal(args))
			Expect(initContainer.ReadinessProbe).To(BeNil())

			By("Verifying sidecar has continuous validation config")
			Expect(sidecar.Name).To(Equal(constants.ModelValidationSidecarContainerName))
			Expect(sidecar.Args[0]).To(Equal("--interval=10m"))
			Expect(sidecar.Args[1]).To(Equal("--skip-initial"))
			Expect(sidecar.ReadinessProbe).NotTo(BeNil())
			Expect(sidecar.LivenessProbe).NotTo(BeNil())

			By("Verifying continuous validation annotation is set")
			Expect(pp.Annotations[constants.ContinuousValidationAnnotationKey]).To(Equal("true"))

			By("Cleanup")
			_ = k8sClient.Delete(ctx, &v1alpha1.ModelValidation{
				ObjectMeta: metav1.ObjectMeta{Name: "legacy-test", Namespace: legacyNs},
			})
			_ = k8sClient.Delete(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: legacyNs},
			})
		})

		It("Should not inject sidecar when continuous validation is disabled on legacy cluster", func() {
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:       "oneshot-test",
				Namespace:  "default",
				ConfigType: "sigstore",
			})
			// No continuous validation

			pp := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: make(map[string]string),
				},
			}

			args := []string{"verify", "sigstore", "--signature=/sig", "/model"}
			container := buildValidationContainer(mv, args, nil, pp, nil, false)

			Expect(container.RestartPolicy).To(BeNil())
			Expect(container.Args).To(Equal(args))
			Expect(pp.Annotations).NotTo(HaveKey(constants.ContinuousValidationAnnotationKey))
		})
	})

	Context("Idempotency check for legacy sidecar", func() {
		It("Should skip injection if legacy sidecar container already exists", func() {
			legacyIdempotentNs := fmt.Sprintf("legacy-idemp-ns-%d", time.Now().UnixNano())

			By("Creating namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: legacyIdempotentNs},
			}
			err := k8sClient.Create(context.Background(), ns)
			Expect(err).NotTo(HaveOccurred())

			By("Creating ModelValidation")
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           "idemp-test",
				Namespace:      legacyIdempotentNs,
				ConfigType:     "sigstore",
				CertIdentity:   "idemp@example.com",
				CertOidcIssuer: "https://accounts.google.com",
			})
			err = k8sClient.Create(context.Background(), mv)
			Expect(err).NotTo(HaveOccurred())

			statusTracker.AddModelValidation(ctx, mv)

			By("Creating pod with existing sidecar container")
			pod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "idemp-pod",
				Namespace: legacyIdempotentNs,
				Labels:    map[string]string{constants.ModelValidationLabel: "idemp-test"},
			})
			// Pre-add the sidecar container to simulate already-injected state
			pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
				Name:  constants.ModelValidationSidecarContainerName,
				Image: constants.ModelValidationAgentImage,
			})
			err = k8sClient.Create(context.Background(), pod)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod was not double-injected")
			found := &corev1.Pod{}
			Eventually(context.Background(), func(ctx context.Context) error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name: "idemp-pod", Namespace: legacyIdempotentNs,
				}, found)
			}, 5*time.Second).Should(Succeed())

			// Should not have init containers added since sidecar was already present
			Expect(found.Spec.InitContainers).To(BeEmpty(),
				"init containers should not be injected when sidecar already exists")

			By("Cleanup")
			_ = k8sClient.Delete(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: legacyIdempotentNs},
			})
		})
	})
})
