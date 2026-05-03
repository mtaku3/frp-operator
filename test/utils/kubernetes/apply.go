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

package kubernetes

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/mtaku3/frp-operator/test/utils"
)

// ApplyServerSide writes yaml to a temp file and runs
// `kubectl apply --server-side --force-conflicts`. Server-side apply
// avoids the 256 KB last-applied-configuration limit on big CRDs.
func ApplyServerSide(_ context.Context, yaml []byte) error {
	f, err := os.CreateTemp("", "e2e-*.yaml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(yaml); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cmd := exec.Command("kubectl", "apply", "--server-side", "--force-conflicts", "-f", f.Name())
	if _, err := utils.Run(cmd); err != nil {
		return fmt.Errorf("kubectl apply: %w", err)
	}
	return nil
}

// DeleteServerSide writes yaml to a temp file and runs
// `kubectl delete --ignore-not-found --wait=false -f`. Used by suite
// teardown to remove kustomize-rendered manifests without blocking on
// finalizers.
func DeleteServerSide(_ context.Context, yaml []byte) error {
	f, err := os.CreateTemp("", "e2e-del-*.yaml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(yaml); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_, err = utils.Run(exec.Command("kubectl", "delete", "-f", f.Name(),
		"--ignore-not-found", "--wait=false"))
	return err
}
