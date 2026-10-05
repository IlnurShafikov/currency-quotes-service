package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A currency that is not part of the seed data.
const probeCurrency = "JPY"

var errAborted = errors.New("aborted by the caller")

// insertProbe stores the probe currency using whatever ctx carries: a
// transaction or nothing.
func insertProbe(ctx context.Context, t *testing.T, db *DB) {
	t.Helper()

	_, err := db.conn(ctx).Exec(ctx, `INSERT INTO currencies (code) VALUES ($1)`, probeCurrency)
	require.NoError(t, err)
}

// probeStored reports whether the probe currency is visible outside of any
// transaction.
func probeStored(t *testing.T, db *DB) bool {
	t.Helper()

	var stored bool

	err := db.pool.
		QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM currencies WHERE code = $1)`, probeCurrency).
		Scan(&stored)
	require.NoError(t, err)

	return stored
}

func TestDB_WithinTx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fnErr      error
		wantStored bool
		wantErr    error
	}{
		{
			name:       "commits when the function succeeds",
			fnErr:      nil,
			wantStored: true,
			wantErr:    nil,
		},
		{
			name:       "rolls back when the function fails",
			fnErr:      errAborted,
			wantStored: false,
			wantErr:    errAborted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)

			err := db.WithinTx(t.Context(), func(ctx context.Context) error {
				insertProbe(ctx, t, db)

				return tt.fnErr
			})
			assert.Equal(t, tt.wantStored, probeStored(t, db))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestDB_WithinTx_NestedCallJoinsOuterTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		outerErr   error
		wantStored bool
		wantErr    error
	}{
		{
			name:       "inner work is committed with the outer transaction",
			outerErr:   nil,
			wantStored: true,
			wantErr:    nil,
		},
		{
			name:       "inner work is rolled back with the outer transaction",
			outerErr:   errAborted,
			wantStored: false,
			wantErr:    errAborted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)

			err := db.WithinTx(t.Context(), func(ctx context.Context) error {
				innerErr := db.WithinTx(ctx, func(ctx context.Context) error {
					insertProbe(ctx, t, db)

					return nil
				})
				require.NoError(t, innerErr)

				return tt.outerErr
			})
			assert.Equal(t, tt.wantStored, probeStored(t, db))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestDB_WithinTx_ChangesAreInvisibleUntilCommit(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	err := db.WithinTx(t.Context(), func(ctx context.Context) error {
		insertProbe(ctx, t, db)

		//nolint:contextcheck // the check must run outside the transaction carried by ctx
		storedOutside := probeStored(t, db)
		assert.False(t, storedOutside, "an uncommitted row must not be visible outside the transaction")

		return nil
	})
	require.NoError(t, err)
	assert.True(t, probeStored(t, db))
}
