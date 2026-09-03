package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive

	"github.com/sigstore/model-validation-operator/internal/testutil"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("InstallNetworkPolicy", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "test-operator-ns"
	})

	Context("when the NetworkPolicy does not exist", func() {
		It("should create the NetworkPolicy with the correct spec", func() {
			fakeClient := testutil.SetupFakeClientWithObjects()

			err := InstallNetworkPolicy(ctx, fakeClient, namespace)
			Expect(err).NotTo(HaveOccurred())

			np := &networkingv1.NetworkPolicy{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: networkPolicyName, Namespace: namespace}, np)
			Expect(err).NotTo(HaveOccurred())

			Expect(np.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "model-validation-operator"))
			Expect(np.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "model-validation-operator"))

			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("control-plane", "controller-manager"))
			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/name", "model-validation-operator"))

			Expect(np.Spec.PolicyTypes).To(ConsistOf(
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			))

			Expect(np.Spec.Ingress).To(HaveLen(3))

			By("webhook ingress restricted by port only (apiserver uses host network)")
			Expect(np.Spec.Ingress[0].From).To(BeEmpty())
			expectPort(np.Spec.Ingress[0].Ports, 9443, corev1.ProtocolTCP)

			By("health probe ingress scoped to same-namespace pods")
			Expect(np.Spec.Ingress[1].From).To(HaveLen(1))
			Expect(np.Spec.Ingress[1].From[0].PodSelector).NotTo(BeNil())
			expectPort(np.Spec.Ingress[1].Ports, 8081, corev1.ProtocolTCP)

			By("metrics ingress scoped to same-namespace pods and metrics-enabled namespaces")
			Expect(np.Spec.Ingress[2].From).To(HaveLen(2))
			Expect(np.Spec.Ingress[2].From[0].PodSelector).NotTo(BeNil())
			Expect(np.Spec.Ingress[2].From[1].NamespaceSelector).NotTo(BeNil())
			Expect(np.Spec.Ingress[2].From[1].NamespaceSelector.MatchLabels).To(HaveKeyWithValue("metrics", "enabled"))
			expectPort(np.Spec.Ingress[2].Ports, 8443, corev1.ProtocolTCP)

			By("DNS egress scoped to kube-system and openshift-dns")
			Expect(np.Spec.Egress).To(HaveLen(2))
			Expect(np.Spec.Egress[0].To).To(HaveLen(2))
			Expect(np.Spec.Egress[0].To[0].NamespaceSelector.MatchLabels).To(
				HaveKeyWithValue("kubernetes.io/metadata.name", "kube-system"))
			Expect(np.Spec.Egress[0].To[1].NamespaceSelector.MatchLabels).To(
				HaveKeyWithValue("kubernetes.io/metadata.name", "openshift-dns"))
			expectPort(np.Spec.Egress[0].Ports, 53, corev1.ProtocolTCP)
			expectPort(np.Spec.Egress[0].Ports, 53, corev1.ProtocolUDP)

			By("API server egress restricted by port only (apiserver uses host network)")
			Expect(np.Spec.Egress[1].To).To(BeEmpty())
			expectPort(np.Spec.Egress[1].Ports, 443, corev1.ProtocolTCP)
			expectPort(np.Spec.Egress[1].Ports, 6443, corev1.ProtocolTCP)
		})
	})

	Context("when the NetworkPolicy already exists with wrong spec", func() {
		It("should update the NetworkPolicy to the desired spec", func() {
			existingNP := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      networkPolicyName,
					Namespace: namespace,
				},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				},
			}

			fakeClient := testutil.SetupFakeClientWithObjects(existingNP)

			err := InstallNetworkPolicy(ctx, fakeClient, namespace)
			Expect(err).NotTo(HaveOccurred())

			np := &networkingv1.NetworkPolicy{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: networkPolicyName, Namespace: namespace}, np)
			Expect(err).NotTo(HaveOccurred())

			Expect(np.Spec.PolicyTypes).To(ConsistOf(
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			))
			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("control-plane", "controller-manager"))
			Expect(np.Spec.Ingress).To(HaveLen(3))
			Expect(np.Spec.Egress).To(HaveLen(2))
		})
	})

	Context("when the NetworkPolicy already has the correct spec", func() {
		It("should be idempotent and not error", func() {
			fakeClient := testutil.SetupFakeClientWithObjects()

			err := InstallNetworkPolicy(ctx, fakeClient, namespace)
			Expect(err).NotTo(HaveOccurred())

			err = InstallNetworkPolicy(ctx, fakeClient, namespace)
			Expect(err).NotTo(HaveOccurred())

			np := &networkingv1.NetworkPolicy{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: networkPolicyName, Namespace: namespace}, np)
			Expect(err).NotTo(HaveOccurred())
			Expect(np.Spec.Ingress).To(HaveLen(3))
			Expect(np.Spec.Egress).To(HaveLen(2))
		})
	})
})

func expectPort(ports []networkingv1.NetworkPolicyPort, portNum int32, protocol corev1.Protocol) {
	port := intstr.FromInt32(portNum)
	found := false
	for _, p := range ports {
		if p.Port != nil && *p.Port == port && p.Protocol != nil && *p.Protocol == protocol {
			found = true
			break
		}
	}
	ExpectWithOffset(1, found).To(BeTrue(), "expected port %d/%s in ports %v", portNum, protocol, ports)
}
