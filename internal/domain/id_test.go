package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

const (
	updateIDText = "0199b0c2-7c3e-7a10-9f6b-3b1d2c4e5f60"
	quoteIDText  = "0199b0c2-8d4f-7b21-a07c-4c2e3d5f6a71"
)

func TestParseUpdateID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    domain.UpdateID
		wantErr error
	}{
		{
			name:    "canonical uuid",
			input:   updateIDText,
			want:    domain.UpdateID(uuid.MustParse(updateIDText)),
			wantErr: nil,
		},
		{
			name:    "empty",
			input:   "",
			want:    domain.UpdateID{},
			wantErr: domain.ErrInvalidID,
		},
		{
			name:    "not a uuid",
			input:   "42",
			want:    domain.UpdateID{},
			wantErr: domain.ErrInvalidID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseUpdateID(tt.input)
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUpdateID_String(t *testing.T) {
	t.Parallel()

	id := domain.UpdateID(uuid.MustParse(updateIDText))

	assert.Equal(t, updateIDText, id.String())
}

func TestQuoteID_String(t *testing.T) {
	t.Parallel()

	id := domain.QuoteID(uuid.MustParse(quoteIDText))

	assert.Equal(t, quoteIDText, id.String())
}

func TestNewUpdateID_IsUnique(t *testing.T) {
	t.Parallel()

	first, err := domain.NewUpdateID()
	require.NoError(t, err)

	second, err := domain.NewUpdateID()
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestNewQuoteID_IsUnique(t *testing.T) {
	t.Parallel()

	first, err := domain.NewQuoteID()
	require.NoError(t, err)

	second, err := domain.NewQuoteID()
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}
