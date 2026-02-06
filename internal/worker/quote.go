package worker

import (
	"context"
	"log"
	"time"
)

// QuoteFetcher defines the interface for fetching and storing external quotes.
type QuoteFetcher interface {
	FetchAndStoreQuotes(ctx context.Context) error
}

// QuoteWorker periodically fetches external price quotes.
type QuoteWorker struct {
	fetcher  QuoteFetcher
	interval time.Duration
}

// NewQuoteWorker creates a new QuoteWorker.
func NewQuoteWorker(fetcher QuoteFetcher, interval time.Duration) *QuoteWorker {
	return &QuoteWorker{
		fetcher:  fetcher,
		interval: interval,
	}
}

// Run starts the quote worker loop. It blocks until the context is cancelled.
func (w *QuoteWorker) Run(ctx context.Context) {
	log.Println("QuoteWorker: starting")

	// Fetch immediately on startup
	if err := w.fetcher.FetchAndStoreQuotes(ctx); err != nil {
		log.Printf("QuoteWorker: initial fetch failed: %v", err)
	} else {
		log.Println("QuoteWorker: initial fetch completed")
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("QuoteWorker: shutting down")
			return
		case <-ticker.C:
			if err := w.fetcher.FetchAndStoreQuotes(ctx); err != nil {
				log.Printf("QuoteWorker: fetch failed: %v", err)
			} else {
				log.Println("QuoteWorker: fetch completed")
			}
		}
	}
}
