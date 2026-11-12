#!/bin/sh
# Cone NAT: each engine can open outbound connections, including to the
# public relay. New inbound forwards to either engine are dropped, and the
# two LANs have no route to each other except through that NAT.
set -eu
sysctl -w net.ipv4.ip_forward=1
iptables -t nat -A POSTROUTING -s 172.30.70.0/24 -j MASQUERADE
iptables -t nat -A POSTROUTING -s 172.30.71.0/24 -j MASQUERADE
iptables -A FORWARD -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A FORWARD -s 172.30.70.0/24 -j ACCEPT
iptables -A FORWARD -s 172.30.71.0/24 -j ACCEPT
iptables -A FORWARD -d 172.30.70.10 -j DROP
iptables -A FORWARD -d 172.30.71.10 -j DROP
sleep infinity
