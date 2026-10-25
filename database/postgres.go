package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresDB is a connected PostgreSQL connection pool. It is safe for
// concurrent use.
//
// Every helper runs with the caller's context, so cancellation and
// deadlines (a canceled HTTP request, a per-query budget) reach the
// server; there is no hidden per-operation timeout. Always pass query
// values as arguments ($1, $2, ...), never by formatting them into the
// SQL string.
type PostgresDB struct {
	pool *pgxpool.Pool
}

// NewPostgresDB creates a connection pool, pings the server and returns
// the handle. Creating the pool and pinging share a 10 second budget.
// When the ping fails the pool is closed before the error is returned,
// so nothing leaks. An empty URL is an error.
func NewPostgresDB(cfg PostgresConfig) (*PostgresDB, error) {
	return newPostgresDB(cfg, connectTimeout)
}

// newPostgresDB is NewPostgresDB with an adjustable budget for tests.
func newPostgresDB(cfg PostgresConfig, timeout time.Duration) (*PostgresDB, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errEmptyPostgresURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// pgx redacts the password in its parse errors.
	pool, err := pgxpool.New(ctx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("database: connect to PostgreSQL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping PostgreSQL: %w", err)
	}
	return &PostgresDB{pool: pool}, nil
}

// Pool returns the underlying pgx connection pool: the escape hatch for
// anything the helpers do not cover (CopyFrom, SendBatch, listen/notify,
// acquiring a dedicated connection).
func (p *PostgresDB) Pool() *pgxpool.Pool {
	return p.pool
}

// Ping checks that the server is reachable.
func (p *PostgresDB) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Close closes every connection in the pool, waiting for acquired
// connections to be released. It is safe to call on a nil *PostgresDB
// and more than once. It always returns nil; the error result keeps the
// signature symmetric with MongoDB.Close and io.Closer.
func (p *PostgresDB) Close() error {
	if p == nil || p.pool == nil {
		return nil
	}
	p.pool.Close()
	return nil
}

// Exec runs a statement that returns no rows (INSERT, UPDATE, DELETE,
// DDL) and returns the number of affected rows.
func (p *PostgresDB) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := p.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Query runs a statement that returns rows and hands back the raw
// pgx.Rows. The caller must Close them (or consume them with a pgx
// collect function). Most code is better served by QueryAll and
// QueryOne, which take a *PostgresDB because it satisfies Querier.
func (p *PostgresDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryMaps runs a statement that returns rows and returns them as maps
// keyed by column name, for ad-hoc queries where declaring a struct is
// not worth it. When there are no rows it returns an empty, non-nil
// slice.
func (p *PostgresDB) QueryMaps(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToMap)
}

// WithTx runs fn inside a transaction. The transaction is committed when
// fn returns nil and rolled back when fn returns an error (which WithTx
// returns unchanged, joined with the rollback error should rolling back
// fail) or panics (the panic is re-raised after the rollback).
//
// Use tx for every statement that belongs to the transaction; tx
// satisfies Querier, so QueryAll and QueryOne work inside fn:
//
//	err := db.WithTx(ctx, func(tx pgx.Tx) error {
//		if _, err := tx.Exec(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", amount, from); err != nil {
//			return err
//		}
//		_, err := tx.Exec(ctx, "UPDATE accounts SET balance = balance + $1 WHERE id = $2", amount, to)
//		return err
//	})
func (p *PostgresDB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("database: begin transaction: %w", err)
	}

	// The rollback must run even when ctx is already canceled, otherwise
	// the connection would go back to the pool mid-transaction.
	rollback := func() error {
		return tx.Rollback(context.WithoutCancel(ctx))
	}

	panicked := true
	defer func() {
		if panicked {
			_ = rollback()
		}
	}()

	fnErr := fn(tx)
	panicked = false

	if fnErr != nil {
		if rbErr := rollback(); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(fnErr, fmt.Errorf("database: roll back transaction: %w", rbErr))
		}
		return fnErr
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database: commit transaction: %w", err)
	}
	return nil
}
