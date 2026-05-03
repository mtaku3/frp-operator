/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/frp/config"
	"github.com/mtaku3/frp-operator/internal/provider"
)

// reconcileProvisioner is the bridge between the controller's view of an
// ExitServer (CR + Status) and the Provisioner SDK. Returns the latest
// observed provider.State, which the caller uses to update status and
// drive nextPhase.
//
// On first call (status.providerID empty), it issues Provisioner.Create
// and records the new ID. On subsequent calls, it Inspects.
func reconcileProvisioner(
	ctx context.Context,
	p provider.Provisioner,
	exit *frpv1alpha1.ExitServer,
	creds Credentials,
	providerCreds []byte,
	cloudInit []byte,
	frpsConfig []byte,
) (provider.State, error) {
	if exit.Status.ProviderID == "" {
		spec := provider.Spec{
			Name:              exit.Namespace + "__" + exit.Name,
			Region:            exit.Spec.Region,
			Size:              exit.Spec.Size,
			BindPort:          portOrDefault(exit.Spec.Frps.BindPort, 7000),
			AdminPort:         portOrDefault(exit.Spec.Frps.AdminPort, 7500),
			CloudInitUserData: cloudInit,
			FrpsConfigTOML:    frpsConfig,
			Credentials:       providerCreds,
		}
		_ = creds // Credentials are baked into FrpsConfigTOML by the caller
		st, err := p.Create(ctx, spec)
		if err != nil {
			return provider.State{}, fmt.Errorf("Provisioner.Create: %w", err)
		}
		return st, nil
	}
	st, err := p.Inspect(ctx, exit.Status.ProviderID)
	if err != nil {
		return st, fmt.Errorf("Provisioner.Inspect: %w", err)
	}
	return st, nil
}

func portOrDefault(p int32, def int32) int {
	if p == 0 {
		return int(def)
	}
	return int(p)
}

// loadProviderCredentials fetches the bytes referenced by
// exit.spec.credentialsRef from a Secret in the same namespace. Returns
// nil with no error if the ref is empty (provisioner will fall back to
// its configured token).
func loadProviderCredentials(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer) ([]byte, error) {
	if exit.Spec.CredentialsRef.Name == "" || exit.Spec.CredentialsRef.Key == "" {
		return nil, nil
	}
	var sec corev1.Secret
	err := c.Get(ctx, types.NamespacedName{
		Name:      exit.Spec.CredentialsRef.Name,
		Namespace: exit.Namespace,
	}, &sec)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("credentials Secret %s/%s not found", exit.Namespace, exit.Spec.CredentialsRef.Name)
		}
		return nil, fmt.Errorf("get credentials Secret: %w", err)
	}
	val, ok := sec.Data[exit.Spec.CredentialsRef.Key]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s missing key %q",
			exit.Namespace, exit.Spec.CredentialsRef.Name, exit.Spec.CredentialsRef.Key)
	}
	return val, nil
}

// parseAllowPorts converts spec entries like "443" or "1024-65535" into
// FrpsPortRange values for the rendered frps.toml.
func parseAllowPorts(specStrings []string) []config.FrpsPortRange {
	out := make([]config.FrpsPortRange, 0, len(specStrings))
	for _, s := range specStrings {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if before, after, ok := strings.Cut(s, "-"); ok {
			start, err1 := strconv.Atoi(strings.TrimSpace(before))
			end, err2 := strconv.Atoi(strings.TrimSpace(after))
			if err1 != nil || err2 != nil {
				continue
			}
			out = append(out, config.FrpsPortRange{Start: start, End: end})
			continue
		}
		single, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		out = append(out, config.FrpsPortRange{Single: single})
	}
	return out
}

// firstAllowPortsRangeString returns the first contiguous range string
// (e.g., "1024-65535"). If no range is found, falls back to the default
// "1024-65535" used by the cloud-init UFW rules.
func firstAllowPortsRangeString(specStrings []string) string {
	for _, s := range specStrings {
		s = strings.TrimSpace(s)
		if strings.Contains(s, "-") {
			return s
		}
	}
	return "1024-65535"
}

// intsFrom32 converts a slice of int32 to []int.
func intsFrom32(in []int32) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}
