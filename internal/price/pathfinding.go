package price

import (
	"context"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

// getPathPrice attempts price discovery via path finding.
// Primary: strictSend. Fallback: strictReceive.
func (s *Service) getPathPrice(ctx context.Context, source, dest domain.AssetInfo, amount string) (domain.TokenPairPrice, error) {
	// Try strictSend first
	paths, err := s.horizon.FetchStrictSendPaths(ctx, source, amount, dest)
	if err != nil {
		if ctx.Err() != nil {
			return domain.TokenPairPrice{}, ctx.Err()
		}
		slog.Warn("strictSend failed, trying strictReceive",
			"source", source.Code, "dest", dest.Code, "error", err)
	} else if len(paths) > 0 {
		if price, ok := pathRecordToPrice(paths[0], source, dest); ok {
			return price, nil
		}
	}

	// Fallback to strictReceive
	paths, err = s.horizon.FetchStrictReceivePaths(ctx, source, dest, amount)
	if err != nil {
		return domain.TokenPairPrice{}, err
	}
	if len(paths) == 0 {
		return domain.TokenPairPrice{}, ErrNoPrice
	}

	price, ok := pathRecordToPrice(paths[0], source, dest)
	if !ok {
		return domain.TokenPairPrice{}, ErrNoPrice
	}
	return price, nil
}

func pathRecordToPrice(record horizon.HorizonPathRecord, source, dest domain.AssetInfo) (domain.TokenPairPrice, bool) {
	srcAmount, err := decimal.NewFromString(record.SourceAmount)
	if err != nil {
		slog.Warn("pathRecordToPrice: unparseable source amount",
			"source", source.Code, "dest", dest.Code, "value", record.SourceAmount, "error", err)
		return domain.TokenPairPrice{}, false
	}
	if srcAmount.IsZero() {
		slog.Warn("pathRecordToPrice: zero source amount",
			"source", source.Code, "dest", dest.Code)
		return domain.TokenPairPrice{}, false
	}

	destAmount, err := decimal.NewFromString(record.DestinationAmount)
	if err != nil {
		slog.Warn("pathRecordToPrice: unparseable destination amount",
			"source", source.Code, "dest", dest.Code, "value", record.DestinationAmount, "error", err)
		return domain.TokenPairPrice{}, false
	}

	price := destAmount.Div(srcAmount)

	srcAmountStr := record.SourceAmount
	destAmountStr := record.DestinationAmount

	return domain.TokenPairPrice{
		TokenA:            source.Canonical(),
		TokenB:            dest.Canonical(),
		Price:             price.String(),
		DestinationAmount: record.DestinationAmount,
		Timestamp:         time.Now(),
		Details: &domain.PriceDetails{
			Source:            "path",
			SourceAmount:      &srcAmountStr,
			DestinationAmount: &destAmountStr,
			Path:              buildPathHops(record),
		},
	}, true
}

func buildPathHops(record horizon.HorizonPathRecord) []domain.PathHop {
	if len(record.Path) == 0 {
		return nil
	}

	hops := make([]domain.PathHop, len(record.Path))
	for i, asset := range record.Path {
		from := record.SourceAssetCode
		if i > 0 {
			from = record.Path[i-1].AssetCode
		}
		if from == "" {
			from = "XLM"
		}
		to := asset.AssetCode
		if to == "" {
			to = "XLM"
		}
		hops[i] = domain.PathHop{From: from, To: to}
	}
	return hops
}
