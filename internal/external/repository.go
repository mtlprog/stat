package external

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// Quote represents an external price quote stored in the database.
type Quote struct {
	Symbol     string          `json:"symbol"`
	PriceInEUR decimal.Decimal `json:"priceInEur"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// QuoteRepository defines persistent storage for external quotes.
type QuoteRepository interface {
	SaveQuote(ctx context.Context, symbol string, priceInEUR decimal.Decimal) error
	GetQuote(ctx context.Context, symbol string) (Quote, error)
	GetAllQuotes(ctx context.Context) ([]Quote, error)
}

// PgQuoteRepository implements QuoteRepository with PostgreSQL.
type PgQuoteRepository struct {
	pool *pgxpool.Pool
}

// NewPgQuoteRepository creates a new PostgreSQL quote repository.
func NewPgQuoteRepository(pool *pgxpool.Pool) *PgQuoteRepository {
	return &PgQuoteRepository{pool: pool}
}

func (r *PgQuoteRepository) SaveQuote(ctx context.Context, symbol string, priceInEUR decimal.Decimal) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO external_quotes (symbol, price_in_eur, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (symbol) DO UPDATE SET price_in_eur = $2, updated_at = NOW()`,
		symbol, priceInEUR)
	if err != nil {
		return fmt.Errorf("saving quote for %s: %w", symbol, err)
	}
	return nil
}

func (r *PgQuoteRepository) GetQuote(ctx context.Context, symbol string) (Quote, error) {
	var q Quote
	err := r.pool.QueryRow(ctx,
		`SELECT symbol, price_in_eur, updated_at FROM external_quotes WHERE symbol = $1`,
		symbol).Scan(&q.Symbol, &q.PriceInEUR, &q.UpdatedAt)
	if err != nil {
		return Quote{}, fmt.Errorf("getting quote for %s: %w", symbol, err)
	}
	return q, nil
}

func (r *PgQuoteRepository) GetAllQuotes(ctx context.Context) ([]Quote, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT symbol, price_in_eur, updated_at FROM external_quotes ORDER BY symbol`)
	if err != nil {
		return nil, fmt.Errorf("getting all quotes: %w", err)
	}
	defer rows.Close()

	var quotes []Quote
	for rows.Next() {
		var q Quote
		if err := rows.Scan(&q.Symbol, &q.PriceInEUR, &q.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning quote: %w", err)
		}
		quotes = append(quotes, q)
	}
	return quotes, rows.Err()
}
