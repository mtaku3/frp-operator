#!/usr/bin/env bash
# S3: wrk HTTP benchmark at two payload sizes. nginx default index for 1KB;
# for 100KB we write a file into origin-http before running.
set -euo pipefail

source "$(dirname "$0")/../lib/common.sh"
DURATION=${DURATION:-30}
CONNS=${CONNS:-64}
THREADS=${THREADS:-4}

up
sleep 2

HTTP_ORIGIN="tbench-${BACKEND}-origin-http"
docker exec "${HTTP_ORIGIN}" sh -c "head -c 102400 /dev/urandom | base64 > /usr/share/nginx/html/100k"

# wrk image on the same wan-net
NETWORK=$(docker inspect "${PROBE}" --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' | awk '{print $1}')

STATS_CSV="${RESULTS_DIR}/raw/s3_${BACKEND}_stats.csv"

mon=()
[ "${BACKEND}" != "b0" ] && mon+=("${SERVER}" "${CLIENT}")
mon+=("${PROBE}")
bash "${BENCH_ROOT}/lib/collect.sh" "${STATS_CSV}" "${mon[@]}" &
COLLECT_PID=$!
trap 'kill "${COLLECT_PID}" 2>/dev/null || true; down' EXIT

for path in "/" "/100k"; do
  tag=$(echo "${path}" | tr -c 'a-z0-9' _)
  out="${RESULTS_DIR}/s3_${BACKEND}${tag}.txt"
  echo "S3/${BACKEND}: wrk ${path} t=${THREADS} c=${CONNS} d=${DURATION}s"
  docker run --rm --network "${NETWORK}" williamyeh/wrk \
    -t"${THREADS}" -c"${CONNS}" -d"${DURATION}s" --latency \
    "http://${HTTP_HOST}:${HTTP_PORT}${path}" | tee "${out}"
done
