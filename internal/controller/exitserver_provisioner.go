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

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
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
) (provider.State, error) {
	if exit.Status.ProviderID == "" {
		spec := provider.Spec{
			Name:           exit.Namespace + "__" + exit.Name,
			Region:         exit.Spec.Region,
			Size:           exit.Spec.Size,
			BindPort:       portOrDefault(exit.Spec.Frps.BindPort, 7000),
			AdminPort:      portOrDefault(exit.Spec.Frps.AdminPort, 7500),
			FrpsConfigTOML: nil, // populated by caller via composeFrpsConfig in Task 5
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
