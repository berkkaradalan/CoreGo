package database

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The documented Querier implementations (*PostgresDB, *pgxpool.Pool,
// pgx.Tx, *pgx.Conn) are asserted at compile time in query.go, so the
// package does not build if one of them stops satisfying the interface.

type scanUser struct {
	ID   int64
	Name string `db:"name"`
}

type scanTimestamp time.Time

func TestScansAsStruct(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"plain struct", reflect.TypeFor[scanUser](), true},
		{"anonymous struct", reflect.TypeFor[struct{ ID int }](), true},
		{"struct defined from time.Time", reflect.TypeFor[scanTimestamp](), true},
		{"int64", reflect.TypeFor[int64](), false},
		{"string", reflect.TypeFor[string](), false},
		{"bytes", reflect.TypeFor[[]byte](), false},
		{"map", reflect.TypeFor[map[string]any](), false},
		{"pointer to struct", reflect.TypeFor[*scanUser](), false},
		{"time.Time", reflect.TypeFor[time.Time](), false},
		{"sql.NullString", reflect.TypeFor[sql.NullString](), false},
		{"pgtype.Numeric", reflect.TypeFor[pgtype.Numeric](), false},
		{"pgtype.Timestamptz", reflect.TypeFor[pgtype.Timestamptz](), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scansAsStruct(tt.typ); got != tt.want {
				t.Fatalf("scansAsStruct(%v) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

// failingQuerier returns the same error for every query.
type failingQuerier struct{ err error }

func (f failingQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, f.err
}

func TestQueryHelpersPropagateQueryErrors(t *testing.T) {
	boom := errors.New("boom")
	q := failingQuerier{err: boom}

	all, err := QueryAll[scanUser](t.Context(), q, "SELECT 1")
	if !errors.Is(err, boom) || all != nil {
		t.Fatalf("QueryAll = (%v, %v), want (nil, boom)", all, err)
	}
	one, err := QueryOne[scanUser](t.Context(), q, "SELECT 1")
	if !errors.Is(err, boom) || one != (scanUser{}) {
		t.Fatalf("QueryOne = (%v, %v), want (zero, boom)", one, err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("a query failure must not look like not found")
	}
}
