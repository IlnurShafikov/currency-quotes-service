// Package service implements the use cases of the quotes service.
//
// It is the application layer of the hexagon. It depends only on the domain
// and declares, as interfaces, everything it needs from the outside world
// (storage, the rate provider, time, identifiers). Adapters implement those
// interfaces; the service never imports an adapter.
package service

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

// UpdateRequestRepository stores update requests and hands them out to
// background workers.
type UpdateRequestRepository interface {
	// CreateOrGetPending stores req unless a pending request for the same
	// pair already exists, in which case the existing request is returned
	// instead. This is what makes requesting an update idempotent.
	CreateOrGetPending(ctx context.Context, req domain.UpdateRequest) (domain.UpdateRequest, error)
	// Get returns the request with the given id.
	// It returns [domain.ErrUpdateNotFound] if there is none.
	Get(ctx context.Context, id domain.UpdateID) (domain.UpdateRequest, error)
	// ClaimPending picks up to limit pending requests for processing and
	// increments their attempt counters. A request is eligible if it has
	// never been claimed or was last claimed more than staleAfter ago.
	// A request claimed by one caller is not handed to another until then.
	ClaimPending(ctx context.Context, limit int, staleAfter time.Duration) ([]domain.UpdateRequest, error)
	// Finish stores the outcome of a claimed request that has been completed
	// or failed. It returns [ErrClaimLost] if the request has been claimed
	// again or finished since the caller claimed it.
	Finish(ctx context.Context, req domain.UpdateRequest) error
}

// QuoteRepository stores quotes. Quotes are append-only.
type QuoteRepository interface {
	// Save stores a new quote.
	Save(ctx context.Context, quote domain.Quote) error
	// Get returns the quote with the given id.
	// It returns [domain.ErrQuoteNotFound] if there is none.
	Get(ctx context.Context, id domain.QuoteID) (domain.Quote, error)
	// Latest returns the most recently obtained quote for pair.
	// It returns [domain.ErrQuoteNotFound] if the pair has no quotes yet.
	Latest(ctx context.Context, pair domain.Pair) (domain.Quote, error)
}

// CurrencyRepository knows which currencies the service provides quotes for.
type CurrencyRepository interface {
	// AllSupported reports whether every one of the given currencies is supported.
	AllSupported(ctx context.Context, currencies ...domain.Currency) (bool, error)
	// All returns every supported currency.
	All(ctx context.Context) ([]domain.Currency, error)
}

// RateProvider fetches current exchange rates from an external source.
type RateProvider interface {
	// Rate returns the price of one unit of pair.Base expressed in pair.Quote.
	Rate(ctx context.Context, pair domain.Pair) (decimal.Decimal, error)
}

// Transactor runs a function atomically.
type Transactor interface {
	// WithinTx calls fn with a context that carries a transaction. Repository
	// calls made with that context are committed together if fn returns nil
	// and rolled back together otherwise.
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock tells the current time.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces new entity identifiers.
type IDGenerator interface {
	NewUpdateID() (domain.UpdateID, error)
	NewQuoteID() (domain.QuoteID, error)
}
