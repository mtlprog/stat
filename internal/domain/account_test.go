package domain

import (
	"testing"
)

func TestAccountRegistryCount(t *testing.T) {
	if got := len(AccountRegistry()); got != 11 {
		t.Errorf("AccountRegistry() has %d accounts, want 11", got)
	}
}

func TestMainAccounts(t *testing.T) {
	main := MainAccounts()
	if got := len(main); got != 6 {
		t.Errorf("MainAccounts() returned %d, want 6 (1 issuer + 4 subfond + 1 operational)", got)
	}

	for _, a := range main {
		if a.Type != AccountTypeIssuer && a.Type != AccountTypeSubfond && a.Type != AccountTypeOperational {
			t.Errorf("MainAccounts() includes account %q of type %q", a.Name, a.Type)
		}
	}
}

func TestMutualAccounts(t *testing.T) {
	mutual := MutualAccounts()
	if got := len(mutual); got != 2 {
		t.Errorf("MutualAccounts() returned %d, want 2", got)
	}

	for _, a := range mutual {
		if a.Type != AccountTypeMutual {
			t.Errorf("MutualAccounts() includes account %q of type %q", a.Name, a.Type)
		}
	}
}

func TestOtherAccounts(t *testing.T) {
	other := OtherAccounts()
	if got := len(other); got != 3 {
		t.Errorf("OtherAccounts() returned %d, want 3", got)
	}

	for _, a := range other {
		if a.Type != AccountTypeOther {
			t.Errorf("OtherAccounts() includes account %q of type %q", a.Name, a.Type)
		}
	}
}

func TestAggregatedAccountsExcludesMutual(t *testing.T) {
	agg := AggregatedAccounts()
	if got := len(agg); got != 6 {
		t.Errorf("AggregatedAccounts() returned %d, want 6", got)
	}

	for _, a := range agg {
		if a.Type == AccountTypeMutual || a.Type == AccountTypeOther {
			t.Errorf("AggregatedAccounts() includes account %q of type %q", a.Name, a.Type)
		}
	}
}

func TestAccountByAddressFound(t *testing.T) {
	a, found := AccountByAddress("GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V")
	if !found {
		t.Fatal("AccountByAddress() did not find MAIN ISSUER")
	}
	if a.Name != "MAIN ISSUER" {
		t.Errorf("AccountByAddress() returned %q, want MAIN ISSUER", a.Name)
	}
	if a.Type != AccountTypeIssuer {
		t.Errorf("AccountByAddress() returned type %q, want issuer", a.Type)
	}
}

func TestAccountByAddressNotFound(t *testing.T) {
	_, found := AccountByAddress("GNOTEXIST")
	if found {
		t.Error("AccountByAddress() found non-existent address")
	}
}

func TestAccountRegistryContainsAllExpected(t *testing.T) {
	expectedNames := []string{
		"MAIN ISSUER", "MABIZ", "MCITY", "DEFI", "BOSS",
		"MFB", "APART", "ADMIN",
		"LABR", "MTLM", "PROGRAMMERS GUILD",
	}

	for _, name := range expectedNames {
		found := false
		for _, a := range AccountRegistry() {
			if a.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AccountRegistry() missing account %q", name)
		}
	}
}

func TestAccountRegistryImmutability(t *testing.T) {
	reg1 := AccountRegistry()
	// Mutate the returned slice
	reg1[0].Name = "HACKED"
	reg1[0].Address = "GHACKED"

	// The original should be unaffected
	reg2 := AccountRegistry()
	if reg2[0].Name == "HACKED" {
		t.Error("AccountRegistry() returned a mutable reference to the global — mutation leaked")
	}
	if reg2[0].Address == "GHACKED" {
		t.Error("AccountRegistry() address mutation leaked to global state")
	}
}

func TestAccountTypeCounts(t *testing.T) {
	counts := map[AccountType]int{}
	for _, a := range AccountRegistry() {
		counts[a.Type]++
	}

	expected := map[AccountType]int{
		AccountTypeIssuer:      1,
		AccountTypeSubfond:     4,
		AccountTypeMutual:      2,
		AccountTypeOperational: 1,
		AccountTypeOther:       3,
	}

	for typ, want := range expected {
		if got := counts[typ]; got != want {
			t.Errorf("count of %q accounts = %d, want %d", typ, got, want)
		}
	}
}
