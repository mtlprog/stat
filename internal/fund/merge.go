package fund

import (
	"github.com/samber/lo"

	"github.com/mtlprog/stat/internal/domain"
)

// mergeValuations returns valuations relevant to the given account with owner priority.
// Owner valuations take precedence over valuations from other accounts.
func mergeValuations(accountID string, allValuations []domain.AssetValuation) []domain.AssetValuation {
	ownerVals := lo.Filter(allValuations, func(v domain.AssetValuation, _ int) bool {
		return v.SourceAccount == accountID
	})
	otherVals := lo.Filter(allValuations, func(v domain.AssetValuation, _ int) bool {
		return v.SourceAccount != accountID
	})

	// Owner valuations first, then non-conflicting others
	seen := lo.SliceToMap(ownerVals, func(v domain.AssetValuation) (string, bool) {
		return v.TokenCode + ":" + string(v.ValuationType), true
	})

	nonConflicting := lo.Filter(otherVals, func(v domain.AssetValuation, _ int) bool {
		key := v.TokenCode + ":" + string(v.ValuationType)
		return !seen[key]
	})

	return append(ownerVals, nonConflicting...)
}
