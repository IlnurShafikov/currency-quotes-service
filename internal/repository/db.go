// Package repository is the PostgreSQL adapter of the service. It implements
// the storage ports declared by the service package.
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

var _ service.Transactor = (*DB)(nil)

// querier is the part of pgx that a connection pool and a transaction have
// in common. Repositories run their statements through it and therefore do
// not care whether they are inside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// txKey is the context key under which WithinTx stores the transaction.
type txKey struct{}

// DB gives repositories access to PostgreSQL and runs transactions.
//
// DB is safe for concurrent use.
type DB struct {
	pool *pgxpool.Pool
}

// NewDB returns a DB on top of pool. The caller keeps ownership of the pool
// and is responsible for closing it.
func NewDB(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool}
}

// WithinTx calls fn with a context that carries a transaction. Repository
// calls made with that context are committed together if fn returns nil and
// rolled back together if it returns an error or panics.
//
// If ctx already carries a transaction, fn joins it instead of starting a
// new one; the outermost WithinTx call decides whether to commit.
func (db *DB) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	err := pgx.BeginFunc(ctx, db.pool, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
	if err != nil {
		return fmt.Errorf("transaction: %w", err)
	}

	return nil
}

// conn returns the transaction carried by ctx or, if there is none, the pool.
func (db *DB) conn(ctx context.Context) querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}

	return db.pool
}
