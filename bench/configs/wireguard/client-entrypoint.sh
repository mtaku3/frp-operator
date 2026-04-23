#!/bin/sh
# tunnel-client: brings up wg0, DNATs wg-inbound traffic to origin containers
# on the internal network.
set -eu

cp /etc/wireguard/client-wg0.conf /tmp/wg0.conf
chmod 600 /tmp/wg0.conf
wg-quick up /tmp/wg0.conf

# Resolve origin hostnames to IPs (iptables needs numeric addresses).
ORIGIN_IP=$(getent hosts origin       | awk '{print $1}')
HTTP_IP=$(getent  hosts origin-http   | awk '{print $1}')
[ -n "${ORIGIN_IP}" ] && [ -n "${HTTP_IP}" ] || { echo "origin DNS resolution failed" >&2; exit 1; }

iptables -t nat -A PREROUTING -i wg0 -p tcp --dport 5201 -j DNAT --to-destination ${ORIGIN_IP}:5201
iptables -t nat -A PREROUTING -i wg0 -p tcp --dport 80   -j DNAT --to-destination ${HTTP_IP}:80
iptables -t nat -A POSTROUTING -j MASQUERADE
iptables -A FORWARD -j ACCEPT

echo "tunnel-client ready (origin=${ORIGIN_IP}, http=${HTTP_IP})"
exec sleep infinity
