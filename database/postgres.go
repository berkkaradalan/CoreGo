package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDB struct {
	pool   *pgxpool.Pool
	config *PostgresConfig
}

func NewPostgresDB(config *PostgresConfig) (*PostgresDB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, config.URL)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	return &PostgresDB{
		pool:   pool,
		config: config,
	}, nil
}

func (p *PostgresDB) GetPool() *pgxpool.Pool {
	return p.pool
}

func (p *PostgresDB) Disconnect() error {
	p.pool.Close()
	// pool.Close doesn't return an error
	return nil
}

// Query executes any SQL and returns results as []map[string]any
// Works for SELECT, INSERT...RETURNING, UPDATE...RETURNING, etc.
func (p *PostgresDB) Query(sql string, args ...any) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return rowsToMaps(rows)
}

// Exec executes SQL without returning rows (INSERT, UPDATE, DELETE)
// Returns number of affected rows
func (p *PostgresDB) Exec(sql string, args ...any) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := p.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// Helper method
func rowsToMaps(rows pgx.Rows) ([]map[string]any, error) {
	results := make([]map[string]any, 0)
	fields := rows.FieldDescriptions()

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}

		row := make(map[string]any)
		for i, fd := range fields {
			row[string(fd.Name)] = values[i]
		}
		results = append(results, row)
	}

	return results, rows.Err()
}