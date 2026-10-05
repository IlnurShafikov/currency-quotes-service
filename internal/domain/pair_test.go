package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

func TestNewPair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    domain.Currency
		quote   domain.Currency
		want    domain.Pair
		wantErr error
	}{
		{
			name:    "distinct currencies",
			base:    "EUR",
			quote:   "MXN",
			want:    domain.Pair{Base: "EUR", Quote: "MXN"},
			wantErr: nil,
		},
		{
			name:    "reverse pair is a different pair",
			base:    "MXN",
			quote:   "EUR",
			want:    domain.Pair{Base: "MXN", Quote: "EUR"},
			wantErr: nil,
		},
		{
			name:    "same currency",
			base:    "EUR",
			quote:   "EUR",
			want:    domain.Pair{Base: "", Quote: ""},
			wantErr: domain.ErrInvalidPair,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NewPair(tt.base, tt.quote)
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestPair_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pair domain.Pair
		want string
	}{
		{
			name: "base comes first",
			pair: domain.Pair{Base: "EUR", Quote: "MXN"},
			want: "EUR/MXN",
		},
		{
			name: "reverse pair",
			pair: domain.Pair{Base: "MXN", Quote: "EUR"},
			want: "MXN/EUR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.pair.String())
		})
	}
}
