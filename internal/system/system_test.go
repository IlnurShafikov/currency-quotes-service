package system_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/system"
)

func TestClock_Now(t *testing.T) {
	t.Parallel()

	got := system.Clock{}.Now()

	assert.WithinDuration(t, time.Now(), got, time.Second)
	assert.Equal(t, time.UTC, got.Location())
	assert.Equal(t, got, got.Truncate(time.Microsecond), "time must not be more precise than a microsecond")
}

func TestIDs_NewUpdateID(t *testing.T) {
	t.Parallel()

	first, err := system.IDs{}.NewUpdateID()
	require.NoError(t, err)

	second, err := system.IDs{}.NewUpdateID()
	require.NoError(t, err)

	assert.NotEqual(t, domain.UpdateID{}, first)
	assert.NotEqual(t, first, second)
}

func TestIDs_NewQuoteID(t *testing.T) {
	t.Parallel()

	first, err := system.IDs{}.NewQuoteID()
	require.NoError(t, err)

	second, err := system.IDs{}.NewQuoteID()
	require.NoError(t, err)

	assert.NotEqual(t, domain.QuoteID{}, first)
	assert.NotEqual(t, first, second)
}
