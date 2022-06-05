#!/bin/bash -eu

ip link add br0 type bridge

ip tuntap add dev tap0 mode tap
ip tuntap add dev tap1 mode tap

ip link set dev tap0 master br0
ip link set dev tap1 master br0

iptables -t nat -A POSTROUTING -o wlp1s0 -j MASQUERADE
