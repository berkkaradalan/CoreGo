package authhttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
)

// verifyLinkPath returns the GET /verify-email path for email and token,
// as DefaultMailer builds it.
func verifyLinkPath(email, token string) string {
	q := url.Values{}
	if email != "" {
		q.Set("email", email)
	}
	if token != "" {
		q.Set("token", token)
	}
	return "/verify-email?" + q.Encode()
}

// expectPageSecurityHeaders asserts the headers every GET /verify-email
// response carries.
func expectPageSecurityHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Fatalf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestVerifyEmailJSON(t *testing.T) {
	app := newTestApp(t, appOptions{})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	app.waitEmails(t)
	token := app.mailer.verificationToken("ada@example.com")

	tests := []struct {
		name    string
		body    map[string]any
		code    int
		message string
	}{
		{"wrong token", map[string]any{"email": "ada@example.com", "token": "wrong"}, 400, msgInvalidToken},
		{"other email", map[string]any{"email": "bob@example.com", "token": token}, 400, msgInvalidToken},
		{"missing token", map[string]any{"email": "ada@example.com"}, 400, msgInvalidToken},
		{"missing email", map[string]any{"token": token}, 400, msgInvalidToken},
		{"success", map[string]any{"email": "ADA@example.com", "token": token}, 204, ""},
		{"reused token", map[string]any{"email": "ada@example.com", "token": token}, 400, msgInvalidToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := app.do(t, req{method: http.MethodPost, path: "/verify-email", body: tt.body})
			if tt.code == http.StatusNoContent {
				expectStatus(t, rec, tt.code)
				return
			}
			expectError(t, rec, tt.code, tt.message)
		})
	}

	user, err := app.service.GetUser(t.Context(), app.userIDByEmail(t, "ada@example.com"))
	if err != nil || !user.EmailVerified {
		t.Fatalf("user not verified: %+v, %v", user, err)
	}
}

func TestVerifyEmailJSONExpiredToken(t *testing.T) {
	app := newTestApp(t, appOptions{authCfg: func(c *auth.Config) { c.EmailVerificationTokenTTL = time.Nanosecond }})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	app.waitEmails(t)
	rec := app.do(t, req{method: http.MethodPost, path: "/verify-email", body: map[string]any{
		"email": "ada@example.com", "token": app.mailer.verificationToken("ada@example.com"),
	}})
	expectError(t, rec, http.StatusBadRequest, msgTokenExpired)
}

func TestVerifyEmailTokenCannotResetPassword(t *testing.T) {
	app := newTestApp(t, appOptions{})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	app.waitEmails(t)
	rec := app.do(t, req{method: http.MethodPost, path: "/reset-password", body: map[string]any{
		"email": "ada@example.com", "token": app.mailer.verificationToken("ada@example.com"), "new_password": "another-password",
	}})
	expectError(t, rec, http.StatusBadRequest, msgInvalidToken)
}

func TestVerifyEmailLinkRendersPage(t *testing.T) {
	const (
		verifiedMsg = "Thank you, your email address has been verified. You can close this page and sign in."
		invalidMsg  = "This verification link is invalid or has already been used. If you already verified your email address, you can simply sign in."
		expiredMsg  = "This verification link has expired. Request a new verification email from the app."
	)
	tests := []struct {
		name        string
		ttl         time.Duration
		path        func(token string) string
		code        int
		wantTitle   string
		wantMessage string
	}{
		{"verified", 0, func(tok string) string { return verifyLinkPath("ada@example.com", tok) }, 200, "Email address verified", verifiedMsg},
		{"wrong token", 0, func(string) string { return verifyLinkPath("ada@example.com", "wrong") }, 400, "Invalid verification link", invalidMsg},
		{"missing token", 0, func(string) string { return verifyLinkPath("ada@example.com", "") }, 400, "Invalid verification link", invalidMsg},
		{"missing email", 0, func(tok string) string { return verifyLinkPath("", tok) }, 400, "Invalid verification link", invalidMsg},
		{"no parameters", 0, func(string) string { return "/verify-email" }, 400, "Invalid verification link", invalidMsg},
		{"unknown address", 0, func(tok string) string { return verifyLinkPath("nobody@example.com", tok) }, 400, "Invalid verification link", invalidMsg},
		{"expired", time.Nanosecond, func(tok string) string { return verifyLinkPath("ada@example.com", tok) }, 400, "Verification link expired", expiredMsg},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, appOptions{authCfg: func(c *auth.Config) { c.EmailVerificationTokenTTL = tt.ttl }})
			app.signUp(t, "Ada", "ada@example.com", testPassword)
			app.waitEmails(t)
			rec := app.do(t, req{method: http.MethodGet, path: tt.path(app.mailer.verificationToken("ada@example.com"))})
			expectStatus(t, rec, tt.code)
			expectPageSecurityHeaders(t, rec)
			if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Fatalf("Content-Type = %q", ct)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "<title>"+tt.wantTitle+"</title>") || !strings.Contains(body, "<h1>"+tt.wantTitle+"</h1>") {
				t.Fatalf("page does not show %q:\n%s", tt.wantTitle, body)
			}
			if !strings.Contains(body, "<p>"+tt.wantMessage+"</p>") {
				t.Fatalf("page does not explain %q:\n%s", tt.wantMessage, body)
			}
			if strings.Contains(body, "ada@example.com") {
				t.Fatal("the page must not echo the email address")
			}
		})
	}
}

func TestVerifyEmailLinkSecondClickIsInvalid(t *testing.T) {
	app := newTestApp(t, appOptions{})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	app.waitEmails(t)
	path := verifyLinkPath("ada@example.com", app.mailer.verificationToken("ada@example.com"))
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: path}), http.StatusOK)
	rec := app.do(t, req{method: http.MethodGet, path: path})
	expectStatus(t, rec, http.StatusBadRequest)
	// Security scanners that open links before the user produce exactly
	// this second click: the page must point an already verified user
	// to sign-in.
	for _, want := range []string{"already been used", "If you already verified your email address, you can simply sign in."} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("the invalid page must say %q: %s", want, rec.Body)
		}
	}
}

func TestVerifyEmailLinkNeverEchoesTheQuery(t *testing.T) {
	app := newTestApp(t, appOptions{})
	payloads := []string{
		`<script>alert(1)</script>`,
		`"><img src=x onerror=alert(1)>`,
		`' onmouseover='alert(1)`,
		`</title><script>x</script>`,
	}
	for _, p := range payloads {
		rec := app.do(t, req{method: http.MethodGet, path: verifyLinkPath(p+"@example.com", p)})
		expectStatus(t, rec, http.StatusBadRequest)
		body := rec.Body.String()
		for _, needle := range []string{"<script", "onerror", "onmouseover", "<img"} {
			if strings.Contains(body, needle) {
				t.Fatalf("payload %q reflected (%q found):\n%s", p, needle, body)
			}
		}
	}
}

func TestRenderVerifyPageEscapesEverything(t *testing.T) {
	html := renderVerifyPage(verifyPage{
		Title:   `<script>alert("t")</script>`,
		Message: `"><img src=x onerror=alert(1)> & more`,
	})
	for _, raw := range []string{`<script>alert`, `<img src=x`, `"><img`} {
		if strings.Contains(html, raw) {
			t.Fatalf("unescaped %q in:\n%s", raw, html)
		}
	}
	for _, escaped := range []string{"&lt;script&gt;", "&lt;img src=x onerror=alert(1)&gt; &amp; more"} {
		if !strings.Contains(html, escaped) {
			t.Fatalf("expected escaped %q in:\n%s", escaped, html)
		}
	}
	// The page uses only inline styles: no scripts, external resources
	// or links that the Content-Security-Policy would block.
	for _, page := range verifyPages {
		out := renderVerifyPage(page)
		for _, forbidden := range []string{"<script", "<link", "src=", "href=", "http://", "https://"} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("page %q contains %q", page.Title, forbidden)
			}
		}
		if !strings.HasPrefix(out, "<!DOCTYPE html>") || !strings.Contains(out, "<style>") {
			t.Fatalf("page %q is not a styled HTML document", page.Title)
		}
	}
}

func TestVerifyEmailLinkRedirects(t *testing.T) {
	tests := []struct {
		name     string
		redirect string
		ttl      time.Duration
		token    func(real string) string
		want     string
	}{
		{"verified", "https://app.example.com/verified", 0, func(r string) string { return r }, "https://app.example.com/verified?status=verified"},
		{"invalid", "https://app.example.com/verified", 0, func(string) string { return "wrong" }, "https://app.example.com/verified?status=invalid"},
		{"missing token", "https://app.example.com/verified", 0, func(string) string { return "" }, "https://app.example.com/verified?status=invalid"},
		{"expired", "https://app.example.com/verified", time.Nanosecond, func(r string) string { return r }, "https://app.example.com/verified?status=expired"},
		{"keeps query and fragment", "https://app.example.com/verified?lang=en&ref=mail#top", 0, func(r string) string { return r },
			"https://app.example.com/verified?lang=en&ref=mail&status=verified#top"},
		{"replaces an existing status", "https://app.example.com/v?status=stale", 0, func(string) string { return "wrong" }, "https://app.example.com/v?status=invalid"},
		{"relative path", "/email-verified", 0, func(r string) string { return r }, "/email-verified?status=verified"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, appOptions{
				httpCfg: Config{VerifyEmailRedirectURL: tt.redirect},
				authCfg: func(c *auth.Config) { c.EmailVerificationTokenTTL = tt.ttl },
			})
			app.signUp(t, "Ada", "ada@example.com", testPassword)
			app.waitEmails(t)
			rec := app.do(t, req{method: http.MethodGet, path: verifyLinkPath("ada@example.com", tt.token(app.mailer.verificationToken("ada@example.com")))})
			expectStatus(t, rec, http.StatusSeeOther)
			expectPageSecurityHeaders(t, rec)
			if got := rec.Header().Get("Location"); got != tt.want {
				t.Fatalf("Location = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVerifyEmailLinkHeadDoesNotConsumeTheToken(t *testing.T) {
	// The router accepts methods in any letter case, so every spelling
	// of HEAD must be recognized.
	for _, method := range []string{http.MethodHead, "head", "Head"} {
		t.Run(method, func(t *testing.T) {
			app := newTestApp(t, appOptions{})
			app.signUp(t, "Ada", "ada@example.com", testPassword)
			app.waitEmails(t)
			path := verifyLinkPath("ada@example.com", app.mailer.verificationToken("ada@example.com"))

			for i := 0; i < 2; i++ {
				head := app.do(t, req{method: method, path: path})
				expectStatus(t, head, http.StatusOK)
				expectPageSecurityHeaders(t, head)
				if head.Body.Len() != 0 {
					t.Fatalf("HEAD with a body: %q", head.Body)
				}
			}
			user, err := app.store.GetUserByEmail(t.Context(), "ada@example.com")
			if err != nil || user.EmailVerified {
				t.Fatalf("HEAD verified the address: %+v, %v", user, err)
			}
			// The real click still verifies.
			expectStatus(t, app.do(t, req{method: http.MethodGet, path: path}), http.StatusOK)
		})
	}
}

func TestVerifyEmailLinkStoreFailureIs500(t *testing.T) {
	store := &failingStore{MemoryStore: auth.NewMemoryStore()}
	app := newTestApp(t, appOptions{store: store})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	app.waitEmails(t)
	token := app.mailer.verificationToken("ada@example.com")
	store.failGetUserByEmail.Store(true)
	rec := app.do(t, req{method: http.MethodGet, path: verifyLinkPath("ada@example.com", token)})
	expectError(t, rec, http.StatusInternalServerError, "Internal Server Error")
}

func TestRequestEmailVerificationSendsAFreshLink(t *testing.T) {
	app := newTestApp(t, appOptions{})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	app.waitEmails(t)
	first := app.mailer.verificationToken("ada@example.com")

	rec := app.do(t, req{method: http.MethodPost, path: "/verify-email/request", body: map[string]any{"email": "ADA@example.com"}})
	expectStatus(t, rec, http.StatusAccepted)
	app.waitEmails(t)
	if verify, _ := app.mailer.counts(); verify != 2 {
		t.Fatalf("%d verification emails, want 2", verify)
	}
	second := app.mailer.verificationToken("ada@example.com")
	if second == "" || second == first {
		t.Fatal("no fresh verification token was sent")
	}
	expectStatus(t, app.do(t, req{method: http.MethodPost, path: "/verify-email", body: map[string]any{
		"email": "ada@example.com", "token": second,
	}}), http.StatusNoContent)

	// Verified addresses get no further emails, with the same response.
	rec = app.do(t, req{method: http.MethodPost, path: "/verify-email/request", body: map[string]any{"email": "ada@example.com"}})
	expectStatus(t, rec, http.StatusAccepted)
	app.waitEmails(t)
	if verify, _ := app.mailer.counts(); verify != 2 {
		t.Fatalf("%d verification emails, want still 2", verify)
	}
}

func TestForgotPasswordSendsAResetLink(t *testing.T) {
	app := newTestApp(t, appOptions{})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	rec := app.do(t, req{method: http.MethodPost, path: "/forgot-password", body: map[string]any{"email": " Ada@Example.com "}})
	expectStatus(t, rec, http.StatusAccepted)
	app.waitEmails(t)
	if app.mailer.resetToken("ada@example.com") == "" {
		t.Fatal("no reset email was sent")
	}
}

// publicEmailRoutes are the unauthenticated routes that must not reveal
// whether an address is registered.
var publicEmailRoutes = []string{"/verify-email/request", "/forgot-password"}

func TestPublicEmailRoutesAreEnumerationSafe(t *testing.T) {
	for _, withMailer := range []bool{true, false} {
		name := "with mailer"
		if !withMailer {
			name = "without mailer"
		}
		t.Run(name, func(t *testing.T) {
			app := newTestApp(t, appOptions{noMailer: !withMailer})
			app.signUp(t, "Unverified", "unverified@example.com", testPassword)
			app.signUp(t, "Verified", "verified@example.com", testPassword)
			app.updateUser(t, "verified@example.com", func(u *auth.User) { u.EmailVerified = true })

			emails := []string{
				"unverified@example.com",
				"verified@example.com",
				"unknown@example.com",
				"UNVERIFIED@example.com",
				"not-an-email",
				"",
			}
			for _, path := range publicEmailRoutes {
				t.Run(path, func(t *testing.T) {
					var reference *httptest.ResponseRecorder
					for _, email := range emails {
						rec := app.do(t, req{method: http.MethodPost, path: path, body: map[string]any{"email": email}})
						if reference == nil {
							reference = rec
							expectStatus(t, rec, http.StatusAccepted)
							var body messageResponse
							decode(t, rec, &body)
							if body.Message != msgEmailRequestAccepted {
								t.Fatalf("message = %q", body.Message)
							}
							continue
						}
						if rec.Code != reference.Code || rec.Body.String() != reference.Body.String() {
							t.Fatalf("%q: %d %s differs from %d %s", email, rec.Code, rec.Body, reference.Code, reference.Body)
						}
						for _, h := range []string{"Content-Type", "Cache-Control", "Set-Cookie"} {
							if rec.Header().Get(h) != reference.Header().Get(h) {
								t.Fatalf("%q: header %s differs", email, h)
							}
						}
					}
				})
			}
			app.waitEmails(t)
			verify, reset := app.mailer.counts()
			if !withMailer && (verify != 0 || reset != 0) {
				t.Fatal("no email may be recorded without a mailer")
			}
		})
	}
}

func TestPublicEmailRoutesRejectMalformedBodies(t *testing.T) {
	app := newTestApp(t, appOptions{})
	for _, path := range publicEmailRoutes {
		t.Run(path, func(t *testing.T) {
			expectError(t, app.do(t, req{method: http.MethodPost, path: path, raw: "{", ctype: "application/json"}), 400, msgInvalidJSON)
			expectError(t, app.do(t, req{method: http.MethodPost, path: path, raw: "", ctype: "application/json"}), 400, msgInvalidJSON)
			expectError(t, app.do(t, req{method: http.MethodPost, path: path, raw: "email=x", ctype: "application/x-www-form-urlencoded"}), 415, msgUnsupportedMediaType)
		})
	}
}

func TestPublicEmailRoutesStoreFailureIs500(t *testing.T) {
	store := &failingStore{MemoryStore: auth.NewMemoryStore()}
	app := newTestApp(t, appOptions{store: store})
	store.failGetUserByEmail.Store(true)
	for _, path := range publicEmailRoutes {
		rec := app.do(t, req{method: http.MethodPost, path: path, body: map[string]any{"email": "ada@example.com"}})
		expectError(t, rec, http.StatusInternalServerError, "Internal Server Error")
	}
}
