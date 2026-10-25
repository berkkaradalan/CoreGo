package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"
)

// The integration suites run against real servers and are opt-in: set
// COREGO_TEST_MONGO_URL and/or COREGO_TEST_POSTGRES_URL (see AGENTS.md
// for the docker one-liners). Without the variables they skip, so
// `go test ./database/` passes on machines without any infrastructure.
//
// Every test uses its own randomly named collection or table and drops
// it on cleanup, so the suites can share servers with other test runs.

// mongoTestDatabase is the database the MongoDB suite works in.
const mongoTestDatabase = "corego_database_test"

// mongoTestURL returns COREGO_TEST_MONGO_URL or skips the test.
func mongoTestURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("COREGO_TEST_MONGO_URL")
	if url == "" {
		t.Skip("COREGO_TEST_MONGO_URL not set; skipping MongoDB integration test")
	}
	return url
}

// postgresTestURL returns COREGO_TEST_POSTGRES_URL or skips the test.
func postgresTestURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("COREGO_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("COREGO_TEST_POSTGRES_URL not set; skipping PostgreSQL integration test")
	}
	return url
}

// newTestMongo connects to the test server and closes the client when
// the test ends.
func newTestMongo(t *testing.T) *MongoDB {
	t.Helper()
	db, err := NewMongoDB(MongoConfig{URL: mongoTestURL(t), Database: mongoTestDatabase})
	if err != nil {
		t.Fatalf("NewMongoDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestPostgres connects to the test server and closes the pool when
// the test ends.
func newTestPostgres(t *testing.T) *PostgresDB {
	t.Helper()
	db, err := NewPostgresDB(PostgresConfig{URL: postgresTestURL(t)})
	if err != nil {
		t.Fatalf("NewPostgresDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// uniqueName returns prefix followed by random hex, safe as a MongoDB
// collection name and an unquoted PostgreSQL identifier.
func uniqueName(t *testing.T, prefix string) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random name: %v", err)
	}
	return prefix + hex.EncodeToString(b)
}

// testCollection returns a fresh collection name that is dropped when
// the test ends.
func testCollection(t *testing.T, db *MongoDB) string {
	t.Helper()
	name := uniqueName(t, "corego_dbtest_")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = db.Collection(name).Drop(ctx)
	})
	return name
}

// testTable creates a fresh "tasks"-shaped table and drops it when the
// test ends. It returns the table name.
func testTable(t *testing.T, db *PostgresDB) string {
	t.Helper()
	name := uniqueName(t, "corego_dbtest_")
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE `+name+` (
		id         BIGSERIAL PRIMARY KEY,
		title      TEXT NOT NULL UNIQUE,
		done       BOOLEAN NOT NULL DEFAULT FALSE,
		priority   INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = db.Exec(ctx, `DROP TABLE IF EXISTS `+name)
	})
	return name
}

// canceledContext returns a context that is already canceled.
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
