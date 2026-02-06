package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// symbolMapping maps internal symbols to CoinGecko IDs.
// Unexported to prevent external mutation.
var symbolMapping = map[string]string{
	"BTC":  "bitcoin",
	"ETH":  "ethereum",
	"XLM":  "stellar",
	"Sats": "bitcoin",
	"USD":  "tether",
	"AU":   "gold",
}

// SymbolMapping returns a copy of the symbol-to-CoinGecko-ID mapping.
func SymbolMapping() map[string]string {
	result := make(map[string]string, len(symbolMapping))
	for k, v := range symbolMapping {
		result[k] = v
	}
	return result
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

var (
	satsDiv = decimal.NewFromInt(100_000_000)
	auDiv   = decimal.RequireFromString("31.1035")
)

// FetchPrices fetches EUR prices for all configured symbols from CoinGecko.
func (c *CoinGeckoClient) FetchPrices(ctx context.Context) (map[string]decimal.Decimal, error) {
	// Collect unique CoinGecko IDs
	uniqueIDs := make(map[string]bool)
	for _, id := range symbolMapping {
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

	// Use json.Decoder with UseNumber to avoid float64 precision loss
	var raw map[string]map[string]json.Number
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing CoinGecko response: %w", err)
	}

	result := make(map[string]decimal.Decimal)
	for symbol, coinID := range symbolMapping {
		prices, ok := raw[coinID]
		if !ok {
			slog.Warn("CoinGecko response missing symbol", "symbol", symbol, "coinID", coinID)
			continue
		}
		eurStr := prices["eur"].String()
		eurPrice, err := decimal.NewFromString(eurStr)
		if err != nil {
			slog.Warn("CoinGecko price unparseable", "symbol", symbol, "value", eurStr, "error", err)
			continue
		}

		switch symbol {
		case "Sats":
			// 1 Sat = 1/100_000_000 BTC
			result[symbol] = eurPrice.Div(satsDiv)
		case "AU":
			// Gold price is per troy ounce, convert to per gram
			result[symbol] = eurPrice.Div(auDiv)
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
