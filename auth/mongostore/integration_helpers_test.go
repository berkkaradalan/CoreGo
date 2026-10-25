package mongostore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/berkkaradalan/CoreGo/database"
)

// The integration suite runs against a real MongoDB server and is
// opt-in: set COREGO_TEST_MONGO_URL (see AGENTS.md for the docker
// one-liner). Without the variable every integration test skips, so
// `go test ./auth/mongostore/` passes on machines without any
// infrastructure. When the variable is set, an unreachable server is a
// test failure, not a skip.
//
// Every test works in its own randomly named database, which is
// dropped when the test ends, so runs never interfere with each other
// or with other suites sharing the server.

// newTestDB connects to a fresh, randomly named database and drops it
// when the test ends.
func newTestDB(t *testing.T) *database.MongoDB {
	t.Helper()
	url := os.Getenv("COREGO_TEST_MONGO_URL")
	if url == "" {
		t.Skip("COREGO_TEST_MONGO_URL not set; skipping MongoDB integration test")
	}

	db, err := database.NewMongoDB(database.MongoConfig{URL: url, Database: randomName(t, "corego_mongostore_")})
	if err != nil {
		t.Fatalf("NewMongoDB: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.Database().Drop(ctx); err != nil {
			t.Errorf("drop test database %q: %v", db.Name(), err)
		}
		_ = db.Close()
	})
	return db
}

// newTestStore returns a store with the given prefix on db whose
// indexes are in place.
func newTestStore(t *testing.T, db *database.MongoDB, prefix string) *Store {
	t.Helper()
	store, err := NewStore(db, Options{CollectionPrefix: prefix})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return store
}

// newIntegrationStore is the common case: a fresh database with a
// default-prefixed, indexed store.
func newIntegrationStore(t *testing.T) (*Store, *database.MongoDB) {
	t.Helper()
	db := newTestDB(t)
	return newTestStore(t, db, ""), db
}

// randomName returns prefix followed by 16 random hex characters.
func randomName(t *testing.T, prefix string) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random name: %v", err)
	}
	return prefix + hex.EncodeToString(buf)
}

// countDocs counts the documents in a collection matching filter.
func countDocs(t *testing.T, db *database.MongoDB, collection string, filter bson.D) int64 {
	t.Helper()
	n, err := db.Collection(collection).CountDocuments(context.Background(), filter)
	if err != nil {
		t.Fatalf("count %s: %v", collection, err)
	}
	return n
}

// msTime returns base plus the given milliseconds as a UTC instant with
// millisecond precision, which BSON stores exactly.
func msTime(base time.Time, ms int) time.Time {
	return base.Add(time.Duration(ms) * time.Millisecond).UTC().Truncate(time.Millisecond)
}

// testBase is a fixed, millisecond precise instant used as the origin
// of explicit test timestamps.
var testBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
