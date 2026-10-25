package mongostore

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoDB error codes for index definition conflicts: 85 is
// IndexOptionsConflict (same keys, different name or options) and 86 is
// IndexKeySpecsConflict (same name, different keys).
const (
	codeIndexOptionsConflict  = 85
	codeIndexKeySpecsConflict = 86
)

// indexKey is one field of an index key pattern; order is 1 for
// ascending and -1 for descending.
type indexKey struct {
	field string
	order int
}

// indexSpec describes one index EnsureIndexes maintains.
type indexSpec struct {
	collection string // prefixed collection name
	name       string
	keys       []indexKey
	unique     bool
	ttl        bool // expireAfterSeconds: 0 on a date field
}

// keysDocument returns the key pattern as an ordered document.
func (spec indexSpec) keysDocument() bson.D {
	keys := make(bson.D, 0, len(spec.keys))
	for _, k := range spec.keys {
		keys = append(keys, bson.E{Key: k.field, Value: k.order})
	}
	return keys
}

// model returns the driver model that creates the index.
func (spec indexSpec) model() mongo.IndexModel {
	opts := options.Index().SetName(spec.name)
	if spec.unique {
		opts.SetUnique(true)
	}
	if spec.ttl {
		opts.SetExpireAfterSeconds(0)
	}
	return mongo.IndexModel{Keys: spec.keysDocument(), Options: opts}
}

// indexSpecs lists every index of the store, grouped by collection in
// a stable order. Index names are scoped to their collection, so they
// do not carry the collection prefix.
func (s *Store) indexSpecs() []indexSpec {
	n := s.names
	return []indexSpec{
		{collection: n.users, name: "ux_users_email",
			keys: []indexKey{{"email", 1}}, unique: true},
		{collection: n.accounts, name: "ux_accounts_provider_provider_account_id",
			keys: []indexKey{{"provider", 1}, {"provider_account_id", 1}}, unique: true},
		{collection: n.accounts, name: "ux_accounts_user_id_provider",
			keys: []indexKey{{"user_id", 1}, {"provider", 1}}, unique: true},
		{collection: n.sessions, name: "ux_sessions_token_hash",
			keys: []indexKey{{"token_hash", 1}}, unique: true},
		{collection: n.sessions, name: "ix_sessions_user_id_created_at",
			keys: []indexKey{{"user_id", 1}, {"created_at", -1}}},
		{collection: n.sessions, name: "ttl_sessions_expires_at",
			keys: []indexKey{{"expires_at", 1}}, ttl: true},
		{collection: n.verificationTokens, name: "ux_verification_tokens_identifier_value_hash",
			keys: []indexKey{{"identifier", 1}, {"value_hash", 1}}, unique: true},
		{collection: n.verificationTokens, name: "ttl_verification_tokens_expires_at",
			keys: []indexKey{{"expires_at", 1}}, ttl: true},
	}
}

// EnsureIndexes creates the indexes the store relies on. Call it once
// at startup (the corego facade does); it is idempotent and safe to run
// from several application instances at once.
//
//	{prefix}users               ux_users_email                               (email) unique
//	{prefix}accounts            ux_accounts_provider_provider_account_id     (provider, provider_account_id) unique
//	{prefix}accounts            ux_accounts_user_id_provider                 (user_id, provider) unique
//	{prefix}sessions            ux_sessions_token_hash                       (token_hash) unique
//	{prefix}sessions            ix_sessions_user_id_created_at               (user_id, created_at desc)
//	{prefix}sessions            ttl_sessions_expires_at                      (expires_at) TTL, expireAfterSeconds 0
//	{prefix}verification_tokens ux_verification_tokens_identifier_value_hash (identifier, value_hash) unique
//	{prefix}verification_tokens ttl_verification_tokens_expires_at           (expires_at) TTL, expireAfterSeconds 0
//
// The unique indexes enforce the duplicate detection of the auth.Store
// contract (auth.ErrDuplicate); without them duplicates are silently
// accepted. The TTL indexes let MongoDB delete expired sessions and
// tokens in the background (its TTL monitor runs about once a minute),
// so the collections stay small even when auth.Auth.CleanupExpired is
// never called.
//
// Re-running EnsureIndexes when the indexes exist is a no-op. When an
// index with one of these names or key patterns already exists with a
// different definition, EnsureIndexes accepts it as long as it still
// provides the required guarantee (an index on the same keys that is
// unique, or has an expireAfterSeconds value, where required); a TTL
// index whose expireAfterSeconds was tuned is therefore kept.
// Otherwise it returns an error naming the collection and index, and
// the conflicting index must be dropped by hand. A duplicate key error
// while building a unique index over existing duplicate documents is
// returned as well: that is a data problem to resolve first.
//
// EnsureIndexes runs with the caller's context and no extra timeout,
// because building an index over a large existing collection can take
// a while.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	collections := map[string]*mongo.Collection{
		s.names.users:              s.users,
		s.names.accounts:           s.accounts,
		s.names.sessions:           s.sessions,
		s.names.verificationTokens: s.verificationTokens,
	}
	specs := s.indexSpecs()
	for start := 0; start < len(specs); {
		end := start
		for end < len(specs) && specs[end].collection == specs[start].collection {
			end++
		}
		if err := ensureCollectionIndexes(ctx, collections[specs[start].collection], specs[start:end]); err != nil {
			return err
		}
		start = end
	}
	return nil
}

// ensureCollectionIndexes creates the indexes of one collection in a
// single round trip. When the server reports a definition conflict it
// falls back to creating and checking the indexes one by one.
func ensureCollectionIndexes(ctx context.Context, coll *mongo.Collection, specs []indexSpec) error {
	models := make([]mongo.IndexModel, 0, len(specs))
	for _, spec := range specs {
		models = append(models, spec.model())
	}
	_, err := coll.Indexes().CreateMany(ctx, models)
	if err == nil {
		return nil
	}
	if !isIndexConflict(err) {
		return fmt.Errorf("mongostore: create indexes on %q: %w", coll.Name(), err)
	}
	for _, spec := range specs {
		if err := ensureIndex(ctx, coll, spec); err != nil {
			return err
		}
	}
	return nil
}

// ensureIndex creates one index. On a definition conflict it accepts
// an existing index that satisfies spec (see indexSatisfies) and
// reports an error otherwise.
func ensureIndex(ctx context.Context, coll *mongo.Collection, spec indexSpec) error {
	_, err := coll.Indexes().CreateOne(ctx, spec.model())
	if err == nil {
		return nil
	}
	if !isIndexConflict(err) {
		return fmt.Errorf("mongostore: create index %q on %q: %w", spec.name, coll.Name(), err)
	}
	existing, listErr := coll.Indexes().ListSpecifications(ctx)
	if listErr != nil {
		return fmt.Errorf("mongostore: list indexes on %q: %w", coll.Name(), listErr)
	}
	for _, index := range existing {
		if indexSatisfies(index, spec) {
			return nil
		}
	}
	return fmt.Errorf("mongostore: collection %q has an index that conflicts with %q and does not provide the same guarantee; drop it and run EnsureIndexes again: %w",
		coll.Name(), spec.name, err)
}

// indexSatisfies reports whether an existing index provides everything
// spec requires: the same key pattern (field order and direction), the
// unique flag when spec is unique and an expireAfterSeconds value when
// spec is a TTL index.
func indexSatisfies(index mongo.IndexSpecification, spec indexSpec) bool {
	if !sameKeys(index.KeysDocument, spec.keys) {
		return false
	}
	if spec.unique && (index.Unique == nil || !*index.Unique) {
		return false
	}
	if spec.ttl && index.ExpireAfterSeconds == nil {
		return false
	}
	return true
}

// sameKeys reports whether an index key document lists exactly the
// given fields, in order, with the same directions. Directions may be
// stored as any numeric BSON type.
func sameKeys(doc bson.Raw, want []indexKey) bool {
	elems, err := doc.Elements()
	if err != nil || len(elems) != len(want) {
		return false
	}
	for i, elem := range elems {
		if elem.Key() != want[i].field {
			return false
		}
		order, ok := elem.Value().AsFloat64OK()
		if !ok || order != float64(want[i].order) {
			return false
		}
	}
	return true
}

// isIndexConflict reports whether err is a server error with an index
// definition conflict code (85 or 86), as returned when an index with
// the same name or keys already exists with a different definition.
func isIndexConflict(err error) bool {
	var serverErr mongo.ServerError
	if !errors.As(err, &serverErr) {
		return false
	}
	return serverErr.HasErrorCode(codeIndexOptionsConflict) ||
		serverErr.HasErrorCode(codeIndexKeySpecsConflict)
}
