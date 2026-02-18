package database

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a PostgreSQL connection pool.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return pool, nil
}

// RunMigrations applies all .up.sql migration files from the given filesystem.
// Already-applied migrations are tracked in schema_migrations and skipped on subsequent runs.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	rows, err := pool.Query(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("reading applied migrations: %w", err)
	}
	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("scanning migration name: %w", err)
		}
		applied[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating applied migrations: %w", err)
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	for _, file := range upFiles {
		if applied[file] {
			continue
		}
		sql, err := fs.ReadFile(fsys, file)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", file, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("executing migration %s: %w", file, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`, file); err != nil {
			return fmt.Errorf("recording migration %s: %w", file, err)
		}
	}

	return nil
}
