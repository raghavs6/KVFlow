#!/usr/bin/env bash
# Prints the time and the ENA bw_<dir>_allowance_exceeded counter every 5 s
# until killed. <dir> is in or out.
dev=$(ip -o -4 route show to default | awk '{print $5}')
while true; do
  echo "$(date +%s.%N),$(ethtool -S "$dev" | awk -F': ' "/bw_$1_allowance_exceeded/{print \$2}")"
  sleep 5
done
