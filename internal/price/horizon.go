package price

import (
	"context"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

// HorizonClient defines the Horizon API subset needed by PriceService.
type HorizonClient interface {
	FetchOrderbook(ctx context.Context, selling, buying domain.AssetInfo, limit int) (horizon.HorizonOrderbook, error)
	FetchStrictSendPaths(ctx context.Context, source domain.AssetInfo, amount string, dest domain.AssetInfo) ([]horizon.HorizonPathRecord, error)
	FetchStrictReceivePaths(ctx context.Context, source domain.AssetInfo, dest domain.AssetInfo, amount string) ([]horizon.HorizonPathRecord, error)
	FetchLiquidityPools(ctx context.Context, reserveA, reserveB domain.AssetInfo) ([]horizon.HorizonLiquidityPool, error)
}
