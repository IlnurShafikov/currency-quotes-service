package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

func TestNewQuote(t *testing.T) {
	t.Parallel()

	var (
		id       = domain.QuoteID(uuid.MustParse(quoteIDText))
		pair     = domain.Pair{Base: "EUR", Quote: "MXN"}
		noonUTC  = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
		noonCST  = time.Date(2026, 10, 4, 6, 0, 0, 0, time.FixedZone("CST", -6*60*60))
		regular  = decimal.RequireFromString("21.4587")
		tiny     = decimal.RequireFromString("0.000000000000000001")
		zero     = decimal.RequireFromString("0")
		negative = decimal.RequireFromString("-1.5")
	)

	tests := []struct {
		name       string
		price      decimal.Decimal
		obtainedAt time.Time
		want       domain.Quote
		wantErr    error
	}{
		{
			name:       "regular price",
			price:      regular,
			obtainedAt: noonUTC,
			want:       domain.Quote{ID: id, Pair: pair, Price: regular, ObtainedAt: noonUTC},
			wantErr:    nil,
		},
		{
			name:       "very small price keeps precision",
			price:      tiny,
			obtainedAt: noonUTC,
			want:       domain.Quote{ID: id, Pair: pair, Price: tiny, ObtainedAt: noonUTC},
			wantErr:    nil,
		},
		{
			name:       "time is normalised to UTC",
			price:      regular,
			obtainedAt: noonCST,
			want:       domain.Quote{ID: id, Pair: pair, Price: regular, ObtainedAt: noonUTC},
			wantErr:    nil,
		},
		{
			name:       "zero price",
			price:      zero,
			obtainedAt: noonUTC,
			want:       domain.Quote{},
			wantErr:    domain.ErrInvalidPrice,
		},
		{
			name:       "negative price",
			price:      negative,
			obtainedAt: noonUTC,
			want:       domain.Quote{},
			wantErr:    domain.ErrInvalidPrice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NewQuote(id, pair, tt.price, tt.obtainedAt)
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
