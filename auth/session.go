package auth

import (
	"context"
	"errors"
)

// createSession creates and persists a session for userID and returns
// the raw token together with the stored record.
func (a *Auth) createSession(ctx context.Context, userID string, rememberMe bool, ipAddress, userAgent string) (string, *Session, error) {
	token, err := NewToken()
	if err != nil {
		return "", nil, err
	}
	ttl := a.cfg.SessionTTL
	if rememberMe {
		ttl = a.cfg.RememberMeTTL
	}
	now := a.now()
	session := &Session{
		UserID:    userID,
		TokenHash: HashToken(token),
		ExpiresAt: now.Add(ttl),
		IPAddress: ipAddress,
		UserAgent: userAgent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.store.CreateSession(ctx, session); err != nil {
		return "", nil, err
	}
	return token, session, nil
}

// SignOut invalidates the session identified by sessionToken. It is
// idempotent: unknown or already invalidated tokens return nil.
func (a *Auth) SignOut(ctx context.Context, sessionToken string) error {
	session, err := a.store.GetSessionByTokenHash(ctx, HashToken(sessionToken))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return a.deleteSession(ctx, session.ID)
}

// GetSession resolves sessionToken to its session and user. A session
// is valid through its ExpiresAt.
//
// Errors:
//   - ErrSessionNotFound: empty or unknown token, or a session whose
//     user no longer exists (the orphaned session is deleted).
//   - ErrSessionExpired: the session has expired; it is deleted, so a
//     second lookup reports ErrSessionNotFound.
//   - ErrUserBanned: the user is currently banned.
//   - ErrEmailNotVerified: Config.RequireVerifiedEmail is enabled and
//     the address is unverified, for example because the user changed
//     it after signing in.
func (a *Auth) GetSession(ctx context.Context, sessionToken string) (*Session, *User, error) {
	if sessionToken == "" {
		return nil, nil, ErrSessionNotFound
	}
	session, err := a.store.GetSessionByTokenHash(ctx, HashToken(sessionToken))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil, ErrSessionNotFound
		}
		return nil, nil, err
	}
	now := a.now()
	if now.After(session.ExpiresAt) {
		// Lazily remove the expired session; a failure to delete must
		// not hide the expiry from the caller.
		if err := a.deleteSession(ctx, session.ID); err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrSessionExpired
	}
	user, err := a.store.GetUser(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if err := a.deleteSession(ctx, session.ID); err != nil {
				return nil, nil, err
			}
			return nil, nil, ErrSessionNotFound
		}
		return nil, nil, err
	}
	if user.IsBannedAt(now) {
		return nil, nil, ErrUserBanned
	}
	if a.cfg.RequireVerifiedEmail && !user.EmailVerified {
		return nil, nil, ErrEmailNotVerified
	}
	return session, user, nil
}

// deleteSession removes one session; a session that is already gone
// counts as deleted.
func (a *Auth) deleteSession(ctx context.Context, id string) error {
	if err := a.store.DeleteSession(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// ListSessions returns all sessions of userID, newest first. Expired
// sessions are included until they are lazily deleted by GetSession or
// removed by CleanupExpired.
func (a *Auth) ListSessions(ctx context.Context, userID string) ([]*Session, error) {
	sessions, err := a.store.ListSessionsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if sessions == nil {
		sessions = []*Session{}
	}
	return sessions, nil
}

// RevokeSession deletes the session with the given ID, but only when
// it belongs to userID. A session that does not exist or belongs to
// another user yields ErrSessionNotFound, so the method cannot be used
// to probe other users' sessions.
func (a *Auth) RevokeSession(ctx context.Context, userID, sessionID string) error {
	session, err := a.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrSessionNotFound
		}
		return err
	}
	if session.UserID != userID {
		return ErrSessionNotFound
	}
	return a.deleteSession(ctx, sessionID)
}

// RevokeAllSessions deletes every session of userID. It succeeds when
// the user has no sessions.
func (a *Auth) RevokeAllSessions(ctx context.Context, userID string) error {
	return a.store.DeleteSessionsByUser(ctx, userID)
}

// RevokeOtherSessions deletes every session of userID except the one
// with the given ID: the "sign out everywhere else" operation.
func (a *Auth) RevokeOtherSessions(ctx context.Context, userID, keepSessionID string) error {
	sessions, err := a.store.ListSessionsByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.ID == keepSessionID {
			continue
		}
		if err := a.deleteSession(ctx, session.ID); err != nil {
			return err
		}
	}
	return nil
}
