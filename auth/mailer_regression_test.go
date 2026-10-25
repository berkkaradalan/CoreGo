package auth

import (
	"context"
	"testing"

	"github.com/berkkaradalan/CoreGo/mail"
)

// TestDefaultMailerWorksWithBuiltInSenders is a regression guard for a
// bug where DefaultMailer composed messages without a From address and
// every built-in sender rejected them during validation, silently
// breaking the verification and reset flows for anyone using the
// documented default setup.
func TestDefaultMailerWorksWithBuiltInSenders(t *testing.T) {
	mailer := NewDefaultMailer(mail.NewLogSender(nil), DefaultMailerConfig{
		VerifyEmailURL:   "https://app.example.test/auth/verify-email",
		ResetPasswordURL: "https://app.example.test/reset-password",
	})
	ctx := context.Background()

	if err := mailer.SendVerificationEmail(ctx, "user@example.test", "verification-token"); err != nil {
		t.Fatalf("SendVerificationEmail through LogSender: %v", err)
	}
	if err := mailer.SendPasswordResetEmail(ctx, "user@example.test", "reset-token"); err != nil {
		t.Fatalf("SendPasswordResetEmail through LogSender: %v", err)
	}
}

// TestDefaultCompositionEndToEnd drives the documented default setup
// (service + DefaultMailer + a built-in sender) through both email
// flows using only what the emails contain, the way a user would.
func TestDefaultCompositionEndToEnd(t *testing.T) {
	sender := mail.NewCaptureSender()
	a, _ := newTestAuth(t, Config{
		RequireVerifiedEmail: true,
		Mailer:               NewDefaultMailer(sender, testMailerConfig),
	})

	if _, err := a.SignUp(testContext, SignUpInput{Name: "Ada", Email: "Ada@Example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	waitForDeliveries(t, a)
	link := linkFromText(t, lastMessage(t, sender).Text)
	if link.Host != "api.example.com" {
		t.Fatalf("verification link host = %q", link.Host)
	}
	if err := a.VerifyEmail(testContext, link.Query().Get("email"), link.Query().Get("token")); err != nil {
		t.Fatalf("VerifyEmail with the emailed link: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "ada@example.com", Password: testPassword}); err != nil {
		t.Fatalf("SignIn after verification: %v", err)
	}

	if err := a.RequestPasswordReset(testContext, "ada@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	waitForDeliveries(t, a)
	link = linkFromText(t, lastMessage(t, sender).Text)
	if link.Host != "app.example.com" {
		t.Fatalf("reset link host = %q", link.Host)
	}
	if err := a.ResetPassword(testContext, link.Query().Get("email"), link.Query().Get("token"), resetNewPassword); err != nil {
		t.Fatalf("ResetPassword with the emailed link: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "ada@example.com", Password: resetNewPassword}); err != nil {
		t.Fatalf("SignIn with the new password: %v", err)
	}
	if got := len(sender.Messages()); got != 2 {
		t.Errorf("messages sent = %d, want 2", got)
	}
}
