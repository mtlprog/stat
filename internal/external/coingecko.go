package external

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SymbolMapping maps internal symbols to CoinGecko IDs.
var SymbolMapping = map[string]string{
	"BTC":  "bitcoin",
	"ETH":  "ethereum",
	"XLM":  "stellar",
	"Sats": "bitcoin",
	"USD":  "tether",
	"AU":   "gold",
}

// CoinGeckoClient fetches prices from the CoinGecko API.
type CoinGeckoClient struct {
	baseURL    string
	httpClient *http.Client
	delay      time.Duration
	maxRetries int
}

// NewCoinGeckoClient creates a new CoinGecko API client.
func NewCoinGeckoClient(baseURL string, delay time.Duration, maxRetries int) *CoinGeckoClient {
	return &CoinGeckoClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		delay:      delay,
		maxRetries: maxRetries,
	}
}

// FetchPrices fetches EUR prices for all configured symbols from CoinGecko.
// Returns a map of symbol -> priceInEUR.
func (c *CoinGeckoClient) FetchPrices(ctx context.Context) (map[string]float64, error) {
	// Collect unique CoinGecko IDs
	uniqueIDs := make(map[string]bool)
	for _, id := range SymbolMapping {
		uniqueIDs[id] = true
	}

	ids := make([]string, 0, len(uniqueIDs))
	for id := range uniqueIDs {
		ids = append(ids, id)
	}

	url := fmt.Sprintf("%s/simple/price?ids=%s&vs_currencies=eur", c.baseURL, strings.Join(ids, ","))

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	// Parse: {"bitcoin":{"eur":45000},"ethereum":{"eur":2500},...}
	var raw map[string]map[string]float64
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing CoinGecko response: %w", err)
	}

	result := make(map[string]float64)
	for symbol, coinID := range SymbolMapping {
		prices, ok := raw[coinID]
		if !ok {
			continue
		}
		eurPrice := prices["eur"]

		switch symbol {
		case "Sats":
			// 1 Sat = 1/100_000_000 BTC
			result[symbol] = eurPrice / 100_000_000
		case "AU":
			// Gold price is per troy ounce, convert to per gram
			result[symbol] = eurPrice / 31.1035
		default:
			result[symbol] = eurPrice
		}
	}

	return result, nil
}

func (c *CoinGeckoClient) fetchWithRetry(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := range c.maxRetries + 1 {
		if attempt > 0 {
			baseDelay := c.delay
			if baseDelay == 0 {
				baseDelay = 10 * time.Second
			}
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating CoinGecko request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("CoinGecko request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading CoinGecko response: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("CoinGecko rate limited (attempt %d/%d)", attempt+1, c.maxRetries+1)
			continue
		}

		return nil, fmt.Errorf("CoinGecko HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil, lastErr
}
