package domain

import "testing"

func TestAssetInfoIsNative(t *testing.T) {
	tests := []struct {
		name  string
		asset AssetInfo
		want  bool
	}{
		{"XLM is native", XLMAsset(), true},
		{"EURMTL is not native", EURMTLAsset(), false},
		{"credit_alphanum4 is not native", AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: AssetTypeCreditAlphanum4}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.asset.IsNative(); got != tt.want {
				t.Errorf("IsNative() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAssetInfoCanonical(t *testing.T) {
	tests := []struct {
		name  string
		asset AssetInfo
		want  string
	}{
		{"XLM canonical", XLMAsset(), "native"},
		{"EURMTL canonical", EURMTLAsset(), "EURMTL:GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"},
		{"credit_alphanum4", AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: AssetTypeCreditAlphanum4}, "MTL:GISSUER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.asset.Canonical(); got != tt.want {
				t.Errorf("Canonical() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAssetTypeFromCode(t *testing.T) {
	tests := []struct {
		name string
		code string
		want AssetType
	}{
		{"XLM is native", "XLM", AssetTypeNative},
		{"native keyword", "native", AssetTypeNative},
		{"4 char code", "MTL", AssetTypeCreditAlphanum4},
		{"exactly 4 chars", "USDC", AssetTypeCreditAlphanum4},
		{"5+ char code", "EURMTL", AssetTypeCreditAlphanum12},
		{"12 char code", "MTLCITY1234A", AssetTypeCreditAlphanum12},
		{"1 char code", "A", AssetTypeCreditAlphanum4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AssetTypeFromCode(tt.code); got != tt.want {
				t.Errorf("AssetTypeFromCode(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestEURMTLAssetFields(t *testing.T) {
	a := EURMTLAsset()
	if a.Code != "EURMTL" {
		t.Errorf("EURMTLAsset().Code = %q, want EURMTL", a.Code)
	}
	if a.Issuer != IssuerAddress {
		t.Error("EURMTLAsset().Issuer mismatch")
	}
	if a.Type != AssetTypeCreditAlphanum12 {
		t.Errorf("EURMTLAsset().Type = %q, want credit_alphanum12", a.Type)
	}
}

func TestMTLAPAssetFields(t *testing.T) {
	a := MTLAPAsset()
	if a.Code != "MTLAP" {
		t.Errorf("MTLAPAsset().Code = %q, want MTLAP", a.Code)
	}
	if a.Issuer != MTLAPAddress {
		t.Error("MTLAPAsset().Issuer mismatch")
	}
	if a.Type != AssetTypeCreditAlphanum12 {
		t.Errorf("MTLAPAsset().Type = %q, want credit_alphanum12 (MTLAP is 5 chars)", a.Type)
	}
}

func TestXLMAssetFields(t *testing.T) {
	a := XLMAsset()
	if a.Code != "XLM" {
		t.Errorf("XLMAsset().Code = %q, want XLM", a.Code)
	}
	if a.Issuer != "" {
		t.Errorf("XLMAsset().Issuer = %q, want empty", a.Issuer)
	}
	if a.Type != AssetTypeNative {
		t.Errorf("XLMAsset().Type = %q, want native", a.Type)
	}
}
