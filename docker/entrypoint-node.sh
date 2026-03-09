#!/usr/bin/env bash
# Run a single Tendermint node + ABCI app (NODE_INDEX=1, 2, or 3).
# Expects chain-nodes volume mounted at /app/nodes with config from init.
set -euo pipefail

APP_ROOT="${APP_ROOT:-/app}"
NODES_DIR="${APP_ROOT}/nodes"
KVSTORE_APP="${APP_ROOT}/kvstore-app"
TENDERMINT="tendermint"
NODE_INDEX="${NODE_INDEX:-1}"

if [[ ! "$NODE_INDEX" =~ ^[123]$ ]]; then
  echo "NODE_INDEX must be 1, 2, or 3"
  exit 1
fi

# Price sources
if [[ "${USE_MOCK_ORACLES:-0}" == "1" ]]; then
  SRC="mock${NODE_INDEX}"
  echo "=== MOCK MODE: node${NODE_INDEX} source=$SRC ==="
else
  case "$NODE_INDEX" in
    1) SRC="coingecko" ;;
    2) SRC="binance" ;;
    3) SRC="kraken" ;;
  esac
  echo "=== LIVE MODE: node${NODE_INDEX} source=$SRC ==="
fi

# Ports per node
case "$NODE_INDEX" in
  1) ABCI_ADDR="tcp://0.0.0.0:26658" ; DATA_DIR="${APP_ROOT}/data1" ;;
  2) ABCI_ADDR="tcp://0.0.0.0:26659" ; DATA_DIR="${APP_ROOT}/data2" ;;
  3) ABCI_ADDR="tcp://0.0.0.0:26662" ; DATA_DIR="${APP_ROOT}/data3" ;;
esac

NODE_HOME="${NODES_DIR}/node${NODE_INDEX}"
if [[ ! -f "${NODE_HOME}/config/genesis.json" ]]; then
  echo "No genesis at ${NODE_HOME}/config/genesis.json — run init first."
  exit 1
fi

PIDS=()
cleanup() {
  echo "Shutting down node${NODE_INDEX}..."
  for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "Starting ABCI app${NODE_INDEX} (${ABCI_ADDR}, data=${DATA_DIR})"
"$KVSTORE_APP" --name "app${NODE_INDEX}" --addr "$ABCI_ADDR" --data-dir "$DATA_DIR" --price-source "$SRC" &
PIDS+=($!)

sleep 2
echo "Starting Tendermint node${NODE_INDEX} (home=${NODE_HOME})"
"$TENDERMINT" start --home "$NODE_HOME" &
PIDS+=($!)

echo "=== Node ${NODE_INDEX} running ==="
wait
