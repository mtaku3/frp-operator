#!/usr/bin/env bash
# Bootstrap a kind cluster + shared host tmpdir for the e2e suite.
# Idempotent: skips cluster creation if it already exists.
set -euo pipefail

KIND_BIN="${KIND_BIN:-kind}"
KIND_CLUSTER="${KIND_CLUSTER:-frp-operator-test-e2e}"
KUBECONFIG_PATH="${KUBECONFIG:-/tmp/frp-operator-e2e.kubeconfig}"
SHARED_DIR="${SHARED_DIR:-/tmp/frp-operator-shared}"
KIND_CONFIG="${KIND_CONFIG:-test/e2e/kind-config.yaml}"

# Idempotent shared-dir setup. CI runners may have left the dir owned by a
# different user from a prior job (e.g. helm/kind-action root-mounting it),
# so chmod can fail with EPERM. Tolerate that path: the only requirement is
# that the dir exists and is writable by the operator container at runtime.
mkdir -p "$SHARED_DIR" 2>/dev/null || sudo mkdir -p "$SHARED_DIR"
chmod 1777 "$SHARED_DIR" 2>/dev/null || sudo chmod 1777 "$SHARED_DIR" || true

if "$KIND_BIN" get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER"; then
  echo "kind cluster '$KIND_CLUSTER' already exists; reusing."
  "$KIND_BIN" export kubeconfig --name "$KIND_CLUSTER" --kubeconfig "$KUBECONFIG_PATH"
else
  echo "creating kind cluster '$KIND_CLUSTER'..."
  "$KIND_BIN" create cluster \
    --name "$KIND_CLUSTER" \
    --config "$KIND_CONFIG" \
    --kubeconfig "$KUBECONFIG_PATH"
fi
