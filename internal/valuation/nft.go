package valuation

import (
	"log/slog"

	"github.com/shopspring/decimal"
)

var oneStroop = decimal.RequireFromString("0.0000001")

// IsNFT returns true if the balance indicates an NFT (exactly 1 stroop = 0.0000001).
func IsNFT(balance string) bool {
	d, err := decimal.NewFromString(balance)
	if err != nil {
		slog.Warn("IsNFT: unparseable balance, assuming not NFT", "balance", balance, "error", err)
		return false
	}
	return d.Equal(oneStroop)
}
