package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type mockQuoteFetcher struct {
	callCount atomic.Int32
}

func (m *mockQuoteFetcher) FetchAndStoreQuotes(_ context.Context) error {
	m.callCount.Add(1)
	return nil
}

func TestQuoteWorkerRunsAndShutdown(t *testing.T) {
	mock := &mockQuoteFetcher{}
	w := NewQuoteWorker(mock, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	w.Run(ctx)

	// Should have run at least the initial fetch + some ticks
	if got := mock.callCount.Load(); got < 1 {
		t.Errorf("call count = %d, want >= 1", got)
	}
}
