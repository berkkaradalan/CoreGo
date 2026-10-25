package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/database"
)

// operationTimeout bounds every store method. Each method derives its
// context with context.WithTimeout from the caller's context, so a
// shorter caller deadline (or a cancellation) always wins.
const operationTimeout = 5 * time.Second

// Compile-time proof that Store satisfies the auth.Store contract.
var _ auth.Store = (*Store)(nil)

// Store implements auth.Store on MongoDB. It only holds driver
// collection handles, so it is safe for concurrent use.
type Store struct {
	names collectionNames

	users              *mongo.Collection
	accounts           *mongo.Collection
	sessions           *mongo.Collection
	verificationTokens *mongo.Collection
}

// NewStore returns an auth.Store that keeps its records in the database
// db is bound to (see database.MongoConfig.Database).
//
// opts.CollectionPrefix selects the collection names (default
// "corego_"); an invalid prefix or a nil db is an error. NewStore does
// not contact the server: call EnsureIndexes once at startup to create
// the indexes the store relies on.
func NewStore(db *database.MongoDB, opts Options) (*Store, error) {
	prefix, err := resolvePrefix(opts.CollectionPrefix)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("mongostore: NewStore needs a connected *database.MongoDB, got nil")
	}
	names := newCollectionNames(prefix)
	return &Store{
		names:              names,
		users:              db.Collection(names.users),
		accounts:           db.Collection(names.accounts),
		sessions:           db.Collection(names.sessions),
		verificationTokens: db.Collection(names.verificationTokens),
	}, nil
}

// withOpTimeout derives the per-operation context.
func withOpTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, operationTimeout)
}

// mapError translates MongoDB driver errors into the auth sentinel
// errors:
//
//	mongo.ErrNoDocuments (also wrapped)       -> auth.ErrNotFound
//	duplicate key violation (code 11000 etc.) -> auth.ErrDuplicate
//
// Every other error (including nil) is returned unchanged. The function
// is pure so it can be unit tested without a server.
func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mongo.ErrNoDocuments):
		return auth.ErrNotFound
	case mongo.IsDuplicateKeyError(err):
		return auth.ErrDuplicate
	default:
		return err
	}
}

// versionFilter selects the document with the given _id only while its
// stored version equals version: the guard of every optimistic
// concurrency write.
func versionFilter(id string, version int64) bson.D {
	return bson.D{{Key: "_id", Value: id}, {Key: "version", Value: version}}
}

// replaceVersioned replaces the document with the given _id by
// replacement, but only while its stored version equals version. A
// single document write is atomic in MongoDB, so of several concurrent
// callers passing the same version exactly one matches. When nothing
// matched, a follow-up lookup by _id tells the two causes apart: a
// missing document yields auth.ErrNotFound, a document whose version
// moved on auth.ErrConflict.
func replaceVersioned(ctx context.Context, coll *mongo.Collection, id string, version int64, replacement any) error {
	result, err := coll.ReplaceOne(ctx, versionFilter(id, version), replacement)
	if err != nil {
		return mapError(err)
	}
	if result.MatchedCount > 0 {
		return nil
	}
	exists := coll.FindOne(ctx, bson.D{{Key: "_id", Value: id}},
		options.FindOne().SetProjection(bson.D{{Key: "_id", Value: 1}}))
	if err := exists.Err(); err != nil {
		// mongo.ErrNoDocuments maps to auth.ErrNotFound.
		return mapError(err)
	}
	return auth.ErrConflict
}
