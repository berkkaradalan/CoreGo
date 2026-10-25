package pgstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// lazyPool returns a pool that is never connected: pgxpool opens
// connections on first use only, so constructors and argument checks
// can be tested without a server.
func lazyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody:secret@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestNewStoreErrors(t *testing.T) {
	pool := lazyPool(t)
	tests := []struct {
		name string
		pool *pgxpool.Pool
		opts Options
		want error
	}{
		{"nil pool", nil, Options{}, ErrNilPool},
		{"nil pool is reported before the prefix", nil, Options{TablePrefix: "Bad"}, ErrNilPool},
		{"uppercase prefix", pool, Options{TablePrefix: "App_"}, ErrInvalidTablePrefix},
		{"injection attempt", pool, Options{TablePrefix: `x"; DROP TABLE users; --`}, ErrInvalidTablePrefix},
		{"too long prefix", pool, Options{TablePrefix: "abcdefghijklmnopqrstuvwxy"}, ErrInvalidTablePrefix},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewStore(tt.pool, tt.opts)
			if !errors.Is(err, tt.want) {
				t.Fatalf("NewStore error = %v, want %v", err, tt.want)
			}
			if store != nil {
				t.Fatalf("NewStore returned a store together with an error")
			}
		})
	}
}

func TestNewStoreResolvesPrefix(t *testing.T) {
	pool := lazyPool(t)
	tests := []struct {
		prefix string
		want   string
	}{
		{"", DefaultTablePrefix},
		{"myapp_", "myapp_"},
		{"auth", "auth"},
		{"1st_", "1st_"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			store, err := NewStore(pool, Options{TablePrefix: tt.prefix})
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			if got := store.TablePrefix(); got != tt.want {
				t.Fatalf("TablePrefix() = %q, want %q", got, tt.want)
			}
			if store.lockKey != migrationLockKey(tt.want) {
				t.Fatalf("lock key not derived from the resolved prefix")
			}
			if len(store.migrations) != len(migrations) {
				t.Fatalf("store has %d planned migrations, want %d", len(store.migrations), len(migrations))
			}
			assertRenderedFor(t, store.q, tt.want)
		})
	}
}

// TestStoreRejectsNilRecords verifies that mutating methods reject nil
// model pointers before ever touching the database.
func TestStoreRejectsNilRecords(t *testing.T) {
	store, err := NewStore(lazyPool(t), Options{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
		want error
	}{
		{"CreateUser", func() error { return store.CreateUser(ctx, nil) }, errNilUser},
		{"UpdateUser", func() error { return store.UpdateUser(ctx, nil) }, errNilUser},
		{"CreateAccount", func() error { return store.CreateAccount(ctx, nil) }, errNilAccount},
		{"UpdateAccount", func() error { return store.UpdateAccount(ctx, nil) }, errNilAccount},
		{"CreateSession", func() error { return store.CreateSession(ctx, nil) }, errNilSession},
		{"UpdateSession", func() error { return store.UpdateSession(ctx, nil) }, errNilSession},
		{"CreateVerificationToken", func() error { return store.CreateVerificationToken(ctx, nil) }, errNilVerificationToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, tt.want) {
				t.Fatalf("%s(nil) = %v, want %v", tt.name, err, tt.want)
			}
		})
	}
}

// TestStoreHonorsCanceledContext checks that a canceled context
// surfaces as context.Canceled (not as a mapped sentinel) without any
// server.
func TestStoreHonorsCanceledContext(t *testing.T) {
	store, err := NewStore(lazyPool(t), Options{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.GetUser(ctx, "id"); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetUser with canceled context = %v, want context.Canceled", err)
	}
	if err := store.DeleteExpiredSessions(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteExpiredSessions with canceled context = %v, want context.Canceled", err)
	}
	if err := store.Migrate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Migrate with canceled context = %v, want context.Canceled", err)
	}
}
