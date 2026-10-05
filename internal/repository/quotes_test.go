package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

func TestQuotes_Get(t *testing.T) {
	t.Parallel()

	var (
		mexicoCity = time.FixedZone("CST", -6*60*60)
		precise    = noon.Add(123456 * time.Microsecond)
	)

	tests := []struct {
		name    string
		saved   []domain.Quote
		ctx     func(t *testing.T) context.Context
		id      domain.QuoteID
		want    domain.Quote
		wantErr error
	}{
		{
			name:    "stored quote is returned as it was saved",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "21.4587", noon)},
			ctx:     liveContext,
			id:      firstQuoteID,
			want:    newQuote(firstQuoteID, eurMXN, "21.4587", noon),
			wantErr: nil,
		},
		{
			name:    "smallest representable price keeps its precision",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "0.000000000000000001", noon)},
			ctx:     liveContext,
			id:      firstQuoteID,
			want:    newQuote(firstQuoteID, eurMXN, "0.000000000000000001", noon),
			wantErr: nil,
		},
		{
			name:    "large price keeps its precision",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "12345678901234567890.123456789012345678", noon)},
			ctx:     liveContext,
			id:      firstQuoteID,
			want:    newQuote(firstQuoteID, eurMXN, "12345678901234567890.123456789012345678", noon),
			wantErr: nil,
		},
		{
			name:    "whole price has no trailing zeros",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "21", noon)},
			ctx:     liveContext,
			id:      firstQuoteID,
			want:    newQuote(firstQuoteID, eurMXN, "21", noon),
			wantErr: nil,
		},
		{
			name:    "time is returned in UTC",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "21.4587", noon.In(mexicoCity))},
			ctx:     liveContext,
			id:      firstQuoteID,
			want:    newQuote(firstQuoteID, eurMXN, "21.4587", noon),
			wantErr: nil,
		},
		{
			name:    "time keeps microsecond precision",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "21.4587", precise)},
			ctx:     liveContext,
			id:      firstQuoteID,
			want:    newQuote(firstQuoteID, eurMXN, "21.4587", precise),
			wantErr: nil,
		},
		{
			name: "quote is found among others",
			saved: []domain.Quote{
				newQuote(firstQuoteID, eurMXN, "21.4587", noon),
				newQuote(secondQuoteID, usdMXN, "18.2", noon),
			},
			ctx:     liveContext,
			id:      secondQuoteID,
			want:    newQuote(secondQuoteID, usdMXN, "18.2", noon),
			wantErr: nil,
		},
		{
			name:    "unknown id",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "21.4587", noon)},
			ctx:     liveContext,
			id:      unknownQuote,
			want:    domain.Quote{},
			wantErr: domain.ErrQuoteNotFound,
		},
		{
			name:    "query fails",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "21.4587", noon)},
			ctx:     cancelledContext,
			id:      firstQuoteID,
			want:    domain.Quote{},
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			quotes := NewQuotes(newTestDB(t))
			for _, quote := range tt.saved {
				require.NoError(t, quotes.Save(t.Context(), quote))
			}

			got, err := quotes.Get(tt.ctx(t), tt.id)
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestQuotes_Latest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		saved   []domain.Quote
		ctx     func(t *testing.T) context.Context
		pair    domain.Pair
		want    domain.Quote
		wantErr error
	}{
		{
			name:    "the only quote of the pair",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "21.4587", noon)},
			ctx:     liveContext,
			pair:    eurMXN,
			want:    newQuote(firstQuoteID, eurMXN, "21.4587", noon),
			wantErr: nil,
		},
		{
			name: "the most recently obtained quote wins",
			saved: []domain.Quote{
				newQuote(firstQuoteID, eurMXN, "21.1", noon),
				newQuote(secondQuoteID, eurMXN, "21.3", noon.Add(2*time.Minute)),
				newQuote(thirdQuoteID, eurMXN, "21.2", noon.Add(time.Minute)),
			},
			ctx:     liveContext,
			pair:    eurMXN,
			want:    newQuote(secondQuoteID, eurMXN, "21.3", noon.Add(2*time.Minute)),
			wantErr: nil,
		},
		{
			name: "quotes obtained at the same instant: the greater id wins",
			saved: []domain.Quote{
				newQuote(secondQuoteID, eurMXN, "21.2", noon),
				newQuote(firstQuoteID, eurMXN, "21.1", noon),
			},
			ctx:     liveContext,
			pair:    eurMXN,
			want:    newQuote(secondQuoteID, eurMXN, "21.2", noon),
			wantErr: nil,
		},
		{
			name: "quotes of other pairs are ignored",
			saved: []domain.Quote{
				newQuote(firstQuoteID, eurMXN, "21.4587", noon),
				newQuote(secondQuoteID, usdMXN, "18.2", noon.Add(time.Minute)),
				newQuote(thirdQuoteID, mxnEUR, "0.0466", noon.Add(2*time.Minute)),
			},
			ctx:     liveContext,
			pair:    eurMXN,
			want:    newQuote(firstQuoteID, eurMXN, "21.4587", noon),
			wantErr: nil,
		},
		{
			name:    "pair has no quotes yet",
			saved:   []domain.Quote{newQuote(firstQuoteID, usdMXN, "18.2", noon)},
			ctx:     liveContext,
			pair:    eurMXN,
			want:    domain.Quote{},
			wantErr: domain.ErrQuoteNotFound,
		},
		{
			name:    "no quotes at all",
			saved:   nil,
			ctx:     liveContext,
			pair:    eurMXN,
			want:    domain.Quote{},
			wantErr: domain.ErrQuoteNotFound,
		},
		{
			name:    "query fails",
			saved:   []domain.Quote{newQuote(firstQuoteID, eurMXN, "21.4587", noon)},
			ctx:     cancelledContext,
			pair:    eurMXN,
			want:    domain.Quote{},
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			quotes := NewQuotes(newTestDB(t))
			for _, quote := range tt.saved {
				require.NoError(t, quotes.Save(t.Context(), quote))
			}

			got, err := quotes.Latest(tt.ctx(t), tt.pair)
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestQuotes_Save_TakesPartInTransaction(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	quotes := NewQuotes(db)
	quote := newQuote(firstQuoteID, eurMXN, "21.4587", noon)

	err := db.WithinTx(t.Context(), func(ctx context.Context) error {
		require.NoError(t, quotes.Save(ctx, quote))

		return errAborted
	})
	require.ErrorIs(t, err, errAborted)

	got, err := quotes.Get(t.Context(), quote.ID)
	assert.Equal(t, domain.Quote{}, got)
	require.ErrorIs(t, err, domain.ErrQuoteNotFound)
}

func TestQuotes_Save_RejectsInvalidRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		saved []domain.Quote
		quote domain.Quote
	}{
		{
			name:  "currency is not supported",
			saved: nil,
			quote: newQuote(firstQuoteID, domain.Pair{Base: "EUR", Quote: "JPY"}, "160", noon),
		},
		{
			name:  "id is already taken",
			saved: []domain.Quote{newQuote(firstQuoteID, eurMXN, "21.4587", noon)},
			quote: newQuote(firstQuoteID, usdMXN, "18.2", noon),
		},
		{
			name:  "price is not positive",
			saved: nil,
			quote: newQuote(firstQuoteID, eurMXN, "0", noon),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			quotes := NewQuotes(newTestDB(t))
			for _, quote := range tt.saved {
				require.NoError(t, quotes.Save(t.Context(), quote))
			}

			require.Error(t, quotes.Save(t.Context(), tt.quote))
		})
	}
}
