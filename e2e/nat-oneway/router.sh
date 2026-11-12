#!/bin/sh
# Both engines can open outbound connections, including QUIC to the relay.
# New inbound to either engine is dropped. Engine A is also told it has no
# inbound path, so it dials and does not wait to be punched.
set -eu
sysctl -w net.ipv4.ip_forward=1
iptables -t nat -A POSTROUTING -s 172.30.80.0/24 -j MASQUERADE
iptables -t nat -A POSTROUTING -s 172.30.81.0/24 -j MASQUERADE
# Return traffic for outbound flows is allowed. New inbound, including UDP,
# is dropped, so nothing can punch in and STUN mappings are not reusable.
iptables -A FORWARD -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A FORWARD -s 172.30.80.0/24 -j ACCEPT
iptables -A FORWARD -s 172.30.81.0/24 -j ACCEPT
iptables -A FORWARD -d 172.30.80.10 -j DROP
iptables -A FORWARD -d 172.30.81.10 -j DROP
sleep infinity
