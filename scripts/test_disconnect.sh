#!/bin/bash
# Must be run as root: sudo ./test_disconnect.sh [duration] [ports]
# Simulates a full network disconnect for N seconds then restores.
#
# Usage:
#   sudo ./test_disconnect.sh                     # 15s on default ports 15657,15667
#   sudo ./test_disconnect.sh 30                  # 30s on default ports
#   sudo ./test_disconnect.sh 30 15657            # 30s on port 15657 only
#   sudo ./test_disconnect.sh 30 15657,15667      # 30s on both ports

if [ "$EUID" -ne 0 ]; then
    echo "Run with sudo"
    exit 1
fi

DURATION=${1:-15}
PORTS=${2:-15657,15667}

packetloss -p "$PORTS" -r 1.0
echo "Disconnected on ports ${PORTS}. Restoring in ${DURATION}s..."
sleep "$DURATION"
packetloss -f
echo "Reconnected"