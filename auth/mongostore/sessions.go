package mongostore

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/berkkaradalan/CoreGo/auth"
)

// CreateSession inserts session, filling in ID and timestamps on the
// passed struct when the caller left them zero. A duplicate token hash
// (or ID) yields auth.ErrDuplicate.
func (s *Store) CreateSession(ctx context.Context, session *auth.Session) error {
	session.ID = ensureID(session.ID)
	ensureCreateTimestamps(&session.CreatedAt, &session.UpdatedAt)

	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	_, err := s.sessions.InsertOne(ctx, sessionToDoc(session))
	return mapError(err)
}

// GetSession fetches a session by ID. It returns auth.ErrNotFound when
// no session has the given ID.
func (s *Store) GetSession(ctx context.Context, id string) (*auth.Session, error) {
	return s.findSession(ctx, bson.D{{Key: "_id", Value: id}})
}

// GetSessionByTokenHash resolves a session token hash. It returns
// auth.ErrNotFound when no session carries the hash.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*auth.Session, error) {
	return s.findSession(ctx, bson.D{{Key: "token_hash", Value: tokenHash}})
}

// findSession returns the first session matching filter.
func (s *Store) findSession(ctx context.Context, filter bson.D) (*auth.Session, error) {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	var doc sessionDoc
	if err := s.sessions.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, mapError(err)
	}
	return docToSession(&doc), nil
}

// ListSessionsByUser returns the user's sessions newest first
// (created_at descending, the ID descending as tie-breaker). The result
// is an empty non-nil slice when the user has no sessions.
func (s *Store) ListSessionsByUser(ctx context.Context, userID string) ([]*auth.Session, error) {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	cursor, err := s.sessions.Find(ctx, bson.D{{Key: "user_id", Value: userID}}, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}))
	if err != nil {
		return nil, mapError(err)
	}
	var docs []sessionDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, mapError(err)
	}
	sessions := make([]*auth.Session, 0, len(docs))
	for i := range docs {
		sessions = append(sessions, docToSession(&docs[i]))
	}
	return sessions, nil
}

// UpdateSession replaces the full session record and sets UpdatedAt to
// the current time (also on the passed struct). It returns
// auth.ErrNotFound when no session has session.ID and auth.ErrDuplicate
// when the new token hash belongs to another session.
func (s *Store) UpdateSession(ctx context.Context, session *auth.Session) error {
	session.UpdatedAt = storeNow()

	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	result, err := s.sessions.ReplaceOne(ctx, bson.D{{Key: "_id", Value: session.ID}}, sessionToDoc(session))
	if err != nil {
		return mapError(err)
	}
	if result.MatchedCount == 0 {
		return auth.ErrNotFound
	}
	return nil
}

// DeleteSession removes a session by ID. It returns auth.ErrNotFound
// when no session has the given ID.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	result, err := s.sessions.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return mapError(err)
	}
	if result.DeletedCount == 0 {
		return auth.ErrNotFound
	}
	return nil
}

// DeleteSessionsByUser removes every session of a user. It succeeds
// when the user has no sessions.
func (s *Store) DeleteSessionsByUser(ctx context.Context, userID string) error {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	_, err := s.sessions.DeleteMany(ctx, bson.D{{Key: "user_id", Value: userID}})
	return mapError(err)
}

// DeleteExpiredSessions removes every session whose ExpiresAt is
// strictly before the given instant and succeeds when none matches.
// The TTL index created by EnsureIndexes also removes expired sessions
// in the background; this method is the deterministic cleanup.
func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	_, err := s.sessions.DeleteMany(ctx, expiredBefore(before))
	return mapError(err)
}

// expiredBefore returns the filter matching records whose stored
// expires_at lies strictly before the given instant (see ceilMillis for
// why the bound is rounded up).
func expiredBefore(before time.Time) bson.D {
	return bson.D{{Key: "expires_at", Value: bson.D{{Key: "$lt", Value: ceilMillis(before)}}}}
}
