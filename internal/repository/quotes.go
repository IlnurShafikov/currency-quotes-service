package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

var _ service.QuoteRepository = (*Quotes)(nil)

// quoteColumns lists the columns scanQuote expects, in order.
//
// The price column is NUMERIC(38, 18), so PostgreSQL pads every value to 18
// fractional digits. trim_scale removes the padding: a price stored as
// 21.4587 is read back as 21.4587, not 21.458700000000000000.
const quoteColumns = `id, base_currency, quote_currency, trim_scale(price), obtained_at`

// Quotes stores quotes in the quotes table.
type Quotes struct {
	db *DB
}

// NewQuotes returns a Quotes repository on top of db.
func NewQuotes(db *DB) *Quotes {
	return &Quotes{db: db}
}

// Save stores a new quote.
func (r *Quotes) Save(ctx context.Context, quote domain.Quote) error {
	const query = `
		INSERT INTO quotes (id, base_currency, quote_currency, price, obtained_at)
		VALUES ($1, $2, $3, $4, $5)`

	_, err := r.db.conn(ctx).Exec(ctx, query,
		uuid.UUID(quote.ID),
		quote.Pair.Base.String(),
		quote.Pair.Quote.String(),
		quote.Price,
		quote.ObtainedAt,
	)
	if err != nil {
		return fmt.Errorf("save quote %s: %w", quote.ID, err)
	}

	return nil
}

// Get returns the quote with the given id.
// It returns [domain.ErrQuoteNotFound] if there is none.
func (r *Quotes) Get(ctx context.Context, id domain.QuoteID) (domain.Quote, error) {
	const query = `SELECT ` + quoteColumns + ` FROM quotes WHERE id = $1`

	quote, err := scanQuote(r.db.conn(ctx).QueryRow(ctx, query, uuid.UUID(id)))
	if err != nil {
		return domain.Quote{}, fmt.Errorf("get quote %s: %w", id, err)
	}

	return quote, nil
}

// Latest returns the most recently obtained quote for pair.
// It returns [domain.ErrQuoteNotFound] if the pair has no quotes yet.
func (r *Quotes) Latest(ctx context.Context, pair domain.Pair) (domain.Quote, error) {
	// Served by quotes_latest_idx. The id breaks ties between quotes
	// obtained at the same instant; UUIDv7 ids grow with time.
	const query = `
		SELECT ` + quoteColumns + `
		FROM quotes
		WHERE base_currency = $1 AND quote_currency = $2
		ORDER BY obtained_at DESC, id DESC
		LIMIT 1`

	quote, err := scanQuote(r.db.conn(ctx).QueryRow(ctx, query, pair.Base.String(), pair.Quote.String()))
	if err != nil {
		return domain.Quote{}, fmt.Errorf("get latest quote for %s: %w", pair, err)
	}

	return quote, nil
}

// scanQuote reads one row with quoteColumns into a quote.
// It returns [domain.ErrQuoteNotFound] if the row does not exist.
func scanQuote(row pgx.Row) (domain.Quote, error) {
	var (
		id         uuid.UUID
		base       string
		quote      string
		price      decimal.Decimal
		obtainedAt time.Time
	)

	err := row.Scan(&id, &base, &quote, &price, &obtainedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Quote{}, domain.ErrQuoteNotFound
	}

	if err != nil {
		return domain.Quote{}, fmt.Errorf("scan quote: %w", err)
	}

	// The row was validated when it was written, so it is restored as is.
	return domain.Quote{
		ID:         domain.QuoteID(id),
		Pair:       domain.Pair{Base: domain.Currency(base), Quote: domain.Currency(quote)},
		Price:      price,
		ObtainedAt: obtainedAt.UTC(),
	}, nil
}
