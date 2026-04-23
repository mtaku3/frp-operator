#!/bin/sh
# Generate rathole server config with iperf + http + idle[1..100] slots,
# then exec rathole. 100 pre-declared slots so S5 has headroom without
# needing to SIGHUP-reload mid-test.
set -e

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
  while [ "${i}" -le 100 ]; do
    port=$((16000 + i))
    cat <<EOF

[server.services.idle${i}]
bind_addr = "0.0.0.0:${port}"
EOF
    i=$((i + 1))
  done
} > /tmp/server.toml

exec rathole /tmp/server.toml
