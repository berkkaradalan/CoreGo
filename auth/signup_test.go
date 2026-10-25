package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSignUpHappyPath(t *testing.T) {
	a, clock := newTestAuth(t, Config{})
	out, err := a.SignUp(testContext, SignUpInput{
		Name:      "  Alice  ",
		Email:     "  Alice@Example.COM ",
		Password:  testPassword,
		Image:     "https://cdn.example.com/alice.png",
		IPAddress: "203.0.113.10",
		UserAgent: "zcode-test/1.0",
	})
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}

	user := out.User
	if user.ID == "" {
		t.Error("user.ID is empty")
	}
	if user.Name != "Alice" {
		t.Errorf("Name = %q, want trimmed %q", user.Name, "Alice")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("Email = %q, want normalized %q", user.Email, "alice@example.com")
	}
	if user.EmailVerified {
		t.Error("EmailVerified = true on a fresh user")
	}
	if user.Image != "https://cdn.example.com/alice.png" {
		t.Errorf("Image = %q", user.Image)
	}
	if !user.CreatedAt.Equal(clock.Now()) {
		t.Errorf("CreatedAt = %v, want %v", user.CreatedAt, clock.Now())
	}

	// Credential account.
	account, err := a.store.GetAccountByUser(testContext, user.ID, CredentialProvider)
	if err != nil {
		t.Fatalf("GetAccountByUser: %v", err)
	}
	if account.Provider != "credential" || account.ProviderAccountID != "alice@example.com" {
		t.Errorf("account = %+v", account)
	}
	if !VerifyPassword(account.Password, testPassword) {
		t.Error("stored account password does not verify")
	}
	if account.Password == testPassword {
		t.Error("plaintext password stored")
	}

	// Session.
	if len(out.SessionToken) != 43 {
		t.Errorf("len(SessionToken) = %d, want 43", len(out.SessionToken))
	}
	if out.Session.TokenHash != HashToken(out.SessionToken) {
		t.Error("session TokenHash does not match the returned token")
	}
	if out.Session.UserID != user.ID {
		t.Errorf("session UserID = %q, want %q", out.Session.UserID, user.ID)
	}
	if got := out.Session.ExpiresAt.Sub(out.Session.CreatedAt); got != DefaultSessionTTL {
		t.Errorf("session lifetime = %v, want %v", got, DefaultSessionTTL)
	}
	if out.Session.IPAddress != "203.0.113.10" || out.Session.UserAgent != "zcode-test/1.0" {
		t.Errorf("session metadata = %+v", out.Session)
	}

	// The token resolves through GetSession.
	session, resolvedUser, err := a.GetSession(testContext, out.SessionToken)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.ID != out.Session.ID || resolvedUser.ID != user.ID {
		t.Error("GetSession resolved the wrong records")
	}

	// No mailer configured: no verification token is created.
	if got := tokenCount(memStore(t, a)); got != 0 {
		t.Errorf("tokenCount = %d, want 0 without a mailer", got)
	}
}

func TestSignUpRememberMeTTL(t *testing.T) {
	a, _ := newTestAuth(t, Config{})

	regular, err := a.SignUp(testContext, SignUpInput{Name: "R", Email: "regular@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignUp(regular): %v", err)
	}
	remembered, err := a.SignUp(testContext, SignUpInput{Name: "M", Email: "remember@example.com", Password: testPassword, RememberMe: true})
	if err != nil {
		t.Fatalf("SignUp(remember): %v", err)
	}

	if got := regular.Session.ExpiresAt.Sub(regular.Session.CreatedAt); got != DefaultSessionTTL {
		t.Errorf("regular lifetime = %v, want %v", got, DefaultSessionTTL)
	}
	if got := remembered.Session.ExpiresAt.Sub(remembered.Session.CreatedAt); got != DefaultRememberMeTTL {
		t.Errorf("remember lifetime = %v, want %v", got, DefaultRememberMeTTL)
	}
}

func TestSignUpValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		in        SignUpInput
		wantError error
	}{
		{"missing name", SignUpInput{Email: "a@example.com", Password: testPassword}, ErrNameRequired},
		{"whitespace name", SignUpInput{Name: "   ", Email: "a@example.com", Password: testPassword}, ErrNameRequired},
		{"missing email", SignUpInput{Name: "A", Password: testPassword}, ErrEmailRequired},
		{"invalid email", SignUpInput{Name: "A", Email: "not-an-email", Password: testPassword}, ErrInvalidEmail},
		{"missing password", SignUpInput{Name: "A", Email: "a@example.com"}, ErrPasswordRequired},
		{"short password", SignUpInput{Name: "A", Email: "a@example.com", Password: "short"}, ErrWeakPassword},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := newTestAuth(t, Config{})
			out, err := a.SignUp(testContext, tt.in)
			if out != nil {
				t.Fatal("SignUp returned a non-nil output on validation failure")
			}
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			// Nothing may be persisted on validation failure.
			if n, _ := a.CountUsers(testContext); n != 0 {
				t.Errorf("CountUsers = %d after failed SignUp, want 0", n)
			}
		})
	}
}

func TestSignUpDuplicateEmailPreCheck(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "taken@example.com")

	out, err := a.SignUp(testContext, SignUpInput{Name: "Second", Email: "TAKEN@example.com", Password: testPassword})
	if out != nil {
		t.Fatal("SignUp returned output for a duplicate email")
	}
	if !errors.Is(err, ErrUserAlreadyExists) {
		t.Fatalf("error = %v, want ErrUserAlreadyExists", err)
	}
}

func TestSignUpDuplicateEmailRaceMapping(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "race@example.com")

	// Simulate a uniqueness race lost between the pre-check and the
	// create: CreateUser always reports ErrDuplicate.
	a.store = duplicateCreateUserStore{Store: a.store}

	out, err := a.SignUp(testContext, SignUpInput{Name: "Second", Email: "race@example.com", Password: testPassword})
	if out != nil {
		t.Fatal("SignUp returned output for a duplicate email")
	}
	if !errors.Is(err, ErrUserAlreadyExists) {
		t.Fatalf("error = %v, want ErrUserAlreadyExists", err)
	}
}

func TestSignUpUnexpectedStoreError(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	a.store = failingGetUserByEmailStore{Store: a.store, err: errStoreFailure}

	if _, err := a.SignUp(testContext, SignUpInput{Name: "A", Email: "a@example.com", Password: testPassword}); !errors.Is(err, errStoreFailure) {
		t.Fatalf("error = %v, want the wrapped store failure", err)
	}
}

func TestSignUpWithMailerSendsVerification(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out, err := a.SignUp(testContext, SignUpInput{
		Name:     "Verify Me",
		Email:    "Verify@Example.com",
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	waitForDeliveries(t, a)

	calls := mailer.verificationCalls()
	if len(calls) != 1 {
		t.Fatalf("verification calls = %d, want 1", len(calls))
	}
	if calls[0].To != "verify@example.com" {
		t.Errorf("sent to %q, want the normalized address", calls[0].To)
	}
	if len(calls[0].Token) != 43 {
		t.Errorf("token length = %d, want 43", len(calls[0].Token))
	}

	// The sent token is persisted as a hash, purpose prefixed, with the
	// configured TTL.
	stored, err := a.store.GetVerificationToken(testContext, "email-verification:verify@example.com", HashToken(calls[0].Token))
	if err != nil {
		t.Fatalf("GetVerificationToken: %v", err)
	}
	if want := out.User.CreatedAt.Add(DefaultEmailVerificationTokenTTL); !stored.ExpiresAt.Equal(want) {
		t.Errorf("token ExpiresAt = %v, want %v", stored.ExpiresAt, want)
	}
	if got := tokenCount(memStore(t, a)); got != 1 {
		t.Errorf("tokenCount = %d, want 1", got)
	}
	if len(mailer.resetCalls()) != 0 {
		t.Error("SignUp sent a password reset email")
	}
}

// TestSignUpDeliveryIsBackground proves SignUp returns before the
// mailer finishes: the mailer is blocked while SignUp completes.
func TestSignUpDeliveryIsBackground(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	gate := make(chan struct{})
	mailer.gate = gate
	mailer.started = make(chan struct{}, 1)

	out, err := a.SignUp(testContext, SignUpInput{Name: "Bg", Email: "bg@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	if out.SessionToken == "" {
		t.Fatal("SignUp returned no session while the mailer was blocked")
	}
	<-mailer.started // the delivery is running and blocked
	if calls := mailer.verificationCalls(); len(calls) != 0 {
		t.Fatalf("verification calls = %d before the mailer was released, want 0", len(calls))
	}
	close(gate)
	waitForDeliveries(t, a)
	if calls := mailer.verificationCalls(); len(calls) != 1 {
		t.Fatalf("verification calls = %d after release, want 1", len(calls))
	}
}

func TestSignUpMailerFailureIsLoggedNotReturned(t *testing.T) {
	logger, logs := newLogCapture()
	deliveryErr := errors.New("smtp refused connection")
	a, _, mailer := newTestAuthWithMailer(t, Config{Logger: logger})
	mailer.setFailure(deliveryErr)

	out, err := a.SignUp(testContext, SignUpInput{
		Name:     "Deliver Later",
		Email:    "later@example.com",
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("SignUp returned %v, want nil: delivery runs in the background", err)
	}
	waitForDeliveries(t, a)

	// The account and session exist and are usable.
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Errorf("session is not usable: %v", err)
	}

	// The failure is logged with the user ID and the cause, but never
	// with the token or the recipient address.
	output := logs.String()
	for _, want := range []string{"level=ERROR", "user_id=" + out.User.ID, "smtp refused connection", "email=email_verification"} {
		if !strings.Contains(output, want) {
			t.Errorf("log output lacks %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "later@example.com") {
		t.Errorf("log output contains the recipient address:\n%s", output)
	}
	attempts := mailer.attemptedCalls()
	if len(attempts) != 1 {
		t.Fatalf("send attempts = %d, want 1", len(attempts))
	}
	if strings.Contains(output, attempts[0].Token) {
		t.Errorf("log output contains the raw token:\n%s", output)
	}

	// SendVerificationEmail can retry delivery for the account.
	mailer.setFailure(nil)
	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("SendVerificationEmail retry: %v", err)
	}
	if calls := mailer.verificationCalls(); len(calls) != 1 {
		t.Fatalf("verification calls after retry = %d, want 1", len(calls))
	}
}

func TestSignUpMailerPanicIsRecoveredAndLogged(t *testing.T) {
	logger, logs := newLogCapture()
	a, _, mailer := newTestAuthWithMailer(t, Config{Logger: logger})
	mailer.panicValue = "mailer exploded"

	out := signUpUser(t, a, "panic@example.com")
	waitForDeliveries(t, a)

	output := logs.String()
	if !strings.Contains(output, "panicked") || !strings.Contains(output, "mailer exploded") ||
		!strings.Contains(output, "user_id="+out.User.ID) {
		t.Errorf("panic was not logged as expected:\n%s", output)
	}
	// The service keeps working after the panic.
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Errorf("GetSession after a mailer panic: %v", err)
	}
}

func TestSignUpTokenPersistenceFailureDoesNotFailSignUp(t *testing.T) {
	logger, logs := newLogCapture()
	a, _, mailer := newTestAuthWithMailer(t, Config{Logger: logger})
	a.store = &faultyStore{Store: a.store, err: errStoreFailure, createToken: true}

	out, err := a.SignUp(testContext, SignUpInput{Name: "T", Email: "tokfail@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignUp = %v, want nil (the account exists)", err)
	}
	if out.User == nil || out.SessionToken == "" {
		t.Fatalf("SignUp output incomplete: %+v", out)
	}
	waitForDeliveries(t, a)
	if calls := mailer.verificationCalls(); len(calls) != 0 {
		t.Errorf("email sent without a persisted token: %v", calls)
	}
	if !strings.Contains(logs.String(), "store backend failure") {
		t.Errorf("token persistence failure was not logged:\n%s", logs.String())
	}
}

func TestSignUpRequireVerifiedEmailCreatesNoSession(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{RequireVerifiedEmail: true})
	out, err := a.SignUp(testContext, SignUpInput{
		Name:       "Gated",
		Email:      "gated-signup@example.com",
		Password:   testPassword,
		RememberMe: true,
	})
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	if out.User == nil || out.User.ID == "" {
		t.Fatal("SignUp did not return the created user")
	}
	if out.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty with RequireVerifiedEmail", out.SessionToken)
	}
	if out.Session != nil {
		t.Errorf("Session = %+v, want nil with RequireVerifiedEmail", out.Session)
	}
	if got := sessionCount(memStore(t, a)); got != 0 {
		t.Errorf("sessionCount = %d, want 0", got)
	}

	// The verification email still goes out, and after verifying the
	// user can sign in.
	token := lastVerificationToken(t, a, mailer)
	if err := a.VerifyEmail(testContext, "gated-signup@example.com", token); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "gated-signup@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn after verification: %v", err)
	}
}

func TestSignUpRequireVerifiedEmailWithoutMailer(t *testing.T) {
	logger, logs := newLogCapture()
	a, _ := newTestAuth(t, Config{RequireVerifiedEmail: true, Logger: logger})
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "RequireVerifiedEmail") {
		t.Errorf("New did not warn about RequireVerifiedEmail without a mailer:\n%s", logs.String())
	}

	out := signUpUser(t, a, "nomail-gated@example.com")
	if out.SessionToken != "" || out.Session != nil {
		t.Error("SignUp created a session with RequireVerifiedEmail")
	}
}

func TestNewDoesNotWarnWithMailerOrWithoutRequirement(t *testing.T) {
	for _, cfg := range []Config{
		{RequireVerifiedEmail: true, Mailer: &recordingMailer{}},
		{RequireVerifiedEmail: false},
	} {
		logger, logs := newLogCapture()
		cfg.Logger = logger
		if _, err := New(cfg, NewMemoryStore()); err != nil {
			t.Fatalf("New: %v", err)
		}
		if logs.String() != "" {
			t.Errorf("unexpected log output for %+v:\n%s", cfg, logs.String())
		}
	}
}

func TestSignUpAccountFailureRemovesUser(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantError error
	}{
		{"duplicate credential account", ErrDuplicate, ErrUserAlreadyExists},
		{"unexpected failure", errStoreFailure, errStoreFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := newTestAuth(t, Config{})
			store := memStore(t, a)
			a.store = &faultyStore{Store: store, err: tt.err, createAccount: true}

			out, err := a.SignUp(testContext, SignUpInput{Name: "Orphan", Email: "orphan@example.com", Password: testPassword})
			if out != nil {
				t.Fatal("SignUp returned output although the account failed")
			}
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			// No orphan user may block the address.
			if _, err := store.GetUserByEmail(testContext, "orphan@example.com"); !errors.Is(err, ErrNotFound) {
				t.Errorf("orphan user left behind: %v", err)
			}
		})
	}
}

func TestSignUpSessionFailureIsReturned(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	a.store = &faultyStore{Store: a.store, err: errStoreFailure, createSession: true}
	if _, err := a.SignUp(testContext, SignUpInput{Name: "S", Email: "s@example.com", Password: testPassword}); !errors.Is(err, errStoreFailure) {
		t.Fatalf("error = %v, want the store failure", err)
	}
}

func TestSignUpCustomMinPasswordLength(t *testing.T) {
	a, _ := newTestAuth(t, Config{MinPasswordLength: 12})
	tooShort := strings.Repeat("x", 11)
	if _, err := a.SignUp(testContext, SignUpInput{Name: "A", Email: "a@example.com", Password: tooShort}); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("error = %v, want ErrWeakPassword", err)
	}
	if _, err := a.SignUp(testContext, SignUpInput{Name: "A", Email: "a@example.com", Password: strings.Repeat("x", 12)}); err != nil {
		t.Fatalf("SignUp with 12 char password: %v", err)
	}
}

func TestSignUpCustomTokenTTLsAreUsed(t *testing.T) {
	a, clock, mailer := newTestAuthWithMailer(t, Config{
		SessionTTL:                time.Hour,
		RememberMeTTL:             24 * time.Hour,
		EmailVerificationTokenTTL: time.Minute,
	})
	if _, err := a.SignUp(testContext, SignUpInput{Name: "TTL", Email: "ttl@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	token := lastVerificationToken(t, a, mailer)
	stored, err := a.store.GetVerificationToken(testContext, "email-verification:ttl@example.com", HashToken(token))
	if err != nil {
		t.Fatalf("GetVerificationToken: %v", err)
	}
	if want := clock.Now().Add(time.Minute); !stored.ExpiresAt.Equal(want) {
		t.Errorf("token ExpiresAt = %v, want %v", stored.ExpiresAt, want)
	}
}
