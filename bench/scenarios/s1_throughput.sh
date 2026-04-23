#!/usr/bin/env bash
# S1: single-stream TCP throughput via iperf3, 60s.
# Usage: s1_throughput.sh <b0|wg|fr|ch>
# Writes: results/s1_<backend>.json + results/raw/s1_<backend>_stats.csv
set -euo pipefail

source "$(dirname "$0")/../lib/common.sh"
DURATION=${DURATION:-60}

up
sleep 2  # let tunnel settle

STATS_CSV="${RESULTS_DIR}/raw/s1_${BACKEND}_stats.csv"
OUT_JSON="${RESULTS_DIR}/s1_${BACKEND}.json"

# Pick containers to monitor: tunnel-server + tunnel-client if present.
mon=()
[ "${BACKEND}" != "b0" ] && mon+=("${SERVER}" "${CLIENT}")
mon+=("${PROBE}" "${ORIGIN}")

if [ ${#mon[@]} -gt 0 ]; then
  bash "${BENCH_ROOT}/lib/collect.sh" "${STATS_CSV}" "${mon[@]}" &
  COLLECT_PID=$!
  trap 'kill "${COLLECT_PID}" 2>/dev/null || true; down' EXIT
else
  trap 'down' EXIT
fi

echo "S1/${BACKEND}: iperf3 -c ${IPERF_HOST}:${IPERF_PORT} -t ${DURATION}"
probe iperf3 -c "${IPERF_HOST}" -p "${IPERF_PORT}" -t "${DURATION}" -J > "${OUT_JSON}"

# Quick summary for human eyes.
python3 -c "
import json, sys
d = json.load(open('${OUT_JSON}'))
bits = d.get('end', {}).get('sum_received', {}).get('bits_per_second')
if bits is None:
    sys.exit('S1/${BACKEND}: no sum_received in JSON — iperf error=' + str(d.get('error')))
print(f'S1/${BACKEND}: {bits/1e6:.2f} Mbps')
"
