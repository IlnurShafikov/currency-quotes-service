package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

// ProcessorConfig tunes the background processing of update requests.
type ProcessorConfig struct {
	// BatchSize is the maximum number of requests claimed per run. Claimed
	// requests are processed concurrently.
	BatchSize int
	// MaxAttempts is how many times a request is tried before it is failed.
	MaxAttempts int
	// Timeout bounds one run: claiming requests, fetching rates and storing
	// the results.
	Timeout time.Duration
	// StaleAfter is how long a claim is honoured. A request claimed longer
	// ago is considered abandoned, e.g. by a crashed worker, and may be
	// claimed again. It also acts as the delay before a retry.
	//
	// StaleAfter must exceed Timeout so that a request is never taken over
	// while a live worker is still processing it.
	StaleAfter time.Duration
}

func (c ProcessorConfig) validate() error {
	switch {
	case c.BatchSize < 1:
		return fmt.Errorf("%w: batch size must be positive", ErrInvalidProcessorConfig)
	case c.MaxAttempts < 1:
		return fmt.Errorf("%w: max attempts must be positive", ErrInvalidProcessorConfig)
	case c.Timeout <= 0:
		return fmt.Errorf("%w: timeout must be positive", ErrInvalidProcessorConfig)
	case c.StaleAfter <= c.Timeout:
		return fmt.Errorf("%w: stale-after must exceed timeout", ErrInvalidProcessorConfig)
	}

	return nil
}

// UpdateProcessor carries out pending update requests: it fetches the rate
// from the provider and stores the quote together with the request outcome.
//
// UpdateProcessor is safe for concurrent use if its dependencies are. Several
// processors, in one process or in many, may work on the same storage.
type UpdateProcessor struct {
	updates UpdateRequestRepository
	quotes  QuoteRepository
	rates   RateProvider
	tx      Transactor
	clock   Clock
	ids     IDGenerator
	cfg     ProcessorConfig
}

// NewUpdateProcessor returns an UpdateProcessor that uses the given
// dependencies. It returns [ErrInvalidProcessorConfig] if cfg is unusable.
func NewUpdateProcessor(
	updates UpdateRequestRepository,
	quotes QuoteRepository,
	rates RateProvider,
	tx Transactor,
	clock Clock,
	ids IDGenerator,
	cfg ProcessorConfig,
) (*UpdateProcessor, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &UpdateProcessor{
		updates: updates,
		quotes:  quotes,
		rates:   rates,
		tx:      tx,
		clock:   clock,
		ids:     ids,
		cfg:     cfg,
	}, nil
}

// ProcessPending claims a batch of pending update requests and processes
// them concurrently. It returns the number of claimed requests, so that the
// caller can tell an idle run from a busy one.
//
// A problem with one request does not stop the others. The returned error
// joins the problems of all requests in the batch and is meant for logging:
// by the time it is returned every request has either been finished or left
// pending for a later run.
func (p *UpdateProcessor) ProcessPending(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()

	claimed, err := p.updates.ClaimPending(ctx, p.cfg.BatchSize, p.cfg.StaleAfter)
	if err != nil {
		return 0, fmt.Errorf("claim pending updates: %w", err)
	}

	errs := make([]error, len(claimed))

	var wg sync.WaitGroup

	for i, req := range claimed {
		wg.Go(func() {
			errs[i] = p.process(ctx, req)
		})
	}

	wg.Wait()

	return len(claimed), errors.Join(errs...)
}

// process carries out one claimed request.
func (p *UpdateProcessor) process(ctx context.Context, req domain.UpdateRequest) error {
	if req.Attempts > p.cfg.MaxAttempts {
		// Every earlier attempt ended without a result, e.g. the worker
		// crashed. Give up instead of trying the same request forever.
		return p.fail(ctx, req, ErrAttemptsExhausted)
	}

	quote, err := p.fetchQuote(ctx, req.Pair)
	if err != nil {
		if req.Attempts >= p.cfg.MaxAttempts {
			return p.fail(ctx, req, err)
		}

		// The request stays pending and is claimed again after StaleAfter.
		return fmt.Errorf("update %s, attempt %d of %d: %w", req.ID, req.Attempts, p.cfg.MaxAttempts, err)
	}

	return p.complete(ctx, req, quote)
}

func (p *UpdateProcessor) fetchQuote(ctx context.Context, pair domain.Pair) (domain.Quote, error) {
	rate, err := p.rates.Rate(ctx, pair)
	if err != nil {
		return domain.Quote{}, fmt.Errorf("fetch rate for %s: %w", pair, err)
	}

	id, err := p.ids.NewQuoteID()
	if err != nil {
		return domain.Quote{}, fmt.Errorf("new quote for %s: %w", pair, err)
	}

	quote, err := domain.NewQuote(id, pair, rate, p.clock.Now())
	if err != nil {
		return domain.Quote{}, fmt.Errorf("new quote for %s: %w", pair, err)
	}

	return quote, nil
}

// complete stores the quote and marks the request as completed atomically,
// so that a completed request always has its quote and vice versa.
func (p *UpdateProcessor) complete(ctx context.Context, req domain.UpdateRequest, quote domain.Quote) error {
	if err := req.Complete(quote.ID, p.clock.Now()); err != nil {
		return fmt.Errorf("complete update %s: %w", req.ID, err)
	}

	err := p.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := p.quotes.Save(ctx, quote); err != nil {
			return fmt.Errorf("save quote: %w", err)
		}

		if err := p.updates.Finish(ctx, req); err != nil {
			return fmt.Errorf("finish update: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("complete update %s: %w", req.ID, err)
	}

	return nil
}

// fail marks the request as failed because of cause. It always returns an
// error: cause itself once the failure is stored, so that it gets logged.
func (p *UpdateProcessor) fail(ctx context.Context, req domain.UpdateRequest, cause error) error {
	if err := req.Fail(cause.Error(), p.clock.Now()); err != nil {
		return fmt.Errorf("fail update %s: %w", req.ID, err)
	}

	if err := p.updates.Finish(ctx, req); err != nil {
		return fmt.Errorf("fail update %s: %w", req.ID, err)
	}

	return fmt.Errorf("update %s failed after %d attempts: %w", req.ID, req.Attempts, cause)
}
