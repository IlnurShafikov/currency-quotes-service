package domain

import "time"

// UpdateStatus is the lifecycle state of an [UpdateRequest].
type UpdateStatus string

// Lifecycle states of an update request. The only allowed transitions are
// pending -> completed and pending -> failed.
const (
	UpdatePending   UpdateStatus = "pending"
	UpdateCompleted UpdateStatus = "completed"
	UpdateFailed    UpdateStatus = "failed"
)

// UpdateRequest is a request to refresh the quote of a currency pair.
// It is created as pending and is finished exactly once, either with a
// reference to the obtained quote or with a failure reason.
type UpdateRequest struct {
	ID     UpdateID
	Pair   Pair
	Status UpdateStatus
	// QuoteID references the obtained quote; set only when Status is completed.
	QuoteID *QuoteID
	// Error holds the failure reason; set only when Status is failed.
	Error string
	// Attempts is the number of times a worker has picked the request up.
	Attempts    int
	RequestedAt time.Time
	// StartedAt is the time the latest attempt began; nil until first picked up.
	StartedAt *time.Time
	// FinishedAt is the time the request left the pending state.
	FinishedAt *time.Time
}

// NewUpdateRequest returns a pending update request for pair.
func NewUpdateRequest(id UpdateID, pair Pair, requestedAt time.Time) UpdateRequest {
	return UpdateRequest{
		ID:          id,
		Pair:        pair,
		Status:      UpdatePending,
		QuoteID:     nil,
		Error:       "",
		Attempts:    0,
		RequestedAt: requestedAt.UTC(),
		StartedAt:   nil,
		FinishedAt:  nil,
	}
}

// IsFinished reports whether the request has left the pending state.
func (r *UpdateRequest) IsFinished() bool {
	return r.Status != UpdatePending
}

// Complete marks the request as completed with the given quote.
// It returns [ErrUpdateAlreadyFinished] if the request is not pending.
func (r *UpdateRequest) Complete(quoteID QuoteID, at time.Time) error {
	if r.IsFinished() {
		return ErrUpdateAlreadyFinished
	}

	finishedAt := at.UTC()
	r.Status = UpdateCompleted
	r.QuoteID = &quoteID
	r.FinishedAt = &finishedAt

	return nil
}

// Fail marks the request as failed with the given reason.
// It returns [ErrUpdateAlreadyFinished] if the request is not pending and
// [ErrEmptyFailureReason] if reason is empty.
func (r *UpdateRequest) Fail(reason string, at time.Time) error {
	if r.IsFinished() {
		return ErrUpdateAlreadyFinished
	}

	if reason == "" {
		return ErrEmptyFailureReason
	}

	finishedAt := at.UTC()
	r.Status = UpdateFailed
	r.Error = reason
	r.FinishedAt = &finishedAt

	return nil
}
