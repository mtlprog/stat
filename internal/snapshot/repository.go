package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound indicates that the requested snapshot was not found.
var ErrNotFound = errors.New("snapshot not found")

// Snapshot represents a stored fund snapshot.
type Snapshot struct {
	ID           int             `json:"id"`
	EntityID     int             `json:"entityId"`
	SnapshotDate time.Time       `json:"snapshotDate"`
	Data         json.RawMessage `json:"data"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// Repository defines persistent storage for snapshots.
type Repository interface {
	Save(ctx context.Context, entityID int, date time.Time, data json.RawMessage) error
	GetLatest(ctx context.Context, entitySlug string) (*Snapshot, error)
	GetByDate(ctx context.Context, entitySlug string, date time.Time) (*Snapshot, error)
	List(ctx context.Context, entitySlug string, limit int) ([]Snapshot, error)
	GetEntityID(ctx context.Context, slug string) (int, error)
	EnsureEntity(ctx context.Context, slug, name, description string) (int, error)
}

// PgRepository implements Repository with PostgreSQL.
type PgRepository struct {
	pool *pgxpool.Pool
}

// NewPgRepository creates a new PostgreSQL snapshot repository.
func NewPgRepository(pool *pgxpool.Pool) *PgRepository {
	return &PgRepository{pool: pool}
}

func (r *PgRepository) Save(ctx context.Context, entityID int, date time.Time, data json.RawMessage) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO fund_snapshots (entity_id, snapshot_date, data)
		 VALUES ($1, $2, $3::jsonb)
		 ON CONFLICT (entity_id, snapshot_date)
		 DO UPDATE SET data = $3::jsonb`,
		entityID, date, data)
	if err != nil {
		return fmt.Errorf("saving snapshot: %w", err)
	}
	return nil
}

func (r *PgRepository) GetLatest(ctx context.Context, entitySlug string) (*Snapshot, error) {
	var s Snapshot
	err := r.pool.QueryRow(ctx,
		`SELECT fs.id, fs.entity_id, fs.snapshot_date, fs.data, fs.created_at
		 FROM fund_snapshots fs
		 JOIN fund_entities fe ON fe.id = fs.entity_id
		 WHERE fe.slug = $1
		 ORDER BY fs.snapshot_date DESC
		 LIMIT 1`, entitySlug).Scan(&s.ID, &s.EntityID, &s.SnapshotDate, &s.Data, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting latest snapshot: %w", err)
	}
	return &s, nil
}

func (r *PgRepository) GetByDate(ctx context.Context, entitySlug string, date time.Time) (*Snapshot, error) {
	var s Snapshot
	err := r.pool.QueryRow(ctx,
		`SELECT fs.id, fs.entity_id, fs.snapshot_date, fs.data, fs.created_at
		 FROM fund_snapshots fs
		 JOIN fund_entities fe ON fe.id = fs.entity_id
		 WHERE fe.slug = $1 AND fs.snapshot_date = $2`, entitySlug, date).Scan(&s.ID, &s.EntityID, &s.SnapshotDate, &s.Data, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting snapshot by date: %w", err)
	}
	return &s, nil
}

func (r *PgRepository) List(ctx context.Context, entitySlug string, limit int) ([]Snapshot, error) {
	if limit <= 0 {
		limit = 30
	}

	rows, err := r.pool.Query(ctx,
		`SELECT fs.id, fs.entity_id, fs.snapshot_date, fs.data, fs.created_at
		 FROM fund_snapshots fs
		 JOIN fund_entities fe ON fe.id = fs.entity_id
		 WHERE fe.slug = $1
		 ORDER BY fs.snapshot_date DESC
		 LIMIT $2`, entitySlug, limit)
	if err != nil {
		return nil, fmt.Errorf("listing snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []Snapshot
	for rows.Next() {
		var s Snapshot
		if err := rows.Scan(&s.ID, &s.EntityID, &s.SnapshotDate, &s.Data, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning snapshot: %w", err)
		}
		snapshots = append(snapshots, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating snapshots: %w", err)
	}
	return snapshots, nil
}

func (r *PgRepository) GetEntityID(ctx context.Context, slug string) (int, error) {
	var id int
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM fund_entities WHERE slug = $1`, slug).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("getting entity ID for %s: %w", slug, err)
	}
	return id, nil
}

func (r *PgRepository) EnsureEntity(ctx context.Context, slug, name, description string) (int, error) {
	var id int
	err := r.pool.QueryRow(ctx,
		`INSERT INTO fund_entities (slug, name, description)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (slug) DO UPDATE SET name = $2
		 RETURNING id`,
		slug, name, description).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensuring entity %s: %w", slug, err)
	}
	return id, nil
}
