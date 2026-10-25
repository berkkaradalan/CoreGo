package pgstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
)

// recordingMailer is an auth.Mailer that remembers the last token sent
// to every address, per email kind.
type recordingMailer struct {
	mu           sync.Mutex
	verification map[string]string
	reset        map[string]string
}

func newRecordingMailer() *recordingMailer {
	return &recordingMailer{verification: map[string]string{}, reset: map[string]string{}}
}

// SendVerificationEmail implements auth.Mailer.
func (m *recordingMailer) SendVerificationEmail(_ context.Context, to, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verification[to] = token
	return nil
}

// SendPasswordResetEmail implements auth.Mailer.
func (m *recordingMailer) SendPasswordResetEmail(_ context.Context, to, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reset[to] = token
	return nil
}

// verificationToken returns the last verification token sent to addr.
func (m *recordingMailer) verificationToken(t *testing.T, addr string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	token, ok := m.verification[addr]
	if !ok {
		t.Fatalf("no verification email was sent to %s", addr)
	}
	return token
}

// resetToken returns the last password reset token sent to addr.
func (m *recordingMailer) resetToken(t *testing.T, addr string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	token, ok := m.reset[addr]
	if !ok {
		t.Fatalf("no password reset email was sent to %s", addr)
	}
	return token
}

// waitForDeliveries blocks until the service's background emails have
// been handed to the mailer.
func waitForDeliveries(t *testing.T, service *auth.Auth) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// TestIntegrationAuthServiceEndToEnd runs the complete account
// lifecycle of the auth service on a PostgreSQL store.
func TestIntegrationAuthServiceEndToEnd(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)
	mailer := newRecordingMailer()
	service, err := auth.New(auth.Config{Mailer: mailer}, store)
	wantNoErr(t, "auth.New", err)

	const (
		email       = "ada@example.com"
		password    = "correct horse battery"
		newPassword = "another long password"
		newEmail    = "ada.king@example.com"
	)

	// Sign up (with a mixed-case address the service normalizes).
	signUp, err := service.SignUp(ctx, auth.SignUpInput{Name: "Ada", Email: "Ada@Example.com", Password: password, IPAddress: "203.0.113.9", UserAgent: "e2e"})
	wantNoErr(t, "SignUp", err)
	if signUp.SessionToken == "" || signUp.User.Email != email {
		t.Fatalf("SignUp output = %+v", signUp)
	}
	userID := signUp.User.ID
	session, user, err := service.GetSession(ctx, signUp.SessionToken)
	wantNoErr(t, "GetSession", err)
	if user.ID != userID || session.IPAddress != "203.0.113.9" || user.EmailVerified {
		t.Fatalf("GetSession = %+v, %+v", session, user)
	}

	// Verify the email with the token from the sign-up email. The token
	// is created right after the user, typically within the same
	// millisecond, which exercises the CreatedAt comparison.
	waitForDeliveries(t, service)
	wantErrIs(t, "VerifyEmail(wrong token)", service.VerifyEmail(ctx, email, "wrong"), auth.ErrInvalidToken)
	wantNoErr(t, "VerifyEmail", service.VerifyEmail(ctx, email, mailer.verificationToken(t, email)))
	user, err = service.GetUser(ctx, userID)
	wantNoErr(t, "GetUser", err)
	if !user.EmailVerified {
		t.Fatal("email not verified after VerifyEmail")
	}
	wantErrIs(t, "VerifyEmail(reused token)", service.VerifyEmail(ctx, email, mailer.verificationToken(t, email)), auth.ErrInvalidToken)

	// Password reset; a verification token can never reset a password.
	wantNoErr(t, "RequestPasswordReset", service.RequestPasswordReset(ctx, email))
	wantNoErr(t, "RequestPasswordReset(unknown)", service.RequestPasswordReset(ctx, "nobody@example.com"))
	waitForDeliveries(t, service)
	resetToken := mailer.resetToken(t, email)
	wantErrIs(t, "VerifyEmail(reset token)", service.VerifyEmail(ctx, email, resetToken), auth.ErrInvalidToken)
	wantNoErr(t, "ResetPassword", service.ResetPassword(ctx, email, resetToken, newPassword))
	_, _, err = service.GetSession(ctx, signUp.SessionToken)
	if err == nil {
		t.Fatal("the session survived a password reset")
	}
	_, err = service.SignIn(ctx, auth.SignInInput{Email: email, Password: password})
	wantErrIs(t, "SignIn(old password)", err, auth.ErrInvalidCredentials)
	signIn, err := service.SignIn(ctx, auth.SignInInput{Email: email, Password: newPassword})
	wantNoErr(t, "SignIn(new password)", err)

	// Change the email: the old address's outstanding tokens are gone,
	// the new address gets a verification email.
	wantNoErr(t, "RequestPasswordReset(before email change)", service.RequestPasswordReset(ctx, email))
	waitForDeliveries(t, service)
	staleReset := mailer.resetToken(t, email)
	_, err = service.ChangeEmail(ctx, userID, newEmail, "wrong password")
	wantErrIs(t, "ChangeEmail(wrong password)", err, auth.ErrInvalidCurrentPassword)
	changed, err := service.ChangeEmail(ctx, userID, newEmail, newPassword)
	wantNoErr(t, "ChangeEmail", err)
	if changed.Email != newEmail || changed.EmailVerified {
		t.Fatalf("ChangeEmail returned %+v", changed)
	}
	if n := countRows(t, store, "corego_verification_tokens", "identifier LIKE '%' || $1", ":"+email); n != 0 {
		t.Fatalf("%d tokens of the old address survived the email change", n)
	}
	wantErrIs(t, "ResetPassword(stale token)", service.ResetPassword(ctx, email, staleReset, "yet another password"), auth.ErrInvalidToken)
	waitForDeliveries(t, service)
	wantNoErr(t, "VerifyEmail(new address)", service.VerifyEmail(ctx, newEmail, mailer.verificationToken(t, newEmail)))
	account, err := store.GetAccountByUser(ctx, userID, auth.CredentialProvider)
	wantNoErr(t, "GetAccountByUser", err)
	if account.ProviderAccountID != newEmail {
		t.Fatalf("credential account id = %q, want %q", account.ProviderAccountID, newEmail)
	}
	if _, _, err := service.GetSession(ctx, signIn.SessionToken); err != nil {
		t.Fatalf("GetSession after email change: %v", err)
	}
	_, err = service.SignIn(ctx, auth.SignInInput{Email: newEmail, Password: newPassword})
	wantNoErr(t, "SignIn(new email)", err)

	// Housekeeping runs on this store.
	wantNoErr(t, "CleanupExpired", service.CleanupExpired(ctx))
	sessions, err := service.ListSessions(ctx, userID)
	wantNoErr(t, "ListSessions", err)
	if len(sessions) != 2 {
		t.Fatalf("ListSessions = %d sessions, want 2", len(sessions))
	}

	// Delete the user: everything belonging to it disappears.
	wantNoErr(t, "RequestPasswordReset(before delete)", service.RequestPasswordReset(ctx, newEmail))
	waitForDeliveries(t, service)
	wantNoErr(t, "DeleteUser", service.DeleteUser(ctx, userID))
	_, err = service.GetUser(ctx, userID)
	wantErrIs(t, "GetUser after delete", err, auth.ErrUserNotFound)
	for _, table := range []string{"corego_users", "corego_accounts", "corego_sessions", "corego_verification_tokens"} {
		if n := countRows(t, store, table, ""); n != 0 {
			t.Fatalf("%s has %d rows after DeleteUser, want 0", table, n)
		}
	}
	_, _, err = service.GetSession(ctx, signIn.SessionToken)
	if err == nil {
		t.Fatal("session survived DeleteUser")
	}
	waitForDeliveries(t, service)
}

// TestIntegrationAuthServiceRequireVerifiedEmail checks the verified
// email gate on the PostgreSQL store.
func TestIntegrationAuthServiceRequireVerifiedEmail(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)
	mailer := newRecordingMailer()
	service, err := auth.New(auth.Config{Mailer: mailer, RequireVerifiedEmail: true}, store)
	wantNoErr(t, "auth.New", err)

	const email, password = "grace@example.com", "a sufficiently long password"
	signUp, err := service.SignUp(ctx, auth.SignUpInput{Name: "Grace", Email: email, Password: password})
	wantNoErr(t, "SignUp", err)
	if signUp.SessionToken != "" || signUp.Session != nil {
		t.Fatal("SignUp created a session although verification is required")
	}
	if n := countRows(t, store, "corego_sessions", ""); n != 0 {
		t.Fatalf("%d sessions stored before verification", n)
	}
	_, err = service.SignIn(ctx, auth.SignInInput{Email: email, Password: password})
	wantErrIs(t, "SignIn(unverified)", err, auth.ErrEmailNotVerified)

	// The public resend flow issues a second token; both are valid.
	wantNoErr(t, "RequestEmailVerification", service.RequestEmailVerification(ctx, email))
	waitForDeliveries(t, service)
	wantNoErr(t, "VerifyEmail", service.VerifyEmail(ctx, email, mailer.verificationToken(t, email)))
	signIn, err := service.SignIn(ctx, auth.SignInInput{Email: email, Password: password})
	wantNoErr(t, "SignIn(verified)", err)
	if signIn.SessionToken == "" {
		t.Fatal("SignIn returned no session token")
	}
	if err := service.RequestEmailVerification(ctx, email); err != nil {
		t.Fatalf("RequestEmailVerification(verified) = %v, want nil", err)
	}
	if errors.Is(err, auth.ErrEmailAlreadyVerified) {
		t.Fatal("public resend revealed the verification state")
	}
	waitForDeliveries(t, service)
}
