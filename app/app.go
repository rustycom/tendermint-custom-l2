package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"my-abci-app/price"

	abcitypes "github.com/tendermint/tendermint/abci/types"
	dbm "github.com/tendermint/tm-db"
)

const priceTxPrefix = "price:"

// ---------------------------------------------------------------------------
// DB key layout
//
// We share a single LevelDB file but use key prefixes to separate concerns:
//
//   "d:<userkey>"  →  <uservalue>        (all application data)
//   "_height"      →  8-byte big-endian  (last committed block height)
//   "_apphash"     →  32-byte SHA-256    (last committed app hash)
//
// The "d:" prefix on user keys lets us iterate ONLY user data when computing
// the app hash, without accidentally picking up internal metadata keys.
// ---------------------------------------------------------------------------
const (
	prefixData     = "d:"       // all user data lives under this prefix
	keyLastHeight  = "_height"  // stores the last committed block height
	keyLastAppHash = "_apphash" // stores the last committed app hash
)

// ---------------------------------------------------------------------------
// KVStoreApp — a persistent ABCI key-value store backed by LevelDB.
//
// Lifecycle of a write:
//
//   1. CheckTx      — format check only; no state change.
//   2. BeginBlock   — reset the staged map; record current block height.
//   3. DeliverTx    — parse "key=value"; write to staged (in-memory).
//   4. EndBlock     — nothing to do.
//   5. Commit       — flush staged → LevelDB via an atomic batch.
//                     Persist height + app hash in the SAME batch so a
//                     crash mid-commit can never leave the DB in a half-
//                     written state.
//
// On the next restart, Info() reads height & app hash from LevelDB so
// Tendermint knows exactly where the app left off and can replay any
// missing blocks from its WAL.
// ---------------------------------------------------------------------------
type KVStoreApp struct {
	abcitypes.BaseApplication

	name           string            // human-readable instance identifier for log output
	db             dbm.DB            // on-disk LevelDB database (survives restarts)
	staged         map[string][]byte // pending writes for the current in-flight block
	currentHeight  int64             // height of the block being processed (set in BeginBlock)
	priceFetcher   price.PriceFetcher // oracle source used in CheckTx to validate price txs (may be nil)
	priceTolerance float64            // max allowed relative deviation (e.g. 0.05 = 5%)

	mu sync.RWMutex // protects staged + currentHeight from concurrent CheckTx / Query
}

// NewKVStoreApp opens (or creates) a LevelDB database at dataDir/kvstore.db
// and returns a ready-to-use KVStoreApp.
//
// dbm.NewDB(name, backend, dir) creates a file named <name>.db inside <dir>.
// GoLevelDBBackend is pure Go — no CGo required, works everywhere.
func NewKVStoreApp(name, dataDir string, fetcher price.PriceFetcher, tolerance float64) (*KVStoreApp, error) {
	db, err := dbm.NewDB("kvstore", dbm.GoLevelDBBackend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dataDir, err)
	}
	return &KVStoreApp{
		name:           name,
		db:             db,
		staged:         make(map[string][]byte),
		priceFetcher:   fetcher,
		priceTolerance: tolerance,
	}, nil
}

// Close shuts down the LevelDB database cleanly.
// Always call this (via defer) before the process exits.
func (app *KVStoreApp) Close() error {
	return app.db.Close()
}

func (app *KVStoreApp) log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("[%s] %s\n", app.name, msg)
}

// ---------------------------------------------------------------------------
// ABCI method: Info
//
// Tendermint calls this at node startup to learn where the app left off.
// We read the last committed height and app hash from LevelDB and return them.
//
// Tendermint then compares our LastBlockHeight with its own WAL:
//   - If they match → no replay needed, proceed normally.
//   - If app is behind → Tendermint replays the missing blocks so our state
//     catches up before new blocks are produced.
//
// Without persisting height/hash, the app would report height=0 on every
// restart, forcing a full replay from genesis every time — very slow.
// ---------------------------------------------------------------------------
func (app *KVStoreApp) Info(req abcitypes.RequestInfo) abcitypes.ResponseInfo {
	height := app.loadLastHeight()
	appHash := app.loadLastAppHash()
	app.log("Info called — last_height=%d", height)

	return abcitypes.ResponseInfo{
		Data:             "persistent-kv-store",
		Version:          "1.0.0",
		AppVersion:       1,
		LastBlockHeight:  height,  // tells Tendermint "I have committed up to this block"
		LastBlockAppHash: appHash, // the app hash at that height (used for integrity checks)
	}
}

// ---------------------------------------------------------------------------
// ABCI method: InitChain
//
// Called exactly once at genesis.  You can seed initial state from the
// genesis.json app_state field here.  We have nothing to seed.
// ---------------------------------------------------------------------------
func (app *KVStoreApp) InitChain(req abcitypes.RequestInitChain) abcitypes.ResponseInitChain {
	app.log("InitChain — chain_id=%s validators=%d", req.ChainId, len(req.Validators))
	return abcitypes.ResponseInitChain{}
}

// ---------------------------------------------------------------------------
// ABCI method: CheckTx
//
// Lightweight gate before the mempool.  Stateless format check only.
// We never write to DB here — CheckTx can be called multiple times per tx.
// ---------------------------------------------------------------------------
func (app *KVStoreApp) CheckTx(req abcitypes.RequestCheckTx) abcitypes.ResponseCheckTx {
	if err := app.validateTx(req.Tx); err != nil {
		app.log("CheckTx REJECTED: %s", err)
		return abcitypes.ResponseCheckTx{Code: 1, Log: err.Error()}
	}

	if err := app.validatePriceTx(req.Tx); err != nil {
		app.log("CheckTx PRICE REJECTED: %s", err)
		return abcitypes.ResponseCheckTx{Code: 2, Log: err.Error()}
	}

	app.log("CheckTx OK: %s", req.Tx)
	return abcitypes.ResponseCheckTx{Code: 0}
}

// ---------------------------------------------------------------------------
// ABCI method: BeginBlock
//
// Called once per block before any transactions are delivered.
// We capture the block height (needed in Commit to persist it) and reset
// the staged write buffer so each block starts clean.
// ---------------------------------------------------------------------------
func (app *KVStoreApp) BeginBlock(req abcitypes.RequestBeginBlock) abcitypes.ResponseBeginBlock {
	app.mu.Lock()
	defer app.mu.Unlock()

	app.currentHeight = req.Header.Height
	app.staged = make(map[string][]byte)
	app.log("BeginBlock height=%d", app.currentHeight)

	return abcitypes.ResponseBeginBlock{}
}

// ---------------------------------------------------------------------------
// ABCI method: DeliverTx
//
// Called once per transaction in order.  Real state transitions happen here.
// We parse "key=value" and write into the staged buffer (not LevelDB yet).
// The data only lands on disk when Commit() is called.
//
// Keeping writes staged means a crash between DeliverTx and Commit leaves
// the DB untouched — Tendermint will simply re-deliver the block on restart.
// ---------------------------------------------------------------------------
func (app *KVStoreApp) DeliverTx(req abcitypes.RequestDeliverTx) abcitypes.ResponseDeliverTx {
	if err := app.validateTx(req.Tx); err != nil {
		return abcitypes.ResponseDeliverTx{Code: 1, Log: err.Error()}
	}

	// Split on the first "=" only — values may themselves contain "=".
	parts := bytes.SplitN(req.Tx, []byte("="), 2)
	key := string(parts[0])
	value := parts[1]

	app.mu.Lock()
	app.staged[key] = value
	app.mu.Unlock()

	app.log("DeliverTx: %s=%s", key, value)
	return abcitypes.ResponseDeliverTx{
		Code: 0,
		Log:  fmt.Sprintf("staged %s", key),
	}
}

// ---------------------------------------------------------------------------
// ABCI method: EndBlock
//
// Called after the last DeliverTx.  Return validator / consensus-param
// updates here if your app needs them.  We have none.
// ---------------------------------------------------------------------------
func (app *KVStoreApp) EndBlock(req abcitypes.RequestEndBlock) abcitypes.ResponseEndBlock {
	return abcitypes.ResponseEndBlock{}
}

// ---------------------------------------------------------------------------
// ABCI method: Commit
//
// This is the point of no return.  We:
//   1. Compute the new app hash (merging DB + staged so it covers new data).
//   2. Open a write batch and add every staged key-value pair.
//   3. Add the new height and app hash to the SAME batch.
//   4. Call WriteSync() — all writes land on disk atomically.
//
// Why a batch?
//   A batch is an atomic unit in LevelDB.  Either ALL entries in the batch
//   are written, or NONE are (even if the process crashes mid-write).
//   This prevents the DB from ending up in a state where some keys are
//   written but the height/hash metadata isn't updated yet.
//
// Why WriteSync() instead of Write()?
//   Write() buffers data in the OS page cache — a crash before the OS flushes
//   could lose the data.  WriteSync() calls fsync, guaranteeing the data is
//   on the physical disk before we return.
// ---------------------------------------------------------------------------
func (app *KVStoreApp) Commit() abcitypes.ResponseCommit {
	app.mu.Lock()
	defer app.mu.Unlock()

	// Step 1: compute the new app hash BEFORE writing.
	// computeAppHash reads the current DB and overlays staged, so it
	// represents the state after this block is applied.
	appHash := app.computeAppHash()

	// Step 2: open a write batch.
	batch := app.db.NewBatch()
	defer batch.Close()

	// Step 3a: write all staged user data with the "d:" prefix.
	for k, v := range app.staged {
		dbKey := []byte(prefixData + k) // e.g. "d:name" → "alice"
		if err := batch.Set(dbKey, v); err != nil {
			panic(fmt.Sprintf("batch.Set key=%s failed: %v", k, err))
		}
	}

	// Step 3b: persist the committed block height.
	if err := batch.Set([]byte(keyLastHeight), encodeInt64(app.currentHeight)); err != nil {
		panic(fmt.Sprintf("batch.Set height failed: %v", err))
	}

	// Step 3c: persist the app hash for this block.
	if err := batch.Set([]byte(keyLastAppHash), appHash); err != nil {
		panic(fmt.Sprintf("batch.Set apphash failed: %v", err))
	}

	// Step 4: flush everything to disk atomically.
	if err := batch.WriteSync(); err != nil {
		panic(fmt.Sprintf("batch.WriteSync failed: %v", err))
	}

	// Clear the staging area — these writes are now in LevelDB.
	app.staged = make(map[string][]byte)

	app.log("Commit height=%d app_hash=%X", app.currentHeight, appHash)
	return abcitypes.ResponseCommit{
		Data: appHash,
	}
}

// ---------------------------------------------------------------------------
// ABCI method: Query
//
// Clients read committed state here.  We look up the key in LevelDB.
// Staged (uncommitted) changes from the current block are NOT visible.
// ---------------------------------------------------------------------------
func (app *KVStoreApp) Query(req abcitypes.RequestQuery) abcitypes.ResponseQuery {
	app.mu.RLock()
	defer app.mu.RUnlock()

	key := string(req.Data)

	// db.Get returns (nil, nil) when the key doesn't exist — not an error.
	value, err := app.db.Get([]byte(prefixData + key))
	if err != nil {
		return abcitypes.ResponseQuery{Code: 2, Log: fmt.Sprintf("db error: %v", err)}
	}
	if value == nil {
		return abcitypes.ResponseQuery{Code: 1, Log: fmt.Sprintf("key '%s' not found", key)}
	}

	app.log("Query: key=%s value=%s", key, value)
	return abcitypes.ResponseQuery{
		Code:  0,
		Key:   req.Data,
		Value: value,
		Log:   "exists",
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// validatePriceTx checks transactions with the "price:" prefix against the
// configured oracle source. Returns nil for non-price transactions or when
// no price fetcher is configured.
//
// This runs in CheckTx only (non-deterministic, per-node). Each validator can
// independently accept or reject based on its own oracle source.
func (app *KVStoreApp) validatePriceTx(tx []byte) error {
	parts := bytes.SplitN(tx, []byte("="), 2)
	key := string(parts[0])

	if !strings.HasPrefix(key, priceTxPrefix) {
		return nil
	}
	if app.priceFetcher == nil {
		return nil
	}

	asset := strings.TrimPrefix(key, priceTxPrefix)
	claimedPrice, err := strconv.ParseFloat(string(parts[1]), 64)
	if err != nil {
		return fmt.Errorf("invalid price value: %w", err)
	}

	oraclePrice, err := app.priceFetcher.FetchPrice(asset)
	if err != nil {
		return fmt.Errorf("oracle fetch failed: %w", err)
	}

	if oraclePrice == 0 {
		return fmt.Errorf("oracle returned zero price for %s", asset)
	}

	deviation := math.Abs(claimedPrice-oraclePrice) / oraclePrice
	if deviation > app.priceTolerance {
		return fmt.Errorf(
			"price %s: claimed=%.2f oracle(%s)=%.2f deviation=%.2f%% exceeds tolerance=%.2f%%",
			asset, claimedPrice, app.priceFetcher.Name(), oraclePrice,
			deviation*100, app.priceTolerance*100,
		)
	}

	app.log("Price validated: %s claimed=%.2f oracle(%s)=%.2f deviation=%.2f%%",
		asset, claimedPrice, app.priceFetcher.Name(), oraclePrice, deviation*100)
	return nil
}

// validateTx ensures the transaction is in "key=value" format.
func (app *KVStoreApp) validateTx(tx []byte) error {
	parts := bytes.SplitN(tx, []byte("="), 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid format: expected 'key=value', got '%s'", tx)
	}
	if len(parts[0]) == 0 {
		return fmt.Errorf("key cannot be empty")
	}
	return nil
}

// computeAppHash builds a deterministic SHA-256 over the ENTIRE application
// state (committed DB keys + current staged writes).
//
// Why deterministic?
//   Every validator runs the same state machine and MUST produce an identical
//   hash.  Go maps iterate in random order, so we collect all keys, SORT
//   them, then build the hash input.  Any two nodes with the same data will
//   always produce the same hash regardless of insertion order.
//
// Algorithm:
//   1. Iterate all "d:" prefixed keys in LevelDB  →  committed state.
//   2. Overlay staged writes (staged takes priority, simulating a commit).
//   3. Sort keys.
//   4. SHA-256( "k1=v1;k2=v2;..." )
func (app *KVStoreApp) computeAppHash() []byte {
	// Step 1: read all committed user data from LevelDB.
	merged := make(map[string][]byte)

	start := []byte(prefixData)
	end := prefixEndBytes(start) // "e:" — the first key that is NOT in "d:" range

	iter, err := app.db.Iterator(start, end)
	if err != nil {
		panic(fmt.Sprintf("db.Iterator failed: %v", err))
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		// Strip the "d:" prefix to recover the raw user key.
		rawKey := strings.TrimPrefix(string(iter.Key()), prefixData)
		// Copy the value — the iterator's underlying buffer may be reused.
		val := make([]byte, len(iter.Value()))
		copy(val, iter.Value())
		merged[rawKey] = val
	}
	if err := iter.Error(); err != nil {
		panic(fmt.Sprintf("iterator error: %v", err))
	}

	// Step 2: overlay staged writes — these are the new changes for this block.
	for k, v := range app.staged {
		merged[k] = v
	}

	// Step 3: sort keys for determinism.
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Step 4: build the hash input string and SHA-256 it.
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.Write(merged[k])
		sb.WriteString(";")
	}

	hash := sha256.Sum256([]byte(sb.String()))
	return hash[:]
}

// loadLastHeight reads the persisted block height from LevelDB.
// Returns 0 if the database is brand new (no block committed yet).
func (app *KVStoreApp) loadLastHeight() int64 {
	b, err := app.db.Get([]byte(keyLastHeight))
	if err != nil || b == nil {
		return 0
	}
	return decodeInt64(b)
}

// loadLastAppHash reads the persisted app hash from LevelDB.
// Returns nil if the database is brand new.
func (app *KVStoreApp) loadLastAppHash() []byte {
	b, err := app.db.Get([]byte(keyLastAppHash))
	if err != nil {
		return nil
	}
	return b
}

// encodeInt64 serialises an int64 as an 8-byte big-endian slice.
// Big-endian keeps byte ordering consistent across architectures.
func encodeInt64(n int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(n))
	return b
}

// decodeInt64 deserialises 8 big-endian bytes back into an int64.
func decodeInt64(b []byte) int64 {
	return int64(binary.BigEndian.Uint64(b))
}

// prefixEndBytes returns the smallest key that is strictly greater than any
// key with the given prefix.  Used to define the upper bound of an iterator.
//
// Example:  "d:" → "e:"
//           "d\xff" → "e"   (carry propagates)
//           "\xff\xff" → nil (no upper bound — all 0xFF overflows)
func prefixEndBytes(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil // overflow: every byte was 0xFF
}
