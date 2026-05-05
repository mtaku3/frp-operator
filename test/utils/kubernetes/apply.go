/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

// Package kubernetes provides thin wrappers over `kubectl apply` and
// generic wait helpers shared across the e2e suite.
package kubernetes

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/mtaku3/frp-operator/test/utils"
)

// ApplyServerSide writes yaml to a temp file and runs kubectl apply
// --server-side --force-conflicts -f. Server-side apply avoids the
// 256 KB last-applied-configuration cap on big CRDs.
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

// DeleteServerSide writes yaml to a temp file and runs kubectl delete.
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
