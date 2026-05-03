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

package utils

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// SetAsOwnedBy sets the controlled object as owned by a certain other
// controller object with his type information
func SetAsOwnedBy(controlled *metav1.ObjectMeta, controller metav1.ObjectMeta, typeMeta metav1.TypeMeta) {
	isController := true

	controlled.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: typeMeta.APIVersion,
			Kind:       typeMeta.Kind,
			Name:       controller.Name,
			UID:        controller.UID,
			Controller: &isController,
		},
	})
}
