package database

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is anything that can run a query returning rows. It is
// satisfied by *PostgresDB, *pgxpool.Pool, pgx.Tx and *pgx.Conn, so the
// same QueryAll / QueryOne call works on the pool and inside WithTx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Compile-time proof of the types documented to satisfy Querier.
var (
	_ Querier = (*PostgresDB)(nil)
	_ Querier = (*pgxpool.Pool)(nil)
	_ Querier = (pgx.Tx)(nil)
	_ Querier = (*pgx.Conn)(nil)
)

// QueryAll runs sql on q and scans every row into a T.
//
// When T is a struct, columns are matched to exported fields by name:
// the `db` struct tag when present, otherwise the field name compared
// case-insensitively (underscores ignored, so a user_id column fills a
// UserID field). Every column must map to a field, every field must be
// selected (tag a field `db:"-"` to skip it), and embedded structs are
// flattened. These are pgx.RowToStructByName semantics.
//
// When T is not a struct (int64, string, bool, []byte, ...) or is a type
// the driver scans as a single value (time.Time, sql.Scanner
// implementations such as sql.NullString or pgtype.Numeric), the query
// must return exactly one column, which is scanned into T.
//
// When there are no rows it returns an empty, non-nil slice.
func QueryAll[T any](ctx context.Context, q Querier, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, rowScanner[T]())
}

// QueryOne runs sql on q and scans the first row into a T, using the same
// mapping rules as QueryAll. Further rows are ignored; add LIMIT 1 or a
// unique condition to make the intent explicit. When there is no row it
// returns the zero T and an error that matches both ErrNotFound and
// pgx.ErrNoRows.
func QueryOne[T any](ctx context.Context, q Querier, sql string, args ...any) (T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	value, err := pgx.CollectOneRow(rows, rowScanner[T]())
	if errors.Is(err, pgx.ErrNoRows) {
		return value, notFound(pgx.ErrNoRows)
	}
	return value, err
}

// rowScanner picks the row-to-T function for QueryAll and QueryOne.
func rowScanner[T any]() pgx.RowToFunc[T] {
	if scansAsStruct(reflect.TypeFor[T]()) {
		return pgx.RowToStructByName[T]
	}
	return pgx.RowTo[T]
}

var (
	timeType    = reflect.TypeFor[time.Time]()
	scannerType = reflect.TypeFor[sql.Scanner]()
)

// scansAsStruct reports whether rows should be mapped column-by-field
// into t. Structs the driver already knows how to fill from a single
// column (time.Time and anything implementing sql.Scanner) are scanned
// as one value instead.
func scansAsStruct(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	if t == timeType || reflect.PointerTo(t).Implements(scannerType) {
		return false
	}
	return true
}
