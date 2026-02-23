package domain

import "fmt"

// AssetType represents the Stellar asset type classification.
type AssetType string

const (
	AssetTypeNative           AssetType = "native"
	AssetTypeCreditAlphanum4  AssetType = "credit_alphanum4"
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

// AssetTypeFromCode determines the Stellar asset type from the code string.
func AssetTypeFromCode(code string) AssetType {
	if code == "XLM" || code == "native" {
		return AssetTypeNative
	}
	if len(code) <= 4 {
		return AssetTypeCreditAlphanum4
	}
	return AssetTypeCreditAlphanum12
}

// NewAssetInfo creates an AssetInfo with the correct type inferred from the code.
func NewAssetInfo(code, issuer string) AssetInfo {
	return AssetInfo{
		Code:   code,
		Issuer: issuer,
		Type:   AssetTypeFromCode(code),
	}
}

// IssuerAddress is the Stellar address of the main fund issuer.
const IssuerAddress = "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"

// MTLAPAddress is the Stellar address of the Montelibero Association issuer.
const MTLAPAddress = "GCNVDZIHGX473FEI7IXCUAEXUJ4BGCKEMHF36VYP5EMS7PX2QBLAMTLA"

// eurmtlAsset, xlmAsset and mtlapAsset are unexported to prevent external mutation.
var (
	eurmtlAsset = AssetInfo{
		Code:   "EURMTL",
		Issuer: IssuerAddress,
		Type:   AssetTypeCreditAlphanum12,
	}
	xlmAsset = AssetInfo{
		Code: "XLM",
		Type: AssetTypeNative,
	}
	mtlapAsset = AssetInfo{
		Code:   "MTLAP",
		Issuer: MTLAPAddress,
		Type:   AssetTypeCreditAlphanum4,
	}
)

// EURMTLAsset returns the fund's base asset (EUR-pegged stablecoin).
func EURMTLAsset() AssetInfo { return eurmtlAsset }

// XLMAsset returns the Stellar native asset info.
func XLMAsset() AssetInfo { return xlmAsset }

// MTLAPAsset returns the Montelibero Association participation token asset.
func MTLAPAsset() AssetInfo { return mtlapAsset }
