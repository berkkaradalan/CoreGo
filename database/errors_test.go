package database

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestNotFoundMatchesBothSentinels(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		driverErr error
	}{
		{"mongo", notFound(mongo.ErrNoDocuments), mongo.ErrNoDocuments},
		{"postgres", notFound(pgx.ErrNoRows), pgx.ErrNoRows},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, ErrNotFound) {
				t.Fatalf("errors.Is(%v, ErrNotFound) = false", tt.err)
			}
			if !errors.Is(tt.err, tt.driverErr) {
				t.Fatalf("errors.Is(%v, %v) = false", tt.err, tt.driverErr)
			}
			// Wrapping by application code must keep both matches.
			wrapped := fmt.Errorf("load task: %w", tt.err)
			if !errors.Is(wrapped, ErrNotFound) || !errors.Is(wrapped, tt.driverErr) {
				t.Fatalf("wrapped error %v lost a sentinel", wrapped)
			}
		})
	}
}

func TestNotFoundPostgresAlsoMatchesSQLErrNoRows(t *testing.T) {
	if err := notFound(pgx.ErrNoRows); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("errors.Is(%v, sql.ErrNoRows) = false", err)
	}
}

func TestNotFoundDoesNotCrossMatch(t *testing.T) {
	if errors.Is(notFound(mongo.ErrNoDocuments), pgx.ErrNoRows) {
		t.Fatal("a MongoDB not-found error matches pgx.ErrNoRows")
	}
	if errors.Is(notFound(pgx.ErrNoRows), mongo.ErrNoDocuments) {
		t.Fatal("a PostgreSQL not-found error matches mongo.ErrNoDocuments")
	}
	if errors.Is(errors.New("database: not found"), ErrNotFound) {
		t.Fatal("an unrelated error with the same text matches ErrNotFound")
	}
}

func TestNotFoundMessage(t *testing.T) {
	want := "database: not found: mongo: no documents in result"
	if got := notFound(mongo.ErrNoDocuments).Error(); got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
