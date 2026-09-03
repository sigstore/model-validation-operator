package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const networkPolicyName = "controller-manager"

// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;create;update;patch

// InstallNetworkPolicy creates or updates the operator's NetworkPolicy once at startup.
func InstallNetworkPolicy(ctx context.Context, c client.Client, namespace string) error {
	logger := log.FromContext(ctx)

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      networkPolicyName,
			Namespace: namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, c, np, func() error {
		np.Labels = map[string]string{
			"app.kubernetes.io/name":       "model-validation-operator",
			"app.kubernetes.io/managed-by": "model-validation-operator",
		}
		np.Spec = networkPolicySpec()
		return nil
	})
	if err != nil {
		return err
	}

	if result != controllerutil.OperationResultNone {
		logger.Info("NetworkPolicy installed", "operation", result)
	}

	return nil
}

func networkPolicySpec() networkingv1.NetworkPolicySpec {
	port53 := intstr.FromInt32(53)
	port443 := intstr.FromInt32(443)
	port6443 := intstr.FromInt32(6443)
	port8081 := intstr.FromInt32(8081)
	port8443 := intstr.FromInt32(8443)
	port9443 := intstr.FromInt32(9443)
	protocolTCP := corev1.ProtocolTCP
	protocolUDP := corev1.ProtocolUDP

	return networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "model-validation-operator",
			},
		},
		PolicyTypes: []networkingv1.PolicyType{
			networkingv1.PolicyTypeIngress,
			networkingv1.PolicyTypeEgress,
		},
		Ingress: []networkingv1.NetworkPolicyIngressRule{
			{
				Ports: []networkingv1.NetworkPolicyPort{
					{Port: &port9443, Protocol: &protocolTCP},
				},
			},
			{
				From: []networkingv1.NetworkPolicyPeer{
					{
						PodSelector: &metav1.LabelSelector{},
					},
				},
				Ports: []networkingv1.NetworkPolicyPort{
					{Port: &port8081, Protocol: &protocolTCP},
				},
			},
			{
				From: []networkingv1.NetworkPolicyPeer{
					{
						PodSelector: &metav1.LabelSelector{},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"metrics": "enabled",
							},
						},
					},
				},
				Ports: []networkingv1.NetworkPolicyPort{
					{Port: &port8443, Protocol: &protocolTCP},
				},
			},
		},
		Egress: []networkingv1.NetworkPolicyEgressRule{
			{
				To: []networkingv1.NetworkPolicyPeer{
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": "kube-system",
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": "openshift-dns",
							},
						},
					},
				},
				Ports: []networkingv1.NetworkPolicyPort{
					{Port: &port53, Protocol: &protocolTCP},
					{Port: &port53, Protocol: &protocolUDP},
				},
			},
			{
				Ports: []networkingv1.NetworkPolicyPort{
					{Port: &port443, Protocol: &protocolTCP},
					{Port: &port6443, Protocol: &protocolTCP},
				},
			},
		},
	}
}
