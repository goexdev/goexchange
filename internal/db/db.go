// Package db provides Postgres connection.
package db

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a Postgres pool.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, url)
}
