#!/usr/bin/env bash
# S7: measure disruption to an existing tunnel when a SECOND live tunnel is
# added mid-flow. At T=DURATION/2 a new client container is spawned that
# really registers with the existing server:
#   wg: generate keypair, add peer on server, spawn wg client, force handshake
#   fr: spawn frpc with a new tcp proxy on a fresh remote port
#   ch: spawn chisel client with a new reverse forward on a fresh remote port
#
# Output: iperf3 JSON (per-interval bps; look for dips near T=HALF) and the
# wall-clock timestamp(s) of the add action.
set -euo pipefail

source "$(dirname "$0")/../lib/common.sh"
DURATION=${DURATION:-30}
HALF=$((DURATION / 2))

if [ "${BACKEND}" = "b0" ]; then
  echo "S7 doesn't apply to baseline." >&2; exit 2
fi

up
sleep 2

OUT_JSON="${RESULTS_DIR}/s7_${BACKEND}.json"
EVENT_LOG="${RESULTS_DIR}/s7_${BACKEND}.events"
: > "${EVENT_LOG}"

WAN_NET=$(docker inspect "${SERVER}" --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' | awk '{print $1}')
INT_NET=""
if [ "${BACKEND}" = "wg" ]; then
  INT_NET=$(docker inspect "${CLIENT}" --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' | awk '{for (i=1;i<=NF;i++) if ($i ~ /internal-net/) print $i}')
fi

TMPDIR_S7=$(mktemp -d)
NEW_CLIENT=""
cleanup() {
  [ -n "${NEW_CLIENT}" ] && docker rm -f "${NEW_CLIENT}" >/dev/null 2>&1 || true
  rm -rf "${TMPDIR_S7}"
  down
}
trap cleanup EXIT

add_peer_wg() {
  NEW_CLIENT="tbench-wg-s7-new"
  # Generate a new peer keypair inside the server container (wg tools live there).
  NEW_PRIV=$(docker exec "${SERVER}" wg genkey)
  NEW_PUB=$(echo "${NEW_PRIV}" | docker exec -i "${SERVER}" wg pubkey)
  SRV_PUB=$(cat "${BENCH_ROOT}/configs/wireguard/server.pub")

  # Register peer on server (control-plane action — the main thing we measure).
  docker exec "${SERVER}" wg set wg0 peer "${NEW_PUB}" allowed-ips 10.200.0.3/32
  date -u +%s.%N >> "${EVENT_LOG}"

  # Build a wg0.conf for the new client and mount it.
  cat > "${TMPDIR_S7}/wg0.conf" <<EOF
[Interface]
Address = 10.200.0.3/24
PrivateKey = ${NEW_PRIV}
[Peer]
PublicKey = ${SRV_PUB}
Endpoint = tunnel-server:51820
AllowedIPs = 10.200.0.0/24
PersistentKeepalive = 5
EOF
  chmod 600 "${TMPDIR_S7}/wg0.conf"

  # Spawn the new wg client; its handshake goes through the same server socket
  # as the existing peer — that's the perturbation we want to measure.
  docker run -d --rm --name "${NEW_CLIENT}" --network "${WAN_NET}" \
    --cap-add NET_ADMIN --sysctl net.ipv4.ip_forward=1 \
    -v "${TMPDIR_S7}/wg0.conf:/etc/wireguard/wg0.conf:ro" \
    tbench/wireguard:local sh -c '
      cp /etc/wireguard/wg0.conf /tmp/wg0.conf && chmod 600 /tmp/wg0.conf
      wg-quick up /tmp/wg0.conf
      # Force handshake immediately + keep link warm
      for i in 1 2 3; do ping -c 1 -W 1 10.200.0.1 >/dev/null 2>&1 || true; done
      exec sleep infinity
    ' >/dev/null
}

add_peer_fr() {
  NEW_CLIENT="tbench-fr-s7-new"
  docker run -d --rm --name "${NEW_CLIENT}" --network "${WAN_NET}" \
    snowdreamtech/frpc:latest \
    frpc tcp -s tunnel-server -P 7000 -t benchtoken \
             -n s7-new -i origin -l 5201 -r 16100 \
    >/dev/null
  date -u +%s.%N >> "${EVENT_LOG}"
}

add_peer_ch() {
  NEW_CLIENT="tbench-ch-s7-new"
  docker run -d --rm --name "${NEW_CLIENT}" --network "${WAN_NET}" \
    jpillora/chisel:latest \
    client --auth=bench:benchpw http://tunnel-server:8000 \
           "R:0.0.0.0:16100:127.0.0.1:9999" \
    >/dev/null
  date -u +%s.%N >> "${EVENT_LOG}"
}

add_peer_bo() {
  NEW_CLIENT="tbench-bo-s7-new"
  docker run -d --rm --name "${NEW_CLIENT}" --network "${WAN_NET}" \
    tbench/bore:local \
    local 5201 --local-host 127.0.0.1 --to tunnel-server --port 16100 --secret benchsecret \
    >/dev/null
  date -u +%s.%N >> "${EVENT_LOG}"
}

add_peer_ra() {
  NEW_CLIENT="tbench-ra-s7-new"
  cat > "${TMPDIR_S7}/client.toml" <<EOF
[client]
remote_addr = "tunnel-server:2333"
default_token = "benchtoken"

[client.services.idle1]
local_addr = "127.0.0.1:9999"
EOF
  docker run -d --rm --name "${NEW_CLIENT}" --network "${WAN_NET}" \
    -v "${TMPDIR_S7}/client.toml:/c.toml:ro" \
    rapiz1/rathole:latest /c.toml \
    >/dev/null
  date -u +%s.%N >> "${EVENT_LOG}"
}

add_peer_go() {
  NEW_CLIENT="tbench-go-s7-new"
  docker run -d --rm --name "${NEW_CLIENT}" --network "${WAN_NET}" \
    gogost/gost:latest \
    -L "rtcp://:16100/127.0.0.1:9999" \
    -F "relay+grpc://bench:benchpw@tunnel-server:2443?keepalive=true&keepalive.time=20s&keepalive.permitWithoutStream=true" \
    >/dev/null
  date -u +%s.%N >> "${EVENT_LOG}"
}

# Remove any container left over from a prior interrupted run.
for n in tbench-wg-s7-new tbench-fr-s7-new tbench-ch-s7-new tbench-bo-s7-new tbench-ra-s7-new tbench-go-s7-new; do
  docker rm -f "$n" >/dev/null 2>&1 || true
done

add_peer() {
  case "${BACKEND}" in
    wg) add_peer_wg ;;
    fr) add_peer_fr ;;
    ch) add_peer_ch ;;
    bo) add_peer_bo ;;
    ra) add_peer_ra ;;
    go) add_peer_go ;;
  esac
}

(
  sleep "${HALF}"
  echo "T=${HALF}: triggering dynamic add on ${BACKEND}"
  add_peer
) &
ADDER_PID=$!

echo "S7/${BACKEND}: iperf3 ${DURATION}s, add event at T=${HALF}"
probe iperf3 -c "${IPERF_HOST}" -p "${IPERF_PORT}" -t "${DURATION}" -i 1 -J > "${OUT_JSON}"
wait "${ADDER_PID}" 2>/dev/null || true

# Report min interval bitrate around the add event as a "dip" indicator.
python3 - <<EOF
import json
d = json.load(open('${OUT_JSON}'))
iv = d.get('intervals', [])
if not iv:
    print('S7/${BACKEND}: no intervals in JSON')
    raise SystemExit(0)
rates = [x['sum']['bits_per_second']/1e6 for x in iv]
median = sorted(rates)[len(rates)//2]
worst = min(rates)
worst_i = rates.index(worst)
print(f'S7/${BACKEND}: median={median:.1f} Mbps, worst={worst:.1f} Mbps at interval {worst_i} (dip={100*(1-worst/median):.1f}%)')
EOF
