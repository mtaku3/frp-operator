#!/bin/sh
# Regenerate server.toml with iperf + http + idle{1..100} slots.
# Run whenever the slot count needs to change.
set -e
out="$(dirname "$0")/server.toml"
{
  cat <<'EOF'
[server]
bind_addr = "0.0.0.0:2333"
default_token = "benchtoken"

[server.services.iperf]
bind_addr = "0.0.0.0:15200"

[server.services.http]
bind_addr = "0.0.0.0:18080"
EOF
  i=1
  while [ "$i" -le 100 ]; do
    port=$((16000 + i))
    printf '\n[server.services.idle%d]\nbind_addr = "0.0.0.0:%d"\n' "$i" "$port"
    i=$((i + 1))
  done
} > "$out"
echo "wrote $out"
