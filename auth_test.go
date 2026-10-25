package corego

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/mail"
)

func TestDefaultMailerConfig(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		basePath   string
		wantVerify string
		wantReset  string
	}{
		{
			name:       "defaults",
			wantVerify: "http://localhost:8080/auth/verify-email",
			wantReset:  "http://localhost:8080/reset-password",
		},
		{
			name:       "PORT changes the default APP_URL",
			env:        map[string]string{"PORT": "3000"},
			wantVerify: "http://localhost:3000/auth/verify-email",
			wantReset:  "http://localhost:3000/reset-password",
		},
		{
			name:       "APP_URL with a trailing slash",
			env:        map[string]string{"APP_URL": "https://api.example.com/"},
			wantVerify: "https://api.example.com/auth/verify-email",
			wantReset:  "https://api.example.com/reset-password",
		},
		{
			name:       "APP_URL with a path prefix",
			env:        map[string]string{"APP_URL": "https://example.com/backend"},
			wantVerify: "https://example.com/backend/auth/verify-email",
			wantReset:  "https://example.com/backend/reset-password",
		},
		{
			name:       "legacy URL variable",
			env:        map[string]string{"URL": "https://legacy.example.com"},
			wantVerify: "https://legacy.example.com/auth/verify-email",
			wantReset:  "https://legacy.example.com/reset-password",
		},
		{
			name:       "custom base path",
			env:        map[string]string{"APP_URL": "https://api.example.com"},
			basePath:   "/api/v1/auth",
			wantVerify: "https://api.example.com/api/v1/auth/verify-email",
			wantReset:  "https://api.example.com/reset-password",
		},
		{
			name:       "base path with a trailing slash",
			env:        map[string]string{"APP_URL": "https://api.example.com"},
			basePath:   "/account/",
			wantVerify: "https://api.example.com/account/verify-email",
			wantReset:  "https://api.example.com/reset-password",
		},
		{
			name:       "routes mounted at the root",
			env:        map[string]string{"APP_URL": "https://api.example.com"},
			basePath:   "/",
			wantVerify: "https://api.example.com/verify-email",
			wantReset:  "https://api.example.com/reset-password",
		},
		{
			name: "COREGO_RESET_PASSWORD_URL points at the frontend",
			env: map[string]string{
				"APP_URL":                   "https://api.example.com",
				"COREGO_RESET_PASSWORD_URL": " https://app.example.com/account/reset?source=email ",
			},
			wantVerify: "https://api.example.com/auth/verify-email",
			wantReset:  "https://app.example.com/account/reset?source=email",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			core := newCore(t, &Config{Auth: &AuthOptions{HTTP: authhttp.Config{BasePath: tt.basePath}}})

			got := core.defaultMailerConfig(core.AuthHandlers().BasePath())
			if got.VerifyEmailURL != tt.wantVerify {
				t.Errorf("VerifyEmailURL = %q, want %q", got.VerifyEmailURL, tt.wantVerify)
			}
			if got.ResetPasswordURL != tt.wantReset {
				t.Errorf("ResetPasswordURL = %q, want %q", got.ResetPasswordURL, tt.wantReset)
			}
		})
	}
}

// TestVerificationLinkFollowsBasePath checks that the link in the
// verification email sent through the facade opens the mounted GET
// verify-email route for every form of AuthOptions.HTTP.BasePath.
func TestVerificationLinkFollowsBasePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		wantPath string
	}{
		{name: "default", basePath: "", wantPath: "/auth/verify-email"},
		{name: "root", basePath: "/", wantPath: "/verify-email"},
		{name: "trailing slash", basePath: "/account/", wantPath: "/account/verify-email"},
		{name: "nested", basePath: "/api/v1/auth", wantPath: "/api/v1/auth/verify-email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("APP_URL", "https://api.example.com")
			sender := mail.NewCaptureSender()
			core := newCore(t, &Config{Auth: &AuthOptions{
				MailSender: sender,
				HTTP:       authhttp.Config{BasePath: tt.basePath},
			}})
			app := core.Server()

			signup := strings.TrimSuffix(tt.wantPath, "/verify-email") + "/signup"
			w := do(t, app, request{method: http.MethodPost, path: signup, body: map[string]any{
				"name": "Ada", "email": "ada@example.com", "password": "analytical-engine",
			}})
			if w.Code != http.StatusCreated {
				t.Fatalf("signup status = %d, body %s", w.Code, w.Body.String())
			}
			if err := core.Auth.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}

			link := lastEmailLink(t, sender)
			if got := link.Scheme + "://" + link.Host + link.Path; got != "https://api.example.com"+tt.wantPath {
				t.Fatalf("verification link target = %q, want %q", got, "https://api.example.com"+tt.wantPath)
			}
			w = do(t, app, request{method: http.MethodGet, path: link.RequestURI()})
			if w.Code != http.StatusOK {
				t.Fatalf("GET verification link = %d, body %s", w.Code, w.Body.String())
			}
		})
	}
}

// linkPattern finds the first absolute link in a plain text email.
var linkPattern = regexp.MustCompile(`https?://\S+`)

// lastEmailLink returns the link of the most recent captured email.
func lastEmailLink(t *testing.T, sender *mail.CaptureSender) *url.URL {
	t.Helper()
	msg, ok := sender.Last()
	if !ok {
		t.Fatal("no email was sent")
	}
	raw := linkPattern.FindString(msg.Text)
	if raw == "" {
		t.Fatalf("email has no link:\n%s", msg.Text)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse link %q: %v", raw, err)
	}
	return u
}

// TestMailSenderEndToEnd runs the default email composition through the
// facade: the verification link of the DefaultMailer opens the mounted
// GET /auth/verify-email route, and the reset link carries a token the
// reset route accepts.
func TestMailSenderEndToEnd(t *testing.T) {
	isolateEnv(t)
	t.Setenv("APP_URL", "https://api.example.com")
	t.Setenv("COREGO_RESET_PASSWORD_URL", "https://app.example.com/reset")
	sender := mail.NewCaptureSender()
	logger, logs := newTestLogger()

	core := newCore(t, &Config{
		Auth:   &AuthOptions{MailSender: sender, HTTP: authhttp.Config{BasePath: "/v1/auth"}},
		Logger: logger,
	})
	if strings.Contains(logs.String(), "auth emails are disabled") {
		t.Fatalf("no-mailer warning logged although MailSender is set; logs:\n%s", logs)
	}
	app := core.Server()

	w := do(t, app, request{method: http.MethodPost, path: "/v1/auth/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": "analytical-engine",
	}})
	if w.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, body %s", w.Code, w.Body.String())
	}
	if err := core.Auth.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	link := lastEmailLink(t, sender)
	if got := link.Scheme + "://" + link.Host + link.Path; got != "https://api.example.com/v1/auth/verify-email" {
		t.Fatalf("verification link target = %q", got)
	}
	if link.Query().Get("email") != "ada@example.com" || link.Query().Get("token") == "" {
		t.Fatalf("verification link query = %q", link.RawQuery)
	}

	// Following the link (as the browser would) verifies the address.
	w = do(t, app, request{method: http.MethodGet, path: link.RequestURI()})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Email address verified") {
		t.Fatalf("GET verification link = %d, body %s", w.Code, w.Body.String())
	}
	user, err := core.Auth.GetUser(t.Context(), mustUserID(t, core, "ada@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if !user.EmailVerified {
		t.Fatal("email not verified after following the link")
	}

	// Password reset: the link targets COREGO_RESET_PASSWORD_URL.
	w = do(t, app, request{method: http.MethodPost, path: "/v1/auth/forgot-password", body: map[string]any{"email": "ada@example.com"}})
	if w.Code != http.StatusAccepted {
		t.Fatalf("forgot-password status = %d", w.Code)
	}
	if err := core.Auth.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	link = lastEmailLink(t, sender)
	if got := link.Scheme + "://" + link.Host + link.Path; got != "https://app.example.com/reset" {
		t.Fatalf("reset link target = %q", got)
	}
	w = do(t, app, request{method: http.MethodPost, path: "/v1/auth/reset-password", body: map[string]any{
		"email": link.Query().Get("email"), "token": link.Query().Get("token"), "new_password": "difference-engine",
	}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("reset-password status = %d, body %s", w.Code, w.Body.String())
	}
	w = do(t, app, request{method: http.MethodPost, path: "/v1/auth/signin", body: map[string]any{
		"email": "ada@example.com", "password": "difference-engine",
	}})
	if w.Code != http.StatusOK {
		t.Fatalf("signin with the new password = %d, body %s", w.Code, w.Body.String())
	}
}

// mustUserID looks a user up by email through the service.
func mustUserID(t *testing.T, core *Core, email string) string {
	t.Helper()
	users, err := core.Auth.ListUsers(t.Context(), auth.ListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Email == email {
			return u.ID
		}
	}
	t.Fatalf("user %q not found", email)
	return ""
}

// recordingMailer is an auth.Mailer that remembers the addresses it was
// asked to email.
type recordingMailer struct{ to chan string }

func (m recordingMailer) SendVerificationEmail(_ context.Context, to, _ string) error {
	m.to <- to
	return nil
}

func (m recordingMailer) SendPasswordResetEmail(_ context.Context, to, _ string) error {
	m.to <- to
	return nil
}

func TestMailerPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		smtpHost    string
		mailer      bool
		sender      bool
		wantWarning bool
		wantVia     string // "mailer", "sender" or "" (nothing sent)
	}{
		{name: "nothing configured logs a warning", wantWarning: true},
		{name: "Service.Mailer is used as-is", mailer: true, wantVia: "mailer"},
		{name: "Service.Mailer wins over MailSender and SMTP_HOST", mailer: true, sender: true, smtpHost: "smtp.invalid", wantVia: "mailer"},
		{name: "MailSender wins over SMTP_HOST", sender: true, smtpHost: "smtp.invalid", wantVia: "sender"},
		{name: "MailSender alone", sender: true, wantVia: "sender"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("SMTP_HOST", tt.smtpHost)
			logger, logs := newTestLogger()
			mailer := recordingMailer{to: make(chan string, 4)}
			sender := mail.NewCaptureSender()

			opts := &AuthOptions{}
			if tt.mailer {
				opts.Service.Mailer = mailer
			}
			if tt.sender {
				opts.MailSender = sender
			}
			core := newCore(t, &Config{Auth: opts, Logger: logger})

			gotWarning := strings.Contains(logs.String(), "auth emails are disabled")
			if gotWarning != tt.wantWarning {
				t.Errorf("no-mailer warning = %v, want %v; logs:\n%s", gotWarning, tt.wantWarning, logs)
			}

			if _, err := core.Auth.SignUp(t.Context(), auth.SignUpInput{
				Name: "Ada", Email: "ada@example.com", Password: "analytical-engine",
			}); err != nil {
				t.Fatal(err)
			}
			if err := core.Auth.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}

			viaMailer := len(mailer.to) > 0
			_, viaSender := sender.Last()
			switch tt.wantVia {
			case "mailer":
				if !viaMailer || viaSender {
					t.Errorf("mailer used = %v, sender used = %v; want only the mailer", viaMailer, viaSender)
				}
			case "sender":
				if viaMailer || !viaSender {
					t.Errorf("mailer used = %v, sender used = %v; want only the sender", viaMailer, viaSender)
				}
			default:
				if viaMailer || viaSender {
					t.Errorf("an email was sent although nothing is configured")
				}
			}
		})
	}
}

func TestAuthHTTPConfigFromEnv(t *testing.T) {
	tests := []struct {
		name         string
		appURL       string
		redirectEnv  string
		cfg          authhttp.Config
		wantSecure   bool
		wantRedirect string
	}{
		{
			name:   "cookies off: Secure untouched",
			appURL: "https://api.example.com",
		},
		{
			name:       "https APP_URL forces Secure",
			appURL:     "https://api.example.com",
			cfg:        authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}},
			wantSecure: true,
		},
		{
			name:       "scheme is case insensitive",
			appURL:     "HTTPS://API.EXAMPLE.COM",
			cfg:        authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}},
			wantSecure: true,
		},
		{
			name:   "http APP_URL keeps Secure off for local development",
			appURL: "http://localhost:8080",
			cfg:    authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}},
		},
		{
			name:       "an explicit Secure is kept on http",
			appURL:     "http://localhost:8080",
			cfg:        authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true, Secure: true}},
			wantSecure: true,
		},
		{
			name:         "redirect URL from the environment",
			redirectEnv:  "https://app.example.com/email-verified",
			wantRedirect: "https://app.example.com/email-verified",
		},
		{
			name:         "explicit redirect URL wins over the environment",
			redirectEnv:  "https://app.example.com/email-verified",
			cfg:          authhttp.Config{VerifyEmailRedirectURL: "/verified"},
			wantRedirect: "/verified",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("APP_URL", tt.appURL)
			t.Setenv("COREGO_VERIFY_EMAIL_REDIRECT_URL", tt.redirectEnv)
			core := newCore(t, nil)

			got := core.authHTTPConfig(tt.cfg)
			if got.Cookie.Secure != tt.wantSecure {
				t.Errorf("Cookie.Secure = %v, want %v", got.Cookie.Secure, tt.wantSecure)
			}
			if got.VerifyEmailRedirectURL != tt.wantRedirect {
				t.Errorf("VerifyEmailRedirectURL = %q, want %q", got.VerifyEmailRedirectURL, tt.wantRedirect)
			}
		})
	}
}

func TestSecureCookieOnHTTPSAppURL(t *testing.T) {
	isolateEnv(t)
	t.Setenv("APP_URL", "https://api.example.com")
	core := newCore(t, &Config{Auth: &AuthOptions{
		HTTP: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}},
	}})

	w := do(t, core.Server(), request{method: http.MethodPost, path: "/auth/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": "analytical-engine",
	}})
	if w.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, body %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if c := cookies[0]; !c.Secure || !c.HttpOnly || c.Name != authhttp.DefaultCookieName {
		t.Errorf("cookie = %+v, want a Secure HttpOnly %s cookie", c, authhttp.DefaultCookieName)
	}
}

func TestVerifyEmailRedirectFromEnv(t *testing.T) {
	isolateEnv(t)
	t.Setenv("COREGO_VERIFY_EMAIL_REDIRECT_URL", "https://app.example.com/verified")
	core := newCore(t, &Config{Auth: &AuthOptions{}})

	w := do(t, core.Server(), request{method: http.MethodGet, path: "/auth/verify-email?email=ada@example.com&token=wrong"})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if got := w.Header().Get("Location"); got != "https://app.example.com/verified?status=invalid" {
		t.Errorf("Location = %q", got)
	}
}

func TestInvalidAuthHTTPConfigIsAnError(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		cfg  authhttp.Config
	}{
		{name: "redirect URL from the environment", env: map[string]string{"COREGO_VERIFY_EMAIL_REDIRECT_URL": "ftp://example.com/done"}},
		{name: "base path without a leading slash", cfg: authhttp.Config{BasePath: "auth"}},
		{name: "SameSite=None without Secure", cfg: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true, SameSite: http.SameSiteNoneMode}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			core, err := New(&Config{Auth: &AuthOptions{HTTP: tt.cfg}})
			if err == nil {
				core.Close()
				t.Fatal("New accepted an invalid HTTP configuration")
			}
			if !strings.Contains(err.Error(), "corego: invalid AuthOptions.HTTP: authhttp:") {
				t.Errorf("error = %q", err)
			}
		})
	}
}

func TestSameSiteNoneAllowedOnHTTPSAppURL(t *testing.T) {
	isolateEnv(t)
	// Secure is forced by the https APP_URL, which makes SameSite=None
	// (cross-site frontends) valid without setting Secure by hand.
	t.Setenv("APP_URL", "https://api.example.com")
	newCore(t, &Config{Auth: &AuthOptions{
		HTTP: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true, SameSite: http.SameSiteNoneMode}},
	}})
}

func TestServiceLoggerDefaultsToConfigLogger(t *testing.T) {
	isolateEnv(t)
	logger, logs := newTestLogger()
	failing := mail.SenderFunc(func(context.Context, mail.Message) error { return errSendFailed })
	core := newCore(t, &Config{Auth: &AuthOptions{MailSender: failing}, Logger: logger})

	if _, err := core.Auth.SignUp(t.Context(), auth.SignUpInput{
		Name: "Ada", Email: "ada@example.com", Password: "analytical-engine",
	}); err != nil {
		t.Fatal(err)
	}
	if err := core.Auth.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), errSendFailed.Error()) {
		t.Errorf("background delivery failure did not reach Config.Logger; logs:\n%s", logs)
	}
}

// errSendFailed is returned by the failing test sender.
var errSendFailed = errors.New("simulated delivery failure")
