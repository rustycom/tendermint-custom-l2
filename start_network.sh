#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TENDERMINT="$(command -v tendermint)"
KVSTORE_APP="${SCRIPT_DIR}/kvstore-app"

# Use --mock flag to switch all three nodes to offline mock price sources.
if [[ "${1:-}" == "--mock" ]]; then
    SRC1="mock1"
    SRC2="mock2"
    SRC3="mock3"
    echo "=== MOCK MODE: using simulated price sources ==="
else
    SRC1="coingecko"
    SRC2="binance"
    SRC3="kraken"
    echo "=== LIVE MODE: using real price APIs ==="
fi

PIDS=()

cleanup() {
    echo ""
    echo "Shutting down all processes..."
    for pid in "${PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null
    echo "All processes stopped."
}
trap cleanup EXIT INT TERM

echo ""
echo "=== Starting 3 ABCI app instances ==="

"$KVSTORE_APP" --name app1 --addr tcp://0.0.0.0:26658 --data-dir "${SCRIPT_DIR}/data1" --price-source "$SRC1" &
PIDS+=($!)
echo "  ABCI app1 (port 26658, data1, source=$SRC1) — PID $!"

"$KVSTORE_APP" --name app2 --addr tcp://0.0.0.0:26659 --data-dir "${SCRIPT_DIR}/data2" --price-source "$SRC2" &
PIDS+=($!)
echo "  ABCI app2 (port 26659, data2, source=$SRC2) — PID $!"

"$KVSTORE_APP" --name app3 --addr tcp://0.0.0.0:26662 --data-dir "${SCRIPT_DIR}/data3" --price-source "$SRC3" &
PIDS+=($!)
echo "  ABCI app3 (port 26662, data3, source=$SRC3) — PID $!"

sleep 2
echo ""
echo "=== Starting 3 Tendermint validator nodes ==="

"$TENDERMINT" start --home ~/.tendermint &
PIDS+=($!)
echo "  Node 1 (P2P=26656, RPC=26657, ABCI->26658) — PID $!"

"$TENDERMINT" start --home ~/.tendermint2 &
PIDS+=($!)
echo "  Node 2 (P2P=26661, RPC=26660, ABCI->26659) — PID $!"

"$TENDERMINT" start --home ~/.tendermint3 &
PIDS+=($!)
echo "  Node 3 (P2P=26664, RPC=26663, ABCI->26662) — PID $!"

echo ""
echo "=== Network running (3 validators). Press Ctrl+C to stop all. ==="
echo "=== Submit price tx: curl 'http://localhost:26657/broadcast_tx_commit?tx=\"price:BTC/USD=95000.00\"' ==="
wait
