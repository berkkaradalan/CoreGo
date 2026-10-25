package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSendVerificationEmailHappy(t *testing.T) {
	a, clock, mailer := newTestAuthWithMailer(t, Config{EmailVerificationTokenTTL: 5 * time.Hour})
	out := signUpUser(t, a, "verify@example.com") // sends the signup email
	waitForDeliveries(t, a)

	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	calls := mailer.verificationCalls()
	if len(calls) != 2 {
		t.Fatalf("verification calls = %d, want 2 (signup + explicit)", len(calls))
	}
	last := calls[len(calls)-1]
	if last.To != "verify@example.com" || len(last.Token) != 43 {
		t.Errorf("call = %+v", last)
	}

	stored, err := a.store.GetVerificationToken(testContext, "email-verification:verify@example.com", HashToken(last.Token))
	if err != nil {
		t.Fatalf("GetVerificationToken: %v", err)
	}
	if want := clock.Now().Add(5 * time.Hour); !stored.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", stored.ExpiresAt, want)
	}
	if !stored.CreatedAt.Equal(clock.Now()) {
		t.Errorf("CreatedAt = %v, want the service clock %v", stored.CreatedAt, clock.Now())
	}
}

func TestSendVerificationEmailIsSynchronous(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "sync@example.com")

	// Installed after sign-up so the only send is the explicit one; it
	// must have happened by the time the call returns, without waiting
	// for background deliveries.
	mailer := &recordingMailer{}
	a.cfg.Mailer = mailer
	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	if calls := mailer.verificationCalls(); len(calls) != 1 {
		t.Fatalf("verification calls = %d right after return, want 1", len(calls))
	}
}

func TestSendVerificationEmailUnknownUser(t *testing.T) {
	a, _, _ := newTestAuthWithMailer(t, Config{})
	if err := a.SendVerificationEmail(testContext, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}

func TestSendVerificationEmailAlreadyVerified(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "verified@example.com")
	verifyUser(t, a, mailer, out.User.ID)

	if err := a.SendVerificationEmail(testContext, out.User.ID); !errors.Is(err, ErrEmailAlreadyVerified) {
		t.Fatalf("error = %v, want ErrEmailAlreadyVerified", err)
	}
}

func TestSendVerificationEmailNoMailer(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "nomailer@example.com")

	if err := a.SendVerificationEmail(testContext, out.User.ID); !errors.Is(err, ErrMailerNotConfigured) {
		t.Fatalf("error = %v, want ErrMailerNotConfigured", err)
	}
	if got := tokenCount(memStore(t, a)); got != 0 {
		t.Errorf("tokenCount = %d, want 0 (no token without a mailer)", got)
	}
}

func TestSendVerificationEmailDeliveryFailure(t *testing.T) {
	deliveryErr := errors.New("provider timeout")
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "fail@example.com") // signup email succeeds
	waitForDeliveries(t, a)
	before := tokenCount(memStore(t, a))

	// Fail only the explicit send.
	mailer.setFailure(deliveryErr)
	err := a.SendVerificationEmail(testContext, out.User.ID)
	if !errors.Is(err, ErrEmailDeliveryFailed) {
		t.Fatalf("error = %v, want ErrEmailDeliveryFailed", err)
	}
	if !errors.Is(err, deliveryErr) {
		t.Fatalf("error = %v, does not preserve the delivery cause", err)
	}
	// The token was persisted before the send attempt; it stays valid.
	if got := tokenCount(memStore(t, a)); got != before+1 {
		t.Errorf("tokenCount = %d, want %d", got, before+1)
	}
	attempts := mailer.attemptedCalls()
	failedToken := attempts[len(attempts)-1].Token
	if err := a.VerifyEmail(testContext, "fail@example.com", failedToken); err != nil {
		t.Errorf("token of the failed delivery is not usable: %v", err)
	}
}

func TestRequestEmailVerificationUniformNil(t *testing.T) {
	tests := []struct {
		name  string
		email string
	}{
		{"unknown address", "ghost@example.com"},
		{"malformed address", "not-an-email"},
		{"empty address", ""},
		{"whitespace address", "   "},
		{"already verified address", "done@example.com"},
	}
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	verified := signUpUser(t, a, "done@example.com")
	verifyUser(t, a, mailer, verified.User.ID)
	before := tokenCount(memStore(t, a))
	sentBefore := len(mailer.attemptedCalls())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := a.RequestEmailVerification(testContext, tt.email); err != nil {
				t.Fatalf("RequestEmailVerification(%q) = %v, want nil", tt.email, err)
			}
		})
	}
	waitForDeliveries(t, a)
	if got := tokenCount(memStore(t, a)); got != before {
		t.Errorf("tokenCount = %d, want %d (no tokens issued)", got, before)
	}
	if got := len(mailer.attemptedCalls()); got != sentBefore {
		t.Errorf("emails sent = %d, want %d", got, sentBefore)
	}
}

func TestRequestEmailVerificationNoMailerReturnsNil(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	signUpUser(t, a, "known@example.com")

	for _, email := range []string{"known@example.com", "ghost@example.com", "bad"} {
		if err := a.RequestEmailVerification(testContext, email); err != nil {
			t.Errorf("RequestEmailVerification(%q) without mailer = %v, want nil", email, err)
		}
	}
	if got := tokenCount(memStore(t, a)); got != 0 {
		t.Errorf("tokenCount = %d, want 0", got)
	}
}

func TestRequestEmailVerificationKnownAddress(t *testing.T) {
	a, clock, mailer := newTestAuthWithMailer(t, Config{EmailVerificationTokenTTL: 2 * time.Hour})
	signUpUser(t, a, "resend@example.com")
	waitForDeliveries(t, a)

	if err := a.RequestEmailVerification(testContext, "  RESEND@Example.com "); err != nil {
		t.Fatalf("RequestEmailVerification: %v", err)
	}
	token := lastVerificationToken(t, a, mailer)
	// Persisted by the background job, next to the sign-up token.
	if got := tokensWithIdentifier(memStore(t, a), "email-verification:resend@example.com"); got != 2 {
		t.Fatalf("verification tokens = %d, want 2", got)
	}
	calls := mailer.verificationCalls()
	if len(calls) != 2 || calls[1].To != "resend@example.com" {
		t.Fatalf("verification calls = %+v", calls)
	}
	stored, err := a.store.GetVerificationToken(testContext, "email-verification:resend@example.com", HashToken(token))
	if err != nil {
		t.Fatalf("GetVerificationToken: %v", err)
	}
	if want := clock.Now().Add(2 * time.Hour); !stored.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", stored.ExpiresAt, want)
	}
	if err := a.VerifyEmail(testContext, "resend@example.com", token); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
}

func TestRequestEmailVerificationDoesNotWaitForMailer(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "slow@example.com")
	waitForDeliveries(t, a)

	gate := make(chan struct{})
	mailer.mu.Lock()
	mailer.gate = gate
	mailer.started = make(chan struct{}, 1)
	mailer.mu.Unlock()

	if err := a.RequestEmailVerification(testContext, "slow@example.com"); err != nil {
		t.Fatalf("RequestEmailVerification: %v", err)
	}
	<-mailer.started
	close(gate)
	waitForDeliveries(t, a)
}

func TestRequestEmailVerificationFailuresAreLogged(t *testing.T) {
	logger, logs := newLogCapture()
	a, _, mailer := newTestAuthWithMailer(t, Config{Logger: logger})
	out := signUpUser(t, a, "flaky@example.com")
	waitForDeliveries(t, a)

	mailer.setFailure(errors.New("relay denied"))
	if err := a.RequestEmailVerification(testContext, "flaky@example.com"); err != nil {
		t.Fatalf("RequestEmailVerification = %v, want nil", err)
	}
	waitForDeliveries(t, a)
	output := logs.String()
	if !strings.Contains(output, "relay denied") || !strings.Contains(output, "user_id="+out.User.ID) {
		t.Errorf("delivery failure not logged:\n%s", output)
	}
	for _, attempt := range mailer.attemptedCalls() {
		if strings.Contains(output, attempt.Token) {
			t.Errorf("log output contains a raw token:\n%s", output)
		}
	}
	if strings.Contains(output, "flaky@example.com") {
		t.Errorf("log output contains the address:\n%s", output)
	}
}

func TestRequestEmailVerificationStoreErrors(t *testing.T) {
	t.Run("lookup failure", func(t *testing.T) {
		a, _, _ := newTestAuthWithMailer(t, Config{})
		a.store = failingGetUserByEmailStore{Store: a.store, err: errStoreFailure}
		if err := a.RequestEmailVerification(testContext, "a@example.com"); !errors.Is(err, errStoreFailure) {
			t.Fatalf("error = %v, want the store failure", err)
		}
	})
	t.Run("token persistence failure is logged, not returned", func(t *testing.T) {
		logger, logs := newLogCapture()
		a, _, mailer := newTestAuthWithMailer(t, Config{Logger: logger})
		out := signUpUser(t, a, "persist@example.com")
		waitForDeliveries(t, a)
		a.store = &faultyStore{Store: a.store, err: errStoreFailure, createToken: true}
		if err := a.RequestEmailVerification(testContext, "persist@example.com"); err != nil {
			t.Fatalf("error = %v, want nil (the token is written in the background)", err)
		}
		waitForDeliveries(t, a)
		if got := len(mailer.attemptedCalls()); got != 1 { // the sign-up email only
			t.Errorf("emails sent = %d, want 1", got)
		}
		output := logs.String()
		for _, want := range []string{"level=ERROR", "email=email_verification", "user_id=" + out.User.ID, "issuing the token", errStoreFailure.Error()} {
			if !strings.Contains(output, want) {
				t.Errorf("log output lacks %q:\n%s", want, output)
			}
		}
		if strings.Contains(output, "persist@example.com") {
			t.Errorf("log output contains the address:\n%s", output)
		}
	})
}

func TestVerifyEmailHappy(t *testing.T) {
	a, clock, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "confirm@example.com")
	token := lastVerificationToken(t, a, mailer)
	clock.Advance(time.Minute)

	if err := a.VerifyEmail(testContext, "confirm@example.com", token); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	user, err := a.GetUser(testContext, out.User.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !user.EmailVerified {
		t.Error("EmailVerified = false after VerifyEmail")
	}

	// Single use: the consumed token is gone.
	if _, err := a.store.GetVerificationToken(testContext, "email-verification:confirm@example.com", HashToken(token)); !errors.Is(err, ErrNotFound) {
		t.Errorf("consumed token still in store: %v", err)
	}
}

func TestVerifyEmailSingleUse(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "single@example.com")
	token := lastVerificationToken(t, a, mailer)

	if err := a.VerifyEmail(testContext, "single@example.com", token); err != nil {
		t.Fatalf("first VerifyEmail: %v", err)
	}
	if err := a.VerifyEmail(testContext, "single@example.com", token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("second VerifyEmail error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyEmailInvalidInput(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "input@example.com")
	token := lastVerificationToken(t, a, mailer)

	tests := []struct {
		name  string
		email string
		token string
	}{
		{"unknown token", "input@example.com", "bogus-token"},
		{"empty token", "input@example.com", ""},
		{"empty email", "", token},
		{"other address", "other@example.com", token},
		{"token with whitespace", "input@example.com", " " + token},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := a.VerifyEmail(testContext, tt.email, tt.token); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("error = %v, want ErrInvalidToken", err)
			}
		})
	}
	// None of the failures consumed the real token.
	if err := a.VerifyEmail(testContext, "input@example.com", token); err != nil {
		t.Fatalf("VerifyEmail with the real token: %v", err)
	}
}

func TestVerifyEmailExpiredTokenDeleted(t *testing.T) {
	a, clock, mailer := newTestAuthWithMailer(t, Config{EmailVerificationTokenTTL: time.Hour})
	out := signUpUser(t, a, "expired-token@example.com")
	probe := lastVerificationToken(t, a, mailer)
	// A second token issued at the same instant, for the expiry check
	// below (redeeming the first consumes it).
	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	token := lastVerificationToken(t, a, mailer)

	// Valid exactly at the expiry instant.
	clock.Advance(time.Hour)
	if _, err := a.redeemToken(testContext, purposeEmailVerification, "expired-token@example.com", probe); err != nil {
		t.Fatalf("token rejected at its expiry instant: %v", err)
	}

	clock.Advance(time.Nanosecond)
	if err := a.VerifyEmail(testContext, "expired-token@example.com", token); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("error = %v, want ErrTokenExpired", err)
	}
	if _, err := a.store.GetVerificationToken(testContext, "email-verification:expired-token@example.com", HashToken(token)); !errors.Is(err, ErrNotFound) {
		t.Error("expired token was not deleted from the store")
	}
	// Once deleted, the same token is simply unknown.
	if err := a.VerifyEmail(testContext, "expired-token@example.com", token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("second attempt error = %v, want ErrInvalidToken", err)
	}
	// The user stays unverified.
	user, _ := a.GetUser(testContext, out.User.ID)
	if user.EmailVerified {
		t.Error("EmailVerified = true despite an expired token")
	}
}

func TestVerifyEmailMultipleOutstandingTokens(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "multi@example.com")
	waitForDeliveries(t, a)

	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("first SendVerificationEmail: %v", err)
	}
	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("second SendVerificationEmail: %v", err)
	}
	calls := mailer.verificationCalls()
	if len(calls) != 3 { // signup + two explicit sends
		t.Fatalf("verification calls = %d, want 3", len(calls))
	}

	// Every outstanding token is valid until consumed.
	if err := a.VerifyEmail(testContext, "multi@example.com", calls[1].Token); err != nil {
		t.Fatalf("VerifyEmail(first): %v", err)
	}
	// An older link still confirms the (already verified) address.
	if err := a.VerifyEmail(testContext, "multi@example.com", calls[2].Token); err != nil {
		t.Fatalf("VerifyEmail(second outstanding token): %v", err)
	}
	if got := tokenCount(memStore(t, a)); got != 1 {
		t.Errorf("tokenCount = %d, want 1 (the signup token remains)", got)
	}
}

func TestVerifyEmailNormalizesEmail(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "case@example.com")
	token := lastVerificationToken(t, a, mailer)

	if err := a.VerifyEmail(testContext, "  CASE@EXAMPLE.COM ", token); err != nil {
		t.Fatalf("VerifyEmail with a differently cased email: %v", err)
	}
}

// TestVerifyEmailRejectsPasswordResetToken is the regression test for
// the purpose confusion bug: both kinds of tokens used to share the
// plain email as identifier, so either one worked in both flows.
func TestVerifyEmailRejectsPasswordResetToken(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "purpose@example.com")
	if err := a.RequestPasswordReset(testContext, "purpose@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	resetToken := lastResetToken(t, a, mailer)

	if err := a.VerifyEmail(testContext, "purpose@example.com", resetToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyEmail with a reset token error = %v, want ErrInvalidToken", err)
	}
	user, _ := a.GetUser(testContext, out.User.ID)
	if user.EmailVerified {
		t.Error("a password reset token verified the email address")
	}
	// The reset token is untouched and still resets the password.
	if err := a.ResetPassword(testContext, "purpose@example.com", resetToken, resetNewPassword); err != nil {
		t.Fatalf("ResetPassword after the rejected verification attempt: %v", err)
	}
}

func TestVerifyEmailTokenOfDeletedUser(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "gone@example.com")
	token := lastVerificationToken(t, a, mailer)

	if err := a.DeleteUser(testContext, out.User.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if err := a.VerifyEmail(testContext, "gone@example.com", token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
}

// TestVerifyEmailTokenOfPreviousOwner covers a token that survived
// for whatever reason (for example a cleanup race) after its address
// was released: it must not act on the next owner of the address.
func TestVerifyEmailTokenOfPreviousOwner(t *testing.T) {
	a, clock, _ := newTestAuthWithMailer(t, Config{})
	store := memStore(t, a)

	// A leftover token issued an hour before the current owner
	// registered.
	leftover := "leftover-token"
	if err := store.CreateVerificationToken(testContext, &VerificationToken{
		Identifier: purposeEmailVerification.identifier("reused@example.com"),
		ValueHash:  HashToken(leftover),
		ExpiresAt:  clock.Now().Add(24 * time.Hour),
		CreatedAt:  clock.Now(),
	}); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}
	clock.Advance(time.Hour)
	out := signUpUser(t, a, "reused@example.com")

	if err := a.VerifyEmail(testContext, "reused@example.com", leftover); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
	if user, _ := a.GetUser(testContext, out.User.ID); user.EmailVerified {
		t.Error("a previous owner's token verified the new owner's address")
	}
	// The useless token was removed.
	if _, err := store.GetVerificationToken(testContext, purposeEmailVerification.identifier("reused@example.com"), HashToken(leftover)); !errors.Is(err, ErrNotFound) {
		t.Errorf("leftover token still stored: %v", err)
	}
}

func TestVerifyEmailStoreFailures(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "storefail@example.com")
	token := lastVerificationToken(t, a, mailer)
	store := a.store

	a.store = &faultyStore{Store: store, err: errStoreFailure, updateUser: true}
	if err := a.VerifyEmail(testContext, "storefail@example.com", token); !errors.Is(err, errStoreFailure) {
		t.Fatalf("error = %v, want the store failure", err)
	}
	// The token was consumed before the failing write (consume first),
	// so it cannot be replayed and the address stays unverified.
	a.store = store
	if err := a.VerifyEmail(testContext, "storefail@example.com", token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyEmail retry = %v, want ErrInvalidToken (token consumed)", err)
	}
	user, err := store.GetUserByEmail(testContext, "storefail@example.com")
	if err != nil || user.EmailVerified {
		t.Fatalf("user = %+v, %v; want unverified", user, err)
	}
}

// contextProbeMailer reports the context state seen by each send.
type contextProbeMailer struct {
	recordingMailer
	seen chan context.Context
}

// SendVerificationEmail implements Mailer.
func (m *contextProbeMailer) SendVerificationEmail(ctx context.Context, to, token string) error {
	err := m.recordingMailer.SendVerificationEmail(ctx, to, token)
	m.seen <- ctx
	return err
}

// probeKey is a context key used to check value propagation.
type probeKey struct{}

func TestBackgroundDeliveryOutlivesRequestContext(t *testing.T) {
	mailer := &contextProbeMailer{seen: make(chan context.Context, 1)}
	gate := make(chan struct{})
	mailer.gate = gate
	mailer.started = make(chan struct{}, 1)
	a, _ := newTestAuth(t, Config{Mailer: mailer})

	ctx, cancel := context.WithCancel(context.WithValue(testContext, probeKey{}, "trace-42"))
	if _, err := a.SignUp(ctx, SignUpInput{Name: "C", Email: "ctx@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	<-mailer.started
	cancel() // the request ends while the email is still being delivered
	close(gate)

	seen := <-mailer.seen
	if err := seen.Err(); err != nil {
		t.Errorf("delivery context error = %v, want nil (request cancellation must not propagate)", err)
	}
	if got := seen.Value(probeKey{}); got != "trace-42" {
		t.Errorf("delivery context value = %v, want the request's value", got)
	}
	waitForDeliveries(t, a)
	if calls := mailer.verificationCalls(); len(calls) != 1 {
		t.Fatalf("verification calls = %d, want 1", len(calls))
	}
}
