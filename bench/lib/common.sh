# Sourced by every scenario script. Resolves backend -> compose file,
# container names, and target endpoints.
# shellcheck shell=bash

set -euo pipefail

BENCH_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_DIR="${BENCH_ROOT}/compose"
RESULTS_DIR="${BENCH_ROOT}/results"
mkdir -p "${RESULTS_DIR}/raw"

backend="${1:-}"
case "${backend}" in
  b0|baseline)  BACKEND=b0; COMPOSE="${COMPOSE_DIR}/baseline.yml"  ;;
  wg|wireguard) BACKEND=wg; COMPOSE="${COMPOSE_DIR}/wireguard.yml" ;;
  fr|frp)       BACKEND=fr; COMPOSE="${COMPOSE_DIR}/frp.yml"       ;;
  ch|chisel)    BACKEND=ch; COMPOSE="${COMPOSE_DIR}/chisel.yml"    ;;
  bo|bore)      BACKEND=bo; COMPOSE="${COMPOSE_DIR}/bore.yml"      ;;
  ra|rathole)   BACKEND=ra; COMPOSE="${COMPOSE_DIR}/rathole.yml"   ;;
  *) echo "usage: $0 <b0|wg|fr|ch|bo|ra>" >&2; exit 2 ;;
esac

PROBE="tbench-${BACKEND}-probe"
SERVER="tbench-${BACKEND}-server"    # tunnel-server (not present for b0)
CLIENT="tbench-${BACKEND}-client"
ORIGIN="tbench-${BACKEND}-origin"

# Where does probe connect to reach the origin?
# For b0, directly to origin container by DNS name on wan-net.
# For tunnel backends, via tunnel-server's service port.
if [ "${BACKEND}" = "b0" ]; then
  IPERF_HOST="origin";  IPERF_PORT=5201
  HTTP_HOST="origin-http"; HTTP_PORT=80
else
  IPERF_HOST="tunnel-server"; IPERF_PORT=15200
  HTTP_HOST="tunnel-server";  HTTP_PORT=18080
fi

dc() { docker compose -f "${COMPOSE}" "$@"; }

up() {
  dc up -d --wait --wait-timeout 60 >/dev/null
}

down() {
  dc down -v --remove-orphans >/dev/null 2>&1 || true
}

# Run a command inside the probe container.
probe() { docker exec "${PROBE}" "$@"; }

# Ensure probe image has the tool we need (iperf3 image is minimal).
probe_has() { docker exec "${PROBE}" sh -c "command -v $1 >/dev/null"; }
