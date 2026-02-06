package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

type mockSnapshotGenerator struct {
	callCount atomic.Int32
}

func (m *mockSnapshotGenerator) Generate(_ context.Context, _ string, _ time.Time) (domain.FundStructureData, error) {
	m.callCount.Add(1)
	return domain.FundStructureData{}, nil
}

func TestReportWorkerRunsAndShutdown(t *testing.T) {
	mock := &mockSnapshotGenerator{}
	w := NewReportWorker(mock, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	w.Run(ctx)

	if got := mock.callCount.Load(); got < 1 {
		t.Errorf("call count = %d, want >= 1", got)
	}
}
