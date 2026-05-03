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

package policy

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Get returns the cluster-scoped SchedulingPolicy by name.
func Get(ctx context.Context, c client.Client, name string) (*frpv1alpha1.SchedulingPolicy, error) {
	var p frpv1alpha1.SchedulingPolicy
	if err := c.Get(ctx, types.NamespacedName{Name: name}, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
