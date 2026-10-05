package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

func TestCurrencies_AllSupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ctx        func(t *testing.T) context.Context
		currencies []domain.Currency
		want       bool
		wantErr    error
	}{
		{
			name:       "all currencies are seeded",
			ctx:        liveContext,
			currencies: []domain.Currency{"EUR", "MXN"},
			want:       true,
			wantErr:    nil,
		},
		{
			name:       "every seeded currency",
			ctx:        liveContext,
			currencies: []domain.Currency{"USD", "EUR", "MXN"},
			want:       true,
			wantErr:    nil,
		},
		{
			name:       "one currency is not supported",
			ctx:        liveContext,
			currencies: []domain.Currency{"EUR", "JPY"},
			want:       false,
			wantErr:    nil,
		},
		{
			name:       "no currency is supported",
			ctx:        liveContext,
			currencies: []domain.Currency{"JPY", "GBP"},
			want:       false,
			wantErr:    nil,
		},
		{
			name:       "duplicates do not confuse the check",
			ctx:        liveContext,
			currencies: []domain.Currency{"EUR", "EUR"},
			want:       true,
			wantErr:    nil,
		},
		{
			name:       "empty list",
			ctx:        liveContext,
			currencies: nil,
			want:       true,
			wantErr:    nil,
		},
		{
			name:       "query fails",
			ctx:        cancelledContext,
			currencies: []domain.Currency{"EUR", "MXN"},
			want:       false,
			wantErr:    context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			currencies := NewCurrencies(newTestDB(t))

			got, err := currencies.AllSupported(tt.ctx(t), tt.currencies...)
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
