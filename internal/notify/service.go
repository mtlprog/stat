package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/indicator"
)

// alertThreshold is the minimum absolute percent change to trigger an alert.
var alertThreshold = decimal.NewFromFloat(5.0)

// keyIndicatorIDs are the indicators always shown in the summary regardless of alerts.
var keyIndicatorIDs = []int{3, 6, 7, 10}

// Config holds notify-specific runtime configuration.
type Config struct {
	Mentions  []string
	ReportURL string
}

// Service assembles and dispatches daily fund notifications.
type Service struct {
	indicatorRepo indicator.Repository
	providers     []Provider
	cfg           Config
}

// NewService creates a Service.
func NewService(indicatorRepo indicator.Repository, providers []Provider, cfg Config) *Service {
	return &Service{
		indicatorRepo: indicatorRepo,
		providers:     providers,
		cfg:           cfg,
	}
}

// ParseMentions splits a space-separated mentions string (e.g. "@user1 @user2") into a slice.
func ParseMentions(raw string) []string {
	return lo.Compact(strings.Fields(raw))
}

// Run checks today's report, builds a Report, and sends it via all providers.
// Returns a non-zero error if the report is missing or any provider fails.
func (s *Service) Run(ctx context.Context) error {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)

	todayIndicators, err := s.indicatorRepo.GetByDate(ctx, "mtlf", today)
	if err != nil {
		if errors.Is(err, indicator.ErrNotFound) {
			slog.Info("today's indicators not found, sending missing-report alert", "date", today.Format("2006-01-02"))
			report := Report{
				Date:          today,
				ReportMissing: true,
				Mentions:      s.cfg.Mentions,
				ReportURL:     fmt.Sprintf("%s/?date=%s", s.cfg.ReportURL, today.Format("2006-01-02")),
			}
			if sendErr := s.sendAll(ctx, report); sendErr != nil {
				return fmt.Errorf("report for %s not found; also failed to send alert: %w", today.Format("2006-01-02"), sendErr)
			}
			return fmt.Errorf("report for %s not found in database", today.Format("2006-01-02"))
		}
		return fmt.Errorf("fetching today's indicators: %w", err)
	}

	yesterdayMap, err := s.indicatorRepo.GetNearestBefore(ctx, "mtlf", yesterday)
	if err != nil {
		return fmt.Errorf("fetching yesterday's indicators: %w", err)
	}
	if len(yesterdayMap) == 0 {
		slog.Info("no prior indicators found, percent-change alerts will be skipped", "date", yesterday.Format("2006-01-02"))
	}

	report := s.buildReport(today, todayIndicators, yesterdayMap)
	return s.sendAll(ctx, report)
}

func (s *Service) buildReport(date time.Time, today []indicator.Indicator, yesterday map[int]indicator.Indicator) Report {
	todayMap := lo.KeyBy(today, func(ind indicator.Indicator) int { return ind.ID })

	keyIndicators := lo.FilterMap(keyIndicatorIDs, func(id int, _ int) (indicator.Indicator, bool) {
		ind, ok := todayMap[id]
		return ind, ok
	})

	alerts := lo.FilterMap(today, func(ind indicator.Indicator, _ int) (Alert, bool) {
		prev, ok := yesterday[ind.ID]
		if !ok {
			return Alert{}, false
		}
		if prev.Value.IsZero() {
			slog.Info("skipping percent-change for indicator with zero previous value", "indicator_id", ind.ID)
			return Alert{}, false
		}
		changePct := ind.Value.Sub(prev.Value).Div(prev.Value).Mul(decimal.NewFromInt(100))
		if changePct.Abs().LessThan(alertThreshold) {
			return Alert{}, false
		}
		return Alert{Indicator: ind, Previous: prev.Value, ChangePercent: changePct.Round(2)}, true
	})

	return Report{
		Date:          date,
		ReportMissing: false,
		KeyIndicators: keyIndicators,
		Alerts:        alerts,
		Mentions:      s.cfg.Mentions,
		ReportURL:     fmt.Sprintf("%s/?date=%s", s.cfg.ReportURL, date.Format("2006-01-02")),
	}
}

func (s *Service) sendAll(ctx context.Context, report Report) error {
	var errs []error
	for _, p := range s.providers {
		if err := p.Send(ctx, report); err != nil {
			slog.Error("provider failed to send notification", "provider", fmt.Sprintf("%T", p), "error", err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("one or more providers failed: %w", errors.Join(errs...))
	}
	return nil
}
