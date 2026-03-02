package price

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var assetToCoinGeckoID = map[string]string{
	"BTC/USD": "bitcoin",
}

type CoinGeckoFetcher struct {
	client *http.Client
}

func NewCoinGeckoFetcher() *CoinGeckoFetcher {
	return &CoinGeckoFetcher{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (f *CoinGeckoFetcher) Name() string { return "coingecko" }

// Response shape: {"bitcoin":{"usd":95000.0}}
func (f *CoinGeckoFetcher) FetchPrice(asset string) (float64, error) {
	id, ok := assetToCoinGeckoID[asset]
	if !ok {
		return 0, fmt.Errorf("coingecko: unsupported asset %q", asset)
	}

	url := fmt.Sprintf(
		"https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=usd", id)

	resp, err := f.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("coingecko: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("coingecko: HTTP %d", resp.StatusCode)
	}

	var result map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("coingecko: decode error: %w", err)
	}

	coin, ok := result[id]
	if !ok {
		return 0, fmt.Errorf("coingecko: missing %q in response", id)
	}
	price, ok := coin["usd"]
	if !ok {
		return 0, fmt.Errorf("coingecko: missing usd price for %q", id)
	}

	return price, nil
}
