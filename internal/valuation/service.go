package valuation

import (
	"context"
	"fmt"
	"sync"

	"github.com/samber/lo"

	"github.com/mtlprog/stat/internal/domain"
)

// Service provides asset valuation from Stellar DATA entries.
type Service struct {
	fetcher AccountFetcher
}

// NewService creates a new ValuationService.
func NewService(fetcher AccountFetcher) *Service {
	return &Service{fetcher: fetcher}
}

// FetchAllValuations scans all 11 fund accounts for DATA entry valuations with concurrency=3.
// Deduplicates by tokenCode:valuationType with owner priority.
func (s *Service) FetchAllValuations(ctx context.Context) ([]domain.AssetValuation, error) {
	accounts := domain.AccountRegistry
	var mu sync.Mutex
	var allValuations []domain.AssetValuation
	var firstErr error

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup

	for _, acc := range accounts {
		wg.Add(1)
		go func(accountID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			vals, err := ScanAccountValuations(ctx, s.fetcher, accountID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			allValuations = append(allValuations, vals...)
		}(acc.Address)
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return deduplicateValuations(allValuations), nil
}

// deduplicateValuations removes duplicates by tokenCode:valuationType, keeping the first occurrence.
func deduplicateValuations(valuations []domain.AssetValuation) []domain.AssetValuation {
	return lo.UniqBy(valuations, func(v domain.AssetValuation) string {
		return fmt.Sprintf("%s:%s", v.TokenCode, v.ValuationType)
	})
}

// LookupValuation finds the best valuation for a given token based on its balance and owner account.
// NFT: prefer _COST, fallback _1COST. Regular: prefer _1COST, fallback _COST.
func LookupValuation(tokenCode, balance, ownerAccount string, valuations []domain.AssetValuation) *domain.AssetValuation {
	isNFT := IsNFT(balance)

	var preferred, fallback domain.ValuationType
	if isNFT {
		preferred = domain.ValuationTypeNFT  // _COST
		fallback = domain.ValuationTypeUnit  // _1COST
	} else {
		preferred = domain.ValuationTypeUnit // _1COST
		fallback = domain.ValuationTypeNFT   // _COST
	}

	// Filter valuations for this token
	tokenVals := lo.Filter(valuations, func(v domain.AssetValuation, _ int) bool {
		return v.TokenCode == tokenCode
	})

	if len(tokenVals) == 0 {
		return nil
	}

	// Try preferred type, with owner priority
	if v, ok := findValuation(tokenVals, preferred, ownerAccount); ok {
		return v
	}

	// Try fallback type, with owner priority
	if v, ok := findValuation(tokenVals, fallback, ownerAccount); ok {
		return v
	}

	return nil
}

func findValuation(vals []domain.AssetValuation, valType domain.ValuationType, ownerAccount string) (*domain.AssetValuation, bool) {
	// Owner priority: prefer owner's valuation
	ownerVal, found := lo.Find(vals, func(v domain.AssetValuation) bool {
		return v.ValuationType == valType && v.SourceAccount == ownerAccount
	})
	if found {
		return &ownerVal, true
	}

	// Any account
	anyVal, found := lo.Find(vals, func(v domain.AssetValuation) bool {
		return v.ValuationType == valType
	})
	if found {
		return &anyVal, true
	}

	return nil, false
}
