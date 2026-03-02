# my-abci-app

A Tendermint ABCI application that provides a persistent key-value store with an optional **price oracle**: each validator checks user-submitted asset prices against its own configured source (CoinGecko, Binance, or Kraken) before allowing the transaction into the block. **Supported pairs:** `BTC/USD`, `ETH/USD`, `SOL/USD`.

## What this project does

- **KV store**: Standard `key=value` transactions are stored in LevelDB and can be queried via Tendermint RPC.
- **Price oracle**: Transactions with the `price:<ASSET>=<value>` format (e.g. `price:BTC/USD=95000`) are validated in `CheckTx` against an external price source. Supported assets: **BTC/USD**, **ETH/USD**, **SOL/USD**. If the claimed price is within a configurable tolerance (default 5%) of the oracle price, the tx is accepted; otherwise it is rejected and never included in a block.
- **Three validators**: The app is designed to run with three Tendermint validator nodes, each connected to its own ABCI instance. Each ABCI instance can use a different price source (e.g. CoinGecko, Binance, Kraken) so the network only accepts prices that multiple oracles agree on.

## Prerequisites

- **Go 1.21+**
- **Tendermint** binary on your `PATH` (e.g. `go install github.com/tendermint/tendermint/cmd/tendermint@v0.34.24` or use your own build).
- Three Tendermint node home directories configured with different ports and the same genesis (e.g. `~/.tendermint`, `~/.tendermint2`, `~/.tendermint3`). Use the project’s setup instructions if you need to create or reset them.

## Build

```bash
go build -o kvstore-app .
```

## Run the network

One script starts all three ABCI apps and all three Tendermint nodes:

```bash
./start_network.sh
```

- **Live mode** (default): Each ABCI instance uses a real price API (CoinGecko, Binance, Kraken). Requires internet.
- **Mock mode** (offline): Use simulated price sources so no API calls are made:

```bash
./start_network.sh --mock
```

Press **Ctrl+C** to stop all processes.

### Port layout

| Component   | Node 1   | Node 2   | Node 3   |
|------------|----------|----------|----------|
| ABCI       | 26658    | 26659    | 26662    |
| RPC        | 26657    | 26660    | 26663    |
| P2P        | 26656    | 26661    | 26664    |

## Send transactions

Use any node’s RPC (e.g. Node 1 on port 26657).

**Key-value (any key=value):**

```bash
curl 'http://localhost:26657/broadcast_tx_commit?tx="name=alice"'
```

**Price (validated by oracle; supported: BTC/USD, ETH/USD, SOL/USD):**

```bash
# BTC/USD — succeed when close to live price or ~95000 (mock)
curl 'http://localhost:26657/broadcast_tx_commit?tx="price:BTC/USD=95000.00"'

# ETH/USD — succeed when close to live price or ~3500 (mock)
curl 'http://localhost:26657/broadcast_tx_commit?tx="price:ETH/USD=3500.00"'

# SOL/USD — succeed when close to live price or ~150 (mock)
curl 'http://localhost:26657/broadcast_tx_commit?tx="price:SOL/USD=150.00"'

# Likely rejected (too far from oracle price)
curl 'http://localhost:26657/broadcast_tx_commit?tx="price:BTC/USD=1.00"'
```

**Query stored value:**

```bash
curl 'http://localhost:26657/abci_query?data="name"'
curl 'http://localhost:26657/abci_query?data="price:BTC/USD"'
curl 'http://localhost:26657/abci_query?data="price:ETH/USD"'
curl 'http://localhost:26657/abci_query?data="price:SOL/USD"'
```

## Verify that nodes are running

**1. Check RPC and block height**

```bash
curl -s http://localhost:26657/status | jq '.result.sync_info.latest_block_height'
```

If the network is producing blocks, this height increases over time. You can do the same for the other nodes (ports 26660 and 26663).

**2. Check that RPC and P2P ports are in use**

```bash
lsof -i :26657   # Node 1 RPC
lsof -i :26656   # Node 1 P2P
lsof -i :26660   # Node 2 RPC
lsof -i :26663   # Node 3 RPC
```

**3. Optional: check peer count**

```bash
curl -s http://localhost:26657/net_info | jq '.result.n_peers'
```

You should see 2 peers when all three validators are running.

## Verify that the ABCI app is running

**1. Check that ABCI ports are listening**

Each Tendermint node talks to its ABCI app on a fixed port:

```bash
lsof -i :26658   # ABCI for Node 1
lsof -i :26659   # ABCI for Node 2
lsof -i :26662   # ABCI for Node 3
```

**2. Confirm blocks are being produced**

If Tendermint is connected to the ABCI app and consensus is working, blocks will advance:

```bash
curl -s http://localhost:26657/status | jq '.result.sync_info'
```

`latest_block_height` should increase every ~1 second when the network is healthy.

**3. Send a transaction and query**

If the ABCI app is running and Tendermint is connected, a committed tx will be applied and queryable:

```bash
curl 'http://localhost:26657/broadcast_tx_commit?tx="test=hello"'
curl 'http://localhost:26657/abci_query?data="test"'
```

You should see `"value":"hello"` (base64-encoded) in the query response.

**4. Console output**

When you run `./start_network.sh`, all six processes share the same terminal. Log lines prefixed with `[app1]`, `[app2]`, or `[app3]` come from the ABCI app (e.g. `BeginBlock`, `Commit`, `CheckTx OK`, `Price validated`). Tendermint logs are the ones with `module=consensus` or `module=state`.

## Running a single ABCI instance (manual)

For one node only:

```bash
./kvstore-app --name app1 --addr tcp://0.0.0.0:26658 --data-dir ./data1 --price-source coingecko
```

Then start Tendermint with the matching home (e.g. `tendermint start --home ~/.tendermint`).

## CLI flags (kvstore-app)

| Flag               | Default           | Description |
|--------------------|-------------------|-------------|
| `--addr`           | `tcp://0.0.0.0:26658` | ABCI listen address |
| `--data-dir`       | `./data`          | LevelDB data directory |
| `--name`           | (same as data-dir)| Instance name for logs |
| `--price-source`   | (none)            | `coingecko`, `binance`, `kraken`, `mock1`, `mock2`, `mock3` |
| `--price-tolerance`| `0.05` (5%)      | Max allowed deviation from oracle price |

## License

See the repository for license information.
