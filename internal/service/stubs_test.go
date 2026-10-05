package service_test

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

// Fixed values shared by the tests of this package.
var (
	updateID   = domain.UpdateID(uuid.MustParse("0199b0c2-7c3e-7a10-9f6b-3b1d2c4e5f60"))
	existingID = domain.UpdateID(uuid.MustParse("0199b0c2-6b2d-7f09-8e5a-2a0c1b3d4e5f"))
	quoteID    = domain.QuoteID(uuid.MustParse("0199b0c2-8d4f-7b21-a07c-4c2e3d5f6a71"))
	eurMXN     = domain.Pair{Base: "EUR", Quote: "MXN"}
	now        = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	price      = decimal.RequireFromString("21.4587")

	errStorage = errors.New("storage unavailable")
	errIDs     = errors.New("id generator unavailable")
)

// fixedClock is a Clock that always returns the same time.
type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

// stubIDs is an IDGenerator that returns preset values.
type stubIDs struct {
	updateID domain.UpdateID
	err      error
}

func (s stubIDs) NewUpdateID() (domain.UpdateID, error) { return s.updateID, s.err }

// stubCurrencies is a CurrencyRepository that returns a preset answer.
type stubCurrencies struct {
	supported bool
	err       error
}

func (s stubCurrencies) AllSupported(context.Context, ...domain.Currency) (bool, error) {
	return s.supported, s.err
}

// stubUpdates is an UpdateRequestRepository that returns preset values and
// records the arguments it was called with.
type stubUpdates struct {
	stored    domain.UpdateRequest // returned by CreateOrGetPending
	createErr error
	found     domain.UpdateRequest // returned by Get
	getErr    error

	created domain.UpdateRequest // argument of the last CreateOrGetPending call
	gotID   domain.UpdateID      // argument of the last Get call
}

func (s *stubUpdates) CreateOrGetPending(
	_ context.Context,
	req domain.UpdateRequest,
) (domain.UpdateRequest, error) {
	s.created = req

	return s.stored, s.createErr
}

func (s *stubUpdates) Get(_ context.Context, id domain.UpdateID) (domain.UpdateRequest, error) {
	s.gotID = id

	return s.found, s.getErr
}

// stubQuotes is a QuoteRepository that returns preset values and records the
// arguments it was called with.
type stubQuotes struct {
	byID      domain.Quote // returned by Get
	getErr    error
	latest    domain.Quote // returned by Latest
	latestErr error

	gotID   domain.QuoteID // argument of the last Get call
	gotPair domain.Pair    // argument of the last Latest call
}

func (s *stubQuotes) Get(_ context.Context, id domain.QuoteID) (domain.Quote, error) {
	s.gotID = id

	return s.byID, s.getErr
}

func (s *stubQuotes) Latest(_ context.Context, pair domain.Pair) (domain.Quote, error) {
	s.gotPair = pair

	return s.latest, s.latestErr
}

func pendingRequest(id domain.UpdateID) domain.UpdateRequest {
	return domain.UpdateRequest{
		ID:          id,
		Pair:        eurMXN,
		Status:      domain.UpdatePending,
		QuoteID:     nil,
		Error:       "",
		Attempts:    0,
		RequestedAt: now,
		StartedAt:   nil,
		FinishedAt:  nil,
	}
}

func completedRequest() domain.UpdateRequest {
	req := pendingRequest(updateID)
	req.Status = domain.UpdateCompleted
	req.QuoteID = new(quoteID)
	req.Attempts = 1
	req.StartedAt = new(now.Add(time.Second))
	req.FinishedAt = new(now.Add(2 * time.Second))

	return req
}

func failedRequest() domain.UpdateRequest {
	req := pendingRequest(updateID)
	req.Status = domain.UpdateFailed
	req.Error = "provider unavailable"
	req.Attempts = 3
	req.StartedAt = new(now.Add(time.Second))
	req.FinishedAt = new(now.Add(2 * time.Second))

	return req
}

func eurMXNQuote() domain.Quote {
	return domain.Quote{
		ID:         quoteID,
		Pair:       eurMXN,
		Price:      price,
		ObtainedAt: now.Add(2 * time.Second),
	}
}
