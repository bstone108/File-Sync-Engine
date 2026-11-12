#!/bin/sh
# Engine A has no inbound path at all: every UDP packet toward it is dropped,
# including replies, so STUN and QUIC cannot build a mapping. TCP replies to
# its own outbound connections still work, which is how it reaches the relay.
# Engine B is a cone NAT: established flows return, and new inbound is dropped.
set -eu
sysctl -w net.ipv4.ip_forward=1
iptables -t nat -A POSTROUTING -s 172.30.80.0/24 -j MASQUERADE
iptables -t nat -A POSTROUTING -s 172.30.81.0/24 -j MASQUERADE
iptables -A FORWARD -d 172.30.80.10 -p udp -j DROP
iptables -A FORWARD -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A FORWARD -s 172.30.80.0/24 -j ACCEPT
iptables -A FORWARD -s 172.30.81.0/24 -j ACCEPT
iptables -A FORWARD -d 172.30.80.10 -j DROP
iptables -A FORWARD -d 172.30.81.10 -j DROP
sleep infinity
