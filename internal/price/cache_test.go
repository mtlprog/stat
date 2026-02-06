package price

import (
	"testing"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

func TestCacheKey(t *testing.T) {
	source := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
	dest := domain.EURMTLAsset
	key := cacheKey(source, dest, "1")
	expected := "MTL:GISSUER=>EURMTL:GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V:1"
	if key != expected {
		t.Errorf("cacheKey() = %q, want %q", key, expected)
	}
}

func TestCacheKeyNative(t *testing.T) {
	key := cacheKey(domain.XLMAsset, domain.EURMTLAsset, "100")
	if key != "native=>EURMTL:GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V:100" {
		t.Errorf("cacheKey() for XLM = %q", key)
	}
}

func TestCacheHitAndMiss(t *testing.T) {
	c := newPriceCache()

	price := domain.TokenPairPrice{Price: "1.5", TokenA: "A", TokenB: "B"}
	c.set("test-key", price)

	got, ok := c.get("test-key")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if got.Price != "1.5" {
		t.Errorf("cached price = %q, want 1.5", got.Price)
	}

	_, ok = c.get("missing-key")
	if ok {
		t.Error("expected cache miss for missing key")
	}
}

func TestCacheExpiry(t *testing.T) {
	c := newPriceCache()

	price := domain.TokenPairPrice{Price: "2.0"}
	c.set("expire-key", price)

	// Manually expire the entry
	c.mu.Lock()
	entry := c.entries["expire-key"]
	entry.expiresAt = time.Now().Add(-1 * time.Second)
	c.entries["expire-key"] = entry
	c.mu.Unlock()

	_, ok := c.get("expire-key")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
}
