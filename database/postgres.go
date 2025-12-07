package database

import (
	"context"
	"time"

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
	//pool.Close doesn't return an error
	return nil
}