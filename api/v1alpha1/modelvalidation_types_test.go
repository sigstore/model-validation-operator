package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ModelValidation", func() {

	// A helper function to generate a valid ModelValidation object for Sigstore.
	generateSigstoreObject := func(name string) *ModelValidation {
		return &ModelValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: ModelValidationSpec{
				Model: Model{
					Path:          "/path/to/model.onnx",
					SignaturePath: "/path/to/model.onnx.sig",
				},
				Config: ValidationConfig{
					SigstoreConfig: &SigstoreConfig{
						CertificateIdentity:   "email:test@example.com",
						CertificateOidcIssuer: "https://accounts.google.com",
					},
				},
			},
		}
	}

	// A helper function to generate a valid ModelValidation object for PKI.
	generatePkiObject := func(name string) *ModelValidation {
		return &ModelValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: ModelValidationSpec{
				Model: Model{
					Path:          "/path/to/model.onnx",
					SignaturePath: "/path/to/model.onnx.sig",
				},
				Config: ValidationConfig{
					PkiConfig: &PkiConfig{
						CertificateAuthority: "/path/to/ca.pem",
					},
				},
			},
		}
	}

	// A helper function to generate a valid ModelValidation object for PublicKey.
	generatePublicKeyObject := func(name string) *ModelValidation {
		return &ModelValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: ModelValidationSpec{
				Model: Model{
					Path:          "/path/to/model.onnx",
					SignaturePath: "/path/to/model.onnx.sig",
				},
				Config: ValidationConfig{
					PublicKeyConfig: &PublicKeyConfig{
						KeyPath: "/path/to/publickey.pem",
					},
				},
			},
		}
	}

	Context("ModelValidationSpec", func() {
		It("can be created and fetched successfully for Sigstore config", func() {
			created := generateSigstoreObject("mv-create")
			Expect(k8sClient.Create(context.Background(), created)).To(Succeed())

			fetched := &ModelValidation{}
			Expect(k8sClient.Get(context.Background(), getKey(created), fetched)).To(Succeed())
			Expect(fetched).To(Equal(created))
		})

		It("can be created and fetched successfully for PKI config", func() {
			created := generatePkiObject("mv-create-pki")
			Expect(k8sClient.Create(context.Background(), created)).To(Succeed())
		})

		It("can be created and fetched successfully for PublicKey config", func() {
			created := generatePublicKeyObject("mv-create-publickey")
			Expect(k8sClient.Create(context.Background(), created)).To(Succeed())
		})

		It("can be updated with allowed fields", func() {
			created := generateSigstoreObject("mv-update")
			Expect(k8sClient.Create(context.Background(), created)).To(Succeed())

			fetched := &ModelValidation{}
			Expect(k8sClient.Get(context.Background(), getKey(created), fetched)).To(Succeed())
			Expect(fetched).To(Equal(created))

			// Status is not immutable and can be updated
			fetched.Status.Conditions = []metav1.Condition{
				{
					Type:               "Ready",
					Status:             "True",
					LastTransitionTime: metav1.Now(),
					Reason:             "ValidationSuccess",
					Message:            "Model signature is valid",
				},
			}
			Expect(k8sClient.Status().Update(context.Background(), fetched)).To(Succeed())
		})

		It("can be deleted", func() {
			created := generateSigstoreObject("mv-delete")
			Expect(k8sClient.Create(context.Background(), created)).To(Succeed())

			Expect(k8sClient.Delete(context.Background(), created)).To(Succeed())
			Expect(k8sClient.Get(context.Background(), getKey(created), created)).ToNot(Succeed())
		})

		Context("is validated", func() {
			It("rejects an empty Model path", func() {
				invalidObject := generateSigstoreObject("model-path-invalid")
				invalidObject.Spec.Model.Path = ""

				err := k8sClient.Create(context.Background(), invalidObject)
				Expect(apierrors.IsInvalid(err)).To(BeTrue())
				Expect(err).To(MatchError(ContainSubstring("spec.model.path: Invalid value: \"\"")))
			})

			It("rejects an empty Signature path", func() {
				invalidObject := generateSigstoreObject("signature-path-invalid")
				invalidObject.Spec.Model.SignaturePath = ""

				err := k8sClient.Create(context.Background(), invalidObject)
				Expect(apierrors.IsInvalid(err)).To(BeTrue())
				Expect(err).To(MatchError(ContainSubstring("spec.model.signaturePath: Invalid value: \"\"")))
			})

			It("rejects multiple configs (XValidation violation)", func() {
				invalidObject := generateSigstoreObject("xor-violation-multi")
				invalidObject.Spec.Config.PkiConfig = &PkiConfig{
					CertificateAuthority: "/path/to/ca.pem",
				}

				err := k8sClient.Create(context.Background(), invalidObject)
				Expect(apierrors.IsInvalid(err)).To(BeTrue())
				Expect(err).To(MatchError(ContainSubstring("exactly one validation method must be specified")))
			})

			It("rejects zero configs (XValidation violation)", func() {
				invalidObject := generateSigstoreObject("xor-violation-zero")
				invalidObject.Spec.Config.SigstoreConfig = nil

				err := k8sClient.Create(context.Background(), invalidObject)
				Expect(apierrors.IsInvalid(err)).To(BeTrue())
				Expect(err).To(MatchError(ContainSubstring("exactly one validation method must be specified")))
			})

			It("rejects a missing required field in SigstoreConfig", func() {
				invalidObject := generateSigstoreObject("sigstore-missing-field")
				invalidObject.Spec.Config.SigstoreConfig.CertificateIdentity = ""

				err := k8sClient.Create(context.Background(), invalidObject)
				Expect(apierrors.IsInvalid(err)).To(BeTrue())
				Expect(err).To(MatchError(ContainSubstring("spec.config.sigstoreConfig.certificateIdentity: Required value")))
			})

			It("rejects a missing required field in PkiConfig", func() {
				invalidObject := generatePkiObject("pki-missing-field")
				invalidObject.Spec.Config.PkiConfig.CertificateAuthority = ""

				err := k8sClient.Create(context.Background(), invalidObject)
				Expect(apierrors.IsInvalid(err)).To(BeTrue())
				Expect(err).To(MatchError(ContainSubstring("spec.config.pkiConfig.certificateAuthority: Required value")))
			})

			It("rejects a missing required field in PublicKeyConfig", func() {
				invalidObject := generatePublicKeyObject("publickey-missing-field")
				invalidObject.Spec.Config.PublicKeyConfig.KeyPath = ""

				err := k8sClient.Create(context.Background(), invalidObject)
				Expect(apierrors.IsInvalid(err)).To(BeTrue())
				Expect(err).To(MatchError(ContainSubstring("spec.config.publicKeyConfig.keyPath: Required value")))
			})

			It("allows an update to the Model path", func() {
				created := generateSigstoreObject("mutable-model-test")
				Expect(k8sClient.Create(context.Background(), created)).To(Succeed())

				fetched := &ModelValidation{}
				Expect(k8sClient.Get(context.Background(), getKey(created), fetched)).To(Succeed())

				newPath := "/new/path/to/model.onnx"
				fetched.Spec.Model.Path = newPath
				Expect(k8sClient.Update(context.Background(), fetched)).To(Succeed())

				// Fetch and verify the change
				updated := &ModelValidation{}
				Expect(k8sClient.Get(context.Background(), getKey(created), updated)).To(Succeed())
				Expect(updated.Spec.Model.Path).To(Equal(newPath))
			})

			It("allows an update to the Config fields", func() {
				created := generateSigstoreObject("mutable-config-test")
				Expect(k8sClient.Create(context.Background(), created)).To(Succeed())

				fetched := &ModelValidation{}
				Expect(k8sClient.Get(context.Background(), getKey(created), fetched)).To(Succeed())

				// Update the config from Sigstore to PKI
				fetched.Spec.Config.SigstoreConfig = nil
				fetched.Spec.Config.PkiConfig = &PkiConfig{
					CertificateAuthority: "new-ca-path",
				}
				Expect(k8sClient.Update(context.Background(), fetched)).To(Succeed())

				// Fetch and verify the change
				updated := &ModelValidation{}
				Expect(k8sClient.Get(context.Background(), getKey(created), updated)).To(Succeed())
				Expect(updated.Spec.Config.SigstoreConfig).To(BeNil())
				Expect(updated.Spec.Config.PkiConfig).ToNot(BeNil())
				Expect(updated.Spec.Config.PkiConfig.CertificateAuthority).To(Equal("new-ca-path"))
			})
		})
	})
})
