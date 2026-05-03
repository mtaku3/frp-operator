/*
Copyright (C) 2026.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<https://www.gnu.org/licenses/agpl-3.0.html>.
*/

package operator

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

const (
	webhookConfigName = "frp-operator-validating-webhook-configuration"
	webhookSecretName = "frp-operator-webhook-cert"
)

// checkWebhookSetup verifies the webhook serving Secret exists and
// every webhook entry in the ValidatingWebhookConfiguration carries a
// caBundle that matches the Secret's tls.crt.
func checkWebhookSetup(ctx context.Context, c client.Client) error {
	var sec corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: Namespace, Name: webhookSecretName}, &sec); err != nil {
		return fmt.Errorf("get webhook Secret: %w", err)
	}
	tlsCrt, ok := sec.Data["tls.crt"]
	if !ok || len(tlsCrt) == 0 {
		return fmt.Errorf("webhook Secret missing tls.crt")
	}
	var cfg admissionv1.ValidatingWebhookConfiguration
	if err := c.Get(ctx, types.NamespacedName{Name: webhookConfigName}, &cfg); err != nil {
		return fmt.Errorf("get ValidatingWebhookConfiguration: %w", err)
	}
	for i := range cfg.Webhooks {
		if !bytes.Equal(cfg.Webhooks[i].ClientConfig.CABundle, tlsCrt) {
			return fmt.Errorf("webhook %q caBundle does not match Secret tls.crt", cfg.Webhooks[i].Name)
		}
	}
	return nil
}

// isWebhookWorking does a dry-run create of an intentionally-invalid
// Tunnel (no spec.service.name, which the CRD's OpenAPI MinLength=1
// constraint rejects) and asserts the apiserver returns an Invalid
// admission rejection mentioning the service field. This proves the
// admission path is reachable and that our CRD is installed. Returns
// false (with no error) if the webhook returns a different /
// transient error.
func isWebhookWorking(ctx context.Context, c client.Client) (bool, error) {
	t := &frpv1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "webhook-probe-",
			Namespace:    "default",
		},
		Spec: frpv1alpha1.TunnelSpec{
			ImmutableWhenReady:  true,
			SchedulingPolicyRef: frpv1alpha1.PolicyRef{Name: "default"},
			// Intentionally omit Service.Name so admission rejects.
			Ports: []frpv1alpha1.TunnelPort{{Name: "p", ServicePort: 80}},
		},
	}
	err := c.Create(ctx, t, &client.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err == nil {
		return false, fmt.Errorf("dry-run create of invalid Tunnel succeeded; webhook not enforcing")
	}
	if !apierrors.IsInvalid(err) {
		// Could be a transient connection / certificate error. Caller
		// retries.
		return false, nil
	}
	if !strings.Contains(err.Error(), "service") {
		return false, fmt.Errorf("dry-run rejection from wrong validator: %v", err)
	}
	return true, nil
}
