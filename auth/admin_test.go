package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestListUsers(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	for i := 0; i < 3; i++ {
		clock.Advance(time.Duration(i) * time.Hour) // distinct timestamps
		signUpUser(t, a, fmt.Sprintf("admin%d@example.com", i))
	}

	users, err := a.ListUsers(testContext, ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("len(users) = %d, want 3", len(users))
	}
	if users[0].Email != "admin2@example.com" {
		t.Errorf("users[0].Email = %q, want the newest user first", users[0].Email)
	}

	page, err := a.ListUsers(testContext, ListOptions{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("ListUsers(paged): %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("len(page) = %d, want 2", len(page))
	}
	if page[0].Email != "admin1@example.com" || page[1].Email != "admin0@example.com" {
		t.Errorf("page = [%s, %s]", page[0].Email, page[1].Email)
	}
}

func TestCountUsers(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	if n, err := a.CountUsers(testContext); err != nil || n != 0 {
		t.Fatalf("CountUsers on empty = (%d, %v), want (0, nil)", n, err)
	}
	signUpUser(t, a, "count1@example.com")
	signUpUser(t, a, "count2@example.com")
	if n, err := a.CountUsers(testContext); err != nil || n != 2 {
		t.Fatalf("CountUsers = (%d, %v), want (2, nil)", n, err)
	}
}

func TestBanUser(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	out := signUpUser(t, a, "banme@example.com")
	if _, err := a.SignIn(testContext, SignInInput{Email: "banme@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	expires := clock.Now().Add(48 * time.Hour)
	if err := a.BanUser(testContext, out.User.ID, "terms of service violation", &expires); err != nil {
		t.Fatalf("BanUser: %v", err)
	}

	user, err := a.GetUser(testContext, out.User.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !user.Banned || user.BanReason != "terms of service violation" {
		t.Errorf("ban fields = %+v", user)
	}
	if user.BanExpires == nil || !user.BanExpires.Equal(expires) {
		t.Errorf("BanExpires = %v, want %v", user.BanExpires, expires)
	}

	// Mutating the caller's time value after the call must not affect
	// the stored ban.
	expires = clock.Now().Add(96 * time.Hour)
	user, _ = a.GetUser(testContext, out.User.ID)
	if user.BanExpires.Equal(expires) {
		t.Error("caller's expiry value leaked into the stored ban")
	}

	// Every session was revoked.
	sessions, _ := a.ListSessions(testContext, out.User.ID)
	if len(sessions) != 0 {
		t.Errorf("sessions survived the ban: %d left", len(sessions))
	}
	// Sign-in is blocked.
	if _, err := a.SignIn(testContext, SignInInput{Email: "banme@example.com", Password: testPassword}); !errors.Is(err, ErrUserBanned) {
		t.Errorf("SignIn error = %v, want ErrUserBanned", err)
	}
}

func TestBanUserPermanent(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "permanent@example.com")

	if err := a.BanUser(testContext, out.User.ID, "permanent", nil); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	user, _ := a.GetUser(testContext, out.User.ID)
	if !user.IsBanned() {
		t.Error("a ban without expiry does not count as banned")
	}
	if user.BanExpires != nil {
		t.Errorf("BanExpires = %v, want nil for a permanent ban", user.BanExpires)
	}
}

func TestBanUserUnknown(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	if err := a.BanUser(testContext, "missing", "reason", nil); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}

func TestUnbanUser(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	out := signUpUser(t, a, "unban@example.com")

	expires := clock.Now().Add(time.Hour)
	if err := a.BanUser(testContext, out.User.ID, "temporary", &expires); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	if err := a.UnbanUser(testContext, out.User.ID); err != nil {
		t.Fatalf("UnbanUser: %v", err)
	}

	user, err := a.GetUser(testContext, out.User.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Banned || user.BanReason != "" || user.BanExpires != nil {
		t.Errorf("ban fields not cleared: %+v", user)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "unban@example.com", Password: testPassword}); err != nil {
		t.Errorf("SignIn after unban: %v", err)
	}
	if err := a.UnbanUser(testContext, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("UnbanUser(missing) error = %v, want ErrUserNotFound", err)
	}
}

func TestExpiredBanIsIgnored(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	out := signUpUser(t, a, "lapsed@example.com")

	past := clock.Now().Add(-time.Minute)
	if err := a.BanUser(testContext, out.User.ID, "already over", &past); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	user, _ := a.GetUser(testContext, out.User.ID)
	if user.IsBannedAt(clock.Now()) {
		t.Error("an expired ban still counts as banned")
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "lapsed@example.com", Password: testPassword}); err != nil {
		t.Errorf("SignIn with an expired ban: %v", err)
	}
}

// TestBanExpiryUsesServiceClock is the regression test for IsBanned
// reading the wall clock: the service must judge bans by its own
// clock. The fake clock (2026-03-01) lies before the real present, so
// a ban expiring one hour after the fake now has already lapsed by
// wall-clock time but is still active for the service.
func TestBanExpiryUsesServiceClock(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	out := signUpUser(t, a, "clock-ban@example.com")
	expires := clock.Now().Add(time.Hour)
	if err := a.BanUser(testContext, out.User.ID, "cool-off", &expires); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	user, _ := a.GetUser(testContext, out.User.ID)
	if user.IsBanned() {
		t.Fatal("test premise broken: the ban should have lapsed by wall-clock time")
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "clock-ban@example.com", Password: testPassword}); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("SignIn during the ban (service clock) error = %v, want ErrUserBanned", err)
	}

	// GetSession honors the service clock too. BanUser revoked the
	// sessions, so flip the flag directly to keep one alive.
	again := signUpUser(t, a, "clock-ban-session@example.com")
	stored, _ := a.store.GetUser(testContext, again.User.ID)
	stored.Banned = true
	stored.BanExpires = &expires
	if err := a.store.UpdateUser(testContext, stored); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, _, err := a.GetSession(testContext, again.SessionToken); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("GetSession during the ban error = %v, want ErrUserBanned", err)
	}

	// Exactly at the expiry the ban has lapsed.
	clock.Advance(time.Hour)
	if _, err := a.SignIn(testContext, SignInInput{Email: "clock-ban@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn at the ban expiry: %v", err)
	}
	if _, _, err := a.GetSession(testContext, again.SessionToken); err != nil {
		t.Fatalf("GetSession at the ban expiry: %v", err)
	}
}

func TestCleanupExpired(t *testing.T) {
	a, clock, mailer := newTestAuthWithMailer(t, Config{PasswordResetTokenTTL: time.Minute, SessionTTL: 2 * time.Hour})
	out := signUpUser(t, a, "cleanup@example.com")
	store := memStore(t, a)

	// The signup token, one explicit verification token and one reset
	// token; then let them all expire by advancing the clock beyond
	// every TTL involved (the verification tokens use the 24h default).
	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	if err := a.RequestPasswordReset(testContext, "cleanup@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	waitForDeliveries(t, a)
	if got := tokenCount(store); got != 3 {
		t.Fatalf("tokenCount = %d before cleanup, want 3", got)
	}
	// A remember-me session outlives the cleanup.
	long, err := a.SignIn(testContext, SignInInput{Email: "cleanup@example.com", Password: testPassword, RememberMe: true})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got := sessionCount(store); got != 2 {
		t.Fatalf("sessionCount = %d, want 2", got)
	}

	clock.Advance(25 * time.Hour) // beyond every TTL except remember me
	if err := a.CleanupExpired(testContext); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if got := tokenCount(store); got != 0 {
		t.Errorf("tokenCount = %d after cleanup, want 0", got)
	}
	if got := sessionCount(store); got != 1 {
		t.Errorf("sessionCount = %d after cleanup, want 1", got)
	}
	if _, _, err := a.GetSession(testContext, long.SessionToken); err != nil {
		t.Errorf("remember-me session removed by cleanup: %v", err)
	}

	// Fresh tokens survive cleanup, and cleanup is idempotent.
	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := a.CleanupExpired(testContext); err != nil {
			t.Fatalf("CleanupExpired: %v", err)
		}
	}
	if got := tokenCount(store); got != 1 {
		t.Errorf("tokenCount = %d, want 1 (fresh token kept)", got)
	}
	if calls := mailer.verificationCalls(); len(calls) != 3 {
		t.Errorf("verification calls = %d, want 3", len(calls))
	}
}

func TestCleanupExpiredBoundary(t *testing.T) {
	a, clock := newTestAuth(t, Config{SessionTTL: time.Hour})
	out := signUpUser(t, a, "boundary@example.com")

	// At the exact expiry instant the session is still valid and must
	// survive cleanup.
	clock.Advance(time.Hour)
	if err := a.CleanupExpired(testContext); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Fatalf("session removed at its expiry instant: %v", err)
	}
	clock.Advance(time.Nanosecond)
	if err := a.CleanupExpired(testContext); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if got := sessionCount(memStore(t, a)); got != 0 {
		t.Errorf("sessionCount = %d, want 0", got)
	}
}

func TestCleanupExpiredRunsBothAndJoinsErrors(t *testing.T) {
	errTokens := errors.New("token cleanup failed")
	errSessions := errors.New("session cleanup failed")

	t.Run("both fail", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		a.store = &joinedFailureStore{Store: a.store, tokensErr: errTokens, sessionsErr: errSessions}
		err := a.CleanupExpired(testContext)
		if !errors.Is(err, errTokens) || !errors.Is(err, errSessions) {
			t.Fatalf("error = %v, want both failures joined", err)
		}
	})
	t.Run("token cleanup fails, sessions still cleaned", func(t *testing.T) {
		a, clock := newTestAuth(t, Config{SessionTTL: time.Minute})
		signUpUser(t, a, "half@example.com")
		store := memStore(t, a)
		a.store = &faultyStore{Store: store, err: errTokens, deleteExpiredTokens: true}
		clock.Advance(time.Hour)
		if err := a.CleanupExpired(testContext); !errors.Is(err, errTokens) {
			t.Fatalf("error = %v, want the token failure", err)
		}
		if got := sessionCount(store); got != 0 {
			t.Errorf("sessionCount = %d, want 0 (session cleanup must still run)", got)
		}
	})
}

// joinedFailureStore fails both expiry cleanups with distinct errors.
type joinedFailureStore struct {
	Store
	tokensErr   error
	sessionsErr error
}

// DeleteExpiredVerificationTokens implements Store.
func (s *joinedFailureStore) DeleteExpiredVerificationTokens(context.Context, time.Time) error {
	return s.tokensErr
}

// DeleteExpiredSessions implements Store.
func (s *joinedFailureStore) DeleteExpiredSessions(context.Context, time.Time) error {
	return s.sessionsErr
}
