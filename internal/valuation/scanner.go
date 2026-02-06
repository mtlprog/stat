package valuation

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

// AccountFetcher defines the subset of Horizon API used by the scanner.
type AccountFetcher interface {
	FetchAccount(ctx context.Context, accountID string) (horizon.HorizonAccount, error)
}

// ScanAccountValuations reads DATA entries from a Stellar account and extracts valuations.
func ScanAccountValuations(ctx context.Context, fetcher AccountFetcher, accountID string) ([]domain.AssetValuation, error) {
	account, err := fetcher.FetchAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("scanning valuations for %s: %w", accountID, err)
	}

	var valuations []domain.AssetValuation

	for key, encodedValue := range account.Data {
		// Check for _1COST suffix first (must precede _COST check to avoid partial suffix match)
		var tokenCode string
		var valType domain.ValuationType

		if strings.HasSuffix(key, "_1COST") {
			tokenCode = strings.TrimSuffix(key, "_1COST")
			valType = domain.ValuationTypeUnit
		} else if strings.HasSuffix(key, "_COST") {
			tokenCode = strings.TrimSuffix(key, "_COST")
			valType = domain.ValuationTypeNFT
		} else {
			continue
		}

		decoded, err := base64.StdEncoding.DecodeString(encodedValue)
		if err != nil {
			slog.Warn("failed to decode DATA entry value",
				"account", accountID, "key", key, "error", err)
			continue
		}

		parsed, err := ParseDataEntryValue(string(decoded))
		if err != nil {
			slog.Warn("skipping unparseable cost DATA entry",
				"account", accountID, "key", key, "value", string(decoded), "error", err)
			continue
		}

		valuations = append(valuations, domain.AssetValuation{
			TokenCode:     tokenCode,
			ValuationType: valType,
			RawValue:      parsed,
			SourceAccount: accountID,
		})
	}

	return valuations, nil
}
