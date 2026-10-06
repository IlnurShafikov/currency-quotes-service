// Package scheduler keeps quotes fresh without waiting for clients to ask.
// It is a driving adapter: like the HTTP handler and the worker it calls
// into the service layer, triggered by a timer.
//
// The scheduler does not fetch rates. It only asks the service to request
// updates for quotes that have gone stale; the worker carries them out.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Refresher is what the scheduler needs from the service layer.
type Refresher interface {
	// RefreshStale requests an update for every supported pair whose latest
	// quote is older than maxAge or missing, and returns how many pairs it
	// requested an update for.
	RefreshStale(ctx context.Context, maxAge time.Duration) (int, error)
}

// Scheduler periodically asks a Refresher to refresh stale quotes.
type Scheduler struct {
	refresher Refresher
	interval  time.Duration
	log       *slog.Logger
}

// New returns a Scheduler that refreshes quotes every interval and reports
// problems to log. It returns [ErrInvalidInterval] if interval is not
// positive.
//
// The interval is also the maximum age of a quote: a quote is refreshed once
// it is at least one interval old. With several instances of the service
// this keeps them from repeating each other's work, since an instance that
// finds a quote already refreshed leaves it alone.
func New(refresher Refresher, interval time.Duration, log *slog.Logger) (*Scheduler, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInterval, interval)
	}

	return &Scheduler{refresher: refresher, interval: interval, log: log}, nil
}

// Run refreshes stale quotes until ctx is cancelled: once right away, so
// that a freshly started service has quotes without anyone asking, and then
// every interval.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		s.refresh(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) refresh(ctx context.Context) {
	requested, err := s.refresher.RefreshStale(ctx, s.interval)

	// An error caused by the shutdown itself is not worth reporting.
	if err != nil && ctx.Err() == nil {
		s.log.ErrorContext(ctx, "refresh stale quotes",
			slog.Int("requested", requested),
			slog.Any("error", err),
		)
	}

	if requested > 0 {
		s.log.InfoContext(ctx, "requested updates for stale quotes", slog.Int("requested", requested))
	}
}
