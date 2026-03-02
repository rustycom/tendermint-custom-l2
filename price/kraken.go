package price

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

var assetToKrakenPair = map[string]string{
	"BTC/USD": "XBTUSD",
}

// krakenPairKey maps our pair name to the key Kraken actually uses in the
// response body (Kraken prefixes with "X" and "Z" for some pairs).
var krakenPairKey = map[string]string{
	"XBTUSD": "XXBTZUSD",
}

type KrakenFetcher struct {
	client *http.Client
}

func NewKrakenFetcher() *KrakenFetcher {
	return &KrakenFetcher{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (f *KrakenFetcher) Name() string { return "kraken" }

// Response shape:
//
//	{"error":[],"result":{"XXBTZUSD":{"a":["95000.0","1","1.000"],... "c":["95000.0","0.001"], ...}}}
//
// "c" is the last trade closed: [price, lot-volume].
func (f *KrakenFetcher) FetchPrice(asset string) (float64, error) {
	pair, ok := assetToKrakenPair[asset]
	if !ok {
		return 0, fmt.Errorf("kraken: unsupported asset %q", asset)
	}

	url := fmt.Sprintf("https://api.kraken.com/0/public/Ticker?pair=%s", pair)

	resp, err := f.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("kraken: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("kraken: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Error  []string                          `json:"error"`
		Result map[string]map[string]interface{} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("kraken: decode error: %w", err)
	}
	if len(result.Error) > 0 {
		return 0, fmt.Errorf("kraken: API error: %v", result.Error)
	}

	responseKey := krakenPairKey[pair]
	ticker, ok := result.Result[responseKey]
	if !ok {
		return 0, fmt.Errorf("kraken: missing pair %q in response", responseKey)
	}

	// "c" = last trade closed, value is [price_string, volume_string]
	cRaw, ok := ticker["c"]
	if !ok {
		return 0, fmt.Errorf("kraken: missing 'c' field in ticker")
	}
	cArr, ok := cRaw.([]interface{})
	if !ok || len(cArr) < 1 {
		return 0, fmt.Errorf("kraken: unexpected 'c' format")
	}
	priceStr, ok := cArr[0].(string)
	if !ok {
		return 0, fmt.Errorf("kraken: price is not a string")
	}

	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return 0, fmt.Errorf("kraken: invalid price %q: %w", priceStr, err)
	}

	return price, nil
}
