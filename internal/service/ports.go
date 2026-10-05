// Package service implements the use cases of the quotes service.
//
// It is the application layer of the hexagon. It depends only on the domain
// and declares, as interfaces, everything it needs from the outside world
// (storage, time, identifiers). Adapters implement those interfaces; the
// service never imports an adapter.
package service

import (
	"context"
	"time"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

// UpdateRequestRepository stores update requests.
type UpdateRequestRepository interface {
	// CreateOrGetPending stores req unless a pending request for the same
	// pair already exists, in which case the existing request is returned
	// instead. This is what makes requesting an update idempotent.
	CreateOrGetPending(ctx context.Context, req domain.UpdateRequest) (domain.UpdateRequest, error)
	// Get returns the request with the given id.
	// It returns [domain.ErrUpdateNotFound] if there is none.
	Get(ctx context.Context, id domain.UpdateID) (domain.UpdateRequest, error)
}

// QuoteRepository stores quotes.
type QuoteRepository interface {
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
}

// Clock tells the current time.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces new entity identifiers.
type IDGenerator interface {
	NewUpdateID() (domain.UpdateID, error)
}
