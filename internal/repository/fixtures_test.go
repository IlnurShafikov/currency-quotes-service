package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

// Fixed values shared by the tests of this package. The quote ids are
// ordered: firstQuoteID < secondQuoteID < thirdQuoteID.
var (
	firstQuoteID  = domain.QuoteID(uuid.MustParse("0199b0c2-8d4f-7b21-a07c-4c2e3d5f6a71"))
	secondQuoteID = domain.QuoteID(uuid.MustParse("0199b0c2-9e50-7c32-b18d-5d3f4e607b82"))
	thirdQuoteID  = domain.QuoteID(uuid.MustParse("0199b0c2-af61-7d43-829e-6e4f5f718c93"))
	unknownQuote  = domain.QuoteID(uuid.MustParse("0199b0c2-ffff-7fff-bfff-ffffffffffff"))

	firstUpdateID  = domain.UpdateID(uuid.MustParse("0199b0c2-7c3e-7a10-9f6b-3b1d2c4e5f60"))
	secondUpdateID = domain.UpdateID(uuid.MustParse("0199b0c2-7d4f-7b21-a07c-4c2e3d5f6a71"))
	thirdUpdateID  = domain.UpdateID(uuid.MustParse("0199b0c2-7e50-7c32-b18d-5d3f4e607b82"))

	eurMXN = domain.Pair{Base: "EUR", Quote: "MXN"}
	mxnEUR = domain.Pair{Base: "MXN", Quote: "EUR"}
	usdMXN = domain.Pair{Base: "USD", Quote: "MXN"}

	noon = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
)

// liveContext returns the context of the test.
func liveContext(t *testing.T) context.Context {
	t.Helper()

	return t.Context()
}

// cancelledContext returns a context that is already cancelled. A storage
// call made with it fails before reaching the database, which is how the
// tests exercise the error paths of the repositories.
func cancelledContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	return ctx
}

func newQuote(id domain.QuoteID, pair domain.Pair, price string, obtainedAt time.Time) domain.Quote {
	return domain.Quote{
		ID:         id,
		Pair:       pair,
		Price:      decimal.RequireFromString(price),
		ObtainedAt: obtainedAt,
	}
}
