#!/usr/bin/env bash
# S5: add N idle peers/clients and sample tunnel-server RSS to estimate
# per-tunnel memory overhead.
#
# Per backend:
#   wg: `wg set peer` — cheap, pure control-plane, no actual client process.
#   fr: spawn N frpc containers, each advertising a unique tcp proxy.
#   ch: spawn N chisel clients, each requesting a unique reverse forward.
#
# The fr/ch numbers include the client-container overhead, so they're
# apples-to-oranges with wg. Interpret deltas per +1 client, not absolutes.
set -euo pipefail

source "$(dirname "$0")/../lib/common.sh"
N=${N:-50}
STEP=${STEP:-10}

if [ "${BACKEND}" = "b0" ]; then
  echo "S5 doesn't apply to baseline." >&2; exit 2
fi

up
sleep 2
TMPDIR_S5=$(mktemp -d)

OUT="${RESULTS_DIR}/s5_${BACKEND}.csv"
echo "peers,rss_bytes" > "${OUT}"

SPAWNED=()
cleanup() {
  for c in "${SPAWNED[@]:-}"; do docker rm -f "$c" >/dev/null 2>&1 || true; done
  rm -rf "${TMPDIR_S5:-}"
  down
}
trap cleanup EXIT

# Resolve the wan-net name from compose project.
WAN_NET=$(docker inspect "${SERVER}" --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' | awk '{print $1}')

# Returns RSS in bytes using `docker stats` "MemUsage" field (e.g. "12.3MiB").
read_rss() {
  docker stats --no-stream --format '{{.MemUsage}}' "${SERVER}" | awk '
    {
      v=$1; unit=$1;
      sub(/[KMGT]iB/, "", v); sub(/[0-9.]+/, "", unit);
      mul=1
      if (unit == "KiB") mul=1024
      else if (unit == "MiB") mul=1024*1024
      else if (unit == "GiB") mul=1024*1024*1024
      printf "%.0f\n", v*mul
    }'
}

printf "1,%s\n" "$(read_rss)" >> "${OUT}"

add_peer_wg() {
  local i=$1
  local pub; pub=$(docker exec "${SERVER}" sh -c 'wg genkey | wg pubkey')
  docker exec "${SERVER}" wg set wg0 peer "${pub}" allowed-ips "10.200.1.$((i % 250))/32"
}

add_peer_fr() {
  local i=$1
  local name="tbench-fr-idle-${i}"
  local port=$((16000 + i))
  docker run -d --rm --name "${name}" --network "${WAN_NET}" \
    snowdreamtech/frpc:latest \
    frpc tcp -s tunnel-server -P 7000 -t benchtoken \
             -n "idle-${i}" -i origin -l 5201 -r "${port}" \
    >/dev/null
  SPAWNED+=("${name}")
}

add_peer_ch() {
  local i=$1
  local name="tbench-ch-idle-${i}"
  local port=$((16000 + i))
  # Chisel client needs access to both nets? No — for S5 we just need it to
  # *register* a reverse forward with the server. The target origin is
  # irrelevant for idle measurement; chisel still opens the listener.
  docker run -d --rm --name "${name}" --network "${WAN_NET}" \
    jpillora/chisel:latest \
    client --auth=bench:benchpw http://tunnel-server:8000 \
           "R:0.0.0.0:${port}:127.0.0.1:9999" \
    >/dev/null
  SPAWNED+=("${name}")
}

add_peer_bo() {
  local i=$1
  local name="tbench-bo-idle-${i}"
  local port=$((16000 + i))
  docker run -d --rm --name "${name}" --network "${WAN_NET}" \
    tbench/bore:local \
    local 5201 --local-host 127.0.0.1 --to tunnel-server \
              --port "${port}" --secret benchsecret \
    >/dev/null
  SPAWNED+=("${name}")
}

add_peer_ra() {
  local i=$1
  local name="tbench-ra-idle-${i}"
  # Uses pre-declared server slot "idle${i}" (bound to port 16000+i).
  # Ephemeral client config written to a tmpfile and bind-mounted.
  local conf="${TMPDIR_S5}/client-idle-${i}.toml"
  cat > "${conf}" <<EOF
[client]
remote_addr = "tunnel-server:2333"
default_token = "benchtoken"

[client.services.idle${i}]
local_addr = "127.0.0.1:9999"
EOF
  docker run -d --rm --name "${name}" --network "${WAN_NET}" \
    -v "${conf}:/c.toml:ro" \
    rapiz1/rathole:latest /c.toml \
    >/dev/null
  SPAWNED+=("${name}")
}

add_peer_go() {
  local i=$1
  local name="tbench-go-idle-${i}"
  local port=$((16000 + i))
  docker run -d --rm --name "${name}" --network "${WAN_NET}" \
    gogost/gost:latest \
    -L "rtcp://:${port}/127.0.0.1:9999" \
    -F "relay+grpc://bench:benchpw@tunnel-server:2443?keepalive=true&keepalive.time=20s&keepalive.permitWithoutStream=true" \
    >/dev/null
  SPAWNED+=("${name}")
}

for i in $(seq 2 "${N}"); do
  case "${BACKEND}" in
    wg) add_peer_wg "${i}" ;;
    fr) add_peer_fr "${i}" ;;
    ch) add_peer_ch "${i}" ;;
    bo) add_peer_bo "${i}" ;;
    ra) add_peer_ra "${i}" ;;
    go) add_peer_go "${i}" ;;
  esac
  if [ $((i % STEP)) -eq 0 ]; then
    sleep 0.5
    printf "%d,%s\n" "${i}" "$(read_rss)" >> "${OUT}"
    printf "  +%d peers, rss=%s\n" "${i}" "$(read_rss)"
  fi
done

echo "S5/${BACKEND}: ${OUT}"
