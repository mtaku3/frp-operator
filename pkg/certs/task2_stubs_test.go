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

package certs

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TODO(cnpg-adoption): these stubs satisfy the symbols that the CNPG
// operator_deployment_test.go shares with k8s_test.go. They are
// removed in Task 3 once k8s_test.go is ported and provides the real
// definitions.
var (
	operatorDeploymentName = "frp-operator-controller-manager"
	operatorNamespaceName  = "operator-namespace"
)

func generateFakeClient() client.Client {
	return fake.NewClientBuilder().Build()
}
