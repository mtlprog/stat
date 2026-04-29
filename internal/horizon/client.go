package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is an HTTP client for the Stellar Horizon API with retry on 429.
type Client struct {
	baseURL    string
	httpClient *http.Client
	maxRetries int
	baseDelay  time.Duration
}

// NewClient creates a new Horizon API client.
func NewClient(baseURL string, maxRetries int, baseDelay time.Duration) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
	}
}

// get performs a GET request, retrying on transient failures (429 + 5xx) with
// exponential backoff. Non-transient errors fail fast.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	url := c.baseURL + path

	var lastErr error
	for attempt := range c.maxRetries + 1 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("executing request: %w", err)
		}

		const maxResponseSize = 10 << 20 // 10 MB
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		if isTransient(resp.StatusCode) {
			lastErr = fmt.Errorf("HTTP %d at %s (attempt %d/%d)", resp.StatusCode, url, attempt+1, c.maxRetries+1)
			if attempt < c.maxRetries {
				delay := c.baseDelay * time.Duration(1<<uint(attempt))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
				continue
			}
			return nil, lastErr
		}

		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(body))
	}

	return nil, lastErr
}

// isTransient reports whether status indicates a temporary failure that's
// worth retrying — 429 (rate-limit) and the standard 5xx gateway-style errors.
func isTransient(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// getJSON performs a GET request and unmarshals the JSON response.
func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	body, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("parsing JSON from %s: %w", path, err)
	}
	return nil
}
