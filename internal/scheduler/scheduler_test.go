package scheduler_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/scheduler"
)

const interval = time.Hour

var errRefresh = errors.New("check freshness of EUR/MXN: storage unavailable")

// run is the outcome of one RefreshStale call.
type run struct {
	requested int
	err       error
}

// stubRefresher is a scheduler.Refresher that returns the preset outcomes
// one by one, reports "nothing was stale" once they are used up, and records
// the max age it was called with.
type stubRefresher struct {
	mu      sync.Mutex
	runs    []run
	maxAges []time.Duration
}

func (r *stubRefresher) RefreshStale(ctx context.Context, maxAge time.Duration) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.maxAges = append(r.maxAges, maxAge)

	// Behave like the real service: a cancelled context is an error.
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if len(r.runs) == 0 {
		return 0, nil
	}

	next := r.runs[0]
	r.runs = r.runs[1:]

	return next.requested, next.err
}

func (r *stubRefresher) calls() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]time.Duration(nil), r.maxAges...)
}

func newScheduler(t *testing.T, refresher scheduler.Refresher, logs *bytes.Buffer) *scheduler.Scheduler {
	t.Helper()

	s, err := scheduler.New(refresher, interval, slog.New(slog.NewTextHandler(logs, nil)))
	require.NoError(t, err)

	return s
}

// start runs the scheduler in the background and returns a function that
// stops it and waits for Run to return.
func start(t *testing.T, s *scheduler.Scheduler) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		s.Run(ctx)
	}()

	return func() {
		cancel()
		<-done
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval time.Duration
		wantOK   bool
		wantErr  error
	}{
		{
			name:     "positive interval",
			interval: time.Hour,
			wantOK:   true,
			wantErr:  nil,
		},
		{
			name:     "zero interval",
			interval: 0,
			wantOK:   false,
			wantErr:  scheduler.ErrInvalidInterval,
		},
		{
			name:     "negative interval",
			interval: -time.Hour,
			wantOK:   false,
			wantErr:  scheduler.ErrInvalidInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := scheduler.New(&stubRefresher{}, tt.interval, slog.New(slog.DiscardHandler))
			assert.Equal(t, tt.wantOK, got != nil)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// The tests below run inside synctest bubbles: time is virtual there, so an
// hour-long interval takes no real time and the number of calls is exact.

func TestScheduler_Run_RefreshesAtStartAndOnEveryTick(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		refresher := &stubRefresher{}
		stop := start(t, newScheduler(t, refresher, &bytes.Buffer{}))

		synctest.Wait()
		assert.Len(t, refresher.calls(), 1, "must not wait for the first tick")

		time.Sleep(interval)
		synctest.Wait()
		assert.Len(t, refresher.calls(), 2)

		time.Sleep(3 * interval)
		synctest.Wait()
		assert.Len(t, refresher.calls(), 5)

		stop()
	})
}

func TestScheduler_Run_UsesIntervalAsMaxAge(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		refresher := &stubRefresher{}
		stop := start(t, newScheduler(t, refresher, &bytes.Buffer{}))

		time.Sleep(interval)
		synctest.Wait()
		stop()

		assert.Equal(t, []time.Duration{interval, interval}, refresher.calls())
	})
}

func TestScheduler_Run_LogsErrorsAndKeepsRunning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer

		refresher := &stubRefresher{runs: []run{{requested: 0, err: errRefresh}}}
		stop := start(t, newScheduler(t, refresher, &logs))

		synctest.Wait()
		assert.Len(t, refresher.calls(), 1)

		time.Sleep(interval)
		synctest.Wait()
		assert.Len(t, refresher.calls(), 2, "an error must not stop the scheduler")

		stop()

		assert.Contains(t, logs.String(), errRefresh.Error())
	})
}

func TestScheduler_Run_LogsHowManyUpdatesWereRequested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		requested  int
		wantLogged bool
	}{
		{
			name:       "stale quotes were found",
			requested:  6,
			wantLogged: true,
		},
		{
			name:       "every quote was fresh: nothing to report",
			requested:  0,
			wantLogged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var logs bytes.Buffer

				refresher := &stubRefresher{runs: []run{{requested: tt.requested, err: nil}}}
				stop := start(t, newScheduler(t, refresher, &logs))

				synctest.Wait()
				stop()

				assert.Equal(t, tt.wantLogged, logs.Len() > 0)
			})
		})
	}
}

func TestScheduler_Run_StopsQuietlyWhenContextIsCancelled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer

		refresher := &stubRefresher{}
		s := newScheduler(t, refresher, &logs)

		// Run with a context that is already cancelled: the refresh fails
		// with the cancellation and Run returns at once.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		s.Run(ctx)

		time.Sleep(10 * interval)
		synctest.Wait()

		assert.Len(t, refresher.calls(), 1, "a stopped scheduler must not refresh anything")
		assert.Empty(t, logs.String(), "an error caused by the shutdown must not be logged")
	})
}
