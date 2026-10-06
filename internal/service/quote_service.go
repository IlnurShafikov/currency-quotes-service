package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

// UpdateResult is the state of an update request together with the quote it
// produced.
type UpdateResult struct {
	Request domain.UpdateRequest
	// Quote is nil until the request is completed.
	Quote *domain.Quote
}

// QuoteService handles the client-facing use cases: requesting a quote
// update and reading quotes. It never fetches rates itself; updates are
// carried out in the background.
//
// QuoteService is safe for concurrent use if its dependencies are.
type QuoteService struct {
	updates    UpdateRequestRepository
	quotes     QuoteRepository
	currencies CurrencyRepository
	clock      Clock
	ids        IDGenerator
}

// NewQuoteService returns a QuoteService that uses the given dependencies.
func NewQuoteService(
	updates UpdateRequestRepository,
	quotes QuoteRepository,
	currencies CurrencyRepository,
	clock Clock,
	ids IDGenerator,
) *QuoteService {
	return &QuoteService{
		updates:    updates,
		quotes:     quotes,
		currencies: currencies,
		clock:      clock,
		ids:        ids,
	}
}

// RequestUpdate registers a request to refresh the quote of the base/quote
// pair and returns its identifier. The quote itself is fetched later, in the
// background.
//
// The operation is idempotent: while a request for the pair is pending,
// repeated calls return the identifier of that request.
//
// It returns [domain.ErrInvalidCurrency] or [domain.ErrInvalidPair] if the
// input is malformed and [domain.ErrUnsupportedCurrency] if the service does
// not provide quotes for one of the currencies.
func (s *QuoteService) RequestUpdate(ctx context.Context, base, quote string) (domain.UpdateID, error) {
	pair, err := s.supportedPair(ctx, base, quote)
	if err != nil {
		return domain.UpdateID{}, err
	}

	return s.enqueue(ctx, pair)
}

// RefreshStale requests an update for every supported currency pair whose
// latest quote is older than maxAge or missing, and returns the number of
// pairs it requested an update for. It is what keeps quotes fresh when no
// client asks for an update.
//
// Like [QuoteService.RequestUpdate] it only registers requests; the updates
// are carried out in the background. A pair that already has a pending
// request is not requested twice.
//
// A problem with one pair does not stop the others; the returned error
// joins the problems of all pairs.
func (s *QuoteService) RefreshStale(ctx context.Context, maxAge time.Duration) (int, error) {
	currencies, err := s.currencies.All(ctx)
	if err != nil {
		return 0, fmt.Errorf("refresh stale quotes: %w", err)
	}

	var (
		now       = s.clock.Now()
		requested int
		errs      []error
	)

	for _, pair := range allPairs(currencies) {
		stale, err := s.isStale(ctx, pair, now, maxAge)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		if !stale {
			continue
		}

		if _, err := s.enqueue(ctx, pair); err != nil {
			errs = append(errs, err)

			continue
		}

		requested++
	}

	return requested, errors.Join(errs...)
}

// enqueue stores a pending update request for pair, or finds the one that is
// already pending, and returns its identifier.
func (s *QuoteService) enqueue(ctx context.Context, pair domain.Pair) (domain.UpdateID, error) {
	id, err := s.ids.NewUpdateID()
	if err != nil {
		return domain.UpdateID{}, fmt.Errorf("request update for %s: %w", pair, err)
	}

	stored, err := s.updates.CreateOrGetPending(ctx, domain.NewUpdateRequest(id, pair, s.clock.Now()))
	if err != nil {
		return domain.UpdateID{}, fmt.Errorf("request update for %s: %w", pair, err)
	}

	return stored.ID, nil
}

// isStale reports whether pair has no quote or its latest quote was obtained
// maxAge ago or earlier.
func (s *QuoteService) isStale(
	ctx context.Context,
	pair domain.Pair,
	now time.Time,
	maxAge time.Duration,
) (bool, error) {
	latest, err := s.quotes.Latest(ctx, pair)
	if errors.Is(err, domain.ErrQuoteNotFound) {
		return true, nil
	}

	if err != nil {
		return false, fmt.Errorf("check freshness of %s: %w", pair, err)
	}

	return now.Sub(latest.ObtainedAt) >= maxAge, nil
}

// allPairs returns every ordered pair of distinct currencies: both EUR/MXN
// and MXN/EUR, since each direction has its own quote.
func allPairs(currencies []domain.Currency) []domain.Pair {
	pairs := make([]domain.Pair, 0, len(currencies)*max(len(currencies)-1, 0))

	for _, base := range currencies {
		for _, quote := range currencies {
			if base == quote {
				continue
			}

			pairs = append(pairs, domain.Pair{Base: base, Quote: quote})
		}
	}

	return pairs
}

// GetUpdate returns the update request with the given identifier and, if it
// is completed, the quote it produced.
//
// It returns [domain.ErrUpdateNotFound] if there is no such request.
func (s *QuoteService) GetUpdate(ctx context.Context, id domain.UpdateID) (UpdateResult, error) {
	req, err := s.updates.Get(ctx, id)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("get update %s: %w", id, err)
	}

	if req.QuoteID == nil {
		return UpdateResult{Request: req, Quote: nil}, nil
	}

	quote, err := s.quotes.Get(ctx, *req.QuoteID)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("get quote of update %s: %w", id, err)
	}

	return UpdateResult{Request: req, Quote: &quote}, nil
}

// GetLatestQuote returns the most recently obtained quote of the base/quote
// pair.
//
// It returns [domain.ErrInvalidCurrency] or [domain.ErrInvalidPair] if the
// input is malformed, [domain.ErrUnsupportedCurrency] if the service does not
// provide quotes for one of the currencies and [domain.ErrQuoteNotFound] if
// the pair has not been updated successfully yet.
func (s *QuoteService) GetLatestQuote(ctx context.Context, base, quote string) (domain.Quote, error) {
	pair, err := s.supportedPair(ctx, base, quote)
	if err != nil {
		return domain.Quote{}, err
	}

	latest, err := s.quotes.Latest(ctx, pair)
	if err != nil {
		return domain.Quote{}, fmt.Errorf("get latest quote for %s: %w", pair, err)
	}

	return latest, nil
}

// supportedPair turns raw client input into a pair the service works with.
func (s *QuoteService) supportedPair(ctx context.Context, base, quote string) (domain.Pair, error) {
	pair, err := parsePair(base, quote)
	if err != nil {
		return domain.Pair{}, err
	}

	supported, err := s.currencies.AllSupported(ctx, pair.Base, pair.Quote)
	if err != nil {
		return domain.Pair{}, fmt.Errorf("check currencies of %s: %w", pair, err)
	}

	if !supported {
		return domain.Pair{}, fmt.Errorf("%w: %s", domain.ErrUnsupportedCurrency, pair)
	}

	return pair, nil
}

func parsePair(base, quote string) (domain.Pair, error) {
	baseCurrency, err := domain.NewCurrency(base)
	if err != nil {
		return domain.Pair{}, fmt.Errorf("base currency: %w", err)
	}

	quoteCurrency, err := domain.NewCurrency(quote)
	if err != nil {
		return domain.Pair{}, fmt.Errorf("quote currency: %w", err)
	}

	pair, err := domain.NewPair(baseCurrency, quoteCurrency)
	if err != nil {
		return domain.Pair{}, fmt.Errorf("currency pair: %w", err)
	}

	return pair, nil
}
