#!/bin/sh
# tunnel-server: brings up wg0, DNATs wan-net ingress to client wg IP.
set -eu

# wg-quick won't work with a read-only mount; copy to tmpfs first.
cp /etc/wireguard/server-wg0.conf /tmp/wg0.conf
chmod 600 /tmp/wg0.conf
wg-quick up /tmp/wg0.conf

CLIENT_WG_IP=10.200.0.2

# DNAT exposed service ports -> client wg IP. Traffic egresses via wg0.
iptables -t nat -A PREROUTING -p tcp --dport 15200 -j DNAT --to-destination ${CLIENT_WG_IP}:5201
iptables -t nat -A PREROUTING -p tcp --dport 18080 -j DNAT --to-destination ${CLIENT_WG_IP}:80
# Must masquerade; otherwise return path from client won't come back here
# (probe's source IP is on wan-net, not routable from client's pov).
iptables -t nat -A POSTROUTING -o wg0 -j MASQUERADE
iptables -A FORWARD -i eth0 -o wg0 -j ACCEPT
iptables -A FORWARD -i wg0 -o eth0 -m state --state RELATED,ESTABLISHED -j ACCEPT

echo "tunnel-server ready"
exec sleep infinity
