package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/migrations"
)

// testDSNEnv names the environment variable with the connection string of a
// PostgreSQL server the integration tests may use.
const testDSNEnv = "TEST_DATABASE_URL"

// newTestDB returns a DB connected to a fresh schema with all migrations
// applied. Every test gets its own schema, so tests can run in parallel and
// never see each other's rows. The schema is dropped when the test ends.
//
// The test is skipped if TEST_DATABASE_URL is not set.
func newTestDB(t *testing.T) *DB {
	t.Helper()

	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set, skipping integration test", testDSNEnv)
	}

	ctx := t.Context()
	schema := pgx.Identifier{"test_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()

	admin, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)

	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)

	t.Cleanup(func() {
		// t.Context is already cancelled when cleanups run.
		cleanupCtx := context.Background()

		_, dropErr := admin.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, dropErr)
		require.NoError(t, admin.Close(cleanupCtx))
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)

	cfg.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	migrate(t, pool)

	return NewDB(pool)
}

// migrate applies all migrations to the schema the pool is bound to.
func migrate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	require.NoError(t, err)

	_, err = provider.Up(t.Context())
	require.NoError(t, err)
}
