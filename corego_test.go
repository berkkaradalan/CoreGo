package corego

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/database"
)

func TestNewNilConfig(t *testing.T) {
	isolateEnv(t)

	core := newCore(t, nil)
	if core.Env == nil {
		t.Fatal("Env not loaded")
	}
	if core.Mongo != nil || core.Postgres != nil {
		t.Fatal("no database may connect without configuration")
	}
	if core.Auth != nil {
		t.Fatal("auth must stay off without AuthOptions")
	}
	if core.AuthHandlers() != nil {
		t.Fatal("AuthHandlers must be nil without auth")
	}
}

func TestNewReadsDotEnv(t *testing.T) {
	isolateEnv(t)
	if err := os.WriteFile(".env", []byte("PORT=4321\nGREETING=hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// PORT is set to "" by isolateEnv, and a set process variable wins
	// over .env even when it is empty; unset it so .env applies.
	os.Unsetenv("PORT")

	core := newCore(t, nil)
	if core.Env.Port != "4321" {
		t.Errorf("Env.Port = %q, want %q", core.Env.Port, "4321")
	}
	if got := core.Env.GetOrDefault("GREETING", ""); got != "hello" {
		t.Errorf("GREETING = %q, want %q", got, "hello")
	}
}

func TestNewMalformedDotEnv(t *testing.T) {
	isolateEnv(t)
	if err := os.WriteFile(".env", []byte("APP_NAME=demo\nBROKEN=\"unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	core, err := New(&Config{Auth: &AuthOptions{}})
	if err == nil {
		core.Close()
		t.Fatal("New succeeded with a malformed .env file")
	}
	if core != nil {
		t.Error("New returned a Core together with an error")
	}
	if !strings.Contains(err.Error(), "corego: load environment") {
		t.Errorf("error %q does not name the failing step", err)
	}
}

func TestNewDatabaseConnectionErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		cfg     *Config
		wantErr string
	}{
		{
			name:    "explicit mongo config with empty URL",
			cfg:     &Config{Mongo: &database.MongoConfig{}},
			wantErr: "corego: connect to MongoDB",
		},
		{
			name:    "explicit mongo config with malformed URL",
			cfg:     &Config{Mongo: &database.MongoConfig{URL: "not-a-mongodb-url"}},
			wantErr: "corego: connect to MongoDB",
		},
		{
			name:    "mongo URL from the environment",
			env:     map[string]string{"MONGODB_CONNECTION_URL": "not-a-mongodb-url"},
			wantErr: "corego: connect to MongoDB",
		},
		{
			name:    "explicit postgres config with empty URL",
			cfg:     &Config{Postgres: &database.PostgresConfig{}},
			wantErr: "corego: connect to PostgreSQL",
		},
		{
			name:    "postgres URL from the environment",
			env:     map[string]string{"POSTGRES_CONNECTION_URL": "postgres://%zz"},
			wantErr: "corego: connect to PostgreSQL",
		},
		{
			name:    "explicit config wins over a valid-looking environment",
			env:     map[string]string{"POSTGRES_CONNECTION_URL": "postgres://user@127.0.0.1:1/db"},
			cfg:     &Config{Postgres: &database.PostgresConfig{URL: "postgres://%zz"}},
			wantErr: "corego: connect to PostgreSQL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			core, err := New(tt.cfg)
			if err == nil {
				core.Close()
				t.Fatal("New succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestNewAuthStoreSelection(t *testing.T) {
	tests := []struct {
		name        string
		option      string
		envStore    string
		wantErr     string
		wantWarning bool
	}{
		{name: "auto without a database falls back to memory", wantWarning: true},
		{name: "explicit memory option", option: StoreMemory},
		{name: "memory from the environment", envStore: "memory"},
		{name: "store names are case insensitive", option: " Memory "},
		{name: "option wins over the environment", option: StoreMemory, envStore: "postgres"},
		{name: "unknown option", option: "redis", wantErr: `unknown auth store "redis" in AuthOptions.Store: valid values are "memory", "mongo" and "postgres"`},
		{name: "unknown environment value", envStore: "sqlite", wantErr: `unknown auth store "sqlite" in COREGO_AUTH_STORE`},
		{name: "mongo without a connection", option: StoreMongo, wantErr: `auth store "mongo" needs a MongoDB connection`},
		{name: "postgres without a connection", envStore: "postgres", wantErr: `auth store "postgres" needs a PostgreSQL connection`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("COREGO_AUTH_STORE", tt.envStore)
			logger, logs := newTestLogger()

			core, err := New(&Config{Auth: &AuthOptions{Store: tt.option}, Logger: logger})
			if tt.wantErr != "" {
				if err == nil {
					core.Close()
					t.Fatalf("New succeeded, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { core.Close() })

			if core.Auth == nil {
				t.Fatal("auth service not started")
			}
			gotWarning := strings.Contains(logs.String(), "auth uses the in-memory store")
			if gotWarning != tt.wantWarning {
				t.Errorf("memory store warning logged = %v, want %v; logs:\n%s", gotWarning, tt.wantWarning, logs)
			}
			// The service works end to end on the selected store.
			out, err := core.Auth.SignUp(t.Context(), auth.SignUpInput{
				Name: "Ada", Email: "ada@example.com", Password: "analytical-engine",
			})
			if err != nil {
				t.Fatalf("SignUp: %v", err)
			}
			if out.SessionToken == "" {
				t.Fatal("SignUp returned no session token")
			}
		})
	}
}

func TestAuthStoreReturnsMemoryStore(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, nil)
	for _, name := range []string{"", StoreMemory} {
		got, err := core.authStore(&AuthOptions{Store: name}, "env_prefix_")
		if err != nil {
			t.Fatalf("authStore(%q): %v", name, err)
		}
		if _, ok := got.store.(*auth.MemoryStore); !ok {
			t.Errorf("authStore(%q) = %T, want *auth.MemoryStore", name, got.store)
		}
		if got.kind != StoreMemory || got.prefix != "" {
			t.Errorf("authStore(%q) kind = %q, prefix = %q; want %q without a prefix", name, got.kind, got.prefix, StoreMemory)
		}
	}
}

// countingStore is a custom auth.Store that counts user creations, so
// tests can tell it is the store in use.
type countingStore struct {
	*auth.MemoryStore
	created atomic.Int32
}

func (s *countingStore) CreateUser(ctx context.Context, u *auth.User) error {
	s.created.Add(1)
	return s.MemoryStore.CreateUser(ctx, u)
}

func TestCustomStoreTakesPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		store    string
		envStore string
	}{
		{name: "automatic selection"},
		{name: "over AuthOptions.Store", store: StorePostgres},
		{name: "over COREGO_AUTH_STORE", envStore: StoreMongo},
		{name: "over an unknown store name", store: "redis"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("COREGO_AUTH_STORE", tt.envStore)
			logger, logs := newTestLogger()
			custom := &countingStore{MemoryStore: auth.NewMemoryStore()}
			core := newCore(t, &Config{Auth: &AuthOptions{Store: tt.store, CustomStore: custom, TablePrefix: "Not Valid!"}, Logger: logger})

			signUp(t, core.Server(), "ada@example.com")
			if got := custom.created.Load(); got != 1 {
				t.Fatalf("custom store saw %d user creations, want 1", got)
			}
			if strings.Contains(logs.String(), "in-memory store") {
				t.Errorf("the memory fallback warning was logged for a custom store; logs:\n%s", logs)
			}
		})
	}
}

func TestTablePrefixIgnoredByMemoryStore(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, &Config{Auth: &AuthOptions{Store: StoreMemory, TablePrefix: "myapp_"}})
	if core.Auth == nil {
		t.Fatal("auth service not started")
	}
}

func TestNewAuthServiceConfigError(t *testing.T) {
	isolateEnv(t)
	core, err := New(&Config{Auth: &AuthOptions{Service: auth.Config{SessionTTL: -1}}})
	if err == nil {
		core.Close()
		t.Fatal("New accepted a negative SessionTTL")
	}
	if !errors.Is(err, auth.ErrInvalidConfig) {
		t.Errorf("error = %v, want it to wrap auth.ErrInvalidConfig", err)
	}
}

func TestCloseIdempotentAndNilSafe(t *testing.T) {
	isolateEnv(t)

	var nilCore *Core
	if err := nilCore.Close(); err != nil {
		t.Fatalf("nil Core Close = %v, want nil", err)
	}

	core, err := New(&Config{Auth: &AuthOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if err := core.Close(); err != nil {
			t.Fatalf("Close call %d = %v, want nil", i+1, err)
		}
	}
	// The service stays usable for reads after Close: Close only waits
	// for background work and releases connections.
	if _, err := core.Auth.CountUsers(t.Context()); err != nil {
		t.Fatalf("CountUsers after Close: %v", err)
	}
}

func TestCloseConcurrent(t *testing.T) {
	isolateEnv(t)
	core, err := New(&Config{Auth: &AuthOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 8)
	for range 8 {
		go func() { errs <- core.Close() }()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Errorf("Close = %v", err)
		}
	}
}
