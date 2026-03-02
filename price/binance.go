package price

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

var assetToBinanceSymbol = map[string]string{
	"BTC/USD": "BTCUSDT",
}

type BinanceFetcher struct {
	client *http.Client
}

func NewBinanceFetcher() *BinanceFetcher {
	return &BinanceFetcher{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (f *BinanceFetcher) Name() string { return "binance" }

// Response shape: {"symbol":"BTCUSDT","price":"95000.00000000"}
func (f *BinanceFetcher) FetchPrice(asset string) (float64, error) {
	symbol, ok := assetToBinanceSymbol[asset]
	if !ok {
		return 0, fmt.Errorf("binance: unsupported asset %q", asset)
	}

	url := fmt.Sprintf(
		"https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)

	resp, err := f.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("binance: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("binance: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Price string `json:"price"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("binance: decode error: %w", err)
	}

	price, err := strconv.ParseFloat(result.Price, 64)
	if err != nil {
		return 0, fmt.Errorf("binance: invalid price %q: %w", result.Price, err)
	}

	return price, nil
}
