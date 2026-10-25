package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/berkkaradalan/CoreGo/auth"
)

// The integration tests run against the PostgreSQL server named by
// COREGO_TEST_POSTGRES_URL and are skipped when it is unset, so a plain
// `go test ./auth/pgstore/` needs no infrastructure.
//
// Every test gets its own freshly created schema (dropped again on
// cleanup) and a pool whose search_path points at it, so tests run in
// parallel, can use the default table prefix, and never touch tables
// outside their schema.
const postgresURLVariable = "COREGO_TEST_POSTGRES_URL"

// integrationTimeout bounds every integration test.
const integrationTimeout = time.Minute

// postgresURL returns the test server URL or skips the test.
func postgresURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(postgresURLVariable)
	if url == "" {
		t.Skip("set " + postgresURLVariable + " to a PostgreSQL URL to run integration tests")
	}
	return url
}

// testContext returns a context bounded by integrationTimeout.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	t.Cleanup(cancel)
	return ctx
}

// randomHex returns n random bytes hex encoded.
func randomHex(t *testing.T, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	return hex.EncodeToString(buf)
}

// newSchemaPool creates a uniquely named schema and returns a pool
// whose connections use it as their only search_path entry. The schema
// and everything in it are dropped when the test ends.
func newSchemaPool(t *testing.T, maxConns int32) *pgxpool.Pool {
	t.Helper()
	url := postgresURL(t)
	ctx := testContext(t)

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(admin.Close)

	// Generated from hex only, so it is safe to put into DDL.
	schema := "pgstore_test_" + randomHex(t, 6)
	if _, err := admin.Exec(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.MaxConns = maxConns
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect with search_path: %v", err)
	}
	// Registered after the schema cleanup, so it runs before it.
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

// migratedStore returns a migrated Store on pool with the given prefix.
func migratedStore(t *testing.T, pool *pgxpool.Pool, prefix string) *Store {
	t.Helper()
	store, err := NewStore(pool, Options{TablePrefix: prefix})
	if err != nil {
		t.Fatalf("NewStore(%q): %v", prefix, err)
	}
	if err := store.Migrate(testContext(t)); err != nil {
		t.Fatalf("Migrate(%q): %v", prefix, err)
	}
	return store
}

// newTestStore returns a migrated Store with the default prefix in a
// fresh schema. Tests using it may run in parallel.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return migratedStore(t, newSchemaPool(t, 4), "")
}

// countRows returns the number of rows in table (a full, trusted table
// name) matching the optional WHERE clause.
func countRows(t *testing.T, store *Store, table, where string, args ...any) int {
	t.Helper()
	sql := `SELECT COUNT(*) FROM "` + table + `"`
	if where != "" {
		sql += " WHERE " + where
	}
	var n int
	if err := store.pool.QueryRow(testContext(t), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// appliedMigrationVersions reads the schema_migrations table of store.
func appliedMigrationVersions(t *testing.T, store *Store) []int {
	t.Helper()
	rows, err := store.pool.Query(testContext(t), `SELECT version FROM "`+store.prefix+`schema_migrations" ORDER BY version`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	return versions
}

// allMigrationVersions returns 1..n for the known migrations.
func allMigrationVersions() []int {
	versions := make([]int, len(migrations))
	for i, m := range migrations {
		versions[i] = m.version
	}
	return versions
}

// relationExists reports whether a table or index with the given name
// exists in the pool's search_path.
func relationExists(t *testing.T, store *Store, name string) bool {
	t.Helper()
	var exists bool
	err := store.pool.QueryRow(testContext(t), `SELECT to_regclass(quote_ident($1)) IS NOT NULL`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("to_regclass(%s): %v", name, err)
	}
	return exists
}

// uniqueEmail returns a unique lowercase email address.
func uniqueEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("user-%s@example.com", randomHex(t, 6))
}

// mustCreateUser creates a minimal user.
func mustCreateUser(t *testing.T, store *Store, email string) *auth.User {
	t.Helper()
	user := &auth.User{Name: "Test User", Email: email}
	if err := store.CreateUser(testContext(t), user); err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return user
}

// mustCreateSession creates a session for userID expiring at expires.
func mustCreateSession(t *testing.T, store *Store, userID string, expires time.Time) *auth.Session {
	t.Helper()
	session := &auth.Session{UserID: userID, TokenHash: auth.HashToken(randomHex(t, 16)), ExpiresAt: expires}
	if err := store.CreateSession(testContext(t), session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return session
}

// mustCreateToken creates a verification token for identifier.
func mustCreateToken(t *testing.T, store *Store, identifier string, expires time.Time) *auth.VerificationToken {
	t.Helper()
	token := &auth.VerificationToken{Identifier: identifier, ValueHash: auth.HashToken(randomHex(t, 16)), ExpiresAt: expires}
	if err := store.CreateVerificationToken(testContext(t), token); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}
	return token
}

// wantErrIs fails the test unless errors.Is(err, want).
func wantErrIs(t *testing.T, op string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: error = %v, want %v", op, err, want)
	}
}

// wantNoErr fails the test on a non-nil error.
func wantNoErr(t *testing.T, op string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
}

// ids returns the IDs of records in order.
func ids[T any](records []*T, id func(*T) string) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = id(r)
	}
	return out
}

// sortedCopy returns a sorted copy of s.
func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
