package mongostore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/berkkaradalan/CoreGo/auth"
)

// captureMailer records the tokens the auth service sends, keyed by
// recipient, so the end-to-end flows can redeem them.
type captureMailer struct {
	mu           sync.Mutex
	verification map[string][]string
	reset        map[string][]string
}

func newCaptureMailer() *captureMailer {
	return &captureMailer{verification: map[string][]string{}, reset: map[string][]string{}}
}

func (m *captureMailer) SendVerificationEmail(_ context.Context, to, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verification[to] = append(m.verification[to], token)
	return nil
}

func (m *captureMailer) SendPasswordResetEmail(_ context.Context, to, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reset[to] = append(m.reset[to], token)
	return nil
}

// lastVerification returns the newest verification token sent to.
func (m *captureMailer) lastVerification(t *testing.T, to string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	tokens := m.verification[to]
	if len(tokens) == 0 {
		t.Fatalf("no verification email was sent to %s", to)
	}
	return tokens[len(tokens)-1]
}

// lastReset returns the newest password reset token sent to.
func (m *captureMailer) lastReset(t *testing.T, to string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	tokens := m.reset[to]
	if len(tokens) == 0 {
		t.Fatalf("no password reset email was sent to %s", to)
	}
	return tokens[len(tokens)-1]
}

// waitForDeliveries waits until the service has handed every background
// email to the mailer.
func waitForDeliveries(t *testing.T, svc *auth.Auth) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// TestIntegrationServiceConformance runs the auth service end to end on
// this store: every flow of the service must behave exactly as it does
// on the in-memory reference store.
func TestIntegrationServiceConformance(t *testing.T) {
	store, db := newIntegrationStore(t)
	ctx := context.Background()
	mailer := newCaptureMailer()
	svc, err := auth.New(auth.Config{Mailer: mailer}, store)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	const (
		email    = "ada@example.com"
		password = "correct-horse-1"
	)

	// Sign up: a session is created and a verification email is sent in
	// the background.
	signUp, err := svc.SignUp(ctx, auth.SignUpInput{Name: "Ada", Email: " Ada@Example.com ", Password: password, UserAgent: "test"})
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	userID := signUp.User.ID
	if signUp.SessionToken == "" || signUp.Session == nil {
		t.Fatal("SignUp did not create a session")
	}
	if _, err := svc.SignUp(ctx, auth.SignUpInput{Name: "Ada", Email: email, Password: password}); !errors.Is(err, auth.ErrUserAlreadyExists) {
		t.Fatalf("second SignUp error = %v, want ErrUserAlreadyExists", err)
	}
	waitForDeliveries(t, svc)
	verifyToken := mailer.lastVerification(t, email)

	_, user, err := svc.GetSession(ctx, signUp.SessionToken)
	if err != nil || user.ID != userID || user.EmailVerified {
		t.Fatalf("GetSession = %+v, %v", user, err)
	}

	// Token purposes are separated and unknown tokens are rejected.
	if err := svc.ResetPassword(ctx, email, verifyToken, "another-pass-1"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("ResetPassword(verification token) error = %v, want ErrInvalidToken", err)
	}
	if err := svc.VerifyEmail(ctx, email, "not-a-token"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("VerifyEmail(unknown token) error = %v, want ErrInvalidToken", err)
	}

	// Verify the email address.
	if err := svc.VerifyEmail(ctx, email, verifyToken); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if err := svc.VerifyEmail(ctx, email, verifyToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("VerifyEmail(reused token) error = %v, want ErrInvalidToken", err)
	}
	if got, err := svc.GetUser(ctx, userID); err != nil || !got.EmailVerified {
		t.Fatalf("GetUser after verification = %+v, %v", got, err)
	}

	// A second session, then reset the password: every session ends.
	signIn, err := svc.SignIn(ctx, auth.SignInInput{Email: email, Password: password, RememberMe: true})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if sessions, err := svc.ListSessions(ctx, userID); err != nil || len(sessions) != 2 {
		t.Fatalf("ListSessions = %d sessions, %v; want 2", len(sessions), err)
	}
	if err := svc.RequestPasswordReset(ctx, email); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	if err := svc.RequestPasswordReset(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset(unknown) error = %v, want nil", err)
	}
	waitForDeliveries(t, svc)
	resetToken := mailer.lastReset(t, email)
	if err := svc.VerifyEmail(ctx, email, resetToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("VerifyEmail(reset token) error = %v, want ErrInvalidToken", err)
	}
	const newPassword = "battery-staple-2"
	if err := svc.ResetPassword(ctx, email, resetToken, newPassword); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	for _, token := range []string{signUp.SessionToken, signIn.SessionToken} {
		if _, _, err := svc.GetSession(ctx, token); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Fatalf("GetSession after reset error = %v, want ErrSessionNotFound", err)
		}
	}
	if _, err := svc.SignIn(ctx, auth.SignInInput{Email: email, Password: password}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("SignIn(old password) error = %v, want ErrInvalidCredentials", err)
	}
	session, err := svc.SignIn(ctx, auth.SignInInput{Email: email, Password: newPassword})
	if err != nil {
		t.Fatalf("SignIn(new password): %v", err)
	}

	// Change the email: the old address's outstanding tokens die, the
	// new address must be verified again.
	if err := svc.RequestPasswordReset(ctx, email); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	waitForDeliveries(t, svc)
	staleReset := mailer.lastReset(t, email)
	const newEmail = "ada.lovelace@example.com"
	if _, err := svc.ChangeEmail(ctx, userID, newEmail, "wrong-password"); !errors.Is(err, auth.ErrInvalidCurrentPassword) {
		t.Fatalf("ChangeEmail(wrong password) error = %v, want ErrInvalidCurrentPassword", err)
	}
	changed, err := svc.ChangeEmail(ctx, userID, newEmail, newPassword)
	if err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if changed.Email != newEmail || changed.EmailVerified {
		t.Fatalf("ChangeEmail returned %+v", changed)
	}
	waitForDeliveries(t, svc)
	if err := svc.ResetPassword(ctx, email, staleReset, "whatever-pass-3"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("ResetPassword(old address token) error = %v, want ErrInvalidToken", err)
	}
	if n := countDocs(t, db, "corego_verification_tokens", bson.D{{Key: "identifier", Value: bson.D{{Key: "$regex", Value: ":" + email + "$"}}}}); n != 0 {
		t.Fatalf("%d tokens of the old address survived the email change", n)
	}
	if err := svc.VerifyEmail(ctx, newEmail, mailer.lastVerification(t, newEmail)); err != nil {
		t.Fatalf("VerifyEmail(new address): %v", err)
	}
	if _, err := svc.SignIn(ctx, auth.SignInInput{Email: newEmail, Password: newPassword}); err != nil {
		t.Fatalf("SignIn(new address): %v", err)
	}
	if accounts, err := svc.ListAccounts(ctx, userID); err != nil || len(accounts) != 1 || accounts[0].ProviderAccountID != newEmail {
		t.Fatalf("ListAccounts = %+v, %v; want the credential account synced to the new address", accounts, err)
	}

	// Profile, sessions and admin operations.
	name := "Ada Lovelace"
	if got, err := svc.UpdateUser(ctx, userID, auth.UpdateUserInput{Name: &name}); err != nil || got.Name != name {
		t.Fatalf("UpdateUser = %+v, %v", got, err)
	}
	if err := svc.RevokeOtherSessions(ctx, userID, session.Session.ID); err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}
	if sessions, err := svc.ListSessions(ctx, userID); err != nil || len(sessions) != 1 || sessions[0].ID != session.Session.ID {
		t.Fatalf("ListSessions after RevokeOtherSessions = %+v, %v", sessions, err)
	}
	if n, err := svc.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("CountUsers = %d, %v", n, err)
	}
	if users, err := svc.ListUsers(ctx, auth.ListOptions{}); err != nil || len(users) != 1 {
		t.Fatalf("ListUsers = %d users, %v", len(users), err)
	}
	banEnds := time.Now().Add(time.Hour)
	if err := svc.BanUser(ctx, userID, "testing", &banEnds); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	if _, err := svc.SignIn(ctx, auth.SignInInput{Email: newEmail, Password: newPassword}); !errors.Is(err, auth.ErrUserBanned) {
		t.Fatalf("SignIn(banned) error = %v, want ErrUserBanned", err)
	}
	if err := svc.UnbanUser(ctx, userID); err != nil {
		t.Fatalf("UnbanUser: %v", err)
	}
	if err := svc.CleanupExpired(ctx); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}

	// Delete the user: nothing that belongs to it survives.
	if err := svc.RequestPasswordReset(ctx, newEmail); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	waitForDeliveries(t, svc)
	if err := svc.CheckPassword(ctx, userID, newPassword); err != nil {
		t.Fatalf("CheckPassword: %v", err)
	}
	if err := svc.DeleteUser(ctx, userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := svc.GetUser(ctx, userID); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("GetUser(deleted) error = %v, want ErrUserNotFound", err)
	}
	if _, _, err := svc.GetSession(ctx, session.SessionToken); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("GetSession(deleted user) error = %v, want ErrSessionNotFound", err)
	}
	for _, collection := range []string{"corego_users", "corego_accounts", "corego_sessions", "corego_verification_tokens"} {
		if n := countDocs(t, db, collection, bson.D{}); n != 0 {
			t.Fatalf("%s still holds %d documents after DeleteUser", collection, n)
		}
	}
	if err := svc.DeleteUser(ctx, userID); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("DeleteUser(again) error = %v, want ErrUserNotFound", err)
	}
}

// TestIntegrationServiceRequireVerifiedEmail covers the verified-email
// policy on this store.
func TestIntegrationServiceRequireVerifiedEmail(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()
	mailer := newCaptureMailer()
	svc, err := auth.New(auth.Config{Mailer: mailer, RequireVerifiedEmail: true}, store)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	const email, password = "grace@example.com", "compiler-pass-1"
	out, err := svc.SignUp(ctx, auth.SignUpInput{Name: "Grace", Email: email, Password: password})
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	if out.SessionToken != "" || out.Session != nil {
		t.Fatal("SignUp created a session although verification is required")
	}
	if _, err := svc.SignIn(ctx, auth.SignInInput{Email: email, Password: password}); !errors.Is(err, auth.ErrEmailNotVerified) {
		t.Fatalf("SignIn(unverified) error = %v, want ErrEmailNotVerified", err)
	}

	// The public resend flow issues another token; both stay valid.
	if err := svc.RequestEmailVerification(ctx, email); err != nil {
		t.Fatalf("RequestEmailVerification: %v", err)
	}
	waitForDeliveries(t, svc)
	if err := svc.VerifyEmail(ctx, email, mailer.lastVerification(t, email)); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	signIn, err := svc.SignIn(ctx, auth.SignInInput{Email: email, Password: password})
	if err != nil {
		t.Fatalf("SignIn(verified): %v", err)
	}
	if _, _, err := svc.GetSession(ctx, signIn.SessionToken); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if err := svc.SignOut(ctx, signIn.SessionToken); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if _, _, err := svc.GetSession(ctx, signIn.SessionToken); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("GetSession after SignOut error = %v, want ErrSessionNotFound", err)
	}
}
