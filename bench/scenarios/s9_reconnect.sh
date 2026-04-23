#!/usr/bin/env bash
# S9: kill tunnel-client, measure time until probe can round-trip through
# the tunnel again. Uses iperf3 -n 1K as the probe — proves the full
# datapath (connect + handshake + 1KB xfer), not just TCP SYN.
set -euo pipefail

source "$(dirname "$0")/../lib/common.sh"
TIMEOUT_S=${TIMEOUT_S:-60}

if [ "${BACKEND}" = "b0" ]; then
  echo "S9 doesn't apply to baseline." >&2; exit 2
fi

up
sleep 2
trap 'down' EXIT

reachable() {
  probe iperf3 -c "${IPERF_HOST}" -p "${IPERF_PORT}" -n 1K \
      --connect-timeout 2000 >/dev/null 2>&1
}

if ! reachable; then
  echo "S9/${BACKEND}: precheck failed — tunnel not reachable before kill" >&2
  docker logs --tail 30 "${SERVER}" >&2 || true
  docker logs --tail 30 "${CLIENT}" >&2 || true
  exit 1
fi

echo "S9/${BACKEND}: killing tunnel-client"
docker kill "${CLIENT}" >/dev/null
t0=$(date +%s.%N)

# bore's server keeps the port reservation briefly after a client drop.
# If we `docker start` too quickly, bore client errors "port already in
# use" and exits. Give the server a beat, then keep restarting the
# container until it stays up and reaches reachability.
start_client() { docker start "${CLIENT}" >/dev/null 2>&1; }

start_client
attempt=0
while :; do
  attempt=$((attempt + 1))
  # If container exited (bore race-lost registration), restart it.
  if ! docker inspect -f '{{.State.Running}}' "${CLIENT}" 2>/dev/null | grep -q true; then
    sleep 1
    start_client
  fi
  if reachable; then break; fi
  dt=$(python3 -c "print($(date +%s.%N) - ${t0})")
  if python3 -c "import sys; sys.exit(0 if ${dt} > ${TIMEOUT_S} else 1)"; then
    echo "S9/${BACKEND}: timed out after ${TIMEOUT_S}s (${attempt} attempts)" >&2
    echo "--- tunnel-server logs ---" >&2; docker logs --tail 30 "${SERVER}" >&2 || true
    echo "--- tunnel-client logs ---" >&2; docker logs --tail 30 "${CLIENT}" >&2 || true
    exit 1
  fi
  if [ $((attempt % 10)) -eq 0 ]; then
    printf "  still waiting... t=%.1fs\n" "${dt}"
  fi
  sleep 0.2
done

t1=$(date +%s.%N)
elapsed=$(python3 -c "print($t1 - $t0)")
printf "S9/%s: reconnect took %.2fs (%d polls)\n" "${BACKEND}" "${elapsed}" "${attempt}"
echo "${BACKEND},${elapsed}" >> "${RESULTS_DIR}/s9_reconnect.csv"
