package worker_test

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

	"github.com/IlnurShafikov/currency-quotes-service/internal/worker"
)

const interval = time.Second

var errProcessing = errors.New("update 0199b0c2 failed after 3 attempts: rate provider unavailable")

// batch is the outcome of one ProcessPending call.
type batch struct {
	processed int
	err       error
}

// stubProcessor is a worker.Processor that returns the preset batches one by
// one and reports an idle run once they are used up.
type stubProcessor struct {
	mu      sync.Mutex
	batches []batch
	calls   int
}

func (p *stubProcessor) ProcessPending(context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls++

	if len(p.batches) == 0 {
		return 0, nil
	}

	next := p.batches[0]
	p.batches = p.batches[1:]

	return next.processed, next.err
}

func (p *stubProcessor) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.calls
}

// blockingProcessor is a worker.Processor whose batch runs until release is
// closed. It records whether its context was cancelled in the meantime.
type blockingProcessor struct {
	started  chan struct{}
	release  chan struct{}
	finished chan error
}

func newBlockingProcessor() *blockingProcessor {
	return &blockingProcessor{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan error, 1),
	}
}

func (p *blockingProcessor) ProcessPending(ctx context.Context) (int, error) {
	close(p.started)
	<-p.release

	p.finished <- ctx.Err()

	return 0, nil
}

// start runs the worker in the background and returns a function that stops
// it and waits for Run to return.
func start(t *testing.T, w *worker.Worker) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		w.Run(ctx)
	}()

	return func() {
		cancel()
		<-done
	}
}

func newWorker(t *testing.T, processor worker.Processor, logs *bytes.Buffer) *worker.Worker {
	t.Helper()

	w, err := worker.New(processor, interval, slog.New(slog.NewTextHandler(logs, nil)))
	require.NoError(t, err)

	return w
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
			interval: time.Second,
			wantOK:   true,
			wantErr:  nil,
		},
		{
			name:     "zero interval",
			interval: 0,
			wantOK:   false,
			wantErr:  worker.ErrInvalidInterval,
		},
		{
			name:     "negative interval",
			interval: -time.Second,
			wantOK:   false,
			wantErr:  worker.ErrInvalidInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := worker.New(&stubProcessor{}, tt.interval, slog.New(slog.DiscardHandler))
			assert.Equal(t, tt.wantOK, got != nil)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// The tests below run inside synctest bubbles: time is virtual there, so a
// tick takes no real time and the number of calls at any moment is exact.

func TestWorker_Run_ProcessesAtStartAndOnEveryTick(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		processor := &stubProcessor{}
		stop := start(t, newWorker(t, processor, &bytes.Buffer{}))

		synctest.Wait()
		assert.Equal(t, 1, processor.callCount(), "must not wait for the first tick")

		time.Sleep(interval)
		synctest.Wait()
		assert.Equal(t, 2, processor.callCount())

		time.Sleep(3 * interval)
		synctest.Wait()
		assert.Equal(t, 5, processor.callCount())

		stop()
	})
}

func TestWorker_Run_DrainsBacklogWithoutWaitingForTicks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		processor := &stubProcessor{batches: []batch{
			{processed: 10, err: nil},
			{processed: 10, err: nil},
			{processed: 4, err: nil},
		}}
		stop := start(t, newWorker(t, processor, &bytes.Buffer{}))

		synctest.Wait()
		assert.Equal(t, 4, processor.callCount(), "three busy batches and the idle one that ends the drain")

		stop()
	})
}

func TestWorker_Run_LogsErrorsAndKeepsRunning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer

		processor := &stubProcessor{batches: []batch{{processed: 0, err: errProcessing}}}
		stop := start(t, newWorker(t, processor, &logs))

		synctest.Wait()
		assert.Equal(t, 1, processor.callCount())

		time.Sleep(interval)
		synctest.Wait()
		assert.Equal(t, 2, processor.callCount(), "an error must not stop the worker")

		stop()

		assert.Contains(t, logs.String(), errProcessing.Error())
	})
}

func TestWorker_Run_StopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		processor := &stubProcessor{}
		stop := start(t, newWorker(t, processor, &bytes.Buffer{}))

		synctest.Wait()
		stop()

		time.Sleep(10 * interval)
		synctest.Wait()
		assert.Equal(t, 1, processor.callCount(), "a stopped worker must not process anything")
	})
}

func TestWorker_Run_LetsRunningBatchFinishOnShutdown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		processor := newBlockingProcessor()
		w := newWorker(t, processor, &bytes.Buffer{})

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			w.Run(ctx)
		}()

		<-processor.started
		cancel()
		synctest.Wait()

		select {
		case <-done:
			require.Fail(t, "Run returned while a batch was still running")
		default:
		}

		close(processor.release)
		<-done

		require.NoError(t, <-processor.finished, "the running batch must not see the shutdown as a cancellation")
	})
}
