package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// An instance that finds the migration lock taken retries every second for
// up to a minute. The library default is every five seconds, which makes
// instances that start together wait needlessly long.
const (
	lockRetrySeconds = 1
	lockRetries      = 60
)

// Up applies every migration that has not been applied to the database yet.
//
// It holds a PostgreSQL advisory lock while it works, so that several
// instances of the service starting at the same time do not run the
// migrations concurrently: one applies them, the others wait and then find
// nothing left to do.
func Up(ctx context.Context, db *sql.DB) error {
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(lockRetrySeconds, lockRetries))
	if err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}
