# my-abci-app

A Tendermint ABCI application that provides a persistent key-value store with an optional **price oracle**: each validator checks user-submitted asset prices against its own configured source (CoinGecko, Binance, or Kraken) before allowing the transaction into the block. **Supported pairs:** `BTC/USD`, `ETH/USD`, `SOL/USD`.

## What this project does

- **KV store**: Standard `key=value` transactions are stored in LevelDB and can be queried via Tendermint RPC.
- **Price oracle**: Transactions with the `price:<ASSET>=<value>` format (e.g. `price:BTC/USD=95000`) are validated in `CheckTx` against an external price source. Supported assets: **BTC/USD**, **ETH/USD**, **SOL/USD**. If the claimed price is within a configurable tolerance (default 5%) of the oracle price, the tx is accepted; otherwise it is rejected.
- **Three validators**: Runs as three Tendermint validator nodes in Docker, each with its own ABCI instance. You can run with **live price APIs** (CoinGecko, Binance, Kraken) or **mock prices** (offline, no API calls).

## Prerequisites

- **Docker** and **Docker Compose** (v2+)

## Run the network (Docker)

From the project root:

```bash
docker compose up --build -d
```

This starts an init container (validator setup), then three node containers. Chain data is stored in Docker volumes; on the first run the chain starts at height 0. On later runs it **resumes from the latest block height** unless you remove the volumes.

**View logs:**

```bash
docker compose logs -f
```

**Stop (containers only; data kept):**

```bash
docker compose down
```

**Stop and wipe all chain data (next start from height 0):**

```bash
docker compose down -v
```

### Price mode: live vs mock

You can run the nodes with **real price APIs** or **mock (simulated) prices**.

| Mode   | Use case              | Internet |
|--------|------------------------|----------|
| **Live** (default) | Real BTC/ETH/SOL prices from CoinGecko, Binance, Kraken | Required |
| **Mock**          | Offline testing; simulated prices (~95000 BTC, ~3500 ETH, ~150 SOL) | Not required |

**Live price (default):**

```bash
docker compose up --build -d
```

**Mock price (offline):**

```bash
USE_MOCK_ORACLES=1 docker compose up --build -d
```

Or create a `.env` file in the project root:

```
USE_MOCK_ORACLES=1
```

Then run `docker compose up --build -d` and it will use mock oracles.

### Port layout

| Component | Node 1 | Node 2 | Node 3 |
|-----------|--------|--------|--------|
| RPC       | 26657  | 26660  | 26663  |
| P2P       | 26656  | 26661  | 26664  |

RPC is exposed on the host; use **Node 1** at `http://localhost:26657` for queries and sending transactions.

## Send transactions

Use Node 1's RPC (or any node's port from the table above).

**Key-value (any key=value):**

```bash
curl 'http://localhost:26657/broadcast_tx_commit?tx="name=alice"'
```

**Price (validated by oracle; supported: BTC/USD, ETH/USD, SOL/USD):**

```bash
# BTC/USD — succeeds when close to live price or ~95000 (mock)
curl 'http://localhost:26657/broadcast_tx_commit?tx="price:BTC/USD=95000.00"'

# ETH/USD — succeeds when close to live price or ~3500 (mock)
curl 'http://localhost:26657/broadcast_tx_commit?tx="price:ETH/USD=3500.00"'

# SOL/USD — succeeds when close to live price or ~150 (mock)
curl 'http://localhost:26657/broadcast_tx_commit?tx="price:SOL/USD=150.00"'

# Likely rejected (too far from oracle price)
curl 'http://localhost:26657/broadcast_tx_commit?tx="price:BTC/USD=1.00"'
```

**Query stored value:**

```bash
curl 'http://localhost:26657/abci_query?data="name"'
curl 'http://localhost:26657/abci_query?data="price:BTC/USD"'
```

## Verify the network

**Block height (should increase over time):**

```bash
curl -s http://localhost:26657/status | jq '.result.sync_info.latest_block_height'
```

**Peer count (expect 2 when all three validators are up):**

```bash
curl -s http://localhost:26657/net_info | jq '.result.n_peers'
```

**Quick test (send and query):**

```bash
curl 'http://localhost:26657/broadcast_tx_commit?tx="test=hello"'
curl 'http://localhost:26657/abci_query?data="test"'
```

## Summary

| Task | Command |
|------|---------|
| Start (live prices) | `docker compose up --build -d` |
| Start (mock prices) | `USE_MOCK_ORACLES=1 docker compose up --build -d` |
| Logs | `docker compose logs -f` |
| Stop (keep data) | `docker compose down` |
| Stop and reset chain | `docker compose down -v` |
| RPC (Node 1) | `http://localhost:26657` |

## License

See the repository for license information.
