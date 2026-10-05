// Package worker runs the background processing of update requests. It is a
// driving adapter: like the HTTP handler it calls into the service layer,
// only it is triggered by a timer instead of a request.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrInvalidInterval is returned when the polling interval is not positive.
var ErrInvalidInterval = errors.New("worker interval must be positive")

// Processor is what the worker needs from the service layer.
type Processor interface {
	// ProcessPending processes one batch of pending update requests and
	// returns how many requests the batch contained.
	ProcessPending(ctx context.Context) (int, error)
}

// Worker periodically asks a Processor to process pending update requests.
type Worker struct {
	processor Processor
	interval  time.Duration
	log       *slog.Logger
}

// New returns a Worker that polls processor every interval and reports
// problems to log. It returns [ErrInvalidInterval] if interval is not
// positive.
func New(processor Processor, interval time.Duration, log *slog.Logger) (*Worker, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInterval, interval)
	}

	return &Worker{processor: processor, interval: interval, log: log}, nil
}

// Run processes pending update requests until ctx is cancelled: once right
// away and then every interval.
//
// Cancelling ctx stops the worker from starting new batches. A batch that is
// already running is allowed to finish, so that a shutdown does not waste
// its attempts; the batch is still bounded by the processor's own timeout.
// Run returns after that batch is done.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		w.drain(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// drain processes batch after batch until nothing is left to claim, so that
// a backlog is worked off without waiting for the next tick between batches.
func (w *Worker) drain(ctx context.Context) {
	for ctx.Err() == nil {
		// The batch gets a context that survives cancellation of ctx.
		processed, err := w.processor.ProcessPending(context.WithoutCancel(ctx))
		if err != nil {
			w.log.ErrorContext(ctx, "process pending updates",
				slog.Int("processed", processed),
				slog.Any("error", err),
			)
		}

		if processed == 0 {
			return
		}

		w.log.DebugContext(ctx, "processed pending updates", slog.Int("processed", processed))
	}
}
