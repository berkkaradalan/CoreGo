package auth

import (
	"context"
	"errors"
	"time"
)

// ListUsers returns users page by page, newest first. opts follows the
// ListOptions contract: zero Limit selects the store default and
// negative Offset is treated as zero.
func (a *Auth) ListUsers(ctx context.Context, opts ListOptions) ([]*User, error) {
	users, err := a.store.ListUsers(ctx, opts)
	if err != nil {
		return nil, err
	}
	if users == nil {
		users = []*User{}
	}
	return users, nil
}

// CountUsers returns the total number of stored users.
func (a *Auth) CountUsers(ctx context.Context) (int64, error) {
	return a.store.CountUsers(ctx)
}

// BanUser marks the user with the given ID as banned, records the
// reason and optional expiry, and immediately revokes every session so
// the user is signed out everywhere.
//
// The ban is written with optimistic concurrency: a concurrent change
// of the same user (for example a profile update) can never silently
// revert it. When the user keeps changing underneath, ErrConflict is
// returned after a few retries.
//
// A nil expires bans permanently. A ban only blocks the user while
// User.IsBannedAt reports true for the service clock, so an expiry in
// the past records the ban without blocking anyone. Unknown users
// yield ErrUserNotFound.
func (a *Auth) BanUser(ctx context.Context, userID, reason string, expires *time.Time) error {
	_, err := a.updateUser(ctx, userID, func(user *User) (bool, error) {
		user.Banned = true
		user.BanReason = reason
		if expires != nil {
			// Copy so the caller cannot mutate stored state through the
			// pointer.
			value := *expires
			user.BanExpires = &value
		} else {
			user.BanExpires = nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	return a.store.DeleteSessionsByUser(ctx, userID)
}

// UnbanUser clears every ban field (Banned, BanReason, BanExpires) of
// the user with the given ID. Sessions are not restored; the user
// signs in again. Unknown users yield ErrUserNotFound; ErrConflict is
// returned when concurrent changes win every retry (see BanUser).
func (a *Auth) UnbanUser(ctx context.Context, userID string) error {
	_, err := a.updateUser(ctx, userID, func(user *User) (bool, error) {
		user.Banned = false
		user.BanReason = ""
		user.BanExpires = nil
		return true, nil
	})
	return err
}

// CleanupExpired removes every verification and password reset token
// and every session whose expiry lies before now. Expired records are
// already useless (the service rejects them and deletes them lazily
// when presented); this keeps the store from growing. It is idempotent
// and safe to run periodically, for example once an hour:
//
//	go func() {
//		ticker := time.NewTicker(time.Hour)
//		defer ticker.Stop()
//		for range ticker.C {
//			if err := svc.CleanupExpired(context.Background()); err != nil {
//				log.Printf("auth cleanup: %v", err)
//			}
//		}
//	}()
//
// Both cleanups always run; their errors are joined.
func (a *Auth) CleanupExpired(ctx context.Context) error {
	now := a.now()
	return errors.Join(
		a.store.DeleteExpiredVerificationTokens(ctx, now),
		a.store.DeleteExpiredSessions(ctx, now),
	)
}
