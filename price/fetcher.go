package price

import (
	"fmt"
	"sync"
	"time"
)

// PriceFetcher retrieves the current price of an asset (e.g. "BTC/USD").
type PriceFetcher interface {
	Name() string
	FetchPrice(asset string) (float64, error)
}

// CachedFetcher wraps any PriceFetcher and caches results for a configurable
// TTL so that repeated CheckTx calls don't hammer external APIs.
type CachedFetcher struct {
	inner PriceFetcher
	ttl   time.Duration

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	price     float64
	fetchedAt time.Time
}

func NewCachedFetcher(inner PriceFetcher, ttl time.Duration) *CachedFetcher {
	return &CachedFetcher{
		inner: inner,
		ttl:   ttl,
		cache: make(map[string]cacheEntry),
	}
}

func (c *CachedFetcher) Name() string { return c.inner.Name() }

func (c *CachedFetcher) FetchPrice(asset string) (float64, error) {
	c.mu.RLock()
	entry, ok := c.cache[asset]
	c.mu.RUnlock()

	if ok && time.Since(entry.fetchedAt) < c.ttl {
		return entry.price, nil
	}

	price, err := c.inner.FetchPrice(asset)
	if err != nil {
		return 0, fmt.Errorf("[%s] %w", c.inner.Name(), err)
	}

	c.mu.Lock()
	c.cache[asset] = cacheEntry{price: price, fetchedAt: time.Now()}
	c.mu.Unlock()

	return price, nil
}
