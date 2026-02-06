package price

import (
	"fmt"
	"sync"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

const cacheTTL = 30 * time.Second

type cacheEntry struct {
	price     domain.TokenPairPrice
	expiresAt time.Time
}

type priceCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

func newPriceCache() *priceCache {
	return &priceCache{
		entries: make(map[string]cacheEntry),
	}
}

// cacheKey formats: "{source.Canonical()}=>{dest.Canonical()}:{amount}" e.g. "EURMTL:GACK...=>native:1"
func cacheKey(source, dest domain.AssetInfo, amount string) string {
	return fmt.Sprintf("%s=>%s:%s", source.Canonical(), dest.Canonical(), amount)
}

func (c *priceCache) get(key string) (domain.TokenPairPrice, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return domain.TokenPairPrice{}, false
	}
	return entry.price, true
}

func (c *priceCache) set(key string, price domain.TokenPairPrice) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = cacheEntry{
		price:     price,
		expiresAt: time.Now().Add(cacheTTL),
	}
}
