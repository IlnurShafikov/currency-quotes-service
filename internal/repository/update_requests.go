package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

var _ service.UpdateRequestRepository = (*UpdateRequests)(nil)

// updateRequestColumns lists the columns scanUpdateRequest expects, in order.
const updateRequestColumns = `
	id, base_currency, quote_currency, status, quote_id, error,
	attempts, requested_at, started_at, finished_at`

// UpdateRequests stores update requests in the quote_update_requests table,
// which also serves as the job queue of the background workers.
type UpdateRequests struct {
	db *DB
}

// NewUpdateRequests returns an UpdateRequests repository on top of db.
func NewUpdateRequests(db *DB) *UpdateRequests {
	return &UpdateRequests{db: db}
}

// CreateOrGetPending stores req unless a pending request for the same pair
// already exists, in which case the existing request is returned instead.
//
// It is a single statement, so concurrent calls for one pair cannot both
// insert: the partial unique index on pending requests lets exactly one of
// them win and the others receive the winner's row.
func (r *UpdateRequests) CreateOrGetPending(
	ctx context.Context,
	req domain.UpdateRequest,
) (domain.UpdateRequest, error) {
	// DO UPDATE assigns a column to itself: it changes nothing, but unlike
	// DO NOTHING it makes RETURNING yield the existing row on a conflict.
	const query = `
		INSERT INTO quote_update_requests
			(id, base_currency, quote_currency, status, attempts, requested_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (base_currency, quote_currency) WHERE status = 'pending'
		DO UPDATE SET base_currency = quote_update_requests.base_currency
		RETURNING ` + updateRequestColumns

	row := r.db.conn(ctx).QueryRow(ctx, query,
		uuid.UUID(req.ID),
		req.Pair.Base.String(),
		req.Pair.Quote.String(),
		string(req.Status),
		req.Attempts,
		req.RequestedAt,
	)

	stored, err := scanUpdateRequest(row)
	if err != nil {
		return domain.UpdateRequest{}, fmt.Errorf("create update request for %s: %w", req.Pair, err)
	}

	return stored, nil
}

// Get returns the request with the given id.
// It returns [domain.ErrUpdateNotFound] if there is none.
func (r *UpdateRequests) Get(ctx context.Context, id domain.UpdateID) (domain.UpdateRequest, error) {
	const query = `SELECT ` + updateRequestColumns + ` FROM quote_update_requests WHERE id = $1`

	req, err := scanUpdateRequest(r.db.conn(ctx).QueryRow(ctx, query, uuid.UUID(id)))
	if err != nil {
		return domain.UpdateRequest{}, fmt.Errorf("get update request %s: %w", id, err)
	}

	return req, nil
}

// ClaimPending picks up to limit pending requests, oldest first, marks them
// as started now and increments their attempt counters. A request is
// eligible if it has never been claimed or was last claimed more than
// staleAfter ago.
//
// Concurrent callers never receive the same request: rows being claimed by
// one caller are locked and skipped by the others.
//
// Time is taken from the database clock, so workers on different machines
// agree on what "stale" means regardless of their own clocks.
func (r *UpdateRequests) ClaimPending(
	ctx context.Context,
	limit int,
	staleAfter time.Duration,
) ([]domain.UpdateRequest, error) {
	const query = `
		UPDATE quote_update_requests
		SET started_at = now(), attempts = attempts + 1
		WHERE id IN (
			SELECT id
			FROM quote_update_requests
			WHERE status = 'pending'
			  AND (started_at IS NULL OR started_at < now() - $2::interval)
			ORDER BY requested_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING ` + updateRequestColumns

	rows, err := r.db.conn(ctx).Query(ctx, query, limit, staleAfter)
	if err != nil {
		return nil, fmt.Errorf("claim pending update requests: %w", err)
	}

	claimed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.UpdateRequest, error) {
		return scanUpdateRequest(row)
	})
	if err != nil {
		return nil, fmt.Errorf("claim pending update requests: %w", err)
	}

	return claimed, nil
}

// Finish stores the outcome of a claimed request that has been completed or
// failed.
//
// The attempt counter acts as a fencing token: it is incremented by every
// claim, so the update only matches if nobody has claimed the request since
// the caller did. Otherwise, or if the request is already finished or does
// not exist, nothing is changed and [service.ErrClaimLost] is returned.
func (r *UpdateRequests) Finish(ctx context.Context, req domain.UpdateRequest) error {
	const query = `
		UPDATE quote_update_requests
		SET status = $3, quote_id = $4, error = $5, finished_at = $6
		WHERE id = $1 AND status = 'pending' AND attempts = $2`

	tag, err := r.db.conn(ctx).Exec(ctx, query,
		uuid.UUID(req.ID),
		req.Attempts,
		string(req.Status),
		nullableQuoteID(req.QuoteID),
		nullableText(req.Error),
		req.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("finish update request %s: %w", req.ID, err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("finish update request %s: %w", req.ID, service.ErrClaimLost)
	}

	return nil
}

// scanUpdateRequest reads one row with updateRequestColumns into a request.
// It returns [domain.ErrUpdateNotFound] if the row does not exist.
func scanUpdateRequest(row pgx.Row) (domain.UpdateRequest, error) {
	var (
		id          uuid.UUID
		base        string
		quote       string
		status      string
		quoteID     *uuid.UUID
		failure     *string
		attempts    int
		requestedAt time.Time
		startedAt   *time.Time
		finishedAt  *time.Time
	)

	err := row.Scan(
		&id, &base, &quote, &status, &quoteID, &failure,
		&attempts, &requestedAt, &startedAt, &finishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UpdateRequest{}, domain.ErrUpdateNotFound
	}

	if err != nil {
		return domain.UpdateRequest{}, fmt.Errorf("scan update request: %w", err)
	}

	// The row was validated when it was written, so it is restored as is.
	return domain.UpdateRequest{
		ID:          domain.UpdateID(id),
		Pair:        domain.Pair{Base: domain.Currency(base), Quote: domain.Currency(quote)},
		Status:      domain.UpdateStatus(status),
		QuoteID:     (*domain.QuoteID)(quoteID),
		Error:       textOrEmpty(failure),
		Attempts:    attempts,
		RequestedAt: requestedAt.UTC(),
		StartedAt:   utcOrNil(startedAt),
		FinishedAt:  utcOrNil(finishedAt),
	}, nil
}

// nullableQuoteID converts an optional quote id to its database form.
func nullableQuoteID(id *domain.QuoteID) *uuid.UUID {
	return (*uuid.UUID)(id)
}

// nullableText maps an empty string to NULL: the error column is NULL
// unless the request has failed.
func nullableText(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func textOrEmpty(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	return new(t.UTC())
}
