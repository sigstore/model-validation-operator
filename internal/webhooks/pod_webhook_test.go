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

// expectIgnorePathsWithValues is a helper function to verify --ignore-paths flag followed by expected paths
func expectIgnorePathsWithValues(args []string, expectedPaths ...string) {
	Expect(args).To(ContainElement("--ignore-paths"), "--ignore-paths flag should be present")

	foundPaths := make(map[string]bool)
	for i := 0; i < len(args); i++ {
		if args[i] == "--ignore-paths" {
			i++
			if i < len(args) {
				foundPaths[args[i]] = true
			}
		}
	}

	Expect(foundPaths).To(HaveLen(len(expectedPaths)),
		fmt.Sprintf("Expected %d paths but found %d", len(expectedPaths), len(foundPaths)))

	for _, expectedPath := range expectedPaths {
		Expect(foundPaths[expectedPath]).To(BeTrue(),
			fmt.Sprintf("Expected path '%s' should follow --ignore-paths flag", expectedPath))
	}
}

var _ = Describe("Pod webhook", func() {
	Context("Pod webhook test", func() {
		Name := "test"
		var Namespace string

		ctx := context.Background()

		var typeNamespaceName types.NamespacedName

		BeforeEach(func() {
			Namespace = fmt.Sprintf("test-ns-%d", time.Now().UnixNano())
			typeNamespaceName = testutil.CreateTestNamespacedName(Name, Namespace)

			By("Creating the Namespace to perform the tests")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: Namespace,
				},
			}
			err := k8sClient.Create(ctx, namespace)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation resource")
			mv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           Name,
				Namespace:      Namespace,
				ConfigType:     "sigstore",
				CertIdentity:   "test@example.com",
				CertOidcIssuer: "https://accounts.google.com",
				ModelPath:      "/path/to/model.onnx",
				SignaturePath:  "/path/to/model.onnx.sig",
			})
			err = k8sClient.Create(ctx, mv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, mv)
		})

		AfterEach(func() {
			// TODO(user): Attention if you improve this code by adding other context test you MUST
			// be aware of the current delete namespace limitations.
			// More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations

			By("Deleting the ModelValidation resource")
			_ = k8sClient.Delete(ctx, &v1alpha1.ModelValidation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      Name,
					Namespace: Namespace,
				},
			})

			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: Namespace}})
		})

		It("Should create sidecar container and add finalizer", func() {
			By("create labeled pod")
			instance := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      Name,
				Namespace: Namespace,
				Labels:    map[string]string{constants.ModelValidationLabel: Name},
			})
			err := k8sClient.Create(ctx, instance)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar was created")
			found := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) error {
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, 5*time.Second).Should(Succeed())

			Eventually(ctx,
				func(g Gomega, ctx context.Context) []corev1.Container {
					g.Expect(k8sClient.Get(ctx, typeNamespaceName, found)).To(Succeed())
					return found.Spec.InitContainers
				}, 5*time.Second,
			).Should(And(
				WithTransform(func(containers []corev1.Container) int { return len(containers) }, Equal(1)),
				WithTransform(
					func(containers []corev1.Container) string { return containers[0].Image },
					Equal(constants.ModelValidationAgentImage)),
			))

			By("Checking that finalizer was added")
			Expect(found.Finalizers).To(ContainElement(constants.ModelValidationFinalizer))
		})

		It("Should track pod in ModelValidation status", func() {
			By("create labeled pod")
			instance := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "tracked-pod",
				Namespace: Namespace,
				Labels:    map[string]string{constants.ModelValidationLabel: Name},
			})
			err := k8sClient.Create(ctx, instance)
			Expect(err).To(Not(HaveOccurred()))

			By("Waiting for pod to be injected")
			found := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: "tracked-pod", Namespace: Namespace}, found)
				return found.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			err = statusTracker.ProcessPodEvent(ctx, found)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking ModelValidation status was updated")
			mv := &v1alpha1.ModelValidation{}
			Eventually(ctx, func(ctx context.Context) int32 {
				_ = k8sClient.Get(ctx, typeNamespaceName, mv)
				return mv.Status.InjectedPodCount
			}, 5*time.Second).Should(BeNumerically(">", 0))

			Expect(mv.Status.AuthMethod).To(Equal("sigstore")) // Sigstore auth method configured in test
			Expect(mv.Status.InjectedPods).ToNot(BeEmpty())

			foundTrackedPod := false
			for _, tp := range mv.Status.InjectedPods {
				if tp.Name == "tracked-pod" {
					foundTrackedPod = true
					break
				}
			}
			Expect(foundTrackedPod).To(BeTrue(), "Pod should be tracked in status")
		})

		It("Should add trust_config argument when ClientTrustConfig is provided", func() {
			trustConfigName := "trust-config-test"
			trustConfigNamespace := fmt.Sprintf("trust-config-ns-%d", time.Now().UnixNano())

			By("Creating the Namespace for trust config test")
			trustConfigNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: trustConfigNamespace,
				},
			}
			err := k8sClient.Create(ctx, trustConfigNs)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation with ClientTrustConfig")
			trustMv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:            trustConfigName,
				Namespace:       trustConfigNamespace,
				ConfigType:      "sigstore",
				CertIdentity:    "trust@example.com",
				CertOidcIssuer:  "https://accounts.google.com",
				TrustConfigPath: "/path/to/trust-config.json",
			})
			err = k8sClient.Create(ctx, trustMv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, trustMv)

			By("create labeled pod with trust config")
			trustPod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "trust-config-pod",
				Namespace: trustConfigNamespace,
				Labels:    map[string]string{constants.ModelValidationLabel: trustConfigName},
			})
			err = k8sClient.Create(ctx, trustPod)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar was created with trust config")
			foundTrustPod := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "trust-config-pod",
					Namespace: trustConfigNamespace,
				}, foundTrustPod)
				return foundTrustPod.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			By("Verifying trust_config argument is present")
			initContainer := foundTrustPod.Spec.InitContainers[0]
			args := initContainer.Args
			Expect(args).To(ContainElement("--trust_config"))

			// Find the index of --trust_config and verify the next element is the path
			trustConfigIndex := -1
			for i, arg := range args {
				if arg == "--trust_config" {
					trustConfigIndex = i
					break
				}
			}
			Expect(trustConfigIndex).To(BeNumerically(">=", 0), "trust_config argument should be present")
			Expect(trustConfigIndex+1).To(BeNumerically("<", len(args)), "trust_config should have a value")
			Expect(args[trustConfigIndex+1]).To(Equal("/path/to/trust-config.json"))

			By("Cleanup trust config namespace")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: trustConfigNamespace}})
		})

		It("Should apply model options from CRD", func() {
			crdTestName := "crd-test"
			crdTestNamespace := fmt.Sprintf("crd-ns-%d", time.Now().UnixNano())

			By("Creating the Namespace for CRD test")
			crdNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: crdTestNamespace,
				},
			}
			err := k8sClient.Create(ctx, crdNs)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation with ignore options in CRD")
			ignoreGitPaths := true
			ignoreUnsignedFiles := false
			allowSymlinks := true
			crdMv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:                crdTestName,
				Namespace:           crdTestNamespace,
				ConfigType:          "sigstore",
				CertIdentity:        "crd@example.com",
				CertOidcIssuer:      "https://accounts.google.com",
				ModelPath:           "/path/to/crd/model.onnx",
				SignaturePath:       "/path/to/crd/model.onnx.sig",
				IgnorePaths:         []string{"/data/temp", "/data/.git"},
				IgnoreGitPaths:      &ignoreGitPaths,
				IgnoreUnsignedFiles: &ignoreUnsignedFiles,
				AllowSymlinks:       &allowSymlinks,
			})
			err = k8sClient.Create(ctx, crdMv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, crdMv)

			By("create pod without annotations")
			crdPod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "crd-pod",
				Namespace: crdTestNamespace,
				Labels:    map[string]string{constants.ModelValidationLabel: crdTestName},
			})
			err = k8sClient.Create(ctx, crdPod)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar was created with CRD options")
			foundCrdPod := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "crd-pod",
					Namespace: crdTestNamespace,
				}, foundCrdPod)
				return foundCrdPod.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			By("Verifying ignore options from CRD are present in arguments")
			initContainer := foundCrdPod.Spec.InitContainers[0]
			args := initContainer.Args

			expectIgnorePathsWithValues(args, "/data/temp", "/data/.git")
			Expect(args).To(ContainElement("--ignore-git-paths"))
			Expect(args).To(ContainElement("--no-ignore_unsigned_files"))
			Expect(args).To(ContainElement("--allow_symlinks"))

			By("Cleanup CRD namespace")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: crdTestNamespace}})
		})

		It("Should apply model options from pod annotations", func() {
			annotationsTestName := "annotations-test"
			annotationsTestNamespace := fmt.Sprintf("annotations-ns-%d", time.Now().UnixNano())

			By("Creating the Namespace for annotations test")
			annotationsNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: annotationsTestNamespace,
				},
			}
			err := k8sClient.Create(ctx, annotationsNs)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation without ignore options")
			annotationsMv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           annotationsTestName,
				Namespace:      annotationsTestNamespace,
				ConfigType:     "sigstore",
				CertIdentity:   "annotations@example.com",
				CertOidcIssuer: "https://accounts.google.com",
				ModelPath:      "/path/to/annotations/model.onnx",
				SignaturePath:  "/path/to/annotations/model.onnx.sig",
			})
			err = k8sClient.Create(ctx, annotationsMv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, annotationsMv)

			By("create pod with ignore annotations")
			annotationsPod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "annotations-pod",
				Namespace: annotationsTestNamespace,
				Labels:    map[string]string{constants.ModelValidationLabel: annotationsTestName},
				Annotations: map[string]string{
					constants.IgnorePathsAnnotationKey:         "/tmp,/cache",
					constants.IgnoreGitPathsAnnotationKey:      "true",
					constants.IgnoreUnsignedFilesAnnotationKey: "false",
					constants.AllowSymlinksAnnotationKey:       "true",
				},
			})
			err = k8sClient.Create(ctx, annotationsPod)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar was created with annotation options")
			foundAnnotationsPod := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "annotations-pod",
					Namespace: annotationsTestNamespace,
				}, foundAnnotationsPod)
				return foundAnnotationsPod.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			By("Verifying ignore options arguments are present")
			initContainer := foundAnnotationsPod.Spec.InitContainers[0]
			args := initContainer.Args

			expectIgnorePathsWithValues(args, "/tmp", "/cache")
			Expect(args).To(ContainElement("--ignore-git-paths"))
			Expect(args).To(ContainElement("--no-ignore_unsigned_files"))
			Expect(args).To(ContainElement("--allow_symlinks"))

			By("Cleanup annotations namespace")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: annotationsTestNamespace}})
		})

		It("Should handle edge cases in ignore-paths annotation", func() {
			edgeTestName := "edge-test"
			edgeTestNamespace := fmt.Sprintf("edge-ns-%d", time.Now().UnixNano())

			By("Creating the Namespace for edge case test")
			edgeNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: edgeTestNamespace,
				},
			}
			err := k8sClient.Create(ctx, edgeNs)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation without ignore options")
			edgeMv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           edgeTestName,
				Namespace:      edgeTestNamespace,
				ConfigType:     "sigstore",
				CertIdentity:   "edge@example.com",
				CertOidcIssuer: "https://accounts.google.com",
				ModelPath:      "/path/to/edge/model.onnx",
				SignaturePath:  "/path/to/edge/model.onnx.sig",
			})
			err = k8sClient.Create(ctx, edgeMv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, edgeMv)

			By("create pod with ignore-paths containing empty/whitespace entries")
			edgePod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "edge-pod",
				Namespace: edgeTestNamespace,
				Labels:    map[string]string{constants.ModelValidationLabel: edgeTestName},
				Annotations: map[string]string{
					// Mixed valid paths with empty strings and whitespace
					constants.IgnorePathsAnnotationKey: "/valid/path1, , /valid/path2,   ,/valid/path3",
				},
			})
			err = k8sClient.Create(ctx, edgePod)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar filters empty entries")
			foundEdgePod := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "edge-pod",
					Namespace: edgeTestNamespace,
				}, foundEdgePod)
				return foundEdgePod.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			By("Verifying only valid paths are present in arguments")
			initContainer := foundEdgePod.Spec.InitContainers[0]
			args := initContainer.Args

			expectIgnorePathsWithValues(args, "/valid/path1", "/valid/path2", "/valid/path3")

			By("Cleanup edge namespace")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: edgeTestNamespace}})
		})

		It("Should handle all-empty ignore-paths annotation", func() {
			allEmptyTestName := "all-empty-test"
			allEmptyTestNamespace := fmt.Sprintf("all-empty-ns-%d", time.Now().UnixNano())

			By("Creating the Namespace for all-empty test")
			allEmptyNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: allEmptyTestNamespace,
				},
			}
			err := k8sClient.Create(ctx, allEmptyNs)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation without ignore options")
			allEmptyMv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           allEmptyTestName,
				Namespace:      allEmptyTestNamespace,
				ConfigType:     "sigstore",
				CertIdentity:   "allempty@example.com",
				CertOidcIssuer: "https://accounts.google.com",
				ModelPath:      "/path/to/allempty/model.onnx",
				SignaturePath:  "/path/to/allempty/model.onnx.sig",
			})
			err = k8sClient.Create(ctx, allEmptyMv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, allEmptyMv)

			By("create pod with ignore-paths containing only empty/whitespace entries")
			allEmptyPod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "all-empty-pod",
				Namespace: allEmptyTestNamespace,
				Labels:    map[string]string{constants.ModelValidationLabel: allEmptyTestName},
				Annotations: map[string]string{
					constants.IgnorePathsAnnotationKey: " , , ,   ",
				},
			})
			err = k8sClient.Create(ctx, allEmptyPod)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar was created")
			foundAllEmptyPod := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "all-empty-pod",
					Namespace: allEmptyTestNamespace,
				}, foundAllEmptyPod)
				return foundAllEmptyPod.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			By("Verifying no ignore-paths arguments are present")
			initContainer := foundAllEmptyPod.Spec.InitContainers[0]
			args := initContainer.Args

			Expect(args).ToNot(ContainElement("--ignore-paths"))

			By("Cleanup all-empty namespace")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: allEmptyTestNamespace}})
		})

		It("Should handle boolean annotations with whitespace", func() {
			whitespaceTestName := "whitespace-test"
			whitespaceTestNamespace := fmt.Sprintf("whitespace-ns-%d", time.Now().UnixNano())

			By("Creating the Namespace for whitespace test")
			whitespaceNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: whitespaceTestNamespace,
				},
			}
			err := k8sClient.Create(ctx, whitespaceNs)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation without ignore options")
			whitespaceMv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           whitespaceTestName,
				Namespace:      whitespaceTestNamespace,
				ConfigType:     "sigstore",
				CertIdentity:   "whitespace@example.com",
				CertOidcIssuer: "https://accounts.google.com",
				ModelPath:      "/path/to/whitespace/model.onnx",
				SignaturePath:  "/path/to/whitespace/model.onnx.sig",
			})
			err = k8sClient.Create(ctx, whitespaceMv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, whitespaceMv)

			By("create pod with boolean annotations containing whitespace")
			whitespacePod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "whitespace-pod",
				Namespace: whitespaceTestNamespace,
				Labels:    map[string]string{constants.ModelValidationLabel: whitespaceTestName},
				Annotations: map[string]string{
					constants.IgnoreGitPathsAnnotationKey:      " true ",
					constants.IgnoreUnsignedFilesAnnotationKey: "  false  ",
					constants.AllowSymlinksAnnotationKey:       " true",
				},
			})
			err = k8sClient.Create(ctx, whitespacePod)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar was created")
			foundWhitespacePod := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "whitespace-pod",
					Namespace: whitespaceTestNamespace,
				}, foundWhitespacePod)
				return foundWhitespacePod.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			By("Verifying boolean flags are correctly parsed despite whitespace")
			initContainer := foundWhitespacePod.Spec.InitContainers[0]
			args := initContainer.Args

			Expect(args).To(ContainElement("--ignore-git-paths"))
			Expect(args).To(ContainElement("--no-ignore_unsigned_files"))
			Expect(args).To(ContainElement("--allow_symlinks"))

			By("Cleanup whitespace namespace")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: whitespaceTestNamespace}})
		})

		It("Should override CRD model options with pod annotations", func() {
			overrideTestName := "override-test"
			overrideTestNamespace := fmt.Sprintf("override-ns-%d", time.Now().UnixNano())

			By("Creating the Namespace for override test")
			overrideNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: overrideTestNamespace,
				},
			}
			err := k8sClient.Create(ctx, overrideNs)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation with ignore options in CRD")
			ignoreGitPathsCrd := false
			ignoreUnsignedFilesCrd := true
			allowSymlinksCrd := false
			overrideMv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:                overrideTestName,
				Namespace:           overrideTestNamespace,
				ConfigType:          "sigstore",
				CertIdentity:        "override@example.com",
				CertOidcIssuer:      "https://accounts.google.com",
				ModelPath:           "/path/to/override/model.onnx",
				SignaturePath:       "/path/to/override/model.onnx.sig",
				IgnorePaths:         []string{"/crd/path1", "/crd/path2"},
				IgnoreGitPaths:      &ignoreGitPathsCrd,
				IgnoreUnsignedFiles: &ignoreUnsignedFilesCrd,
				AllowSymlinks:       &allowSymlinksCrd,
			})
			err = k8sClient.Create(ctx, overrideMv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, overrideMv)

			By("create pod with annotations that override CRD values")
			overridePod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "override-pod",
				Namespace: overrideTestNamespace,
				Labels:    map[string]string{constants.ModelValidationLabel: overrideTestName},
				Annotations: map[string]string{
					constants.IgnorePathsAnnotationKey:         "/annotation/path1,/annotation/path2",
					constants.IgnoreGitPathsAnnotationKey:      "true",  // opposite of CRD
					constants.IgnoreUnsignedFilesAnnotationKey: "false", // opposite of CRD
					constants.AllowSymlinksAnnotationKey:       "true",  // opposite of CRD
				},
			})
			err = k8sClient.Create(ctx, overridePod)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that validation sidecar was created with annotation options")
			foundOverridePod := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) []corev1.Container {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "override-pod",
					Namespace: overrideTestNamespace,
				}, foundOverridePod)
				return foundOverridePod.Spec.InitContainers
			}, 5*time.Second).Should(HaveLen(1))

			By("Verifying annotation values override CRD values")
			initContainer := foundOverridePod.Spec.InitContainers[0]
			args := initContainer.Args

			expectIgnorePathsWithValues(args, "/annotation/path1", "/annotation/path2")
			Expect(args).ToNot(ContainElement("/crd/path1"))
			Expect(args).ToNot(ContainElement("/crd/path2"))

			Expect(args).To(ContainElement("--ignore-git-paths"))
			Expect(args).ToNot(ContainElement("--no-ignore-git-paths"))

			Expect(args).To(ContainElement("--no-ignore_unsigned_files"))
			Expect(args).ToNot(ContainElement("--ignore_unsigned_files"))

			Expect(args).To(ContainElement("--allow_symlinks"))

			By("Cleanup override namespace")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: overrideTestNamespace}})
		})

		It("Should use configured ImagePullPolicy for init container", func() {
			pullPolicyTestName := "pull-policy-test"
			pullPolicyTestNamespace := fmt.Sprintf("pull-policy-ns-%d", time.Now().UnixNano())

			By("Creating the Namespace for pull policy test")
			pullPolicyNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: pullPolicyTestNamespace,
				},
			}
			err := k8sClient.Create(ctx, pullPolicyNs)
			Expect(err).To(Not(HaveOccurred()))

			By("Create ModelValidation with IfNotPresent ImagePullPolicy")
			pullPolicyMv := testutil.CreateTestModelValidation(testutil.TestModelValidationOptions{
				Name:           pullPolicyTestName,
				Namespace:      pullPolicyTestNamespace,
				ConfigType:     "sigstore",
				CertIdentity:   "pullpolicy@example.com",
				CertOidcIssuer: "https://accounts.google.com",
			})
			pullPolicyMv.Spec.ImagePullPolicy = corev1.PullIfNotPresent
			err = k8sClient.Create(ctx, pullPolicyMv)
			Expect(err).To(Not(HaveOccurred()))

			statusTracker.AddModelValidation(ctx, pullPolicyMv)

			By("create labeled pod")
			pullPolicyPod := testutil.CreateTestPod(testutil.TestPodOptions{
				Name:      "pull-policy-pod",
				Namespace: pullPolicyTestNamespace,
				Labels:    map[string]string{constants.ModelValidationLabel: pullPolicyTestName},
			})
			err = k8sClient.Create(ctx, pullPolicyPod)
			Expect(err).To(Not(HaveOccurred()))

			By("Checking that init container has IfNotPresent pull policy")
			foundPullPolicyPod := &corev1.Pod{}
			Eventually(ctx, func(ctx context.Context) corev1.PullPolicy {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "pull-policy-pod",
					Namespace: pullPolicyTestNamespace,
				}, foundPullPolicyPod)
				if len(foundPullPolicyPod.Spec.InitContainers) == 0 {
					return ""
				}
				return foundPullPolicyPod.Spec.InitContainers[0].ImagePullPolicy
			}, 5*time.Second).Should(Equal(corev1.PullIfNotPresent))

			By("Cleanup pull policy namespace")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: pullPolicyTestNamespace}})
		})
	})
})
