package grist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxResponseBytes = 1 << 20

// Client is a minimal HTTP client for the Grist REST API.
type Client struct {
	baseURL    string
	docID      string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a Client. baseURL is the Grist instance root (e.g. "https://montelibero.getgrist.com").
func NewClient(baseURL, docID, apiKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		docID:      docID,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type addRecordsRequest struct {
	Records []recordBody `json:"records"`
}

type recordBody struct {
	Fields map[string]any `json:"fields"`
}

// AddRecords inserts one or more rows into tableID.
func (c *Client) AddRecords(ctx context.Context, tableID string, records []map[string]any) error {
	body := addRecordsRequest{
		Records: make([]recordBody, len(records)),
	}
	for i, r := range records {
		body.Records[i] = recordBody{Fields: r}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshalling grist records: %w", err)
	}

	url := fmt.Sprintf("%s/api/docs/%s/tables/%s/records", c.baseURL, c.docID, tableID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building grist request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling grist add-records: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("reading grist response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		preview := respBody
		if len(preview) > 512 {
			preview = preview[:512]
		}
		return fmt.Errorf("grist add-records returned %s: %s", resp.Status, string(preview))
	}

	return nil
}
