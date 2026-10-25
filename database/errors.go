package database

import (
	"errors"
	"fmt"
)

// ErrNotFound reports that a lookup, update or delete matched nothing.
//
// Errors returned by this package for that situation match ErrNotFound
// with errors.Is. They also match the underlying driver sentinel:
// mongo.ErrNoDocuments for MongoDB helpers and pgx.ErrNoRows (and
// therefore sql.ErrNoRows) for PostgreSQL helpers, so code written
// against either driver keeps working.
var ErrNotFound = errors.New("database: not found")

// notFound returns an error that matches both ErrNotFound and the given
// driver sentinel.
func notFound(driverErr error) error {
	return fmt.Errorf("%w: %w", ErrNotFound, driverErr)
}
