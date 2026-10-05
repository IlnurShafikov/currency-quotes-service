package service_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

const (
	maxAttempts = 3

	// Failure reasons the processor is expected to store.
	providerFailureReason  = "fetch rate for EUR/MXN: rate provider unavailable"
	exhaustedFailureReason = "attempts exhausted"
)

var (
	otherUpdateID = domain.UpdateID(uuid.MustParse("0199b0c2-5a1c-7e08-9d49-190b0a2c3d4e"))
	claimedAt     = now.Add(time.Second)
	processedAt   = now.Add(5 * time.Second)
)

func processorConfig() service.ProcessorConfig {
	return service.ProcessorConfig{
		BatchSize:   10,
		MaxAttempts: maxAttempts,
		Timeout:     20 * time.Second,
		StaleAfter:  30 * time.Second,
	}
}

// claimedRequest is a pending request as ClaimPending hands it out.
func claimedRequest(id domain.UpdateID, attempts int) domain.UpdateRequest {
	req := pendingRequest(id)
	req.Attempts = attempts
	req.StartedAt = new(claimedAt)

	return req
}

func completedByProcessor(id domain.UpdateID, attempts int) domain.UpdateRequest {
	req := claimedRequest(id, attempts)
	req.Status = domain.UpdateCompleted
	req.QuoteID = new(quoteID)
	req.FinishedAt = new(processedAt)

	return req
}

func failedByProcessor(id domain.UpdateID, attempts int, reason string) domain.UpdateRequest {
	req := claimedRequest(id, attempts)
	req.Status = domain.UpdateFailed
	req.Error = reason
	req.FinishedAt = new(processedAt)

	return req
}

func fetchedQuote() domain.Quote {
	return domain.Quote{
		ID:         quoteID,
		Pair:       eurMXN,
		Price:      price,
		ObtainedAt: processedAt,
	}
}

func TestNewUpdateProcessor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     service.ProcessorConfig
		wantOK  bool
		wantErr error
	}{
		{
			name: "valid config",
			cfg: service.ProcessorConfig{
				BatchSize:   10,
				MaxAttempts: 3,
				Timeout:     20 * time.Second,
				StaleAfter:  30 * time.Second,
			},
			wantOK:  true,
			wantErr: nil,
		},
		{
			name: "zero batch size",
			cfg: service.ProcessorConfig{
				BatchSize:   0,
				MaxAttempts: 3,
				Timeout:     20 * time.Second,
				StaleAfter:  30 * time.Second,
			},
			wantOK:  false,
			wantErr: service.ErrInvalidProcessorConfig,
		},
		{
			name: "zero max attempts",
			cfg: service.ProcessorConfig{
				BatchSize:   10,
				MaxAttempts: 0,
				Timeout:     20 * time.Second,
				StaleAfter:  30 * time.Second,
			},
			wantOK:  false,
			wantErr: service.ErrInvalidProcessorConfig,
		},
		{
			name: "zero timeout",
			cfg: service.ProcessorConfig{
				BatchSize:   10,
				MaxAttempts: 3,
				Timeout:     0,
				StaleAfter:  30 * time.Second,
			},
			wantOK:  false,
			wantErr: service.ErrInvalidProcessorConfig,
		},
		{
			name: "stale-after equal to timeout",
			cfg: service.ProcessorConfig{
				BatchSize:   10,
				MaxAttempts: 3,
				Timeout:     30 * time.Second,
				StaleAfter:  30 * time.Second,
			},
			wantOK:  false,
			wantErr: service.ErrInvalidProcessorConfig,
		},
		{
			name: "stale-after shorter than timeout",
			cfg: service.ProcessorConfig{
				BatchSize:   10,
				MaxAttempts: 3,
				Timeout:     30 * time.Second,
				StaleAfter:  20 * time.Second,
			},
			wantOK:  false,
			wantErr: service.ErrInvalidProcessorConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := service.NewUpdateProcessor(
				&stubUpdates{}, &stubQuotes{}, &stubRates{}, stubTx{}, fixedClock{now: now}, stubIDs{}, tt.cfg,
			)
			assert.Equal(t, tt.wantOK, got != nil)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUpdateProcessor_ProcessPending(t *testing.T) {
	t.Parallel()

	var (
		workingIDs = stubIDs{quoteID: quoteID, quoteErr: nil}
		brokenIDs  = stubIDs{quoteID: domain.QuoteID{}, quoteErr: errIDs}
	)

	tests := []struct {
		name          string
		claimed       []domain.UpdateRequest
		claimErr      error
		rate          decimal.Decimal
		rateErr       error
		ids           stubIDs
		saveErr       error
		finishErr     error
		want          int
		wantRateCalls int
		wantSaved     []saveCall
		wantFinished  []finishCall
		wantErr       error
	}{
		{
			name:          "nothing is pending",
			claimed:       nil,
			claimErr:      nil,
			rate:          price,
			rateErr:       nil,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          0,
			wantRateCalls: 0,
			wantSaved:     nil,
			wantFinished:  nil,
			wantErr:       nil,
		},
		{
			name:          "claiming fails",
			claimed:       nil,
			claimErr:      errStorage,
			rate:          price,
			rateErr:       nil,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          0,
			wantRateCalls: 0,
			wantSaved:     nil,
			wantFinished:  nil,
			wantErr:       errStorage,
		},
		{
			name:          "quote is stored and request completed in one transaction",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, 1)},
			claimErr:      nil,
			rate:          price,
			rateErr:       nil,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          1,
			wantRateCalls: 1,
			wantSaved:     []saveCall{{quote: fetchedQuote(), inTx: true}},
			wantFinished:  []finishCall{{req: completedByProcessor(updateID, 1), inTx: true}},
			wantErr:       nil,
		},
		{
			name: "every request of the batch is processed",
			claimed: []domain.UpdateRequest{
				claimedRequest(updateID, 1),
				claimedRequest(otherUpdateID, 2),
			},
			claimErr:      nil,
			rate:          price,
			rateErr:       nil,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          2,
			wantRateCalls: 2,
			wantSaved: []saveCall{
				{quote: fetchedQuote(), inTx: true},
				{quote: fetchedQuote(), inTx: true},
			},
			wantFinished: []finishCall{
				{req: completedByProcessor(updateID, 1), inTx: true},
				{req: completedByProcessor(otherUpdateID, 2), inTx: true},
			},
			wantErr: nil,
		},
		{
			name:          "provider fails with attempts left: request stays pending",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, maxAttempts-1)},
			claimErr:      nil,
			rate:          decimal.Zero,
			rateErr:       errProvider,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          1,
			wantRateCalls: 1,
			wantSaved:     nil,
			wantFinished:  nil,
			wantErr:       errProvider,
		},
		{
			name:          "provider fails on the last attempt: request is failed",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, maxAttempts)},
			claimErr:      nil,
			rate:          decimal.Zero,
			rateErr:       errProvider,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          1,
			wantRateCalls: 1,
			wantSaved:     nil,
			wantFinished: []finishCall{
				{req: failedByProcessor(updateID, maxAttempts, providerFailureReason), inTx: false},
			},
			wantErr: errProvider,
		},
		{
			name:          "attempts already exhausted: request is failed without calling the provider",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, maxAttempts+1)},
			claimErr:      nil,
			rate:          price,
			rateErr:       nil,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          1,
			wantRateCalls: 0,
			wantSaved:     nil,
			wantFinished: []finishCall{
				{req: failedByProcessor(updateID, maxAttempts+1, exhaustedFailureReason), inTx: false},
			},
			wantErr: service.ErrAttemptsExhausted,
		},
		{
			name:          "provider returns a non-positive rate: treated as a failed attempt",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, 1)},
			claimErr:      nil,
			rate:          decimal.Zero,
			rateErr:       nil,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          1,
			wantRateCalls: 1,
			wantSaved:     nil,
			wantFinished:  nil,
			wantErr:       domain.ErrInvalidPrice,
		},
		{
			name:          "quote id generation fails: treated as a failed attempt",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, 1)},
			claimErr:      nil,
			rate:          price,
			rateErr:       nil,
			ids:           brokenIDs,
			saveErr:       nil,
			finishErr:     nil,
			want:          1,
			wantRateCalls: 1,
			wantSaved:     nil,
			wantFinished:  nil,
			wantErr:       errIDs,
		},
		{
			name:          "saving the quote fails: request is not finished",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, 1)},
			claimErr:      nil,
			rate:          price,
			rateErr:       nil,
			ids:           workingIDs,
			saveErr:       errStorage,
			finishErr:     nil,
			want:          1,
			wantRateCalls: 1,
			wantSaved:     nil,
			wantFinished:  nil,
			wantErr:       errStorage,
		},
		{
			// The stub transaction cannot roll back, so the quote stays
			// recorded here; undoing it is the job of the real Transactor.
			name:          "claim is lost: result is reported as lost",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, 1)},
			claimErr:      nil,
			rate:          price,
			rateErr:       nil,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     service.ErrClaimLost,
			want:          1,
			wantRateCalls: 1,
			wantSaved:     []saveCall{{quote: fetchedQuote(), inTx: true}},
			wantFinished:  nil,
			wantErr:       service.ErrClaimLost,
		},
		{
			name:          "storing a failure fails",
			claimed:       []domain.UpdateRequest{claimedRequest(updateID, maxAttempts)},
			claimErr:      nil,
			rate:          decimal.Zero,
			rateErr:       errProvider,
			ids:           workingIDs,
			saveErr:       nil,
			finishErr:     errStorage,
			want:          1,
			wantRateCalls: 1,
			wantSaved:     nil,
			wantFinished:  nil,
			wantErr:       errStorage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updates := &stubUpdates{claimed: tt.claimed, claimErr: tt.claimErr, finishErr: tt.finishErr}
			quotes := &stubQuotes{saveErr: tt.saveErr}
			rates := &stubRates{rate: tt.rate, err: tt.rateErr}

			processor, err := service.NewUpdateProcessor(
				updates, quotes, rates, stubTx{}, fixedClock{now: processedAt}, tt.ids, processorConfig(),
			)
			require.NoError(t, err)

			got, err := processor.ProcessPending(t.Context())
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantRateCalls, rates.calls)
			assert.ElementsMatch(t, tt.wantSaved, quotes.saved)
			assert.ElementsMatch(t, tt.wantFinished, updates.finished)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUpdateProcessor_ProcessPending_UsesConfig(t *testing.T) {
	t.Parallel()

	cfg := processorConfig()
	updates := &stubUpdates{claimed: []domain.UpdateRequest{claimedRequest(updateID, 1)}}
	rates := &stubRates{rate: price}

	processor, err := service.NewUpdateProcessor(
		updates, &stubQuotes{}, rates, stubTx{}, fixedClock{now: processedAt}, stubIDs{quoteID: quoteID}, cfg,
	)
	require.NoError(t, err)

	_, err = processor.ProcessPending(t.Context())
	require.NoError(t, err)

	assert.Equal(t, cfg.BatchSize, updates.claimLimit)
	assert.Equal(t, cfg.StaleAfter, updates.claimStaleAfter)
	assert.True(t, rates.hadDeadline, "the provider must be called with a deadline")
}
