package worker

import (
	"context"
	"log"
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
	log.Println("ReportWorker: starting")

	// Generate immediately on startup
	if _, err := w.generator.Generate(ctx, "mtlf", time.Now()); err != nil {
		log.Printf("ReportWorker: initial generation failed: %v", err)
	} else {
		log.Println("ReportWorker: initial generation completed")
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("ReportWorker: shutting down")
			return
		case <-ticker.C:
			if _, err := w.generator.Generate(ctx, "mtlf", time.Now()); err != nil {
				log.Printf("ReportWorker: generation failed: %v", err)
			} else {
				log.Println("ReportWorker: generation completed")
			}
		}
	}
}
