#!/bin/bash
# Must be run as root: sudo ./test_restore.sh
# Clears all packet loss and network impairment rules.

if [ "$EUID" -ne 0 ]; then
    echo "Run with sudo"
    exit 1
fi

packetloss -f
netimpair -f 2>/dev/null
echo "All network impairments cleared"