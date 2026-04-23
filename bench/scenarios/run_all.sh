#!/usr/bin/env bash
# Run the implemented scenario set across all backends.
# Each scenario brings its stack up/down, so they don't conflict.
set -u

here="$(cd "$(dirname "$0")" && pwd)"
cd "${here}"

# Override via env, e.g.: BACKENDS="bo ra go" bash run_all.sh
# b0 in BACKENDS is auto-skipped for tunnel-only scenarios.
if [ -n "${BACKENDS:-}" ]; then
  read -ra all_backends <<<"${BACKENDS}"
  tunnel_backends=()
  for b in "${all_backends[@]}"; do
    [ "${b}" != "b0" ] && tunnel_backends+=("${b}")
  done
else
  all_backends=(b0 wg fr ch bo ra)
  tunnel_backends=(wg fr ch bo ra)
fi

# Throughput/HTTP scenarios include baseline for a ceiling reference.
for s in s1_throughput.sh s2_parallel.sh s3_http.sh; do
  for b in "${all_backends[@]}"; do
    echo "=== ${s} ${b} ==="
    bash "${s}" "${b}" || echo "FAILED: ${s} ${b}"
  done
done

# Tunnel-only scenarios (S5/S7/S9 don't apply to baseline).
for s in s7_dynamic_add.sh s5_idle_scale.sh s9_reconnect.sh; do
  for b in "${tunnel_backends[@]}"; do
    echo "=== ${s} ${b} ==="
    bash "${s}" "${b}" || echo "FAILED: ${s} ${b}"
  done
done
