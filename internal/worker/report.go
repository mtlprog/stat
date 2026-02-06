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

// ReportWorker periodically generates fund snapshots.
type ReportWorker struct {
	generator SnapshotGenerator
	interval  time.Duration
}

// NewReportWorker creates a new ReportWorker.
func NewReportWorker(generator SnapshotGenerator, interval time.Duration) *ReportWorker {
	return &ReportWorker{
		generator: generator,
		interval:  interval,
	}
}

// Run starts the report worker loop. It blocks until the context is cancelled.
func (w *ReportWorker) Run(ctx context.Context) {
	slog.Info("ReportWorker: starting")

	// Generate immediately on startup
	if _, err := w.generator.Generate(ctx, "mtlf", time.Now()); err != nil {
		slog.Error("ReportWorker: initial generation failed", "error", err)
	} else {
		slog.Info("ReportWorker: initial generation completed")
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("ReportWorker: shutting down")
			return
		case <-ticker.C:
			if _, err := w.generator.Generate(ctx, "mtlf", time.Now()); err != nil {
				slog.Error("ReportWorker: generation failed", "error", err)
			} else {
				slog.Info("ReportWorker: generation completed")
			}
		}
	}
}
