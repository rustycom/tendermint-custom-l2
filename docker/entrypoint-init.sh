#!/usr/bin/env bash
# One-time init: if no genesis exists, run validator setup with Docker peer hostnames (node1, node2, node3).
# Writes to /app/nodes (chain-nodes volume). Exit 0 so node containers can start.
set -euo pipefail

APP_ROOT="${APP_ROOT:-/app}"
NODES_DIR="${NODES_DIR:-${APP_ROOT}/nodes}"
cd "$APP_ROOT"

if [[ -f "${NODES_DIR}/node1/config/genesis.json" ]]; then
  echo "=== Genesis already present — skip init ==="
  exit 0
fi

echo "=== Running validator setup for Docker (peer hostnames: node1, node2, node3) ==="
export NODES_DIR
export P2P_NODE1_HOST=node1
export P2P_NODE2_HOST=node2
export P2P_NODE3_HOST=node3
bash "${APP_ROOT}/scripts/setup_validators.sh"
echo "=== Init complete ==="
exit 0
