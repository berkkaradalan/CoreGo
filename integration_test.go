package corego

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/database"
	"github.com/berkkaradalan/CoreGo/web"
)

// The integration tests are opt-in, like the store suites: they run only
// when COREGO_TEST_MONGO_URL and/or COREGO_TEST_POSTGRES_URL point at
// disposable servers (see docs/testing.md) and skip otherwise. Each test
// works in its own MongoDB database or PostgreSQL schema and drops it
// afterwards.

// randomSuffix returns 12 random hex characters.
func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// useTestMongo points MONGODB_CONNECTION_URL at the test server with a
// fresh database name, dropped when the test ends.
func useTestMongo(t *testing.T) {
	t.Helper()
	raw := os.Getenv("COREGO_TEST_MONGO_URL")
	if raw == "" {
		t.Skip("COREGO_TEST_MONGO_URL not set; skipping MongoDB integration test")
	}
	name := "corego_facade_it_" + randomSuffix(t)
	t.Setenv("MONGODB_CONNECTION_URL", raw)
	t.Setenv("MONGODB_DATABASE", name)
	t.Cleanup(func() {
		db, err := database.NewMongoDB(database.MongoConfig{URL: raw, Database: name})
		if err != nil {
			t.Errorf("connect for cleanup: %v", err)
			return
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.Database().Drop(ctx); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})
}

// useTestPostgres creates a dedicated schema on the test server and
// points POSTGRES_CONNECTION_URL at it through search_path, so the
// corego_ tables never collide with other suites. The schema is dropped
// when the test ends.
func useTestPostgres(t *testing.T) {
	t.Helper()
	raw := os.Getenv("COREGO_TEST_POSTGRES_URL")
	if raw == "" {
		t.Skip("COREGO_TEST_POSTGRES_URL not set; skipping PostgreSQL integration test")
	}
	schema := "corego_facade_it_" + randomSuffix(t)

	admin, err := database.NewPostgresDB(database.PostgresConfig{URL: raw})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
		admin.Close()
	})
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	t.Setenv("POSTGRES_CONNECTION_URL", u.String())
}

// signUpAndSignIn exercises the mounted routes and a RequireAuth route
// end to end and returns the signed-in user's ID.
func signUpAndSignIn(t *testing.T, core *Core) string {
	t.Helper()
	app := core.Server()
	app.Get("/api/me", func(c *web.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"id": auth.MustUser(c).ID})
	}, core.RequireAuth())

	signUp(t, app, "ada@example.com")
	w := do(t, app, request{method: http.MethodPost, path: "/auth/signin", body: map[string]any{
		"email": "ada@example.com", "password": "analytical-engine",
	}})
	if w.Code != http.StatusOK {
		t.Fatalf("signin status = %d, body %s", w.Code, w.Body.String())
	}
	var signin struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	decode(t, w, &signin)

	w = do(t, app, request{method: http.MethodGet, path: "/api/me", token: signin.Token})
	if w.Code != http.StatusOK {
		t.Fatalf("protected route status = %d, body %s", w.Code, w.Body.String())
	}
	var me map[string]string
	decode(t, w, &me)
	if me["id"] != signin.User.ID {
		t.Fatalf("protected route user = %q, want %q", me["id"], signin.User.ID)
	}
	return signin.User.ID
}

func TestIntegrationFacadeSelectsMongo(t *testing.T) {
	isolateEnv(t)
	useTestMongo(t)

	core := newCore(t, &Config{Auth: &AuthOptions{}})
	if core.Mongo == nil {
		t.Fatal("MongoDB not connected from MONGODB_CONNECTION_URL")
	}
	if got, want := core.Mongo.Name(), os.Getenv("MONGODB_DATABASE"); got != want {
		t.Fatalf("database = %q, want MONGODB_DATABASE %q", got, want)
	}
	userID := signUpAndSignIn(t, core)

	n, err := core.Mongo.Count(t.Context(), "corego_users", bson.M{"_id": userID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("user %s not stored in MongoDB (count %d)", userID, n)
	}
}

func TestIntegrationFacadeSelectsPostgres(t *testing.T) {
	isolateEnv(t)
	useTestPostgres(t)

	core := newCore(t, &Config{Auth: &AuthOptions{}})
	if core.Postgres == nil {
		t.Fatal("PostgreSQL not connected from POSTGRES_CONNECTION_URL")
	}
	userID := signUpAndSignIn(t, core)

	n, err := database.QueryOne[int64](t.Context(), core.Postgres, "SELECT count(*) FROM corego_users WHERE id = $1", userID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("user %s not stored in PostgreSQL (count %d)", userID, n)
	}
}

func TestIntegrationFacadeStorePriority(t *testing.T) {
	tests := []struct {
		name      string
		envStore  string
		wantMongo bool
	}{
		{name: "auto prefers MongoDB", wantMongo: true},
		{name: "COREGO_AUTH_STORE selects PostgreSQL", envStore: StorePostgres, wantMongo: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			useTestMongo(t)
			useTestPostgres(t)
			t.Setenv("COREGO_AUTH_STORE", tt.envStore)

			core := newCore(t, &Config{Auth: &AuthOptions{}})
			if core.Mongo == nil || core.Postgres == nil {
				t.Fatal("both databases must be connected")
			}
			userID := signUpAndSignIn(t, core)

			inMongo, err := core.Mongo.Count(t.Context(), "corego_users", bson.M{"_id": userID})
			if err != nil {
				t.Fatal(err)
			}
			if got := inMongo == 1; got != tt.wantMongo {
				t.Fatalf("user stored in MongoDB = %v, want %v", got, tt.wantMongo)
			}
			if !tt.wantMongo {
				n, err := database.QueryOne[int64](t.Context(), core.Postgres, "SELECT count(*) FROM corego_users WHERE id = $1", userID)
				if err != nil {
					t.Fatal(err)
				}
				if n != 1 {
					t.Fatalf("user not stored in PostgreSQL (count %d)", n)
				}
			}
		})
	}
}

func TestIntegrationFacadeTablePrefix(t *testing.T) {
	tests := []struct {
		name  string
		store string
		setup func(*testing.T)
		// count reports how many users with userID the named table
		// (or collection) holds; 0 when it does not exist.
		count func(t *testing.T, core *Core, table, userID string) int64
	}{
		{
			name: "MongoDB", store: StoreMongo, setup: useTestMongo,
			count: func(t *testing.T, core *Core, collection, userID string) int64 {
				n, err := core.Mongo.Count(t.Context(), collection, bson.M{"_id": userID})
				if err != nil {
					t.Fatal(err)
				}
				return n
			},
		},
		{
			name: "PostgreSQL", store: StorePostgres, setup: useTestPostgres,
			count: func(t *testing.T, core *Core, table, userID string) int64 {
				exists, err := database.QueryOne[bool](t.Context(), core.Postgres, "SELECT to_regclass($1) IS NOT NULL", table)
				if err != nil {
					t.Fatal(err)
				}
				if !exists {
					return 0
				}
				n, err := database.QueryOne[int64](t.Context(), core.Postgres, "SELECT count(*) FROM "+table+" WHERE id = $1", userID)
				if err != nil {
					t.Fatal(err)
				}
				return n
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			tt.setup(t)

			core := newCore(t, &Config{Auth: &AuthOptions{Store: tt.store, TablePrefix: "myapp_"}})
			userID := signUpAndSignIn(t, core)
			if n := tt.count(t, core, "myapp_users", userID); n != 1 {
				t.Fatalf("user not stored under the prefix (count %d)", n)
			}
			if n := tt.count(t, core, "corego_users", userID); n != 0 {
				t.Fatalf("user stored under the default prefix (count %d)", n)
			}
		})
		t.Run(tt.name+" prefix from COREGO_AUTH_TABLE_PREFIX", func(t *testing.T) {
			isolateEnv(t)
			tt.setup(t)
			t.Setenv("COREGO_AUTH_TABLE_PREFIX", "envapp_")
			logger, logs := newTestLogger()

			core := newCore(t, &Config{Auth: &AuthOptions{Store: tt.store}, Logger: logger})
			userID := signUpAndSignIn(t, core)
			if n := tt.count(t, core, "envapp_users", userID); n != 1 {
				t.Fatalf("user not stored under the environment prefix (count %d)", n)
			}
			if n := tt.count(t, core, "corego_users", userID); n != 0 {
				t.Fatalf("user stored under the default prefix (count %d)", n)
			}
			attrs := mustAuthReadyAttrs(t, logs.String())
			if attrs["store"] != tt.store || attrs["table_prefix"] != "envapp_" {
				t.Errorf("auth ready attrs = %v, want store %s and table_prefix envapp_", attrs, tt.store)
			}
		})
		t.Run(tt.name+" AuthOptions.TablePrefix wins over the environment", func(t *testing.T) {
			isolateEnv(t)
			tt.setup(t)
			t.Setenv("COREGO_AUTH_TABLE_PREFIX", "envapp_")

			core := newCore(t, &Config{Auth: &AuthOptions{Store: tt.store, TablePrefix: "codeapp_"}})
			userID := signUpAndSignIn(t, core)
			if n := tt.count(t, core, "codeapp_users", userID); n != 1 {
				t.Fatalf("user not stored under the code prefix (count %d)", n)
			}
			if n := tt.count(t, core, "envapp_users", userID); n != 0 {
				t.Fatalf("user stored under the environment prefix (count %d)", n)
			}
		})
		t.Run(tt.name+" rejects an invalid prefix from the environment", func(t *testing.T) {
			isolateEnv(t)
			tt.setup(t)
			t.Setenv("COREGO_AUTH_TABLE_PREFIX", "Bad-Prefix")

			core, err := New(&Config{Auth: &AuthOptions{Store: tt.store}})
			if err == nil {
				core.Close()
				t.Fatal("New accepted an invalid COREGO_AUTH_TABLE_PREFIX")
			}
			if missing := missingSubstrings(err.Error(), "COREGO_AUTH_TABLE_PREFIX", "Bad-Prefix"); len(missing) > 0 {
				t.Errorf("error %q is missing %v", err, missing)
			}
		})
		t.Run(tt.name+" rejects an invalid prefix", func(t *testing.T) {
			isolateEnv(t)
			tt.setup(t)

			core, err := New(&Config{Auth: &AuthOptions{Store: tt.store, TablePrefix: "Bad-Prefix"}})
			if err == nil {
				core.Close()
				t.Fatal("New accepted an invalid TablePrefix")
			}
			if !strings.Contains(err.Error(), "Bad-Prefix") {
				t.Errorf("error %q does not name the prefix", err)
			}
		})
	}
}

// TestIntegrationPostgresSearchPathSchema puts the auth tables into a
// dedicated schema through search_path in POSTGRES_CONNECTION_URL, the
// setup docs/auth-storage.md describes, and checks that a schema that
// does not exist makes New fail instead of creating the tables
// elsewhere.
func TestIntegrationPostgresSearchPathSchema(t *testing.T) {
	t.Run("existing schema", func(t *testing.T) {
		isolateEnv(t)
		useTestPostgres(t)
		logger, logs := newTestLogger()
		core := newCore(t, &Config{Auth: &AuthOptions{}, Logger: logger})

		schema, err := database.QueryOne[string](t.Context(), core.Postgres, "SELECT current_schema()")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(schema, "corego_facade_it_") {
			t.Fatalf("current_schema() = %q, want the schema from search_path", schema)
		}
		userID := signUpAndSignIn(t, core)
		n, err := database.QueryOne[int64](t.Context(), core.Postgres,
			"SELECT count(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name LIKE 'corego\\_%'", schema)
		if err != nil {
			t.Fatal(err)
		}
		if n != 5 { // users, accounts, sessions, verification_tokens, schema_migrations
			t.Fatalf("schema %s holds %d corego_ tables, want 5", schema, n)
		}
		inSchema, err := database.QueryOne[int64](t.Context(), core.Postgres,
			"SELECT count(*) FROM "+schema+".corego_users WHERE id = $1", userID)
		if err != nil {
			t.Fatal(err)
		}
		if inSchema != 1 {
			t.Fatalf("user not stored in %s.corego_users", schema)
		}
		if attrs := mustAuthReadyAttrs(t, logs.String()); attrs["store"] != StorePostgres || attrs["table_prefix"] != "corego_" {
			t.Errorf("auth ready attrs = %v", attrs)
		}
	})
	t.Run("missing schema", func(t *testing.T) {
		isolateEnv(t)
		raw := os.Getenv("COREGO_TEST_POSTGRES_URL")
		if raw == "" {
			t.Skip("COREGO_TEST_POSTGRES_URL not set; skipping PostgreSQL integration test")
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", "corego_missing_schema_"+randomSuffix(t))
		u.RawQuery = q.Encode()
		t.Setenv("POSTGRES_CONNECTION_URL", u.String())

		core, err := New(&Config{Auth: &AuthOptions{}})
		if err == nil {
			core.Close()
			t.Fatal("New succeeded although the search_path schema does not exist")
		}
		if missing := missingSubstrings(err.Error(), "corego: migrate PostgreSQL auth schema", "no schema has been selected to create in"); len(missing) > 0 {
			t.Errorf("error %q is missing %v", err, missing)
		}
	})
}
