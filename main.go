package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"my-abci-app/app"
	"my-abci-app/price"

	abciserver "github.com/tendermint/tendermint/abci/server"
	"github.com/tendermint/tendermint/libs/log"
)

func main() {
	// ---------------------------------------------------------------------------
	// CLI flags
	//
	// --addr     : TCP address the ABCI server listens on.
	//              Tendermint connects here by default on port 26658.
	//
	// --data-dir : Directory where LevelDB stores its files.
	//              The database file will be created at <data-dir>/kvstore.db
	//              If the directory doesn't exist we create it automatically.
	// ---------------------------------------------------------------------------
	listenAddr := flag.String("addr", "tcp://0.0.0.0:26658", "ABCI server listen address")
	dataDir := flag.String("data-dir", "./data", "Directory for the LevelDB database")
	name := flag.String("name", "", "Human-readable instance name for log output (e.g. app1)")
	priceSource := flag.String("price-source", "", "Price oracle source: coingecko, binance, kraken, mock1, mock2, mock3")
	priceTolerance := flag.Float64("price-tolerance", 0.05, "Max allowed price deviation (0.05 = 5%)")
	flag.Parse()

	if *name == "" {
		*name = *dataDir
	}

	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout)).With("module", "abci-server")

	// ---------------------------------------------------------------------------
	// Ensure the data directory exists.
	// MkdirAll does nothing if the directory is already there.
	// ---------------------------------------------------------------------------
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create data directory %s: %v\n", *dataDir, err)
		os.Exit(1)
	}

	// ---------------------------------------------------------------------------
	// Build the price fetcher (if configured).
	// ---------------------------------------------------------------------------
	var fetcher price.PriceFetcher
	if *priceSource != "" {
		raw, err := newPriceFetcher(*priceSource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fetcher = price.NewCachedFetcher(raw, 10*time.Second)
		fmt.Printf("Price oracle: source=%s tolerance=%.1f%%\n", raw.Name(), *priceTolerance*100)
	}

	// ---------------------------------------------------------------------------
	// Create the application.
	// ---------------------------------------------------------------------------
	kvApp, err := app.NewKVStoreApp(*name, *dataDir, fetcher, *priceTolerance)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create app: %v\n", err)
		os.Exit(1)
	}
	// Always close the database cleanly so LevelDB can flush its write-ahead log.
	defer func() {
		if err := kvApp.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing database: %v\n", err)
		}
	}()

	// ---------------------------------------------------------------------------
	// Create and start the ABCI socket server.
	// ---------------------------------------------------------------------------
	srv, err := abciserver.NewServer(*listenAddr, "socket", kvApp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create ABCI server: %v\n", err)
		os.Exit(1)
	}
	srv.SetLogger(logger)

	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start ABCI server: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := srv.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "error stopping server: %v\n", err)
		}
	}()

	fmt.Printf("ABCI app listening on %s  |  data dir: %s\n", *listenAddr, *dataDir)

	// Block until Ctrl+C or SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down gracefully...")
}

func newPriceFetcher(source string) (price.PriceFetcher, error) {
	switch source {
	case "coingecko":
		return price.NewCoinGeckoFetcher(), nil
	case "binance":
		return price.NewBinanceFetcher(), nil
	case "kraken":
		return price.NewKrakenFetcher(), nil
	case "mock1":
		return price.NewMock1(), nil
	case "mock2":
		return price.NewMock2(), nil
	case "mock3":
		return price.NewMock3(), nil
	default:
		return nil, fmt.Errorf("unknown price source %q (valid: coingecko, binance, kraken, mock1, mock2, mock3)", source)
	}
}
