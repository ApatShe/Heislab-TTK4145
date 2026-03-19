#!/bin/bash
# Must be run as root: sudo ./test_packetloss.sh [rate] [ports]
# Applies packet loss on elevator network ports.
# Mirrors the packetloss.d script used by graders.
# Ctrl+C or run 'sudo ./test_restore.sh' to clear.
#
# Usage:
#   sudo ./test_packetloss.sh                     # 50% loss on default ports 15657,15667
#   sudo ./test_packetloss.sh 0.25                # 25% loss on default ports
#   sudo ./test_packetloss.sh 0.25 15657          # 25% loss on port 15657 only
#   sudo ./test_packetloss.sh 0.25 15657,15667    # 25% loss on both ports

if [ "$EUID" -ne 0 ]; then
    echo "Run with sudo"
    exit 1
fi

RATE=${1:-0.5}
PORTS=${2:-15657,15667}

packetloss -p "$PORTS" -r "$RATE"
echo "Packet loss set to ${RATE} on ports ${PORTS}"
echo "Ctrl+C or run 'sudo ./test_restore.sh' to clear"

trap 'packetloss -f; echo "Cleared"' EXIT
while true; do sleep 1; done