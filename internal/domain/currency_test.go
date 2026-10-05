package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

func TestNewCurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		code    string
		want    domain.Currency
		wantErr error
	}{
		{
			name:    "ISO code",
			code:    "EUR",
			want:    "EUR",
			wantErr: nil,
		},
		{
			name:    "lower case is normalised",
			code:    "mxn",
			want:    "MXN",
			wantErr: nil,
		},
		{
			name:    "mixed case is normalised",
			code:    "Usd",
			want:    "USD",
			wantErr: nil,
		},
		{
			name:    "empty",
			code:    "",
			want:    "",
			wantErr: domain.ErrInvalidCurrency,
		},
		{
			name:    "too short",
			code:    "EU",
			want:    "",
			wantErr: domain.ErrInvalidCurrency,
		},
		{
			name:    "too long",
			code:    "EURO",
			want:    "",
			wantErr: domain.ErrInvalidCurrency,
		},
		{
			name:    "well-formed but unknown",
			code:    "XYZ",
			want:    "",
			wantErr: domain.ErrInvalidCurrency,
		},
		{
			name:    "crypto ticker is not ISO",
			code:    "BTC",
			want:    "",
			wantErr: domain.ErrInvalidCurrency,
		},
		{
			name:    "contains digit",
			code:    "U5D",
			want:    "",
			wantErr: domain.ErrInvalidCurrency,
		},
		{
			name:    "contains space",
			code:    "EU ",
			want:    "",
			wantErr: domain.ErrInvalidCurrency,
		},
		{
			name:    "non-ASCII letters",
			code:    "ЕВР",
			want:    "",
			wantErr: domain.ErrInvalidCurrency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NewCurrency(tt.code)
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
