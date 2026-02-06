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

// get performs a GET request with retry on 429.
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

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("HTTP 429 at %s (attempt %d/%d)", url, attempt+1, c.maxRetries+1)
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
