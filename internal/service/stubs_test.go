package service_test

import (
	"context"
	"errors"
	"sync"
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

	errStorage  = errors.New("storage unavailable")
	errIDs      = errors.New("id generator unavailable")
	errProvider = errors.New("rate provider unavailable")
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
	quoteID  domain.QuoteID
	quoteErr error
}

func (s stubIDs) NewUpdateID() (domain.UpdateID, error) { return s.updateID, s.err }

func (s stubIDs) NewQuoteID() (domain.QuoteID, error) { return s.quoteID, s.quoteErr }

// stubCurrencies is a CurrencyRepository that returns a preset answer.
type stubCurrencies struct {
	supported bool
	err       error
}

func (s stubCurrencies) AllSupported(context.Context, ...domain.Currency) (bool, error) {
	return s.supported, s.err
}

// txKey marks a context as carrying a transaction of stubTx.
type txKey struct{}

// stubTx is a Transactor that runs the function right away and marks its
// context, so that repository stubs can record whether a call was made
// inside a transaction. It cannot roll anything back.
type stubTx struct{}

func (stubTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(context.WithValue(ctx, txKey{}, true))
}

func inTx(ctx context.Context) bool {
	marked, _ := ctx.Value(txKey{}).(bool)

	return marked
}

// finishCall is a successful UpdateRequestRepository.Finish call.
type finishCall struct {
	req  domain.UpdateRequest
	inTx bool
}

// saveCall is a successful QuoteRepository.Save call.
type saveCall struct {
	quote domain.Quote
	inTx  bool
}

// stubUpdates is an UpdateRequestRepository that returns preset values and
// records the calls it received. It is safe for concurrent use.
type stubUpdates struct {
	stored    domain.UpdateRequest // returned by CreateOrGetPending
	createErr error
	found     domain.UpdateRequest // returned by Get
	getErr    error
	claimed   []domain.UpdateRequest // returned by ClaimPending
	claimErr  error
	finishErr error

	mu              sync.Mutex
	created         domain.UpdateRequest // argument of the last CreateOrGetPending call
	gotID           domain.UpdateID      // argument of the last Get call
	claimLimit      int                  // arguments of the last ClaimPending call
	claimStaleAfter time.Duration
	finished        []finishCall // successful Finish calls
}

func (s *stubUpdates) CreateOrGetPending(
	_ context.Context,
	req domain.UpdateRequest,
) (domain.UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.created = req

	return s.stored, s.createErr
}

func (s *stubUpdates) Get(_ context.Context, id domain.UpdateID) (domain.UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gotID = id

	return s.found, s.getErr
}

func (s *stubUpdates) ClaimPending(
	_ context.Context,
	limit int,
	staleAfter time.Duration,
) ([]domain.UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.claimLimit = limit
	s.claimStaleAfter = staleAfter

	return s.claimed, s.claimErr
}

func (s *stubUpdates) Finish(ctx context.Context, req domain.UpdateRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.finishErr != nil {
		return s.finishErr
	}

	s.finished = append(s.finished, finishCall{req: req, inTx: inTx(ctx)})

	return nil
}

// stubQuotes is a QuoteRepository that returns preset values and records the
// calls it received. It is safe for concurrent use.
type stubQuotes struct {
	byID      domain.Quote // returned by Get
	getErr    error
	latest    domain.Quote // returned by Latest
	latestErr error
	saveErr   error

	mu      sync.Mutex
	gotID   domain.QuoteID // argument of the last Get call
	gotPair domain.Pair    // argument of the last Latest call
	saved   []saveCall     // successful Save calls
}

func (s *stubQuotes) Save(ctx context.Context, quote domain.Quote) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.saveErr != nil {
		return s.saveErr
	}

	s.saved = append(s.saved, saveCall{quote: quote, inTx: inTx(ctx)})

	return nil
}

func (s *stubQuotes) Get(_ context.Context, id domain.QuoteID) (domain.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gotID = id

	return s.byID, s.getErr
}

func (s *stubQuotes) Latest(_ context.Context, pair domain.Pair) (domain.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gotPair = pair

	return s.latest, s.latestErr
}

// stubRates is a RateProvider that returns a preset rate and records how it
// was called. It is safe for concurrent use.
type stubRates struct {
	rate decimal.Decimal
	err  error

	mu          sync.Mutex
	calls       int
	hadDeadline bool // whether the context of the last call had a deadline
}

func (s *stubRates) Rate(ctx context.Context, _ domain.Pair) (decimal.Decimal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++
	_, s.hadDeadline = ctx.Deadline()

	return s.rate, s.err
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
