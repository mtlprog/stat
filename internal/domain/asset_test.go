package domain

import "testing"

func TestAssetInfoIsNative(t *testing.T) {
	tests := []struct {
		name  string
		asset AssetInfo
		want  bool
	}{
		{"XLM is native", XLMAsset, true},
		{"EURMTL is not native", EURMTLAsset, false},
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
		{"XLM canonical", XLMAsset, "native"},
		{"EURMTL canonical", EURMTLAsset, "EURMTL:GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"},
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
	if EURMTLAsset.Code != "EURMTL" {
		t.Errorf("EURMTLAsset.Code = %q, want EURMTL", EURMTLAsset.Code)
	}
	if EURMTLAsset.Issuer != "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V" {
		t.Error("EURMTLAsset.Issuer mismatch")
	}
	if EURMTLAsset.Type != AssetTypeCreditAlphanum12 {
		t.Errorf("EURMTLAsset.Type = %q, want credit_alphanum12", EURMTLAsset.Type)
	}
}

func TestXLMAssetFields(t *testing.T) {
	if XLMAsset.Code != "XLM" {
		t.Errorf("XLMAsset.Code = %q, want XLM", XLMAsset.Code)
	}
	if XLMAsset.Issuer != "" {
		t.Errorf("XLMAsset.Issuer = %q, want empty", XLMAsset.Issuer)
	}
	if XLMAsset.Type != AssetTypeNative {
		t.Errorf("XLMAsset.Type = %q, want native", XLMAsset.Type)
	}
}
