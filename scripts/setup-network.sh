#!/bin/bash -eu

switch='br0'

ip link add $switch type bridge
