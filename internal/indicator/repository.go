package indicator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// ErrNotFound indicates that no indicator rows were found for the requested query.
var ErrNotFound = errors.New("indicators not found")

// HistoryPoint is a single (date, indicator_id, value) tuple.
type HistoryPoint struct {
	SnapshotDate time.Time
	IndicatorID  int
	Value        decimal.Decimal
}

// Repository persists and retrieves computed indicators.
type Repository interface {
	Save(ctx context.Context, entityID int, date time.Time, indicators []Indicator) error
	GetByDate(ctx context.Context, slug string, date time.Time) ([]Indicator, error)
	GetLatest(ctx context.Context, slug string) ([]Indicator, time.Time, error)
	GetHistory(ctx context.Context, slug string, ids []int, from time.Time) ([]HistoryPoint, error)
	GetNearestBefore(ctx context.Context, slug string, date time.Time) (map[int]Indicator, error)
}

// PgRepository implements Repository with PostgreSQL.
type PgRepository struct {
	pool *pgxpool.Pool
}

// NewPgRepository creates a new PostgreSQL indicator repository.
func NewPgRepository(pool *pgxpool.Pool) *PgRepository {
	return &PgRepository{pool: pool}
}

// Save bulk-upserts all indicators for one (entity, date) tuple atomically.
// On any failure, the entire batch is rolled back so partial state never reaches the table.
func (r *PgRepository) Save(ctx context.Context, entityID int, date time.Time, indicators []Indicator) error {
	if len(indicators) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning indicator save tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	for _, ind := range indicators {
		batch.Queue(
			`INSERT INTO fund_indicators (entity_id, snapshot_date, indicator_id, value)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (entity_id, snapshot_date, indicator_id)
			 DO UPDATE SET value = EXCLUDED.value, computed_at = NOW()`,
			entityID, date, ind.ID, ind.Value,
		)
	}
	br := tx.SendBatch(ctx, batch)
	for i := range indicators {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("upserting indicator I%d: %w", indicators[i].ID, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("closing batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing indicator save tx: %w", err)
	}
	return nil
}

// GetByDate returns all indicators for a (slug, date) tuple, joined with registry metadata.
// Returns ErrNotFound if no rows exist for that date.
func (r *PgRepository) GetByDate(ctx context.Context, slug string, date time.Time) ([]Indicator, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT fi.indicator_id, fi.value
		 FROM fund_indicators fi
		 JOIN fund_entities fe ON fe.id = fi.entity_id
		 WHERE fe.slug = $1 AND fi.snapshot_date = $2
		 ORDER BY fi.indicator_id`,
		slug, date)
	if err != nil {
		return nil, fmt.Errorf("querying indicators by date: %w", err)
	}
	defer rows.Close()

	indicators, err := scanIndicators(rows)
	if err != nil {
		return nil, err
	}
	if len(indicators) == 0 {
		return nil, ErrNotFound
	}
	return indicators, nil
}

// GetLatest returns the most recent value for each indicator ID. Different indicator
// IDs may land on different dates (e.g. Layer0 from `stat report` vs Layer1+ from
// `import-indicators-from-sheets`); each is returned at its latest observation.
// The second return value is the most recent date across all indicators.
func (r *PgRepository) GetLatest(ctx context.Context, slug string) ([]Indicator, time.Time, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (fi.indicator_id)
		        fi.snapshot_date, fi.indicator_id, fi.value
		 FROM fund_indicators fi
		 JOIN fund_entities fe ON fe.id = fi.entity_id
		 WHERE fe.slug = $1
		 ORDER BY fi.indicator_id, fi.snapshot_date DESC`,
		slug)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("querying latest indicators: %w", err)
	}
	defer rows.Close()

	var indicators []Indicator
	var latest time.Time
	for rows.Next() {
		var d time.Time
		var id int
		var value decimal.Decimal
		if err := rows.Scan(&d, &id, &value); err != nil {
			return nil, time.Time{}, fmt.Errorf("scanning indicator row: %w", err)
		}
		if d.After(latest) {
			latest = d
		}
		indicators = append(indicators, NewIndicator(id, value, "", ""))
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("iterating indicators: %w", err)
	}
	if len(indicators) == 0 {
		return nil, time.Time{}, ErrNotFound
	}
	return indicators, latest, nil
}

// GetHistory returns time-series points for the given indicator IDs at or after `from`.
// Results are ordered by snapshot_date ASC, then indicator_id ASC.
func (r *PgRepository) GetHistory(ctx context.Context, slug string, ids []int, from time.Time) ([]HistoryPoint, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx,
		`SELECT fi.snapshot_date, fi.indicator_id, fi.value
		 FROM fund_indicators fi
		 JOIN fund_entities fe ON fe.id = fi.entity_id
		 WHERE fe.slug = $1
		   AND fi.indicator_id = ANY($2::int[])
		   AND fi.snapshot_date >= $3
		 ORDER BY fi.snapshot_date ASC, fi.indicator_id ASC`,
		slug, ids, from)
	if err != nil {
		return nil, fmt.Errorf("querying indicator history: %w", err)
	}
	defer rows.Close()

	var points []HistoryPoint
	for rows.Next() {
		var p HistoryPoint
		if err := rows.Scan(&p.SnapshotDate, &p.IndicatorID, &p.Value); err != nil {
			return nil, fmt.Errorf("scanning history row: %w", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating history: %w", err)
	}
	return points, nil
}

// GetNearestBefore returns the latest value PER indicator ID at or before the given date.
// Different IDs may resolve to different dates — sparse indicators still get a comparison
// from their own most recent observation in the window. Returns nil (without error) if none exists.
func (r *PgRepository) GetNearestBefore(ctx context.Context, slug string, date time.Time) (map[int]Indicator, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (fi.indicator_id)
		        fi.indicator_id, fi.value
		 FROM fund_indicators fi
		 JOIN fund_entities fe ON fe.id = fi.entity_id
		 WHERE fe.slug = $1 AND fi.snapshot_date <= $2
		 ORDER BY fi.indicator_id, fi.snapshot_date DESC`,
		slug, date)
	if err != nil {
		return nil, fmt.Errorf("querying nearest-before indicators: %w", err)
	}
	defer rows.Close()

	result := make(map[int]Indicator)
	for rows.Next() {
		var id int
		var value decimal.Decimal
		if err := rows.Scan(&id, &value); err != nil {
			return nil, fmt.Errorf("scanning nearest-before row: %w", err)
		}
		result[id] = NewIndicator(id, value, "", "")
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating nearest-before: %w", err)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func scanIndicators(rows pgx.Rows) ([]Indicator, error) {
	var indicators []Indicator
	for rows.Next() {
		var id int
		var value decimal.Decimal
		if err := rows.Scan(&id, &value); err != nil {
			return nil, fmt.Errorf("scanning indicator row: %w", err)
		}
		indicators = append(indicators, NewIndicator(id, value, "", ""))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating indicators: %w", err)
	}
	return indicators, nil
}
