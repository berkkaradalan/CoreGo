package mongostore

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/database"
)

// indexesByName lists the indexes of a collection keyed by name.
func indexesByName(t *testing.T, db *database.MongoDB, collection string) map[string]mongo.IndexSpecification {
	t.Helper()
	specs, err := db.Collection(collection).Indexes().ListSpecifications(context.Background())
	if err != nil {
		t.Fatalf("list indexes of %s: %v", collection, err)
	}
	out := make(map[string]mongo.IndexSpecification, len(specs))
	for _, spec := range specs {
		out[spec.Name] = spec
	}
	return out
}

func TestIntegrationEnsureIndexes(t *testing.T) {
	store, db := newIntegrationStore(t)
	ctx := context.Background()

	// Running again (twice) is a no-op.
	for i := 0; i < 2; i++ {
		if err := store.EnsureIndexes(ctx); err != nil {
			t.Fatalf("EnsureIndexes re-run %d: %v", i+1, err)
		}
	}

	for _, spec := range store.indexSpecs() {
		indexes := indexesByName(t, db, spec.collection)
		got, ok := indexes[spec.name]
		if !ok {
			t.Fatalf("collection %s lacks index %s", spec.collection, spec.name)
		}
		if !indexSatisfies(got, spec) {
			t.Fatalf("index %s on %s does not match its definition: %+v", spec.name, spec.collection, got)
		}
		if spec.unique != (got.Unique != nil && *got.Unique) {
			t.Fatalf("index %s unique = %v, want %v", spec.name, got.Unique, spec.unique)
		}
		if spec.ttl {
			if got.ExpireAfterSeconds == nil || *got.ExpireAfterSeconds != 0 {
				t.Fatalf("TTL index %s expireAfterSeconds = %v, want 0", spec.name, got.ExpireAfterSeconds)
			}
		} else if got.ExpireAfterSeconds != nil {
			t.Fatalf("index %s unexpectedly has expireAfterSeconds %d", spec.name, *got.ExpireAfterSeconds)
		}
	}

	// Exactly the expected indexes (plus _id_) exist: re-runs created
	// no duplicates.
	want := map[string][]string{
		"corego_users":               {"_id_", "ux_users_email"},
		"corego_accounts":            {"_id_", "ux_accounts_provider_provider_account_id", "ux_accounts_user_id_provider"},
		"corego_sessions":            {"_id_", "ix_sessions_user_id_created_at", "ttl_sessions_expires_at", "ux_sessions_token_hash"},
		"corego_verification_tokens": {"_id_", "ttl_verification_tokens_expires_at", "ux_verification_tokens_identifier_value_hash"},
	}
	for collection, names := range want {
		var got []string
		for name := range indexesByName(t, db, collection) {
			got = append(got, name)
		}
		slices.Sort(got)
		if !slices.Equal(got, names) {
			t.Fatalf("indexes of %s = %v, want %v", collection, got, names)
		}
	}
}

func TestIntegrationEnsureIndexesWithExistingIndexes(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// newRawStore returns an unindexed store with a fresh prefix.
	newRawStore := func(t *testing.T) *Store {
		t.Helper()
		store, err := NewStore(db, Options{CollectionPrefix: randomName(t, "p") + "_"})
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		return store
	}
	createIndex := func(t *testing.T, coll *mongo.Collection, keys bson.D, opts *options.IndexOptionsBuilder) {
		t.Helper()
		if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: keys, Options: opts}); err != nil {
			t.Fatalf("create index on %s: %v", coll.Name(), err)
		}
	}

	t.Run("equivalent unique index under another name is accepted", func(t *testing.T) {
		store := newRawStore(t)
		createIndex(t, store.users, bson.D{{Key: "email", Value: 1}}, options.Index().SetName("my_email").SetUnique(true))
		if err := store.EnsureIndexes(ctx); err != nil {
			t.Fatalf("EnsureIndexes: %v", err)
		}
		// The remaining indexes of the collection set were still created
		// and the unique constraint is effective.
		if _, ok := indexesByName(t, db, store.names.sessions)["ttl_sessions_expires_at"]; !ok {
			t.Fatal("TTL index on sessions missing")
		}
		if err := store.CreateUser(ctx, &auth.User{Email: "a@example.com"}); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if err := store.CreateUser(ctx, &auth.User{Email: "a@example.com"}); !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("CreateUser(duplicate) error = %v, want ErrDuplicate", err)
		}
	})

	t.Run("tuned TTL index is kept", func(t *testing.T) {
		store := newRawStore(t)
		createIndex(t, store.sessions, bson.D{{Key: "expires_at", Value: 1}},
			options.Index().SetName("ttl_sessions_expires_at").SetExpireAfterSeconds(3600))
		if err := store.EnsureIndexes(ctx); err != nil {
			t.Fatalf("EnsureIndexes: %v", err)
		}
		indexes := indexesByName(t, db, store.names.sessions)
		if got := indexes["ttl_sessions_expires_at"].ExpireAfterSeconds; got == nil || *got != 3600 {
			t.Fatalf("expireAfterSeconds = %v, want the tuned 3600 kept", got)
		}
		for _, name := range []string{"ux_sessions_token_hash", "ix_sessions_user_id_created_at"} {
			if _, ok := indexes[name]; !ok {
				t.Fatalf("index %s missing after the per-index fallback", name)
			}
		}
	})

	t.Run("non-unique index on the unique keys coexists with the unique one", func(t *testing.T) {
		// MongoDB lets a unique and a non-unique index share a key
		// pattern, so the unique index is simply created next to it.
		store := newRawStore(t)
		createIndex(t, store.sessions, bson.D{{Key: "token_hash", Value: 1}}, options.Index().SetName("by_token"))
		if err := store.EnsureIndexes(ctx); err != nil {
			t.Fatalf("EnsureIndexes: %v", err)
		}
		if got, ok := indexesByName(t, db, store.names.sessions)["ux_sessions_token_hash"]; !ok || got.Unique == nil || !*got.Unique {
			t.Fatalf("unique token hash index = %+v (present %v)", got, ok)
		}
		if err := store.CreateSession(ctx, &auth.Session{TokenHash: "h"}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if err := store.CreateSession(ctx, &auth.Session{TokenHash: "h"}); !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("CreateSession(duplicate) error = %v, want ErrDuplicate", err)
		}
	})

	conflicts := []struct {
		name  string
		setup func(t *testing.T, store *Store)
		index string
	}{
		{
			name: "non-unique index under the unique index name",
			setup: func(t *testing.T, store *Store) {
				createIndex(t, store.users, bson.D{{Key: "email", Value: 1}}, options.Index().SetName("ux_users_email"))
			},
			index: "ux_users_email",
		},
		{
			name: "same name on different keys",
			setup: func(t *testing.T, store *Store) {
				createIndex(t, store.verificationTokens, bson.D{{Key: "identifier", Value: 1}},
					options.Index().SetName("ux_verification_tokens_identifier_value_hash").SetUnique(true))
			},
			index: "ux_verification_tokens_identifier_value_hash",
		},
		{
			name: "plain index where the TTL index is required",
			setup: func(t *testing.T, store *Store) {
				createIndex(t, store.verificationTokens, bson.D{{Key: "expires_at", Value: 1}}, options.Index().SetName("by_expiry"))
			},
			index: "ttl_verification_tokens_expires_at",
		},
	}
	for _, tt := range conflicts {
		t.Run(tt.name, func(t *testing.T) {
			store := newRawStore(t)
			tt.setup(t, store)
			err := store.EnsureIndexes(ctx)
			if err == nil {
				t.Fatal("EnsureIndexes accepted an index that does not provide the required guarantee")
			}
			if !strings.Contains(err.Error(), tt.index) || !strings.Contains(err.Error(), "drop it") {
				t.Fatalf("error %q does not name the index %q and the remedy", err, tt.index)
			}
		})
	}

	t.Run("duplicate data blocks a unique index", func(t *testing.T) {
		store := newRawStore(t)
		for i := 0; i < 2; i++ {
			if err := store.CreateUser(ctx, &auth.User{Email: "twice@example.com"}); err != nil {
				t.Fatalf("CreateUser without indexes: %v", err)
			}
		}
		err := store.EnsureIndexes(ctx)
		if err == nil || !mongo.IsDuplicateKeyError(err) {
			t.Fatalf("EnsureIndexes error = %v, want the duplicate key error of the index build", err)
		}
		if !strings.Contains(err.Error(), store.names.users) {
			t.Fatalf("error %q does not name the collection", err)
		}
	})
}

func TestIntegrationCollectionPrefixes(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	defaultStore := newTestStore(t, db, "")
	tenantA := newTestStore(t, db, "tenant_a_")
	tenantB := newTestStore(t, db, "tenant_b_")

	// The same email can exist once per store: unique indexes are per
	// collection set.
	for _, store := range []*Store{defaultStore, tenantA, tenantB} {
		if err := store.CreateUser(ctx, &auth.User{ID: "same-id", Email: "shared@example.com"}); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if err := store.CreateSession(ctx, &auth.Session{UserID: "same-id", TokenHash: "same-hash"}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	if err := tenantA.DeleteUser(ctx, "same-id"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := tenantA.GetUserByEmail(ctx, "shared@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("tenant A still has the user: %v", err)
	}
	for name, store := range map[string]*Store{"default": defaultStore, "tenant B": tenantB} {
		if _, err := store.GetUserByEmail(ctx, "shared@example.com"); err != nil {
			t.Fatalf("%s store lost its user: %v", name, err)
		}
		if _, err := store.GetSessionByTokenHash(ctx, "same-hash"); err != nil {
			t.Fatalf("%s store lost its session: %v", name, err)
		}
		if n, err := store.CountUsers(ctx); err != nil || n != 1 {
			t.Fatalf("%s CountUsers = %d, %v; want 1", name, n, err)
		}
	}

	names, err := db.Database().ListCollectionNames(ctx, bson.D{})
	if err != nil {
		t.Fatalf("ListCollectionNames: %v", err)
	}
	slices.Sort(names)
	want := []string{
		"corego_accounts", "corego_sessions", "corego_users", "corego_verification_tokens",
		"tenant_a_accounts", "tenant_a_sessions", "tenant_a_users", "tenant_a_verification_tokens",
		"tenant_b_accounts", "tenant_b_sessions", "tenant_b_users", "tenant_b_verification_tokens",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("collections = %v, want %v", names, want)
	}
}
