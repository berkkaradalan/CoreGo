package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const resetNewPassword = "new-password-456"

func TestRequestPasswordResetUniformNil(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "real@example.com") // ensure the store is not empty
	waitForDeliveries(t, a)
	before := tokenCount(memStore(t, a)) // the signup token

	for _, email := range []string{"ghost@example.com", "not-an-email", "", "  "} {
		if err := a.RequestPasswordReset(testContext, email); err != nil {
			t.Fatalf("RequestPasswordReset(%q) = %v, want nil (anti enumeration)", email, err)
		}
	}
	waitForDeliveries(t, a)
	if calls := mailer.resetCalls(); len(calls) != 0 {
		t.Errorf("reset emails sent for unknown addresses: %v", calls)
	}
	if got := tokenCount(memStore(t, a)); got != before {
		t.Errorf("tokenCount = %d, want %d (no token for unknown addresses)", got, before)
	}
}

func TestRequestPasswordResetHappy(t *testing.T) {
	a, clock, mailer := newTestAuthWithMailer(t, Config{PasswordResetTokenTTL: 30 * time.Minute})
	signUpUser(t, a, "reset@example.com")

	if err := a.RequestPasswordReset(testContext, "  RESET@example.com "); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	// Persisted by the background job, not by the request itself.
	waitForDeliveries(t, a)
	if got := tokensWithIdentifier(memStore(t, a), "password-reset:reset@example.com"); got != 1 {
		t.Fatalf("reset tokens = %d, want 1", got)
	}
	calls := mailer.resetCalls()
	if len(calls) != 1 {
		t.Fatalf("reset calls = %d, want 1", len(calls))
	}
	if calls[0].To != "reset@example.com" || len(calls[0].Token) != 43 {
		t.Errorf("call = %+v", calls[0])
	}

	stored, err := a.store.GetVerificationToken(testContext, "password-reset:reset@example.com", HashToken(calls[0].Token))
	if err != nil {
		t.Fatalf("GetVerificationToken: %v", err)
	}
	if want := clock.Now().Add(30 * time.Minute); !stored.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", stored.ExpiresAt, want)
	}
	// The plain email identifier of v1 is never used.
	if _, err := a.store.GetVerificationToken(testContext, "reset@example.com", HashToken(calls[0].Token)); !errors.Is(err, ErrNotFound) {
		t.Errorf("token stored under the unprefixed identifier: %v", err)
	}
}

func TestRequestPasswordResetNoMailer(t *testing.T) {
	// A missing mailer must behave exactly like an unknown address:
	// distinguishing the two (nil vs ErrMailerNotConfigured) would let
	// HTTP clients enumerate registered addresses on deployments
	// without SMTP configuration (unknown email -> 202, registered
	// email -> 503).
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "nomailer-reset@example.com")

	if err := a.RequestPasswordReset(testContext, "nomailer-reset@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset(known email, no mailer) = %v, want nil", err)
	}
	if err := a.RequestPasswordReset(testContext, "ghost@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset(unknown, no mailer) = %v, want nil", err)
	}
	if got := tokenCount(memStore(t, a)); got != 0 {
		t.Errorf("tokenCount = %d, want 0 (no reset tokens without a mailer)", got)
	}
}

// TestRequestPasswordResetDoesNotDependOnSMTP is the regression test
// for the enumeration oracle: a failing or slow mailer must neither
// change the result nor delay the response for registered addresses.
func TestRequestPasswordResetDoesNotDependOnSMTP(t *testing.T) {
	logger, logs := newLogCapture()
	a, _, mailer := newTestAuthWithMailer(t, Config{Logger: logger})
	out := signUpUser(t, a, "delivery@example.com")
	waitForDeliveries(t, a)

	// A blocked mailer: the call must still return.
	gate := make(chan struct{})
	mailer.mu.Lock()
	mailer.gate = gate
	mailer.started = make(chan struct{}, 1)
	mailer.failErr = errors.New("connection reset by peer")
	mailer.mu.Unlock()

	if err := a.RequestPasswordReset(testContext, "delivery@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset = %v, want nil although delivery fails", err)
	}
	<-mailer.started // still blocked in the mailer
	close(gate)
	waitForDeliveries(t, a)

	output := logs.String()
	for _, want := range []string{"level=ERROR", "email=password_reset", "user_id=" + out.User.ID, "connection reset by peer"} {
		if !strings.Contains(output, want) {
			t.Errorf("log output lacks %q:\n%s", want, output)
		}
	}
	for _, attempt := range mailer.attemptedCalls() {
		if strings.Contains(output, attempt.Token) {
			t.Errorf("log output contains a raw token:\n%s", output)
		}
	}
	if strings.Contains(output, "delivery@example.com") {
		t.Errorf("log output contains the address:\n%s", output)
	}
}

func TestRequestPasswordResetStoreErrors(t *testing.T) {
	t.Run("lookup failure", func(t *testing.T) {
		a, _, _ := newTestAuthWithMailer(t, Config{})
		a.store = failingGetUserByEmailStore{Store: a.store, err: errStoreFailure}
		if err := a.RequestPasswordReset(testContext, "a@example.com"); !errors.Is(err, errStoreFailure) {
			t.Fatalf("error = %v, want the store failure", err)
		}
	})
	t.Run("token persistence failure is logged, not returned", func(t *testing.T) {
		logger, logs := newLogCapture()
		a, _, mailer := newTestAuthWithMailer(t, Config{Logger: logger})
		out := signUpUser(t, a, "persist@example.com")
		waitForDeliveries(t, a)
		a.store = &faultyStore{Store: a.store, err: errStoreFailure, createToken: true}
		if err := a.RequestPasswordReset(testContext, "persist@example.com"); err != nil {
			t.Fatalf("error = %v, want nil (the token is written in the background)", err)
		}
		waitForDeliveries(t, a)
		if calls := mailer.resetCalls(); len(calls) != 0 {
			t.Errorf("reset email sent without a persisted token: %v", calls)
		}
		output := logs.String()
		for _, want := range []string{"level=ERROR", "email=password_reset", "user_id=" + out.User.ID, "issuing the token", errStoreFailure.Error()} {
			if !strings.Contains(output, want) {
				t.Errorf("log output lacks %q:\n%s", want, output)
			}
		}
		if strings.Contains(output, "persist@example.com") {
			t.Errorf("log output contains the address:\n%s", output)
		}
	})
}

func TestResetPasswordHappy(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "resetting@example.com")
	extra, err := a.SignIn(testContext, SignInInput{Email: "resetting@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if err := a.RequestPasswordReset(testContext, "resetting@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	token := lastResetToken(t, a, mailer)

	if err := a.ResetPassword(testContext, "resetting@example.com", token, resetNewPassword); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	// The old password no longer authenticates.
	if _, err := a.SignIn(testContext, SignInInput{Email: "resetting@example.com", Password: testPassword}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old password still works: %v", err)
	}
	// The new one does.
	if _, err := a.SignIn(testContext, SignInInput{Email: "resetting@example.com", Password: resetNewPassword}); err != nil {
		t.Errorf("SignIn with the new password: %v", err)
	}
	// All pre-reset sessions were revoked.
	if _, _, err := a.GetSession(testContext, out.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("signup session not revoked: %v", err)
	}
	if _, _, err := a.GetSession(testContext, extra.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("sign-in session not revoked: %v", err)
	}
	// The reset token is single use.
	if err := a.ResetPassword(testContext, "resetting@example.com", token, "another-pass-9"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("reused token error = %v, want ErrInvalidToken", err)
	}
}

func TestResetPasswordWeakNewPassword(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "weak@example.com")

	if err := a.RequestPasswordReset(testContext, "weak@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	token := lastResetToken(t, a, mailer)

	if err := a.ResetPassword(testContext, "weak@example.com", token, "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("error = %v, want ErrWeakPassword", err)
	}
	// The policy failure must not consume the token.
	if _, err := a.store.GetVerificationToken(testContext, "password-reset:weak@example.com", HashToken(token)); err != nil {
		t.Errorf("token consumed by a rejected password: %v", err)
	}
	// The same token still works with a valid password.
	if err := a.ResetPassword(testContext, "weak@example.com", token, resetNewPassword); err != nil {
		t.Errorf("ResetPassword with a valid password: %v", err)
	}
}

func TestResetPasswordUnknownToken(t *testing.T) {
	a, _, _ := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "unknown-reset@example.com")

	if err := a.ResetPassword(testContext, "unknown-reset@example.com", "bogus", resetNewPassword); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
	// The password is unchanged.
	if _, err := a.SignIn(testContext, SignInInput{Email: "unknown-reset@example.com", Password: testPassword}); err != nil {
		t.Errorf("password changed without a valid token: %v", err)
	}
}

func TestResetPasswordExpiredToken(t *testing.T) {
	a, clock, mailer := newTestAuthWithMailer(t, Config{PasswordResetTokenTTL: time.Hour})
	signUpUser(t, a, "expired-reset@example.com")

	if err := a.RequestPasswordReset(testContext, "expired-reset@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	token := lastResetToken(t, a, mailer)

	clock.Advance(time.Hour + time.Nanosecond)
	if err := a.ResetPassword(testContext, "expired-reset@example.com", token, resetNewPassword); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("error = %v, want ErrTokenExpired", err)
	}
	if _, err := a.store.GetVerificationToken(testContext, "password-reset:expired-reset@example.com", HashToken(token)); !errors.Is(err, ErrNotFound) {
		t.Error("expired reset token was not deleted")
	}
	// The old password still works.
	if _, err := a.SignIn(testContext, SignInInput{Email: "expired-reset@example.com", Password: testPassword}); err != nil {
		t.Errorf("password changed through an expired token: %v", err)
	}
}

func TestResetPasswordNoCredentialAccount(t *testing.T) {
	a, _, _ := newTestAuthWithMailer(t, Config{})
	// A user with a valid reset token but no credential account.
	user := &User{Email: "oauth-reset@example.com", Name: "OAuth", CreatedAt: a.now()}
	if err := a.store.CreateUser(testContext, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	raw := "direct-token"
	if err := a.store.CreateVerificationToken(testContext, &VerificationToken{
		Identifier: purposePasswordReset.identifier(user.Email),
		ValueHash:  HashToken(raw),
		ExpiresAt:  a.now().Add(time.Hour),
		CreatedAt:  a.now(),
	}); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}

	if err := a.ResetPassword(testContext, user.Email, raw, resetNewPassword); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("error = %v, want ErrAccountNotFound", err)
	}
}

// TestResetPasswordRejectsVerificationToken is the regression test
// for the confirmed purpose confusion bug: the verification token from
// the sign-up email could be used to set a new password.
func TestResetPasswordRejectsVerificationToken(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "victim@example.com")
	verificationToken := lastVerificationToken(t, a, mailer)

	if err := a.ResetPassword(testContext, "victim@example.com", verificationToken, resetNewPassword); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("ResetPassword with a verification token error = %v, want ErrInvalidToken", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "victim@example.com", Password: testPassword}); err != nil {
		t.Errorf("password changed through a verification token: %v", err)
	}
	// The verification token is untouched and still verifies.
	if err := a.VerifyEmail(testContext, "victim@example.com", verificationToken); err != nil {
		t.Errorf("VerifyEmail after the rejected reset attempt: %v", err)
	}
}

func TestResetPasswordInvalidInput(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "input-reset@example.com")
	if err := a.RequestPasswordReset(testContext, "input-reset@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	token := lastResetToken(t, a, mailer)

	tests := []struct {
		name  string
		email string
		token string
	}{
		{"empty token", "input-reset@example.com", ""},
		{"empty email", "", token},
		{"other address", "someone-else@example.com", token},
		{"unknown token", "input-reset@example.com", "nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := a.ResetPassword(testContext, tt.email, tt.token, resetNewPassword); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("error = %v, want ErrInvalidToken", err)
			}
		})
	}
	// The email is normalized before lookup.
	if err := a.ResetPassword(testContext, " Input-Reset@EXAMPLE.com", token, resetNewPassword); err != nil {
		t.Fatalf("ResetPassword with a differently cased email: %v", err)
	}
}

func TestResetPasswordInvalidatesEveryOutstandingResetToken(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "many@example.com")
	for i := 0; i < 3; i++ {
		if err := a.RequestPasswordReset(testContext, "many@example.com"); err != nil {
			t.Fatalf("RequestPasswordReset: %v", err)
		}
	}
	waitForDeliveries(t, a)
	calls := mailer.resetCalls()
	if len(calls) != 3 {
		t.Fatalf("reset calls = %d, want 3", len(calls))
	}

	if err := a.ResetPassword(testContext, "many@example.com", calls[1].Token, resetNewPassword); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	for _, i := range []int{0, 2} {
		if err := a.ResetPassword(testContext, "many@example.com", calls[i].Token, "attacker-pass-1"); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("token %d after a successful reset: error = %v, want ErrInvalidToken", i, err)
		}
	}
	if got := tokensWithIdentifier(memStore(t, a), "password-reset:many@example.com"); got != 0 {
		t.Errorf("reset tokens left = %d, want 0", got)
	}
	// Verification tokens are a different purpose and are kept.
	if got := tokensWithIdentifier(memStore(t, a), "email-verification:many@example.com"); got != 1 {
		t.Errorf("verification tokens = %d, want 1", got)
	}
}

func TestResetPasswordTokenOfDeletedUser(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "deleted-reset@example.com")
	if err := a.RequestPasswordReset(testContext, "deleted-reset@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	token := lastResetToken(t, a, mailer)
	if err := a.DeleteUser(testContext, out.User.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	// Somebody else registers the freed address.
	signUpUser(t, a, "deleted-reset@example.com")
	if err := a.ResetPassword(testContext, "deleted-reset@example.com", token, "attacker-pass-1"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "deleted-reset@example.com", Password: testPassword}); err != nil {
		t.Errorf("new owner's password changed: %v", err)
	}
}

// TestResetPasswordStoreFailureConsumesToken documents the consume
// first trade-off: the token is gone even when applying the reset
// fails, and the password stays unchanged.
func TestResetPasswordStoreFailureConsumesToken(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "retry-reset@example.com")
	if err := a.RequestPasswordReset(testContext, "retry-reset@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	token := lastResetToken(t, a, mailer)
	store := a.store

	a.store = &faultyStore{Store: store, err: errStoreFailure, updateAccount: true}
	if err := a.ResetPassword(testContext, "retry-reset@example.com", token, resetNewPassword); !errors.Is(err, errStoreFailure) {
		t.Fatalf("error = %v, want the store failure", err)
	}
	a.store = store
	if err := a.ResetPassword(testContext, "retry-reset@example.com", token, resetNewPassword); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("ResetPassword retry = %v, want ErrInvalidToken (token consumed)", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "retry-reset@example.com", Password: testPassword}); err != nil {
		t.Errorf("old password no longer works after the failed reset: %v", err)
	}
}
