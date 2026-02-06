package domain

import (
	"github.com/samber/lo"
)

// AccountType classifies fund accounts.
type AccountType string

const (
	AccountTypeIssuer      AccountType = "issuer"
	AccountTypeSubfond     AccountType = "subfond"
	AccountTypeMutual      AccountType = "mutual"
	AccountTypeOperational AccountType = "operational"
	AccountTypeOther       AccountType = "other"
)

// FundAccount represents a Stellar account managed by the fund.
type FundAccount struct {
	Name        string      `json:"name"`
	Type        AccountType `json:"type"`
	Address     string      `json:"address"`
	Description string      `json:"description"`
}

// AccountRegistry holds all 11 fund accounts per Section 2.2.
var AccountRegistry = []FundAccount{
	// 8 main accounts
	{Name: "MAIN ISSUER", Type: AccountTypeIssuer, Address: "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V", Description: "Main token issuer account"},
	{Name: "MABIZ", Type: AccountTypeSubfond, Address: "GAQ5ERJVI6IW5UVNPEVXUUVMXH3GCDHJ4BJAXMAAKPR5VBWWAUOMABIZ", Description: "Sub-fund for business investments"},
	{Name: "MCITY", Type: AccountTypeSubfond, Address: "GCOJHUKGHI6IATN7AIEK4PSNBPXIAIZ7KB2AWTTUCNIAYVPUB2DMCITY", Description: "Sub-fund for city development"},
	{Name: "DEFI", Type: AccountTypeSubfond, Address: "GAEZHXMFRW2MWLWCXSBNZNUSE6SN3ODZDDOMPFH3JPMJXN4DKBPMDEFI", Description: "Sub-fund for DeFi operations"},
	{Name: "BOSS", Type: AccountTypeSubfond, Address: "GC72CB75VWW7CLGXS76FGN3CC5K7EELDAQCPXYMZLNMOTC42U3XJBOSS", Description: "Sub-fund for management"},
	{Name: "MFB", Type: AccountTypeMutual, Address: "GCKCV7T56CAPFUYMCQUYSEUMZRC7GA7CAQ2BOL3RPS4NQXDTRCSULMFB", Description: "Mutual fund account"},
	{Name: "APART", Type: AccountTypeMutual, Address: "GD2SNF4QHUJD6VRAXWDA4CDUYENYB23YDFQ74DVC4P5SYR54AAVCUMFA", Description: "Mutual fund apartment account"},
	{Name: "ADMIN", Type: AccountTypeOperational, Address: "GBSCMGJCE4DLQ6TYRNUMXUZZUXGZBM4BXVZUIHBBL5CSRRW2GWEHUADM", Description: "Operations and administration"},
	// 3 other accounts
	{Name: "LABR", Type: AccountTypeOther, Address: "GA7I6SGUHQ26ARNCD376WXV5WSE7VJRX6OEFNFCEGRLFGZWQIV73LABR", Description: "Affiliated labor account"},
	{Name: "MTLM", Type: AccountTypeOther, Address: "GCR5J3NU2NNG2UKDQ5XSZVX7I6TDLB3LEN2HFUR2EPJUMNWCUL62MTLM", Description: "Affiliated MTLM account"},
	{Name: "PROGRAMMERS GUILD", Type: AccountTypeOther, Address: "GDRLJC6EOKRR3BPKWGJPGI5GUN4GZFZRWQFDG3RJNZJEIBYA7B3EPROG", Description: "Programmers guild account"},
}

// MainAccounts returns accounts of type issuer, subfond, or operational.
func MainAccounts() []FundAccount {
	return lo.Filter(AccountRegistry, func(a FundAccount, _ int) bool {
		return a.Type == AccountTypeIssuer || a.Type == AccountTypeSubfond || a.Type == AccountTypeOperational
	})
}

// MutualAccounts returns accounts of type mutual.
func MutualAccounts() []FundAccount {
	return lo.Filter(AccountRegistry, func(a FundAccount, _ int) bool {
		return a.Type == AccountTypeMutual
	})
}

// OtherAccounts returns accounts of type other.
func OtherAccounts() []FundAccount {
	return lo.Filter(AccountRegistry, func(a FundAccount, _ int) bool {
		return a.Type == AccountTypeOther
	})
}

// AggregatedAccounts returns accounts included in fund totals (issuer + subfond + operational).
// Same as MainAccounts — mutual and other are excluded from aggregation per Section 6.3.
func AggregatedAccounts() []FundAccount {
	return MainAccounts()
}

// AccountByAddress looks up a fund account by its Stellar address.
// Returns the account and true if found, zero value and false otherwise.
func AccountByAddress(address string) (FundAccount, bool) {
	return lo.Find(AccountRegistry, func(a FundAccount) bool {
		return a.Address == address
	})
}
