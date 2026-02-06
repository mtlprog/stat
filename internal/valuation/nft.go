package valuation

// IsNFT returns true if the balance indicates an NFT (exactly 1 stroop = 0.0000001).
func IsNFT(balance string) bool {
	return balance == "0.0000001"
}
