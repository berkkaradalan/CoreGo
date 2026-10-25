package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/berkkaradalan/CoreGo/mail"
)

// testMailerConfig is a complete DefaultMailerConfig.
var testMailerConfig = DefaultMailerConfig{
	VerifyEmailURL:   "https://api.example.com/auth/verify-email",
	ResetPasswordURL: "https://app.example.com/reset-password",
}

// newCapturingMailer returns a DefaultMailer over a CaptureSender.
func newCapturingMailer(cfg DefaultMailerConfig) (*DefaultMailer, *mail.CaptureSender) {
	sender := mail.NewCaptureSender()
	return NewDefaultMailer(sender, cfg), sender
}

// lastMessage returns the most recently captured message.
func lastMessage(t *testing.T, sender *mail.CaptureSender) mail.Message {
	t.Helper()
	msg, ok := sender.Last()
	if !ok {
		t.Fatal("no message was sent")
	}
	return msg
}

// linkFromText extracts the single link line of a plain text body.
func linkFromText(t *testing.T, text string) *url.URL {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			u, err := url.Parse(line)
			if err != nil {
				t.Fatalf("parsing link %q: %v", line, err)
			}
			return u
		}
	}
	t.Fatalf("no link in body:\n%s", text)
	return nil
}

func TestDefaultMailerImplementsMailer(t *testing.T) {
	var _ Mailer = NewDefaultMailer(nil, DefaultMailerConfig{})
}

func TestDefaultMailerSendVerificationEmail(t *testing.T) {
	mailer, sender := newCapturingMailer(testMailerConfig)

	if err := mailer.SendVerificationEmail(testContext, "user@example.com", "abc123"); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	msg := lastMessage(t, sender)
	if len(msg.To) != 1 || msg.To[0] != "user@example.com" {
		t.Errorf("To = %v, want [user@example.com]", msg.To)
	}
	if msg.From != "" {
		t.Errorf("From = %q, want empty (the sender supplies its default)", msg.From)
	}
	if msg.Subject != "Verify your email address" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	if msg.Text == "" || msg.HTML == "" {
		t.Fatalf("want both a text and an HTML part, got text=%d bytes html=%d bytes", len(msg.Text), len(msg.HTML))
	}
	wantLink := "https://api.example.com/auth/verify-email?email=user%40example.com&token=abc123"
	if !strings.Contains(msg.Text, wantLink) {
		t.Errorf("text body does not contain %q:\n%s", wantLink, msg.Text)
	}
	wantHref := `href="https://api.example.com/auth/verify-email?email=user%40example.com&amp;token=abc123"`
	if !strings.Contains(msg.HTML, wantHref) {
		t.Errorf("HTML body does not contain %q:\n%s", wantHref, msg.HTML)
	}
	if strings.Contains(msg.Text, "reset-password") || strings.Contains(msg.HTML, "reset-password") {
		t.Error("verification email links to the reset page")
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("message does not validate: %v", err)
	}
}

func TestDefaultMailerSendPasswordResetEmail(t *testing.T) {
	mailer, sender := newCapturingMailer(testMailerConfig)

	if err := mailer.SendPasswordResetEmail(testContext, "user@example.com", "tok123"); err != nil {
		t.Fatalf("SendPasswordResetEmail: %v", err)
	}
	msg := lastMessage(t, sender)
	if msg.Subject != "Reset your password" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	wantLink := "https://app.example.com/reset-password?email=user%40example.com&token=tok123"
	if !strings.Contains(msg.Text, wantLink) {
		t.Errorf("text body does not contain %q:\n%s", wantLink, msg.Text)
	}
	if !strings.Contains(msg.HTML, "Choose a new password") {
		t.Errorf("HTML body lacks the call to action:\n%s", msg.HTML)
	}
	if strings.Contains(msg.Text, "verify-email") {
		t.Error("reset email links to the verification route")
	}
}

func TestDefaultMailerAppName(t *testing.T) {
	cfg := testMailerConfig
	cfg.AppName = "  Acme\r\nCloud  "
	mailer, sender := newCapturingMailer(cfg)

	if err := mailer.SendVerificationEmail(testContext, "user@example.com", "t"); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	msg := lastMessage(t, sender)
	if msg.Subject != "Verify your email for Acme Cloud" {
		t.Errorf("Subject = %q, want whitespace collapsed", msg.Subject)
	}
	if !strings.Contains(msg.Text, "for Acme Cloud") || !strings.Contains(msg.HTML, "for Acme Cloud") {
		t.Errorf("bodies do not mention the app name:\n%s\n%s", msg.Text, msg.HTML)
	}

	if err := mailer.SendPasswordResetEmail(testContext, "user@example.com", "t"); err != nil {
		t.Fatalf("SendPasswordResetEmail: %v", err)
	}
	if msg := lastMessage(t, sender); msg.Subject != "Reset your password for Acme Cloud" {
		t.Errorf("Subject = %q", msg.Subject)
	}
}

func TestDefaultMailerEscapesHTML(t *testing.T) {
	cfg := testMailerConfig
	cfg.AppName = `<script>alert("x")</script> & Co`
	cfg.VerifyEmailURL = `https://api.example.com/verify?next="><img src=x>`
	mailer, sender := newCapturingMailer(cfg)

	if err := mailer.SendVerificationEmail(testContext, "user@example.com", `"><b>tok`); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	msg := lastMessage(t, sender)
	for _, raw := range []string{"<script>", "<img", "<b>"} {
		if strings.Contains(msg.HTML, raw) {
			t.Errorf("HTML body contains unescaped %q:\n%s", raw, msg.HTML)
		}
	}
	if !strings.Contains(msg.HTML, "&lt;script&gt;") || !strings.Contains(msg.HTML, "&amp; Co") {
		t.Errorf("HTML body does not contain the escaped app name:\n%s", msg.HTML)
	}
	// The plain text part is not HTML and keeps the name verbatim.
	if !strings.Contains(msg.Text, `<script>alert("x")</script> & Co`) {
		t.Errorf("text body changed the app name:\n%s", msg.Text)
	}
}

func TestDefaultMailerLinkBuilding(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		email string
		token string
		want  url.Values
		path  string
	}{
		{
			name: "plain", base: "https://api.example.com/auth/verify-email",
			email: "user@example.com", token: "abc",
			want: url.Values{"email": {"user@example.com"}, "token": {"abc"}}, path: "/auth/verify-email",
		},
		{
			name: "existing query preserved", base: "https://app.example.com/verify?lang=en&ref=mail",
			email: "user@example.com", token: "abc",
			want: url.Values{"lang": {"en"}, "ref": {"mail"}, "email": {"user@example.com"}, "token": {"abc"}}, path: "/verify",
		},
		{
			name: "existing token and email replaced", base: "https://app.example.com/verify?token=stale&email=old%40example.com",
			email: "user@example.com", token: "fresh",
			want: url.Values{"email": {"user@example.com"}, "token": {"fresh"}}, path: "/verify",
		},
		{
			name: "reserved characters escaped", base: "https://app.example.com/verify",
			email: "first+tag@example.com", token: "a+b/c&d=e?f#g",
			want: url.Values{"email": {"first+tag@example.com"}, "token": {"a+b/c&d=e?f#g"}}, path: "/verify",
		},
		{
			name: "root path and port", base: "http://localhost:8080",
			email: "user@example.com", token: "abc",
			want: url.Values{"email": {"user@example.com"}, "token": {"abc"}}, path: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testMailerConfig
			cfg.VerifyEmailURL = tt.base
			mailer, sender := newCapturingMailer(cfg)
			if err := mailer.SendVerificationEmail(testContext, tt.email, tt.token); err != nil {
				t.Fatalf("SendVerificationEmail: %v", err)
			}
			link := linkFromText(t, lastMessage(t, sender).Text)
			if link.Path != tt.path {
				t.Errorf("path = %q, want %q", link.Path, tt.path)
			}
			got := link.Query()
			if len(got) != len(tt.want) {
				t.Errorf("query = %v, want %v", got, tt.want)
			}
			for key, values := range tt.want {
				if got.Get(key) != values[0] || len(got[key]) != 1 {
					t.Errorf("query[%q] = %v, want %v", key, got[key], values)
				}
			}
		})
	}
}

func TestDefaultMailerLinkKeepsFragment(t *testing.T) {
	cfg := testMailerConfig
	cfg.ResetPasswordURL = "https://app.example.com/#/reset"
	mailer, sender := newCapturingMailer(cfg)
	if err := mailer.SendPasswordResetEmail(testContext, "user@example.com", "abc"); err != nil {
		t.Fatalf("SendPasswordResetEmail: %v", err)
	}
	want := "https://app.example.com/?email=user%40example.com&token=abc#/reset"
	if !strings.Contains(lastMessage(t, sender).Text, want) {
		t.Errorf("text body does not contain %q:\n%s", want, lastMessage(t, sender).Text)
	}
}

func TestDefaultMailerInvalidURLConfig(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantInErr string
	}{
		{"empty", "", "is empty"},
		{"whitespace", "   ", "is empty"},
		{"relative path", "/auth/verify-email", "absolute http or https URL"},
		{"missing host", "https://", "absolute http or https URL"},
		{"javascript scheme", "javascript:alert(1)", "absolute http or https URL"},
		{"ftp scheme", "ftp://files.example.com/verify", "absolute http or https URL"},
		{"unparseable", "http://[::1", "not a valid URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verify := testMailerConfig
			verify.VerifyEmailURL = tt.url
			mailer, sender := newCapturingMailer(verify)
			err := mailer.SendVerificationEmail(testContext, "user@example.com", "secret-token-value")
			if err == nil {
				t.Fatal("SendVerificationEmail succeeded with an invalid URL")
			}
			if !strings.Contains(err.Error(), "VerifyEmailURL") || !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error = %q, want it to name VerifyEmailURL and contain %q", err, tt.wantInErr)
			}
			if strings.Contains(err.Error(), "secret-token-value") {
				t.Errorf("error leaks the token: %v", err)
			}
			if _, sent := sender.Last(); sent {
				t.Error("a message was sent despite the invalid configuration")
			}

			reset := testMailerConfig
			reset.ResetPasswordURL = tt.url
			mailer, _ = newCapturingMailer(reset)
			err = mailer.SendPasswordResetEmail(testContext, "user@example.com", "secret-token-value")
			if err == nil || !strings.Contains(err.Error(), "ResetPasswordURL") {
				t.Errorf("SendPasswordResetEmail error = %v, want one naming ResetPasswordURL", err)
			}
		})
	}
}

func TestDefaultMailerZeroConfigErrorsInsteadOfPanicking(t *testing.T) {
	mailer := NewDefaultMailer(mail.NewCaptureSender(), DefaultMailerConfig{})
	if err := mailer.SendVerificationEmail(testContext, "user@example.com", "t"); err == nil {
		t.Error("SendVerificationEmail with zero config succeeded")
	}
	if err := mailer.SendPasswordResetEmail(testContext, "user@example.com", "t"); err == nil {
		t.Error("SendPasswordResetEmail with zero config succeeded")
	}
}

func TestDefaultMailerPropagatesSenderError(t *testing.T) {
	senderErr := errors.New("smtp unavailable")
	sender := mail.SenderFunc(func(_ context.Context, _ mail.Message) error {
		return senderErr
	})
	mailer := NewDefaultMailer(sender, testMailerConfig)

	err := mailer.SendVerificationEmail(testContext, "user@example.com", "secret-token-value")
	if !errors.Is(err, senderErr) {
		t.Errorf("error = %v, does not wrap the sender cause", err)
	}
	if err != nil && strings.Contains(err.Error(), "secret-token-value") {
		t.Errorf("error leaks the token: %v", err)
	}

	err = mailer.SendPasswordResetEmail(testContext, "user@example.com", "t")
	if !errors.Is(err, senderErr) {
		t.Errorf("error = %v, does not wrap the sender cause", err)
	}
}

func TestDefaultMailerNilSender(t *testing.T) {
	mailer := NewDefaultMailer(nil, testMailerConfig)
	if err := mailer.SendVerificationEmail(testContext, "user@example.com", "t"); err == nil {
		t.Fatal("SendVerificationEmail with nil sender succeeded, want error")
	}
	if err := mailer.SendPasswordResetEmail(testContext, "user@example.com", "t"); err == nil {
		t.Fatal("SendPasswordResetEmail with nil sender succeeded, want error")
	}
}

func TestDefaultMailerPassesContext(t *testing.T) {
	type key struct{}
	var seen any
	sender := mail.SenderFunc(func(ctx context.Context, _ mail.Message) error {
		seen = ctx.Value(key{})
		return nil
	})
	mailer := NewDefaultMailer(sender, testMailerConfig)
	ctx := context.WithValue(testContext, key{}, "value")
	if err := mailer.SendVerificationEmail(ctx, "user@example.com", "t"); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	if seen != "value" {
		t.Errorf("sender saw context value %v, want %q", seen, "value")
	}
}
