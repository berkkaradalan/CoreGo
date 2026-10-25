package mongostore

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/berkkaradalan/CoreGo/auth"
)

// CreateVerificationToken inserts token, filling in ID and CreatedAt on
// the passed struct when the caller left them zero; a CreatedAt set by
// the caller (the auth service always sets one) is persisted as is, at
// millisecond precision. A duplicate (identifier, value_hash) pair (or
// ID) yields auth.ErrDuplicate.
func (s *Store) CreateVerificationToken(ctx context.Context, token *auth.VerificationToken) error {
	token.ID = ensureID(token.ID)
	ensureCreateTimestamps(&token.CreatedAt, nil)

	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	_, err := s.verificationTokens.InsertOne(ctx, tokenToDoc(token))
	return mapError(err)
}

// GetVerificationToken fetches a token by its exact identifier and
// value hash. It returns auth.ErrNotFound when the pair does not match
// a stored token.
func (s *Store) GetVerificationToken(ctx context.Context, identifier, valueHash string) (*auth.VerificationToken, error) {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	var doc verificationTokenDoc
	err := s.verificationTokens.FindOne(ctx, bson.D{
		{Key: "identifier", Value: identifier},
		{Key: "value_hash", Value: valueHash},
	}).Decode(&doc)
	if err != nil {
		return nil, mapError(err)
	}
	return docToToken(&doc), nil
}

// DeleteVerificationToken removes a token by ID. It returns
// auth.ErrNotFound when no token has the given ID.
func (s *Store) DeleteVerificationToken(ctx context.Context, id string) error {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	result, err := s.verificationTokens.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return mapError(err)
	}
	if result.DeletedCount == 0 {
		return auth.ErrNotFound
	}
	return nil
}

// DeleteVerificationTokensByIdentifier removes every token whose
// identifier matches exactly (no prefix or case-insensitive matching)
// and succeeds when none matches.
func (s *Store) DeleteVerificationTokensByIdentifier(ctx context.Context, identifier string) error {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	_, err := s.verificationTokens.DeleteMany(ctx, bson.D{{Key: "identifier", Value: identifier}})
	return mapError(err)
}

// DeleteExpiredVerificationTokens removes every token whose ExpiresAt
// is strictly before the given instant and succeeds when none matches.
// The TTL index created by EnsureIndexes also removes expired tokens in
// the background; this method is the deterministic cleanup.
func (s *Store) DeleteExpiredVerificationTokens(ctx context.Context, before time.Time) error {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	_, err := s.verificationTokens.DeleteMany(ctx, expiredBefore(before))
	return mapError(err)
}
