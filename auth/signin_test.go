package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSignInHappyPath(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	signUpUser(t, a, "alice@example.com")
	clock.Advance(time.Minute)

	out, err := a.SignIn(testContext, SignInInput{
		Email:     "  ALICE@example.com ",
		Password:  testPassword,
		IPAddress: "198.51.100.7",
		UserAgent: "sign-in-agent/2.0",
	})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if out.User.Email != "alice@example.com" {
		t.Errorf("User.Email = %q", out.User.Email)
	}
	if len(out.SessionToken) != 43 {
		t.Errorf("len(SessionToken) = %d, want 43", len(out.SessionToken))
	}
	if out.Session.UserID != out.User.ID {
		t.Errorf("session UserID = %q, want %q", out.Session.UserID, out.User.ID)
	}
	if got := out.Session.ExpiresAt.Sub(out.Session.CreatedAt); got != DefaultSessionTTL {
		t.Errorf("session lifetime = %v, want %v", got, DefaultSessionTTL)
	}
	if out.Session.IPAddress != "198.51.100.7" || out.Session.UserAgent != "sign-in-agent/2.0" {
		t.Errorf("session metadata = %+v", out.Session)
	}
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Errorf("GetSession with the fresh token: %v", err)
	}
}

func TestSignInRememberMeUsesExtendedTTL(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "remember@example.com")

	out, err := a.SignIn(testContext, SignInInput{Email: "remember@example.com", Password: testPassword, RememberMe: true})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got := out.Session.ExpiresAt.Sub(out.Session.CreatedAt); got != DefaultRememberMeTTL {
		t.Errorf("session lifetime = %v, want %v", got, DefaultRememberMeTTL)
	}
}

func TestSignInWrongPassword(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "alice@example.com")

	out, err := a.SignIn(testContext, SignInInput{Email: "alice@example.com", Password: "wrong-password"})
	if out != nil {
		t.Fatal("SignIn returned output on failure")
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}

func TestSignInUnknownEmailRunsDummyCompare(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	count, restore := withDummyCompareCounter()
	defer restore()

	out, err := a.SignIn(testContext, SignInInput{Email: "ghost@example.com", Password: testPassword})
	if out != nil {
		t.Fatal("SignIn returned output on failure")
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
	if got := count.Load(); got != 1 {
		t.Errorf("dummy compare ran %d times, want exactly 1", got)
	}
}

func TestSignInMissingCredentialAccountRunsDummyCompare(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	// A user record without a credential account.
	if err := a.store.CreateUser(testContext, &User{Email: "oauth-only@example.com", Name: "OAuth"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	count, restore := withDummyCompareCounter()
	defer restore()

	_, err := a.SignIn(testContext, SignInInput{Email: "oauth-only@example.com", Password: testPassword})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
	if got := count.Load(); got != 1 {
		t.Errorf("dummy compare ran %d times, want exactly 1", got)
	}
}

func TestSignInKnownUserDoesNotRunDummyCompare(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "alice@example.com")

	count, restore := withDummyCompareCounter()
	defer restore()

	if _, err := a.SignIn(testContext, SignInInput{Email: "alice@example.com", Password: "wrong-password"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
	if got := count.Load(); got != 0 {
		t.Errorf("dummy compare ran %d times for an existing user, want 0", got)
	}
}

func TestSignInMalformedEmail(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out, err := a.SignIn(testContext, SignInInput{Email: "not-an-email", Password: testPassword})
	if out != nil {
		t.Fatal("SignIn returned output on failure")
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}

func TestSignInEmptyCredentials(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out, err := a.SignIn(testContext, SignInInput{})
	if out != nil {
		t.Fatal("SignIn returned output on failure")
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}

func TestSignInRequiresVerifiedEmail(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{RequireVerifiedEmail: true})
	signUpUser(t, a, "gated@example.com")

	// Blocked before verification, but only for the right password:
	// the verification state is never revealed to someone without it.
	_, err := a.SignIn(testContext, SignInInput{Email: "gated@example.com", Password: testPassword})
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("error = %v, want ErrEmailNotVerified", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "gated@example.com", Password: "wrong-password"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v, want ErrInvalidCredentials", err)
	}
	if got := sessionCount(memStore(t, a)); got != 0 {
		t.Errorf("sessionCount = %d, want 0 before verification", got)
	}

	// Verify with the sign-up email, then sign in.
	if err := a.VerifyEmail(testContext, "gated@example.com", lastVerificationToken(t, a, mailer)); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	out, err := a.SignIn(testContext, SignInInput{Email: "gated@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignIn after verification: %v", err)
	}
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Fatalf("GetSession after verification: %v", err)
	}
}

func TestSignInUnverifiedAllowedByDefault(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "open@example.com")

	if _, err := a.SignIn(testContext, SignInInput{Email: "open@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn without verification: %v", err)
	}
}

func TestSignInBannedUser(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "banned@example.com")
	if err := a.BanUser(testContext, out.User.ID, "abuse", nil); err != nil {
		t.Fatalf("BanUser: %v", err)
	}

	signIn, err := a.SignIn(testContext, SignInInput{Email: "banned@example.com", Password: testPassword})
	if signIn != nil {
		t.Fatal("SignIn returned output for a banned user")
	}
	if !errors.Is(err, ErrUserBanned) {
		t.Fatalf("error = %v, want ErrUserBanned", err)
	}
}

func TestSignInBannedButExpiredBanAllowed(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	out := signUpUser(t, a, "temp-ban@example.com")

	past := clock.Now().Add(-time.Hour)
	if err := a.BanUser(testContext, out.User.ID, "cooling off", &past); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "temp-ban@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn with an expired ban: %v", err)
	}
}

func TestSignInWithFutureBanExpiryBlocked(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	out := signUpUser(t, a, "future-ban@example.com")

	future := clock.Now().Add(time.Hour)
	if err := a.BanUser(testContext, out.User.ID, "suspended", &future); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	_, err := a.SignIn(testContext, SignInInput{Email: "future-ban@example.com", Password: testPassword})
	if !errors.Is(err, ErrUserBanned) {
		t.Fatalf("error = %v, want ErrUserBanned", err)
	}
}

func TestSignInUnexpectedStoreError(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	a.store = failingGetUserByEmailStore{Store: a.store, err: errStoreFailure}

	if _, err := a.SignIn(testContext, SignInInput{Email: "a@example.com", Password: testPassword}); !errors.Is(err, errStoreFailure) {
		t.Fatalf("error = %v, want the wrapped store failure", err)
	}
}

func TestSignInEmptyPasswordFailsClosed(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "alice@example.com")
	if _, err := a.SignIn(testContext, SignInInput{Email: "alice@example.com"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}

// TestSignInContextIsPropagated guards the "ctx first" convention.
func TestSignInContextIsPropagated(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	ctx, cancel := context.WithCancel(testContext)
	cancel() // MemoryStore ignores ctx, but the call must accept it.

	_ = ctx
	signUpUser(t, a, "ctx@example.com")
	if _, err := a.SignIn(ctx, SignInInput{Email: "ctx@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn with a canceled context: %v", err)
	}
}
