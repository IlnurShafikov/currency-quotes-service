package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

const failureReason = "provider unavailable"

var (
	updateID     = domain.UpdateID(uuid.MustParse(updateIDText))
	quoteID      = domain.QuoteID(uuid.MustParse(quoteIDText))
	otherQuoteID = domain.QuoteID(uuid.MustParse("0199b0c2-9e50-7c32-b18d-5d3f4e607b82"))
	eurMXN       = domain.Pair{Base: "EUR", Quote: "MXN"}
	requestedAt  = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	finishedAt   = requestedAt.Add(2 * time.Second)
	later        = finishedAt.Add(time.Minute)
)

func pendingRequest() domain.UpdateRequest {
	return domain.UpdateRequest{
		ID:          updateID,
		Pair:        eurMXN,
		Status:      domain.UpdatePending,
		QuoteID:     nil,
		Error:       "",
		Attempts:    0,
		RequestedAt: requestedAt,
		StartedAt:   nil,
		FinishedAt:  nil,
	}
}

func completedRequest() domain.UpdateRequest {
	req := pendingRequest()
	req.Status = domain.UpdateCompleted
	req.QuoteID = new(quoteID)
	req.FinishedAt = new(finishedAt)

	return req
}

func failedRequest() domain.UpdateRequest {
	req := pendingRequest()
	req.Status = domain.UpdateFailed
	req.Error = failureReason
	req.FinishedAt = new(finishedAt)

	return req
}

func TestNewUpdateRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		requestedAt time.Time
		want        domain.UpdateRequest
	}{
		{
			name:        "starts as pending with no result",
			requestedAt: requestedAt,
			want:        pendingRequest(),
		},
		{
			name:        "time is normalised to UTC",
			requestedAt: requestedAt.In(time.FixedZone("CST", -6*60*60)),
			want:        pendingRequest(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := domain.NewUpdateRequest(updateID, eurMXN, tt.requestedAt)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUpdateRequest_IsFinished(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  domain.UpdateRequest
		want bool
	}{
		{
			name: "pending",
			req:  pendingRequest(),
			want: false,
		},
		{
			name: "completed",
			req:  completedRequest(),
			want: true,
		},
		{
			name: "failed",
			req:  failedRequest(),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.req.IsFinished())
		})
	}
}

func TestUpdateRequest_Complete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     domain.UpdateRequest
		quoteID domain.QuoteID
		at      time.Time
		want    domain.UpdateRequest
		wantErr error
	}{
		{
			name:    "pending request is completed",
			req:     pendingRequest(),
			quoteID: quoteID,
			at:      finishedAt,
			want:    completedRequest(),
			wantErr: nil,
		},
		{
			name:    "completed request is left unchanged",
			req:     completedRequest(),
			quoteID: otherQuoteID,
			at:      later,
			want:    completedRequest(),
			wantErr: domain.ErrUpdateAlreadyFinished,
		},
		{
			name:    "failed request is left unchanged",
			req:     failedRequest(),
			quoteID: otherQuoteID,
			at:      later,
			want:    failedRequest(),
			wantErr: domain.ErrUpdateAlreadyFinished,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.req.Complete(tt.quoteID, tt.at)
			assert.Equal(t, tt.want, tt.req)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUpdateRequest_Fail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     domain.UpdateRequest
		reason  string
		at      time.Time
		want    domain.UpdateRequest
		wantErr error
	}{
		{
			name:    "pending request is failed",
			req:     pendingRequest(),
			reason:  failureReason,
			at:      finishedAt,
			want:    failedRequest(),
			wantErr: nil,
		},
		{
			name:    "empty reason is rejected",
			req:     pendingRequest(),
			reason:  "",
			at:      finishedAt,
			want:    pendingRequest(),
			wantErr: domain.ErrEmptyFailureReason,
		},
		{
			name:    "completed request is left unchanged",
			req:     completedRequest(),
			reason:  "another reason",
			at:      later,
			want:    completedRequest(),
			wantErr: domain.ErrUpdateAlreadyFinished,
		},
		{
			name:    "failed request is left unchanged",
			req:     failedRequest(),
			reason:  "another reason",
			at:      later,
			want:    failedRequest(),
			wantErr: domain.ErrUpdateAlreadyFinished,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.req.Fail(tt.reason, tt.at)
			assert.Equal(t, tt.want, tt.req)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
