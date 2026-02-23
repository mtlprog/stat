package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

// SnapshotGenerator defines the interface for generating snapshots.
type SnapshotGenerator interface {
	Generate(ctx context.Context, slug string, date time.Time) (domain.FundStructureData, error)
}

// AfterSnapshotHook is called after each successful snapshot generation.
type AfterSnapshotHook interface {
	Export(ctx context.Context, data domain.FundStructureData) error
}

// ReportWorker periodically generates fund snapshots.
type ReportWorker struct {
	generator SnapshotGenerator
	interval  time.Duration
	hook      AfterSnapshotHook // optional
}

// NewReportWorker creates a new ReportWorker with an optional post-generation hook.
func NewReportWorker(generator SnapshotGenerator, interval time.Duration, hook AfterSnapshotHook) *ReportWorker {
	return &ReportWorker{
		generator: generator,
		interval:  interval,
		hook:      hook,
	}
}

// runHook calls the post-generation hook if one is configured.
func (w *ReportWorker) runHook(ctx context.Context, data domain.FundStructureData) {
	if w.hook == nil {
		return
	}
	if err := w.hook.Export(ctx, data); err != nil {
		slog.Error("ReportWorker: export hook failed", "error", err)
	} else {
		slog.Info("ReportWorker: export hook completed")
	}
}

// utcDate returns the current date normalized to midnight UTC.
func utcDate() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// Run starts the report worker loop. It blocks until the context is cancelled.
func (w *ReportWorker) Run(ctx context.Context) {
	slog.Info("ReportWorker: starting")

	// Generate immediately on startup
	if data, err := w.generator.Generate(ctx, "mtlf", utcDate()); err != nil {
		slog.Error("ReportWorker: initial generation failed", "error", err)
	} else {
		slog.Info("ReportWorker: initial generation completed")
		w.runHook(ctx, data)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("ReportWorker: shutting down")
			return
		case <-ticker.C:
			if data, err := w.generator.Generate(ctx, "mtlf", utcDate()); err != nil {
				slog.Error("ReportWorker: generation failed", "error", err)
			} else {
				slog.Info("ReportWorker: generation completed")
				w.runHook(ctx, data)
			}
		}
	}
}
