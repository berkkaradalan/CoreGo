package mongostore

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/berkkaradalan/CoreGo/auth"
)

// Pagination limits used by normalizeListOptions.
const (
	defaultListLimit = 100
	maxListLimit     = 1000
)

// normalizeListOptions applies the pagination rules of this store:
//
//	limit  <= 0    -> defaultListLimit (100), the default the auth.Store
//	                  contract documents for Limit 0
//	limit  >  1000 -> maxListLimit; large values are capped
//	offset <  0    -> 0
//
// The clamping keeps negative caller input away from the driver, which
// rejects a negative skip.
func normalizeListOptions(opts auth.ListOptions) (limit, offset int64) {
	limit = int64(opts.Limit)
	offset = int64(opts.Offset)
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// CreateUser inserts user. When the caller left ID, CreatedAt or
// UpdatedAt zero they are filled in on the passed struct; Version is
// set to 1 (also on the passed struct), whatever the caller put there.
// A duplicate email (or ID) yields auth.ErrDuplicate.
func (s *Store) CreateUser(ctx context.Context, user *auth.User) error {
	user.ID = ensureID(user.ID)
	ensureCreateTimestamps(&user.CreatedAt, &user.UpdatedAt)
	user.Version = 1

	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	_, err := s.users.InsertOne(ctx, userToDoc(user))
	return mapError(err)
}

// GetUser fetches a user by ID. It returns auth.ErrNotFound when no
// user has the given ID.
func (s *Store) GetUser(ctx context.Context, id string) (*auth.User, error) {
	return s.findUser(ctx, bson.D{{Key: "_id", Value: id}})
}

// GetUserByEmail fetches a user by exact email match. The service
// lowercases addresses, so no case folding happens here. It returns
// auth.ErrNotFound when no user matches.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*auth.User, error) {
	return s.findUser(ctx, bson.D{{Key: "email", Value: email}})
}

// findUser returns the first user matching filter.
func (s *Store) findUser(ctx context.Context, filter bson.D) (*auth.User, error) {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	var doc userDoc
	if err := s.users.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, mapError(err)
	}
	return docToUser(&doc), nil
}

// ListUsers returns users newest first (created_at descending, the ID
// descending as tie-breaker) with pagination; see normalizeListOptions
// for how Limit and Offset are interpreted. The result is an empty
// non-nil slice when nothing matches.
func (s *Store) ListUsers(ctx context.Context, opts auth.ListOptions) ([]*auth.User, error) {
	limit, offset := normalizeListOptions(opts)

	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	cursor, err := s.users.Find(ctx, bson.D{}, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(offset).
		SetLimit(limit))
	if err != nil {
		return nil, mapError(err)
	}
	var docs []userDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, mapError(err)
	}
	users := make([]*auth.User, 0, len(docs))
	for i := range docs {
		users = append(users, docToUser(&docs[i]))
	}
	return users, nil
}

// CountUsers returns the number of stored users.
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	count, err := s.users.CountDocuments(ctx, bson.D{})
	if err != nil {
		return 0, mapError(err)
	}
	return count, nil
}

// UpdateUser replaces the full user record with optimistic
// concurrency: the document is written only while its stored version
// still equals user.Version. On success the version is incremented and
// UpdatedAt set to the current time, both in the document and on the
// passed struct. It returns auth.ErrNotFound when no user has user.ID,
// auth.ErrConflict when the stored version differs (the document is
// left untouched) and auth.ErrDuplicate when the new email belongs to
// another user. The passed struct is only modified on success.
func (s *Store) UpdateUser(ctx context.Context, user *auth.User) error {
	doc := userToDoc(user)
	doc.UpdatedAt = storeNow()
	doc.Version = user.Version + 1

	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	if err := replaceVersioned(ctx, s.users, user.ID, user.Version, doc); err != nil {
		return err
	}
	user.UpdatedAt = doc.UpdatedAt
	user.Version = doc.Version
	return nil
}

// DeleteUser removes the user and cascades to its accounts and
// sessions. Verification tokens are not touched: the auth service
// deletes them itself because their identifiers are purpose prefixed.
// It returns auth.ErrNotFound when no user has the given ID, without
// touching any other collection.
//
// Transactions are deliberately not used: they require a replica set,
// and this store must also work on standalone servers. The deletes run
// in this order instead:
//
//  1. check that the user exists (auth.ErrNotFound otherwise)
//  2. delete the user's accounts
//  3. delete the user's sessions
//  4. delete the user itself, last
//
// The user is the anchor every dependent record references. Deleting
// it last means a cascade that fails halfway leaves the user in place,
// and simply calling DeleteUser again finishes the job (the bulk
// deletes of already cleaned collections are no-ops). Deleting it
// first could leave orphaned accounts and sessions behind.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	filter := bson.D{{Key: "_id", Value: id}}
	if err := s.users.FindOne(ctx, filter).Err(); err != nil {
		return mapError(err)
	}
	owned := bson.D{{Key: "user_id", Value: id}}
	if _, err := s.accounts.DeleteMany(ctx, owned); err != nil {
		return mapError(err)
	}
	if _, err := s.sessions.DeleteMany(ctx, owned); err != nil {
		return mapError(err)
	}
	result, err := s.users.DeleteOne(ctx, filter)
	if err != nil {
		return mapError(err)
	}
	if result.DeletedCount == 0 {
		// Deleted concurrently between the check and now.
		return auth.ErrNotFound
	}
	return nil
}
