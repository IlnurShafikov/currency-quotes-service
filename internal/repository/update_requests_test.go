package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

const (
	// staleAfter is the claim lifetime used by the tests.
	staleAfter = 30 * time.Second

	failureReason = "rate provider unavailable"
)

var finishedAt = noon.Add(5 * time.Second)

// pendingUpdate is a request as the service creates it.
func pendingUpdate(id domain.UpdateID, pair domain.Pair, requestedAt time.Time) domain.UpdateRequest {
	return domain.NewUpdateRequest(id, pair, requestedAt)
}

// claimedUpdate is req after it has been claimed the given number of times.
// StartedAt is left empty: it comes from the database clock, so tests
// compare requests with StartedAt cleared.
func claimedUpdate(req domain.UpdateRequest, attempts int) domain.UpdateRequest {
	req.Attempts = attempts

	return req
}

// completedUpdate is req after it has been claimed and completed.
func completedUpdate(req domain.UpdateRequest, attempts int, quoteID domain.QuoteID) domain.UpdateRequest {
	req.Attempts = attempts
	req.Status = domain.UpdateCompleted
	req.QuoteID = new(quoteID)
	req.FinishedAt = new(finishedAt)

	return req
}

// failedUpdate is req after it has been claimed and failed.
func failedUpdate(req domain.UpdateRequest, attempts int) domain.UpdateRequest {
	req.Attempts = attempts
	req.Status = domain.UpdateFailed
	req.Error = failureReason
	req.FinishedAt = new(finishedAt)

	return req
}

func clearStartedAt(req domain.UpdateRequest) domain.UpdateRequest {
	req.StartedAt = nil

	return req
}

func clearAllStartedAt(reqs []domain.UpdateRequest) []domain.UpdateRequest {
	cleared := make([]domain.UpdateRequest, len(reqs))
	for i, req := range reqs {
		cleared[i] = clearStartedAt(req)
	}

	return cleared
}

func mustCreate(t *testing.T, db *DB, reqs ...domain.UpdateRequest) {
	t.Helper()

	for _, req := range reqs {
		_, err := NewUpdateRequests(db).CreateOrGetPending(t.Context(), req)
		require.NoError(t, err)
	}
}

// mustClaimOne claims the only eligible request and returns it.
func mustClaimOne(t *testing.T, db *DB) domain.UpdateRequest {
	t.Helper()

	claimed, err := NewUpdateRequests(db).ClaimPending(t.Context(), 1, staleAfter)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	return claimed[0]
}

// makeStale moves the claim of a request into the past, as if the worker
// that made it had crashed long ago.
func makeStale(t *testing.T, db *DB, id domain.UpdateID) {
	t.Helper()

	_, err := db.pool.Exec(t.Context(),
		`UPDATE quote_update_requests SET started_at = now() - interval '1 hour' WHERE id = $1`, id.String())
	require.NoError(t, err)
}

// mustComplete claims the only eligible request and completes it with a
// newly stored quote.
func mustComplete(t *testing.T, db *DB, quoteID domain.QuoteID) {
	t.Helper()

	req := mustClaimOne(t, db)
	require.NoError(t, NewQuotes(db).Save(t.Context(), newQuote(quoteID, req.Pair, "21.4587", finishedAt)))
	require.NoError(t, req.Complete(quoteID, finishedAt))
	require.NoError(t, NewUpdateRequests(db).Finish(t.Context(), req))
}

// mustFail claims the only eligible request and fails it.
func mustFail(t *testing.T, db *DB) {
	t.Helper()

	req := mustClaimOne(t, db)
	require.NoError(t, req.Fail(failureReason, finishedAt))
	require.NoError(t, NewUpdateRequests(db).Finish(t.Context(), req))
}

// setupFunc brings the storage into the state a test case starts from.
type setupFunc func(t *testing.T, db *DB)

// noSetup leaves the storage as the migrations created it.
func noSetup(t *testing.T, _ *DB) {
	t.Helper()
}

// withPending stores the given requests as pending.
func withPending(reqs ...domain.UpdateRequest) setupFunc {
	return func(t *testing.T, db *DB) {
		t.Helper()
		mustCreate(t, db, reqs...)
	}
}

// withClaimed stores req and has a worker claim it.
func withClaimed(req domain.UpdateRequest) setupFunc {
	return func(t *testing.T, db *DB) {
		t.Helper()
		mustCreate(t, db, req)
		mustClaimOne(t, db)
	}
}

// withCompleted stores req and has a worker complete it.
func withCompleted(req domain.UpdateRequest) setupFunc {
	return func(t *testing.T, db *DB) {
		t.Helper()
		mustCreate(t, db, req)
		mustComplete(t, db, firstQuoteID)
	}
}

// withFailed stores req and has a worker fail it.
func withFailed(req domain.UpdateRequest) setupFunc {
	return func(t *testing.T, db *DB) {
		t.Helper()
		mustCreate(t, db, req)
		mustFail(t, db)
	}
}

// stale moves the claim of the request into the past.
func stale(id domain.UpdateID) setupFunc {
	return func(t *testing.T, db *DB) {
		t.Helper()
		makeStale(t, db, id)
	}
}

// inOrder runs several setups one after another.
func inOrder(steps ...setupFunc) setupFunc {
	return func(t *testing.T, db *DB) {
		t.Helper()

		for _, step := range steps {
			step(t, db)
		}
	}
}

// storedUpdate returns the stored request, or the zero value if there is none.
func storedUpdate(t *testing.T, db *DB, id domain.UpdateID) domain.UpdateRequest {
	t.Helper()

	stored, err := NewUpdateRequests(db).Get(t.Context(), id)
	if err != nil {
		require.ErrorIs(t, err, domain.ErrUpdateNotFound)
	}

	return stored
}

func TestUpdateRequests_CreateOrGetPending(t *testing.T) {
	t.Parallel()

	var (
		existing = pendingUpdate(firstUpdateID, eurMXN, noon)
		incoming = pendingUpdate(secondUpdateID, eurMXN, noon.Add(time.Minute))
	)

	tests := []struct {
		name    string
		ctx     func(t *testing.T) context.Context
		setup   setupFunc
		req     domain.UpdateRequest
		want    domain.UpdateRequest
		wantErr error
	}{
		{
			name:    "first request for the pair is stored",
			ctx:     liveContext,
			setup:   noSetup,
			req:     incoming,
			want:    incoming,
			wantErr: nil,
		},
		{
			name:    "pending request for the pair is returned instead",
			ctx:     liveContext,
			setup:   withPending(existing),
			req:     incoming,
			want:    existing,
			wantErr: nil,
		},
		{
			name:    "claimed but unfinished request is returned instead",
			ctx:     liveContext,
			setup:   withClaimed(existing),
			req:     incoming,
			want:    claimedUpdate(existing, 1),
			wantErr: nil,
		},
		{
			name:    "pending request for the reverse pair does not count",
			ctx:     liveContext,
			setup:   withPending(pendingUpdate(firstUpdateID, mxnEUR, noon)),
			req:     incoming,
			want:    incoming,
			wantErr: nil,
		},
		{
			name:    "pending request for another pair does not count",
			ctx:     liveContext,
			setup:   withPending(pendingUpdate(firstUpdateID, usdMXN, noon)),
			req:     incoming,
			want:    incoming,
			wantErr: nil,
		},
		{
			name:    "completed request for the pair does not count",
			ctx:     liveContext,
			setup:   withCompleted(existing),
			req:     incoming,
			want:    incoming,
			wantErr: nil,
		},
		{
			name:    "failed request for the pair does not count",
			ctx:     liveContext,
			setup:   withFailed(existing),
			req:     incoming,
			want:    incoming,
			wantErr: nil,
		},
		{
			name:    "query fails",
			ctx:     cancelledContext,
			setup:   noSetup,
			req:     incoming,
			want:    domain.UpdateRequest{},
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)
			tt.setup(t, db)

			got, err := NewUpdateRequests(db).CreateOrGetPending(tt.ctx(t), tt.req)
			assert.Equal(t, tt.want, clearStartedAt(got))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUpdateRequests_CreateOrGetPending_ConcurrentCallsShareOneRequest(t *testing.T) {
	t.Parallel()

	const callers = 8

	db := newTestDB(t)
	updates := NewUpdateRequests(db)

	ids := make([]domain.UpdateID, callers)
	errs := make([]error, callers)

	var wg sync.WaitGroup

	for i := range callers {
		wg.Go(func() {
			newID, err := domain.NewUpdateID()
			if err != nil {
				errs[i] = err

				return
			}

			stored, err := updates.CreateOrGetPending(t.Context(), pendingUpdate(newID, eurMXN, noon))
			ids[i], errs[i] = stored.ID, err
		})
	}

	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
		assert.Equal(t, ids[0], ids[i], "every caller must receive the same request")
	}
}

func TestUpdateRequests_CreateOrGetPending_RejectsUnsupportedCurrency(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	req := pendingUpdate(firstUpdateID, domain.Pair{Base: "EUR", Quote: "JPY"}, noon)

	_, err := NewUpdateRequests(db).CreateOrGetPending(t.Context(), req)
	require.Error(t, err)
}

func TestUpdateRequests_Get(t *testing.T) {
	t.Parallel()

	request := pendingUpdate(firstUpdateID, eurMXN, noon)

	tests := []struct {
		name    string
		ctx     func(t *testing.T) context.Context
		setup   setupFunc
		id      domain.UpdateID
		want    domain.UpdateRequest
		wantErr error
	}{
		{
			name:    "pending request",
			ctx:     liveContext,
			setup:   withPending(request),
			id:      firstUpdateID,
			want:    request,
			wantErr: nil,
		},
		{
			name:    "claimed request",
			ctx:     liveContext,
			setup:   withClaimed(request),
			id:      firstUpdateID,
			want:    claimedUpdate(request, 1),
			wantErr: nil,
		},
		{
			name:    "completed request",
			ctx:     liveContext,
			setup:   withCompleted(request),
			id:      firstUpdateID,
			want:    completedUpdate(request, 1, firstQuoteID),
			wantErr: nil,
		},
		{
			name:    "failed request",
			ctx:     liveContext,
			setup:   withFailed(request),
			id:      firstUpdateID,
			want:    failedUpdate(request, 1),
			wantErr: nil,
		},
		{
			name:    "unknown id",
			ctx:     liveContext,
			setup:   withPending(request),
			id:      secondUpdateID,
			want:    domain.UpdateRequest{},
			wantErr: domain.ErrUpdateNotFound,
		},
		{
			name:    "query fails",
			ctx:     cancelledContext,
			setup:   withPending(request),
			id:      firstUpdateID,
			want:    domain.UpdateRequest{},
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)
			tt.setup(t, db)

			got, err := NewUpdateRequests(db).Get(tt.ctx(t), tt.id)
			assert.Equal(t, tt.want, clearStartedAt(got))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUpdateRequests_ClaimPending(t *testing.T) {
	t.Parallel()

	var (
		oldest = pendingUpdate(firstUpdateID, eurMXN, noon)
		middle = pendingUpdate(secondUpdateID, usdMXN, noon.Add(time.Minute))
		newest = pendingUpdate(thirdUpdateID, mxnEUR, noon.Add(2*time.Minute))
	)

	tests := []struct {
		name    string
		ctx     func(t *testing.T) context.Context
		setup   setupFunc
		limit   int
		want    []domain.UpdateRequest
		wantErr error
	}{
		{
			name:    "nothing is pending",
			ctx:     liveContext,
			setup:   noSetup,
			limit:   10,
			want:    nil,
			wantErr: nil,
		},
		{
			name:    "never claimed request is claimed",
			ctx:     liveContext,
			setup:   withPending(oldest),
			limit:   10,
			want:    []domain.UpdateRequest{claimedUpdate(oldest, 1)},
			wantErr: nil,
		},
		{
			name:    "recently claimed request is left to its worker",
			ctx:     liveContext,
			setup:   withClaimed(oldest),
			limit:   10,
			want:    nil,
			wantErr: nil,
		},
		{
			name:    "request with a stale claim is claimed again",
			ctx:     liveContext,
			setup:   inOrder(withClaimed(oldest), stale(oldest.ID)),
			limit:   10,
			want:    []domain.UpdateRequest{claimedUpdate(oldest, 2)},
			wantErr: nil,
		},
		{
			name:    "completed request is not claimed",
			ctx:     liveContext,
			setup:   inOrder(withCompleted(oldest), stale(oldest.ID)),
			limit:   10,
			want:    nil,
			wantErr: nil,
		},
		{
			name:    "failed request is not claimed",
			ctx:     liveContext,
			setup:   inOrder(withFailed(oldest), stale(oldest.ID)),
			limit:   10,
			want:    nil,
			wantErr: nil,
		},
		{
			name:    "every pending request is claimed when the limit allows",
			ctx:     liveContext,
			setup:   withPending(newest, oldest, middle),
			limit:   10,
			want:    []domain.UpdateRequest{claimedUpdate(oldest, 1), claimedUpdate(middle, 1), claimedUpdate(newest, 1)},
			wantErr: nil,
		},
		{
			name:    "the oldest requests are claimed first when the limit is reached",
			ctx:     liveContext,
			setup:   withPending(newest, oldest, middle),
			limit:   2,
			want:    []domain.UpdateRequest{claimedUpdate(oldest, 1), claimedUpdate(middle, 1)},
			wantErr: nil,
		},
		{
			name:    "query fails",
			ctx:     cancelledContext,
			setup:   withPending(oldest),
			limit:   10,
			want:    nil,
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)
			tt.setup(t, db)

			got, err := NewUpdateRequests(db).ClaimPending(tt.ctx(t), tt.limit, staleAfter)
			assert.ElementsMatch(t, tt.want, clearAllStartedAt(got))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUpdateRequests_ClaimPending_StampsStartTime(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	mustCreate(t, db, pendingUpdate(firstUpdateID, eurMXN, noon))

	claimed := mustClaimOne(t, db)
	stored := storedUpdate(t, db, firstUpdateID)

	require.NotNil(t, claimed.StartedAt)
	assert.WithinDuration(t, time.Now(), *claimed.StartedAt, time.Minute)
	assert.Equal(t, time.UTC, claimed.StartedAt.Location())
	assert.Equal(t, claimed, stored, "the claim must be stored, not only returned")
}

func TestUpdateRequests_ClaimPending_ConcurrentWorkersGetDistinctRequests(t *testing.T) {
	t.Parallel()

	// Every ordered pair of the three seeded currencies: the most requests
	// that can be pending at once.
	pairs := []domain.Pair{
		{Base: "EUR", Quote: "MXN"},
		{Base: "MXN", Quote: "EUR"},
		{Base: "EUR", Quote: "USD"},
		{Base: "USD", Quote: "EUR"},
		{Base: "USD", Quote: "MXN"},
		{Base: "MXN", Quote: "USD"},
	}

	const workers = 6

	db := newTestDB(t)
	updates := NewUpdateRequests(db)

	for _, pair := range pairs {
		id, err := domain.NewUpdateID()
		require.NoError(t, err)
		mustCreate(t, db, pendingUpdate(id, pair, noon))
	}

	batches := make([][]domain.UpdateRequest, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup

	for i := range workers {
		wg.Go(func() {
			batches[i], errs[i] = updates.ClaimPending(t.Context(), len(pairs), staleAfter)
		})
	}

	wg.Wait()

	claimedBy := make(map[domain.UpdateID]int, len(pairs))

	for i := range workers {
		require.NoError(t, errs[i])

		for _, req := range batches[i] {
			claimedBy[req.ID]++
			assert.Equal(t, 1, req.Attempts)
		}
	}

	assert.Len(t, claimedBy, len(pairs), "every request must be claimed")

	for id, times := range claimedBy {
		assert.Equal(t, 1, times, "request %s must be claimed by exactly one worker", id)
	}
}

func TestUpdateRequests_Finish(t *testing.T) {
	t.Parallel()

	request := pendingUpdate(firstUpdateID, eurMXN, noon)

	// Each setup prepares the storage and returns the request to finish.
	var (
		completed = func(t *testing.T, db *DB) domain.UpdateRequest {
			t.Helper()
			mustCreate(t, db, request)

			req := mustClaimOne(t, db)
			require.NoError(t, NewQuotes(db).Save(t.Context(), newQuote(firstQuoteID, eurMXN, "21.4587", finishedAt)))
			require.NoError(t, req.Complete(firstQuoteID, finishedAt))

			return req
		}
		failed = func(t *testing.T, db *DB) domain.UpdateRequest {
			t.Helper()
			mustCreate(t, db, request)

			req := mustClaimOne(t, db)
			require.NoError(t, req.Fail(failureReason, finishedAt))

			return req
		}
		takenOver = func(t *testing.T, db *DB) domain.UpdateRequest {
			t.Helper()

			req := failed(t, db)
			makeStale(t, db, req.ID)
			mustClaimOne(t, db) // another worker takes the request over

			return req
		}
		alreadyFinished = func(t *testing.T, db *DB) domain.UpdateRequest {
			t.Helper()

			req := failed(t, db)
			require.NoError(t, NewUpdateRequests(db).Finish(t.Context(), req))

			return req
		}
		neverStored = func(t *testing.T, _ *DB) domain.UpdateRequest {
			t.Helper()

			req := claimedUpdate(request, 1)
			require.NoError(t, req.Fail(failureReason, finishedAt))

			return req
		}
	)

	tests := []struct {
		name       string
		ctx        func(t *testing.T) context.Context
		setup      func(t *testing.T, db *DB) domain.UpdateRequest
		wantStored domain.UpdateRequest
		wantErr    error
	}{
		{
			name:       "completed request is stored with its quote",
			ctx:        liveContext,
			setup:      completed,
			wantStored: completedUpdate(request, 1, firstQuoteID),
			wantErr:    nil,
		},
		{
			name:       "failed request is stored with its reason",
			ctx:        liveContext,
			setup:      failed,
			wantStored: failedUpdate(request, 1),
			wantErr:    nil,
		},
		{
			name:       "request taken over by another worker is left to it",
			ctx:        liveContext,
			setup:      takenOver,
			wantStored: claimedUpdate(request, 2),
			wantErr:    service.ErrClaimLost,
		},
		{
			name:       "finished request is not changed",
			ctx:        liveContext,
			setup:      alreadyFinished,
			wantStored: failedUpdate(request, 1),
			wantErr:    service.ErrClaimLost,
		},
		{
			name:       "unknown request",
			ctx:        liveContext,
			setup:      neverStored,
			wantStored: domain.UpdateRequest{},
			wantErr:    service.ErrClaimLost,
		},
		{
			name:       "query fails",
			ctx:        cancelledContext,
			setup:      failed,
			wantStored: claimedUpdate(request, 1),
			wantErr:    context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)
			req := tt.setup(t, db)

			err := NewUpdateRequests(db).Finish(tt.ctx(t), req)
			assert.Equal(t, tt.wantStored, clearStartedAt(storedUpdate(t, db, firstUpdateID)))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// When the claim is lost, the quote saved in the same transaction must be
// rolled back: this is what keeps a late worker from leaving a stray quote.
func TestUpdateRequests_Finish_LostClaimRollsBackTheQuote(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	updates := NewUpdateRequests(db)
	quotes := NewQuotes(db)

	mustCreate(t, db, pendingUpdate(firstUpdateID, eurMXN, noon))

	late := mustClaimOne(t, db)
	makeStale(t, db, late.ID)
	mustClaimOne(t, db) // another worker takes the request over

	quote := newQuote(firstQuoteID, eurMXN, "21.4587", finishedAt)
	require.NoError(t, late.Complete(quote.ID, finishedAt))

	err := db.WithinTx(t.Context(), func(ctx context.Context) error {
		require.NoError(t, quotes.Save(ctx, quote))

		return updates.Finish(ctx, late)
	})
	require.ErrorIs(t, err, service.ErrClaimLost)

	_, err = quotes.Get(t.Context(), quote.ID)
	require.ErrorIs(t, err, domain.ErrQuoteNotFound)
}
