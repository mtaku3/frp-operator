#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
out=$(mktemp)
trap 'rm -f "$out"' EXIT
./wrap-crd-template.sh testdata/wrap-crd-input.yaml "$out"
diff -u testdata/wrap-crd-expected.yaml "$out"
echo "PASS"
