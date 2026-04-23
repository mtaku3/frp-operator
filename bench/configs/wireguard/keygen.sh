#!/bin/sh
# Generate WireGuard keypairs and wg0.conf files into this directory.
# Idempotent: skips if keys already exist. Run from repo root or bench/.
set -eu

cd "$(dirname "$0")"

if ! command -v wg >/dev/null 2>&1; then
  echo "wg (wireguard-tools) not found on host. Install or run this inside a container." >&2
  echo "Fallback: docker run --rm -v \"\$PWD:/w\" -w /w alpine:3.19 sh -c 'apk add -q wireguard-tools && sh keygen.sh'" >&2
  exit 1
fi

if [ ! -f server.key ]; then
  wg genkey | tee server.key | wg pubkey > server.pub
  wg genkey | tee client.key | wg pubkey > client.pub
  chmod 600 server.key client.key
  echo "Generated keys."
fi

SERVER_KEY=$(cat server.key)
SERVER_PUB=$(cat server.pub)
CLIENT_KEY=$(cat client.key)
CLIENT_PUB=$(cat client.pub)

cat > server-wg0.conf <<EOF
[Interface]
Address = 10.200.0.1/24
ListenPort = 51820
PrivateKey = ${SERVER_KEY}

[Peer]
PublicKey = ${CLIENT_PUB}
AllowedIPs = 10.200.0.2/32
EOF

cat > client-wg0.conf <<EOF
[Interface]
Address = 10.200.0.2/24
PrivateKey = ${CLIENT_KEY}

[Peer]
PublicKey = ${SERVER_PUB}
Endpoint = tunnel-server:51820
AllowedIPs = 10.200.0.0/24
PersistentKeepalive = 25
EOF

chmod 600 server-wg0.conf client-wg0.conf
echo "Wrote server-wg0.conf, client-wg0.conf."
