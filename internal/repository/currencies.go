package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

var _ service.CurrencyRepository = (*Currencies)(nil)

// Currencies answers which currencies the service provides quotes for. The
// list lives in the currencies table and is changed by migrations.
type Currencies struct {
	db *DB
}

// NewCurrencies returns a Currencies repository on top of db.
func NewCurrencies(db *DB) *Currencies {
	return &Currencies{db: db}
}

// AllSupported reports whether every one of the given currencies is
// supported. It reports true for an empty list.
func (r *Currencies) AllSupported(ctx context.Context, currencies ...domain.Currency) (bool, error) {
	codes := make([]string, len(currencies))
	for i, currency := range currencies {
		codes[i] = currency.String()
	}

	// True unless some requested code is missing from the table. Unlike
	// comparing counts, this is not confused by duplicate codes.
	const query = `
		SELECT NOT EXISTS (
			SELECT 1
			FROM unnest($1::text[]) AS requested (code)
			WHERE NOT EXISTS (SELECT 1 FROM currencies WHERE currencies.code = requested.code)
		)`

	var supported bool
	if err := r.db.conn(ctx).QueryRow(ctx, query, codes).Scan(&supported); err != nil {
		return false, fmt.Errorf("check supported currencies: %w", err)
	}

	return supported, nil
}

// All returns every supported currency, ordered by code.
func (r *Currencies) All(ctx context.Context) ([]domain.Currency, error) {
	const query = `SELECT code FROM currencies ORDER BY code`

	rows, err := r.db.conn(ctx).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list supported currencies: %w", err)
	}

	currencies, err := pgx.CollectRows(rows, pgx.RowTo[domain.Currency])
	if err != nil {
		return nil, fmt.Errorf("list supported currencies: %w", err)
	}

	return currencies, nil
}
