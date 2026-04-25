package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/frp/config"
)

// frpcImage is the image the operator runs as the tunnel client. Aligned
// with provider/localdocker so behavior is consistent across local-docker
// and real-cloud paths.
const frpcImage = "snowdreamtech/frpc:0.68.1"

// frpcSecretName returns the Secret name that holds the rendered frpc.toml
// for a given Tunnel.
func frpcSecretName(t *frpv1alpha1.Tunnel) string {
	return t.Name + "-frpc-config"
}

// frpcDeploymentName returns the Deployment name running the frpc container.
func frpcDeploymentName(t *frpv1alpha1.Tunnel) string {
	return t.Name + "-frpc"
}

// ensureFrpcSecret renders the frpc.toml for this tunnel and writes it to a
// Secret in the tunnel's namespace. The Secret is owned by the tunnel.
//
// Returns the rendered bytes so the caller can compare them across reconciles
// (used by ensureFrpcDeployment to decide if a rollout-restart annotation
// should change).
func ensureFrpcSecret(
	ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel,
	serverAddr string, serverPort int, authToken string, ports []frpv1alpha1.TunnelPort,
) ([]byte, error) {
	cfg := config.FrpcConfig{
		ServerAddr: serverAddr,
		ServerPort: serverPort,
		Auth:       config.FrpcAuth{Method: "token", Token: authToken},
	}
	for _, p := range ports {
		pub := p.ServicePort
		if p.PublicPort != nil {
			pub = *p.PublicPort
		}
		cfg.Proxies = append(cfg.Proxies, config.FrpcProxy{
			Name:       fmt.Sprintf("%s_%s_%s", t.Namespace, t.Name, p.Name),
			Type:       toFrpcType(p.Protocol),
			LocalIP:    fmt.Sprintf("%s.%s.svc", t.Spec.Service.Name, t.Spec.Service.Namespace),
			LocalPort:  int(p.ServicePort),
			RemotePort: int(pub),
		})
	}
	body, err := cfg.Render()
	if err != nil {
		return nil, fmt.Errorf("render frpc.toml: %w", err)
	}

	name := frpcSecretName(t)
	key := types.NamespacedName{Name: name, Namespace: t.Namespace}
	var existing corev1.Secret
	err = c.Get(ctx, key, &existing)
	if err == nil {
		// Update if drifted.
		if string(existing.Data["frpc.toml"]) != string(body) {
			existing.Data = map[string][]byte{"frpc.toml": body}
			if err := c.Update(ctx, &existing); err != nil {
				return nil, fmt.Errorf("update frpc secret: %w", err)
			}
		}
		return body, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get frpc secret: %w", err)
	}

	sec := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       t.Namespace,
			Labels:          map[string]string{"frp-operator.io/tunnel": t.Name},
			OwnerReferences: []metav1.OwnerReference{tunnelOwnerRef(t)},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"frpc.toml": body},
	}
	if err := c.Create(ctx, &sec); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race: re-fetch and update.
			if err := c.Get(ctx, key, &existing); err == nil {
				existing.Data = map[string][]byte{"frpc.toml": body}
				_ = c.Update(ctx, &existing)
			}
			return body, nil
		}
		return nil, fmt.Errorf("create frpc secret: %w", err)
	}
	return body, nil
}

// ensureFrpcDeployment creates or updates the frpc Deployment for a tunnel.
// One replica, one container, the rendered Secret mounted at /etc/frp.
func ensureFrpcDeployment(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) error {
	name := frpcDeploymentName(t)
	desired := frpcDeploymentSpec(t)

	var existing appsv1.Deployment
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: t.Namespace}, &existing)
	if err == nil {
		// Idempotent update: only patch if image or replicas drifted.
		if existing.Spec.Template.Spec.Containers[0].Image != frpcImage ||
			existing.Spec.Replicas == nil || *existing.Spec.Replicas != 1 {
			existing.Spec = desired.Spec
			return c.Update(ctx, &existing)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get deployment: %w", err)
	}
	return c.Create(ctx, desired)
}

// frpcDeploymentSpec is the desired Deployment for one tunnel.
func frpcDeploymentSpec(t *frpv1alpha1.Tunnel) *appsv1.Deployment {
	labels := map[string]string{"frp-operator.io/tunnel": t.Name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            frpcDeploymentName(t),
			Namespace:       t.Namespace,
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{tunnelOwnerRef(t)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "frpc",
						Image:   frpcImage,
						Command: []string{"/usr/bin/frpc", "-c", "/etc/frp/frpc.toml"},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config",
							MountPath: "/etc/frp",
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: frpcSecretName(t)},
						},
					}},
				},
			},
		},
	}
}

// tunnelOwnerRef builds a metav1.OwnerReference with controller=true so
// owned resources (Secret, Deployment) GC when the Tunnel is deleted.
func tunnelOwnerRef(t *frpv1alpha1.Tunnel) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         frpv1alpha1.GroupVersion.String(),
		Kind:               "Tunnel",
		Name:               t.Name,
		UID:                t.UID,
		BlockOwnerDeletion: ptr.To(true),
		Controller:         ptr.To(true),
	}
}

// toFrpcType converts a TunnelProtocol to the frp config string.
func toFrpcType(p frpv1alpha1.TunnelProtocol) string {
	if p == frpv1alpha1.ProtocolUDP {
		return "udp"
	}
	return "tcp"
}
