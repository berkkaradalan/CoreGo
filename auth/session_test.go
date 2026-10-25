package auth

import (
	"errors"
	"testing"
	"time"
)

func TestGetSessionValid(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUp := signUpUser(t, a, "session@example.com")

	session, user, err := a.GetSession(testContext, signUp.SessionToken)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.ID != signUp.Session.ID {
		t.Errorf("session ID = %q, want %q", session.ID, signUp.Session.ID)
	}
	if user.ID != signUp.User.ID {
		t.Errorf("user ID = %q, want %q", user.ID, signUp.User.ID)
	}
}

func TestGetSessionUnknownToken(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	session, user, err := a.GetSession(testContext, "no-such-token")
	if session != nil || user != nil {
		t.Fatal("GetSession returned records for an unknown token")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
}

func TestGetSessionExpiredIsDeleted(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	signUp := signUpUser(t, a, "expired@example.com")

	// Still valid right before expiry.
	clock.Advance(DefaultSessionTTL - time.Second)
	if _, _, err := a.GetSession(testContext, signUp.SessionToken); err != nil {
		t.Fatalf("GetSession before expiry: %v", err)
	}

	clock.Advance(2 * time.Second)
	_, _, err := a.GetSession(testContext, signUp.SessionToken)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want ErrSessionExpired", err)
	}

	// The expired session must have been removed from the store.
	if _, err := a.store.GetSessionByTokenHash(testContext, HashToken(signUp.SessionToken)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session still in store: %v", err)
	}
	// A second lookup now reports "not found".
	if _, _, err := a.GetSession(testContext, signUp.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("second GetSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestGetSessionBannedUser(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUp := signUpUser(t, a, "ban-session@example.com")

	// Flip the ban flag directly in the store so the session is not
	// revoked by BanUser; GetSession must still refuse.
	user := signUp.User
	user.Banned = true
	if err := a.store.UpdateUser(testContext, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	_, _, err := a.GetSession(testContext, signUp.SessionToken)
	if !errors.Is(err, ErrUserBanned) {
		t.Fatalf("error = %v, want ErrUserBanned", err)
	}
}

func TestSignOut(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUp := signUpUser(t, a, "signout@example.com")

	if err := a.SignOut(testContext, signUp.SessionToken); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if _, _, err := a.GetSession(testContext, signUp.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession after SignOut error = %v, want ErrSessionNotFound", err)
	}

	// Idempotent: repeating SignOut and using unknown tokens is fine.
	if err := a.SignOut(testContext, signUp.SessionToken); err != nil {
		t.Errorf("second SignOut = %v, want nil", err)
	}
	if err := a.SignOut(testContext, "unknown-token"); err != nil {
		t.Errorf("SignOut(unknown) = %v, want nil", err)
	}
	if err := a.SignOut(testContext, ""); err != nil {
		t.Errorf("SignOut(empty) = %v, want nil", err)
	}
}

func TestListSessions(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	signUp := signUpUser(t, a, "list@example.com")

	clock.Advance(time.Minute)
	first, err := a.SignIn(testContext, SignInInput{Email: "list@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	clock.Advance(time.Minute)
	second, err := a.SignIn(testContext, SignInInput{Email: "list@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	sessions, err := a.ListSessions(testContext, signUp.User.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("len(sessions) = %d, want 3", len(sessions))
	}
	wantOrder := []string{second.Session.ID, first.Session.ID, signUp.Session.ID}
	for i, want := range wantOrder {
		if sessions[i].ID != want {
			t.Errorf("sessions[%d].ID = %q, want %q (newest first)", i, sessions[i].ID, want)
		}
	}

	// A user without sessions gets an empty non-nil slice.
	empty, err := a.ListSessions(testContext, "nobody")
	if err != nil {
		t.Fatalf("ListSessions(nobody): %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("ListSessions(nobody) = %v, want empty non-nil", empty)
	}
}

func TestRevokeSessionOwnership(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	alice := signUpUser(t, a, "alice@example.com")
	bob := signUpUser(t, a, "bob@example.com")

	// Bob cannot revoke Alice's session and must see ErrSessionNotFound.
	if err := a.RevokeSession(testContext, bob.User.ID, alice.Session.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("RevokeSession(other owner) error = %v, want ErrSessionNotFound", err)
	}
	if _, _, err := a.GetSession(testContext, alice.SessionToken); err != nil {
		t.Errorf("Alice's session was revoked through RevokeSession by another user: %v", err)
	}

	// The owner can.
	if err := a.RevokeSession(testContext, alice.User.ID, alice.Session.ID); err != nil {
		t.Fatalf("RevokeSession(owner): %v", err)
	}
	if _, _, err := a.GetSession(testContext, alice.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession after revoke error = %v, want ErrSessionNotFound", err)
	}

	// Unknown session IDs map to ErrSessionNotFound.
	if err := a.RevokeSession(testContext, alice.User.ID, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("RevokeSession(missing) error = %v, want ErrSessionNotFound", err)
	}
}

func TestRevokeAllSessions(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	first := signUpUser(t, a, "all@example.com")
	if _, err := a.SignIn(testContext, SignInInput{Email: "all@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "all@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	sessions, _ := a.ListSessions(testContext, first.User.ID)
	if len(sessions) != 3 {
		t.Fatalf("len(sessions) = %d before revoke, want 3", len(sessions))
	}
	if err := a.RevokeAllSessions(testContext, first.User.ID); err != nil {
		t.Fatalf("RevokeAllSessions: %v", err)
	}
	sessions, _ = a.ListSessions(testContext, first.User.ID)
	if len(sessions) != 0 {
		t.Errorf("len(sessions) = %d after revoke, want 0", len(sessions))
	}
	if _, _, err := a.GetSession(testContext, first.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession after RevokeAllSessions error = %v", err)
	}
}

func TestRevokeOtherSessions(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	first := signUpUser(t, a, "others@example.com")

	clock.Advance(time.Minute)
	if _, err := a.SignIn(testContext, SignInInput{Email: "others@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	clock.Advance(time.Minute)
	keep, err := a.SignIn(testContext, SignInInput{Email: "others@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if err := a.RevokeOtherSessions(testContext, first.User.ID, keep.Session.ID); err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}

	sessions, _ := a.ListSessions(testContext, first.User.ID)
	if len(sessions) != 1 || sessions[0].ID != keep.Session.ID {
		t.Fatalf("remaining sessions = %+v, want only %q", sessions, keep.Session.ID)
	}
	if _, _, err := a.GetSession(testContext, keep.SessionToken); err != nil {
		t.Errorf("kept session is not usable: %v", err)
	}
	if _, _, err := a.GetSession(testContext, first.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("old session not revoked: %v", err)
	}
}

func TestGetSessionEmptyToken(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "empty-token@example.com")
	if _, _, err := a.GetSession(testContext, ""); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
}

// TestGetSessionRequireVerifiedEmail covers sessions that exist while
// the address is unverified, for example sessions created before the
// requirement was switched on or before an email change.
func TestGetSessionRequireVerifiedEmail(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "late-gate@example.com")

	a.cfg.RequireVerifiedEmail = true
	session, user, err := a.GetSession(testContext, out.SessionToken)
	if session != nil || user != nil {
		t.Error("GetSession returned records for an unverified user")
	}
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("error = %v, want ErrEmailNotVerified", err)
	}
	// The session is kept, not deleted: it works again once verified.
	if got := sessionCount(memStore(t, a)); got != 1 {
		t.Fatalf("sessionCount = %d, want 1", got)
	}
	if err := a.VerifyEmail(testContext, "late-gate@example.com", lastVerificationToken(t, a, mailer)); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Fatalf("GetSession after verification: %v", err)
	}
}

func TestGetSessionBanCheckedBeforeVerification(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "banned-unverified@example.com")
	user := out.User
	user.Banned = true
	if err := a.store.UpdateUser(testContext, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	a.cfg.RequireVerifiedEmail = true
	if _, _, err := a.GetSession(testContext, out.SessionToken); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("error = %v, want ErrUserBanned", err)
	}
}

func TestGetSessionOrphanedSessionIsDeleted(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	store := memStore(t, a)
	raw := "orphan-session-token"
	if err := store.CreateSession(testContext, &Session{
		UserID:    "deleted-user",
		TokenHash: HashToken(raw),
		ExpiresAt: clock.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, _, err := a.GetSession(testContext, raw); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
	if got := sessionCount(store); got != 0 {
		t.Errorf("orphaned session kept: sessionCount = %d", got)
	}
}

func TestGetSessionValidAtExpiryInstant(t *testing.T) {
	a, clock := newTestAuth(t, Config{SessionTTL: time.Hour})
	out := signUpUser(t, a, "instant@example.com")

	clock.Advance(time.Hour)
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Fatalf("session rejected at its expiry instant: %v", err)
	}
	clock.Advance(time.Nanosecond)
	if _, _, err := a.GetSession(testContext, out.SessionToken); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want ErrSessionExpired", err)
	}
}
