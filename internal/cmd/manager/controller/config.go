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

package controller

const (
	// CaSecretName is the name of the operator's self-signed CA Secret.
	CaSecretName = "frp-operator-ca-secret"

	// WebhookSecretName is the name of the leaf-cert Secret mounted on
	// the manager Pod.
	WebhookSecretName = "frp-operator-webhook-cert"

	// WebhookServiceName is the name of the Service that fronts the
	// validating webhook.
	WebhookServiceName = "frp-operator-webhook-service"

	// ValidatingWebhookConfigurationName is the cluster-scoped
	// ValidatingWebhookConfiguration object the operator patches with
	// its CA bundle.
	ValidatingWebhookConfigurationName = "frp-operator-validating-webhook-configuration"

	// OperatorDeploymentLabelSelector is the label selector that
	// resolves to the operator's own Deployment, so cert Secrets can
	// be owner-ref'd to it.
	OperatorDeploymentLabelSelector = "app.kubernetes.io/name=frp-operator"
)
