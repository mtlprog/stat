package domain

import "fmt"

// AssetType represents the Stellar asset type classification.
type AssetType string

const (
	AssetTypeNative          AssetType = "native"
	AssetTypeCreditAlphanum4 AssetType = "credit_alphanum4"
	AssetTypeCreditAlphanum12 AssetType = "credit_alphanum12"
)

// AssetInfo describes a Stellar asset.
type AssetInfo struct {
	Code   string    `json:"code"`
	Issuer string    `json:"issuer"`
	Type   AssetType `json:"type"`
}

// IsNative returns true if this asset is the native XLM.
func (a AssetInfo) IsNative() bool {
	return a.Type == AssetTypeNative
}

// Canonical returns a canonical string representation: "native" for XLM, "CODE:ISSUER" for credits.
func (a AssetInfo) Canonical() string {
	if a.IsNative() {
		return "native"
	}
	return fmt.Sprintf("%s:%s", a.Code, a.Issuer)
}

// AssetTypeFromCode determines the Stellar asset type from the code length.
func AssetTypeFromCode(code string) AssetType {
	if code == "XLM" || code == "native" {
		return AssetTypeNative
	}
	if len(code) <= 4 {
		return AssetTypeCreditAlphanum4
	}
	return AssetTypeCreditAlphanum12
}

// EURMTLAsset is the fund's base stablecoin.
var EURMTLAsset = AssetInfo{
	Code:   "EURMTL",
	Issuer: "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V",
	Type:   AssetTypeCreditAlphanum12,
}

// XLMAsset is the Stellar native asset.
var XLMAsset = AssetInfo{
	Code: "XLM",
	Type: AssetTypeNative,
}
