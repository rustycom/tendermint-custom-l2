#!/usr/bin/env bash
# Creates three Tendermint validator node directories (./nodes/node1, node2, node3)
# with a shared genesis containing all three validators, and configs that use
# different ports and persistent_peers so they can run on one machine.
# Run once after cloning. Requires: tendermint (or we install via go), python3.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
NODES_DIR="${ROOT_DIR}/nodes"

# Ensure tendermint binary exists
TENDERMINT="$(command -v tendermint 2>/dev/null)" || true
if [[ -z "$TENDERMINT" ]]; then
  echo "Tendermint not found in PATH. Installing via go install..."
  (cd "${ROOT_DIR}" && go install github.com/tendermint/tendermint/cmd/tendermint@v0.34.24)
  TENDERMINT="$(go env GOPATH)/bin/tendermint"
  if [[ ! -x "$TENDERMINT" ]]; then
    echo "Failed to install tendermint. Install manually: go install github.com/tendermint/tendermint/cmd/tendermint@v0.34.24"
    exit 1
  fi
  echo "Installed Tendermint at $TENDERMINT"
fi

echo "Using Tendermint: $TENDERMINT"
"$TENDERMINT" version

# Remove only node subdirs (so Docker volume mount at NODES_DIR is not removed)
rm -rf "${NODES_DIR}/node1" "${NODES_DIR}/node2" "${NODES_DIR}/node3"
mkdir -p "${NODES_DIR}"

# Initialize each node (creates config + data dirs and keys)
for n in 1 2 3; do
  "${TENDERMINT}" init --home "${NODES_DIR}/node${n}" 2>/dev/null || true
done

# Build merged genesis with all three validators
export NODES_DIR
python3 << 'PY'
import json, os
nodes_dir = os.environ["NODES_DIR"]
validators = []
for n in [1, 2, 3]:
    path = os.path.join(nodes_dir, f"node{n}", "config", "priv_validator_key.json")
    with open(path) as f:
        key = json.load(f)
    validators.append({
        "address": key["address"],
        "pub_key": key["pub_key"],
        "power": "10",
        "name": ""
    })
# Use node1's genesis as template
with open(os.path.join(nodes_dir, "node1", "config", "genesis.json")) as f:
    genesis = json.load(f)
genesis["validators"] = validators
for n in [1, 2, 3]:
    out = os.path.join(nodes_dir, f"node{n}", "config", "genesis.json")
    with open(out, "w") as f:
        json.dump(genesis, f, indent=2)
print("Merged genesis with 3 validators written to all nodes.")
PY

# Get node IDs for persistent_peers (extract 40-char hex only — unreleased Tendermint prints deprecation to stdout)
get_node_id() {
  "$TENDERMINT" show_node_id --home "$1" 2>/dev/null | tr -d '\n' | grep -oE '[0-9a-f]{40}$' || "$TENDERMINT" show_node_id --home "$1" 2>/dev/null | tail -1 | tr -d '\n'
}
NODE1_ID=$(get_node_id "${NODES_DIR}/node1")
NODE2_ID=$(get_node_id "${NODES_DIR}/node2")
NODE3_ID=$(get_node_id "${NODES_DIR}/node3")

# P2P peer hostnames (default 127.0.0.1 for local; set to node1, node2, node3 for Docker)
P2P_NODE1_HOST="${P2P_NODE1_HOST:-127.0.0.1}"
P2P_NODE2_HOST="${P2P_NODE2_HOST:-127.0.0.1}"
P2P_NODE3_HOST="${P2P_NODE3_HOST:-127.0.0.1}"

PEERS_1="${NODE2_ID}@${P2P_NODE2_HOST}:26661,${NODE3_ID}@${P2P_NODE3_HOST}:26664"
PEERS_2="${NODE1_ID}@${P2P_NODE1_HOST}:26656,${NODE3_ID}@${P2P_NODE3_HOST}:26664"
PEERS_3="${NODE1_ID}@${P2P_NODE1_HOST}:26656,${NODE2_ID}@${P2P_NODE2_HOST}:26661"

# When using Docker peer hostnames, bind RPC to 0.0.0.0 so the host can reach it
if [[ "$P2P_NODE1_HOST" != "127.0.0.1" ]]; then
  export RPC_BIND_HOST="0.0.0.0"
else
  export RPC_BIND_HOST="${RPC_BIND_HOST:-127.0.0.1}"
fi

# Patch config.toml for each node (proxy_app, rpc.laddr, p2p.laddr, persistent_peers, addr_book_strict)
# Note: Tendermint may use tcp://0.0.0.0:26657 or tcp://127.0.0.1:26657 for RPC depending on version
patch_config() {
  local home="$1"
  local proxy_app="$2"
  local rpc_laddr="$3"
  local p2p_laddr="$4"
  local peers="$5"
  peers="${peers//$'\n'/}"   # strip newlines for sed
  local config="${home}/config/config.toml"
  local rpc_bind="${RPC_BIND_HOST:-127.0.0.1}"
  if [[ ! -f "$config" ]]; then
    echo "Missing config: $config"
    return 1
  fi
  if sed --version 2>/dev/null | grep -q GNU; then
    sed -i "s|^proxy_app = .*|proxy_app = \"${proxy_app}\"|" "$config"
    sed -i "s|tcp://127.0.0.1:26657|tcp://${rpc_bind}:${rpc_laddr}|" "$config"
    sed -i "s|tcp://0.0.0.0:26657|tcp://${rpc_bind}:${rpc_laddr}|" "$config"
    sed -i "s|tcp://0.0.0.0:26656|tcp://0.0.0.0:${p2p_laddr}|" "$config"
    sed -i "s|^persistent_peers = .*|persistent_peers = \"${peers}\"|" "$config"
    grep -q '^persistent_peers_max_dial_period' "$config" && sed -i 's|^persistent_peers_max_dial_period = .*|persistent_peers_max_dial_period = "15s"|' "$config" || true
    grep -q '^addr_book_strict' "$config" && sed -i 's/^addr_book_strict = .*/addr_book_strict = false/' "$config" || true
  else
    sed -i '' "s|^proxy_app = .*|proxy_app = \"${proxy_app}\"|" "$config"
    sed -i '' "s|tcp://127.0.0.1:26657|tcp://${rpc_bind}:${rpc_laddr}|" "$config"
    sed -i '' "s|tcp://0.0.0.0:26657|tcp://${rpc_bind}:${rpc_laddr}|" "$config"
    sed -i '' "s|tcp://0.0.0.0:26656|tcp://0.0.0.0:${p2p_laddr}|" "$config"
    sed -i '' "s|^persistent_peers = .*|persistent_peers = \"${peers}\"|" "$config"
    grep -q '^persistent_peers_max_dial_period' "$config" && sed -i '' 's|^persistent_peers_max_dial_period = .*|persistent_peers_max_dial_period = "15s"|' "$config" || true
    grep -q '^addr_book_strict' "$config" && sed -i '' 's/^addr_book_strict = .*/addr_book_strict = false/' "$config" || true
  fi
}

patch_config "${NODES_DIR}/node1" "tcp://127.0.0.1:26658" "26657" "26656" "$PEERS_1"
patch_config "${NODES_DIR}/node2" "tcp://127.0.0.1:26659" "26660" "26661" "$PEERS_2"
patch_config "${NODES_DIR}/node3" "tcp://127.0.0.1:26662" "26663" "26664" "$PEERS_3"

echo ""
echo "=== Validator setup complete ==="
echo "Node directories: ${NODES_DIR}/node1, node2, node3"
echo "Run the network from repo root: ./start_network.sh"
echo "Or with mock oracles: ./start_network.sh --mock"
