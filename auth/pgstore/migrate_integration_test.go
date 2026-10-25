package pgstore

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/berkkaradalan/CoreGo/auth"
)

func TestIntegrationMigrateCreatesSchema(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	for _, name := range []string{
		"corego_users", "corego_accounts", "corego_sessions", "corego_verification_tokens", "corego_schema_migrations",
		"corego_users_created_at_idx", "corego_sessions_user_id_idx", "corego_sessions_expires_at_idx",
		"corego_verification_tokens_expires_at_idx", "corego_users_email_key", "corego_accounts_user_provider_key",
		"corego_accounts_provider_account_key", "corego_sessions_token_hash_key", "corego_verification_tokens_lookup_key",
	} {
		if !relationExists(t, store, name) {
			t.Errorf("relation %s does not exist after Migrate", name)
		}
	}

	if got, want := appliedMigrationVersions(t, store), allMigrationVersions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("applied versions = %v, want %v", got, want)
	}
	var appliedAt time.Time
	if err := store.pool.QueryRow(ctx, `SELECT applied_at FROM "corego_schema_migrations" WHERE version = 1`).Scan(&appliedAt); err != nil {
		t.Fatalf("read applied_at: %v", err)
	}
	if time.Since(appliedAt) > time.Minute || time.Since(appliedAt) < -time.Minute {
		t.Fatalf("applied_at = %v, want about now", appliedAt)
	}

	// Both foreign keys cascade on delete ('c').
	rows, err := store.pool.Query(ctx, `
SELECT conname, confdeltype::text FROM pg_constraint
WHERE contype = 'f' AND connamespace = current_schema()::regnamespace ORDER BY conname`)
	if err != nil {
		t.Fatalf("read foreign keys: %v", err)
	}
	foreignKeys := map[string]string{}
	for rows.Next() {
		var name, action string
		if err := rows.Scan(&name, &action); err != nil {
			t.Fatalf("scan foreign key: %v", err)
		}
		foreignKeys[name] = action
	}
	rows.Close()
	want := map[string]string{"corego_accounts_user_id_fkey": "c", "corego_sessions_user_id_fkey": "c"}
	if !reflect.DeepEqual(foreignKeys, want) {
		t.Fatalf("foreign keys = %v, want %v", foreignKeys, want)
	}

	// accounts.user_id lookups are served by the unique index whose
	// leading column is user_id.
	var indexDef string
	if err := store.pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname = 'corego_accounts_user_provider_key' AND schemaname = current_schema()`).Scan(&indexDef); err != nil {
		t.Fatalf("read index definition: %v", err)
	}
	if want := "(user_id, provider)"; !strings.Contains(indexDef, want) {
		t.Fatalf("index definition %q does not lead with user_id", indexDef)
	}
}

func TestIntegrationMigrateIsIdempotent(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	user := mustCreateUser(t, store, uniqueEmail(t))
	for i := 0; i < 3; i++ {
		wantNoErr(t, "repeated Migrate", store.Migrate(ctx))
	}
	if got, want := appliedMigrationVersions(t, store), allMigrationVersions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("applied versions after repeated Migrate = %v, want %v", got, want)
	}
	if _, err := store.GetUser(ctx, user.ID); err != nil {
		t.Fatalf("data lost after repeated Migrate: %v", err)
	}

	// A second Store value with the same prefix sees a migrated schema.
	other, err := NewStore(store.pool, Options{})
	wantNoErr(t, "NewStore", err)
	wantNoErr(t, "Migrate via second store", other.Migrate(ctx))
	if n, err := other.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("CountUsers via second store = %d, %v; want 1", n, err)
	}
}

func TestIntegrationMigrateConcurrentCallers(t *testing.T) {
	t.Parallel()
	const callers = 8
	pool := newSchemaPool(t, callers+2)
	ctx := testContext(t)

	var wg sync.WaitGroup
	errs := make([]error, callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Every caller is an independent Store value, like separate
			// application instances.
			store, err := NewStore(pool, Options{TablePrefix: "race_"})
			if err != nil {
				errs[i] = err
				return
			}
			<-start
			errs[i] = store.Migrate(ctx)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: Migrate: %v", i, err)
		}
	}
	store, err := NewStore(pool, Options{TablePrefix: "race_"})
	wantNoErr(t, "NewStore", err)
	if got, want := appliedMigrationVersions(t, store), allMigrationVersions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("applied versions = %v, want %v", got, want)
	}
	mustCreateUser(t, store, uniqueEmail(t))
}

func TestIntegrationMigrateRollsBackOnFailure(t *testing.T) {
	t.Parallel()
	pool := newSchemaPool(t, 4)
	ctx := testContext(t)

	// A foreign table already uses one of the store's names: Migrate
	// must fail loudly instead of adopting it, and leave nothing behind.
	if _, err := pool.Exec(ctx, `CREATE TABLE "corego_sessions" (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create conflicting table: %v", err)
	}
	store, err := NewStore(pool, Options{})
	wantNoErr(t, "NewStore", err)

	err = store.Migrate(ctx)
	if err == nil {
		t.Fatal("Migrate succeeded despite a conflicting table")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P07" {
		t.Fatalf("Migrate error = %v, want a wrapped duplicate_table (42P07) error", err)
	}
	for _, name := range []string{"corego_users", "corego_accounts", "corego_verification_tokens", "corego_schema_migrations"} {
		if relationExists(t, store, name) {
			t.Errorf("%s exists after a failed migration; the transaction was not rolled back", name)
		}
	}

	// Once the conflict is gone, Migrate succeeds.
	if _, err := pool.Exec(ctx, `DROP TABLE "corego_sessions"`); err != nil {
		t.Fatalf("drop conflicting table: %v", err)
	}
	wantNoErr(t, "Migrate after resolving the conflict", store.Migrate(ctx))
}

func TestIntegrationMigrateKeepsUnknownVersions(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	// A newer release recorded a version this release does not know.
	if _, err := store.pool.Exec(ctx, `INSERT INTO "corego_schema_migrations" (version, applied_at) VALUES (9999, now())`); err != nil {
		t.Fatalf("insert future version: %v", err)
	}
	wantNoErr(t, "Migrate", store.Migrate(ctx))
	want := append(allMigrationVersions(), 9999)
	if got := appliedMigrationVersions(t, store); !reflect.DeepEqual(got, want) {
		t.Fatalf("applied versions = %v, want %v", got, want)
	}
}

func TestIntegrationPrefixesAreIsolated(t *testing.T) {
	t.Parallel()
	pool := newSchemaPool(t, 4)
	ctx := testContext(t)
	alpha := migratedStore(t, pool, "alpha_")
	beta := migratedStore(t, pool, "beta_")

	// The same email exists independently in both stores.
	email := uniqueEmail(t)
	a := mustCreateUser(t, alpha, email)
	b := mustCreateUser(t, beta, email)
	mustCreateUser(t, beta, uniqueEmail(t))

	if n, _ := alpha.CountUsers(ctx); n != 1 {
		t.Fatalf("alpha has %d users, want 1", n)
	}
	if n, _ := beta.CountUsers(ctx); n != 2 {
		t.Fatalf("beta has %d users, want 2", n)
	}
	if _, err := alpha.GetUser(ctx, b.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("alpha sees beta's user: %v", err)
	}

	wantNoErr(t, "alpha DeleteUser", alpha.DeleteUser(ctx, a.ID))
	if _, err := beta.GetUserByEmail(ctx, email); err != nil {
		t.Fatalf("deleting from alpha affected beta: %v", err)
	}
	for _, store := range []*Store{alpha, beta} {
		if got, want := appliedMigrationVersions(t, store), allMigrationVersions(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s applied versions = %v, want %v", store.prefix, got, want)
		}
	}
}

func TestIntegrationApplicationTablesAreUntouched(t *testing.T) {
	t.Parallel()
	pool := newSchemaPool(t, 4)
	ctx := testContext(t)

	// The application owns tables with the unprefixed names.
	for _, ddl := range []string{
		`CREATE TABLE users (id SERIAL PRIMARY KEY, nickname TEXT NOT NULL)`,
		`CREATE TABLE sessions (sid TEXT PRIMARY KEY, payload JSONB)`,
		`INSERT INTO users (nickname) VALUES ('app-user')`,
		`INSERT INTO sessions (sid, payload) VALUES ('s1', '{}')`,
	} {
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("set up application tables: %v", err)
		}
	}

	store := migratedStore(t, pool, "")
	user := mustCreateUser(t, store, uniqueEmail(t))
	mustCreateSession(t, store, user.ID, time.Now().Add(time.Hour))
	wantNoErr(t, "DeleteExpiredSessions", store.DeleteExpiredSessions(ctx, time.Now().Add(2*time.Hour)))
	wantNoErr(t, "DeleteUser", store.DeleteUser(ctx, user.ID))

	var nickname string
	if err := pool.QueryRow(ctx, `SELECT nickname FROM users`).Scan(&nickname); err != nil || nickname != "app-user" {
		t.Fatalf("application users table changed: %q, %v", nickname, err)
	}
	var sessions int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("application sessions table changed: %d rows, %v", sessions, err)
	}
	var columns int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'users'`).Scan(&columns); err != nil || columns != 2 {
		t.Fatalf("application users table has %d columns, want 2 (%v)", columns, err)
	}
}
