package horizon

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mtlprog/stat/internal/domain"
)

// FetchStrictSendPaths queries: "If I send `amount` of `source`, how much `dest` do I get?"
func (c *Client) FetchStrictSendPaths(ctx context.Context, source domain.AssetInfo, amount string, dest domain.AssetInfo) ([]HorizonPathRecord, error) {
	params := url.Values{}
	if source.IsNative() {
		params.Set("source_asset_type", "native")
	} else {
		params.Set("source_asset_type", string(source.Type))
		params.Set("source_asset_code", source.Code)
		params.Set("source_asset_issuer", source.Issuer)
	}
	params.Set("source_amount", amount)
	if dest.IsNative() {
		params.Set("destination_assets", "native")
	} else {
		params.Set("destination_assets", fmt.Sprintf("%s:%s", dest.Code, dest.Issuer))
	}

	var resp HorizonPathResponse
	if err := c.getJSON(ctx, "/paths/strict-send?"+params.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("fetching strict send paths: %w", err)
	}
	return resp.Embedded.Records, nil
}

// FetchStrictReceivePaths queries: "To receive `amount` of `dest`, how much `source` do I need?"
func (c *Client) FetchStrictReceivePaths(ctx context.Context, source domain.AssetInfo, dest domain.AssetInfo, amount string) ([]HorizonPathRecord, error) {
	params := url.Values{}
	if source.IsNative() {
		params.Set("source_assets", "native")
	} else {
		params.Set("source_assets", fmt.Sprintf("%s:%s", source.Code, source.Issuer))
	}
	if dest.IsNative() {
		params.Set("destination_asset_type", "native")
	} else {
		params.Set("destination_asset_type", string(dest.Type))
		params.Set("destination_asset_code", dest.Code)
		params.Set("destination_asset_issuer", dest.Issuer)
	}
	params.Set("destination_amount", amount)

	var resp HorizonPathResponse
	if err := c.getJSON(ctx, "/paths/strict-receive?"+params.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("fetching strict receive paths: %w", err)
	}
	return resp.Embedded.Records, nil
}
