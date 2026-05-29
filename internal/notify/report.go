package notify

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/indicator"
)

// Report is the assembled notification payload for one day.
type Report struct {
	Date          time.Time
	ReportMissing bool
	KeyIndicators []indicator.Indicator
	Alerts        []Alert
	Mentions      []string
	ReportURL     string
}

// Alert describes an indicator that changed sharply vs the previous observation.
type Alert struct {
	Indicator indicator.Indicator
	Previous  decimal.Decimal
	// ChangePercent is signed: positive = increase, negative = decrease.
	ChangePercent decimal.Decimal
}
