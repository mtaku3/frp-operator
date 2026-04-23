#!/usr/bin/env bash
# S2: 32-parallel TCP streams via iperf3 -P 32, 60s.
set -euo pipefail

source "$(dirname "$0")/../lib/common.sh"
DURATION=${DURATION:-60}
STREAMS=${STREAMS:-32}

up
sleep 2

STATS_CSV="${RESULTS_DIR}/raw/s2_${BACKEND}_stats.csv"
OUT_JSON="${RESULTS_DIR}/s2_${BACKEND}.json"

mon=()
[ "${BACKEND}" != "b0" ] && mon+=("${SERVER}" "${CLIENT}")
mon+=("${PROBE}" "${ORIGIN}")
bash "${BENCH_ROOT}/lib/collect.sh" "${STATS_CSV}" "${mon[@]}" &
COLLECT_PID=$!
trap 'kill "${COLLECT_PID}" 2>/dev/null || true; down' EXIT

echo "S2/${BACKEND}: iperf3 -P ${STREAMS} -t ${DURATION}"
probe iperf3 -c "${IPERF_HOST}" -p "${IPERF_PORT}" -P "${STREAMS}" -t "${DURATION}" -J > "${OUT_JSON}"

python3 -c "
import json, sys
d = json.load(open('${OUT_JSON}'))
bits = d.get('end', {}).get('sum_received', {}).get('bits_per_second')
if bits is None:
    sys.exit('S2/${BACKEND}: no sum_received in JSON — iperf error=' + str(d.get('error')))
print(f'S2/${BACKEND}: {bits/1e6:.2f} Mbps (P=${STREAMS})')
"
