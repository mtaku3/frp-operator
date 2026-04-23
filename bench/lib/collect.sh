#!/usr/bin/env bash
# Sample `docker stats` for a list of containers at 1Hz and write CSV.
# Usage: collect.sh <out.csv> <container> [container ...]
# Sends SIGTERM to stop.
set -euo pipefail

out="$1"; shift
containers=("$@")

echo "ts,container,cpu_pct,mem_bytes,mem_pct,net_rx,net_tx" > "${out}"

stop=0
trap 'stop=1' TERM INT

while [ "${stop}" -eq 0 ]; do
  ts=$(date -u +%s.%N)
  # One shot, no stream, all requested containers.
  docker stats --no-stream --format \
    '{{.Name}},{{.CPUPerc}},{{.MemUsage}},{{.MemPerc}},{{.NetIO}}' \
    "${containers[@]}" 2>/dev/null | \
    awk -F, -v ts="${ts}" '{
      gsub("%","",$2); gsub("%","",$4);
      split($3, mu, " / "); mem=mu[1];
      split($5, ni, " / "); rx=ni[1]; tx=ni[2];
      print ts "," $1 "," $2 "," mem "," $4 "," rx "," tx
    }' >> "${out}"
  sleep 1
done
