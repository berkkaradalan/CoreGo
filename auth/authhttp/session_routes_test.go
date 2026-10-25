package authhttp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
)

func TestSignUpCreatesUserAndSession(t *testing.T) {
	app := newTestApp(t, appOptions{})
	rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": " Ada Lovelace ", "email": "Ada@Example.com", "password": testPassword, "image": "https://img.example/ada.png",
	}, headers: map[string]string{"User-Agent": "test-agent/1.0"}, remote: "203.0.113.7:5555"})
	expectStatus(t, rec, http.StatusCreated)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store on a response carrying a token", got)
	}

	var out signUpResult
	decode(t, rec, &out)
	if out.Token == "" || out.ExpiresAt == nil {
		t.Fatalf("token/expires_at missing: %s", rec.Body)
	}
	if out.VerificationRequired == nil || *out.VerificationRequired {
		t.Fatalf("verification_required must be present and false: %s", rec.Body)
	}
	if out.User.Name != "Ada Lovelace" || out.User.Email != "ada@example.com" || out.User.EmailVerified ||
		out.User.Image != "https://img.example/ada.png" || out.User.ID == "" {
		t.Fatalf("unexpected user %+v", out.User)
	}
	if !out.ExpiresAt.After(time.Now().Add(auth.DefaultSessionTTL - time.Minute)) {
		t.Fatalf("expires_at %v does not match the default session TTL", out.ExpiresAt)
	}
	if responseCookie(rec, DefaultCookieName) != nil {
		t.Fatal("no cookie may be set while the cookie transport is disabled")
	}

	// The session records the client IP and the user agent.
	session, _, err := app.service.GetSession(t.Context(), out.Token)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.IPAddress != "203.0.113.7" || session.UserAgent != "test-agent/1.0" {
		t.Fatalf("session ip/ua = %q/%q", session.IPAddress, session.UserAgent)
	}

	// The verification email goes out in the background.
	app.waitEmails(t)
	if app.mailer.verificationToken("ada@example.com") == "" {
		t.Fatal("no verification email was sent")
	}
}

func TestSignUpResponseNeverLeaksInternals(t *testing.T) {
	app := newTestApp(t, appOptions{})
	rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": testPassword,
	}})
	expectStatus(t, rec, http.StatusCreated)
	var top map[string]json.RawMessage
	decode(t, rec, &top)
	var user map[string]any
	if err := json.Unmarshal(top["user"], &user); err != nil {
		t.Fatalf("user: %v", err)
	}
	want := []string{"created_at", "email", "email_verified", "id", "image", "name", "updated_at"}
	if len(user) != len(want) {
		t.Fatalf("user has fields %v, want exactly %v", keys(user), want)
	}
	for _, k := range want {
		if _, ok := user[k]; !ok {
			t.Fatalf("user lacks %q: %v", k, keys(user))
		}
	}
	body := strings.ToLower(rec.Body.String())
	for _, secret := range []string{"password", "hash", "banned", "version"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaks %q: %s", secret, rec.Body)
		}
	}
}

// keys returns the keys of m.
func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSignUpRememberMeExtendsTheSession(t *testing.T) {
	app := newTestApp(t, appOptions{})
	rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": testPassword, "remember_me": true,
	}})
	expectStatus(t, rec, http.StatusCreated)
	var out signUpResult
	decode(t, rec, &out)
	if !out.ExpiresAt.After(time.Now().Add(auth.DefaultRememberMeTTL - time.Minute)) {
		t.Fatalf("expires_at %v does not match the remember-me TTL", out.ExpiresAt)
	}
}

func TestSignUpWithRequiredVerification(t *testing.T) {
	app := newTestApp(t, appOptions{
		authCfg: func(c *auth.Config) { c.RequireVerifiedEmail = true },
		httpCfg: Config{Cookie: CookieConfig{Enabled: true}},
	})
	rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": testPassword,
	}})
	expectStatus(t, rec, http.StatusCreated)

	var top map[string]json.RawMessage
	decode(t, rec, &top)
	for _, absent := range []string{"token", "expires_at"} {
		if _, ok := top[absent]; ok {
			t.Fatalf("%q must be omitted when verification is required: %s", absent, rec.Body)
		}
	}
	if string(top["verification_required"]) != "true" {
		t.Fatalf("verification_required = %s, want true", top["verification_required"])
	}
	if len(rec.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("no cookie may be set without a session: %v", rec.Header().Values("Set-Cookie"))
	}

	// Sign-in is refused until the address is verified.
	signin := app.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
		"email": "ada@example.com", "password": testPassword,
	}})
	expectError(t, signin, http.StatusForbidden, msgEmailNotVerified)

	// Following the emailed link unlocks sign-in.
	app.waitEmails(t)
	token := app.mailer.verificationToken("ada@example.com")
	verify := app.do(t, req{method: http.MethodPost, path: "/verify-email", body: map[string]any{
		"email": "ada@example.com", "token": token,
	}})
	expectStatus(t, verify, http.StatusNoContent)
	if app.signIn(t, "ada@example.com", testPassword) == "" {
		t.Fatal("sign-in after verification returned no token")
	}
}

func TestSignUpFailures(t *testing.T) {
	app := newTestApp(t, appOptions{})
	app.signUp(t, "Taken", "taken@example.com", testPassword)

	tests := []struct {
		name    string
		body    map[string]any
		code    int
		message string
	}{
		{"missing name", map[string]any{"email": "a@example.com", "password": testPassword}, 400, msgNameRequired},
		{"blank name", map[string]any{"name": "   ", "email": "a@example.com", "password": testPassword}, 400, msgNameRequired},
		{"missing email", map[string]any{"name": "A", "password": testPassword}, 400, msgEmailRequired},
		{"invalid email", map[string]any{"name": "A", "email": "not-an-email", "password": testPassword}, 400, msgInvalidEmail},
		{"display name email", map[string]any{"name": "A", "email": "A <a@example.com>", "password": testPassword}, 400, msgInvalidEmail},
		{"missing password", map[string]any{"name": "A", "email": "a@example.com"}, 400, msgPasswordRequired},
		{"short password", map[string]any{"name": "A", "email": "a@example.com", "password": "short"}, 400, msgWeakPassword},
		{"too long password", map[string]any{"name": "A", "email": "a@example.com", "password": strings.Repeat("x", 73)}, 400, msgWeakPassword},
		{"duplicate", map[string]any{"name": "A", "email": "taken@example.com", "password": testPassword}, 409, msgUserAlreadyExists},
		{"duplicate other case", map[string]any{"name": "A", "email": " TAKEN@example.com ", "password": testPassword}, 409, msgUserAlreadyExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: tt.body})
			expectError(t, rec, tt.code, tt.message)
		})
	}
}

func TestSignIn(t *testing.T) {
	app := newTestApp(t, appOptions{})
	app.signUp(t, "Ada", "ada@example.com", testPassword)

	t.Run("success", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
			"email": " ADA@example.com", "password": testPassword,
		}})
		expectStatus(t, rec, http.StatusOK)
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q", got)
		}
		var top map[string]json.RawMessage
		decode(t, rec, &top)
		for _, k := range []string{"user", "token", "expires_at"} {
			if _, ok := top[k]; !ok {
				t.Fatalf("missing %q in %s", k, rec.Body)
			}
		}
		if _, ok := top["verification_required"]; ok {
			t.Fatalf("sign-in responses carry no verification_required: %s", rec.Body)
		}
	})

	failures := []struct {
		name string
		body map[string]any
	}{
		{"wrong password", map[string]any{"email": "ada@example.com", "password": "wrong-password"}},
		{"unknown email", map[string]any{"email": "nobody@example.com", "password": testPassword}},
		{"empty body fields", map[string]any{}},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			rec := app.do(t, req{method: http.MethodPost, path: "/signin", body: tt.body})
			expectError(t, rec, http.StatusUnauthorized, msgInvalidCredentials)
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Fatal("credential failures are not challenges for a protected resource")
			}
		})
	}

	t.Run("banned", func(t *testing.T) {
		app.updateUser(t, "ada@example.com", func(u *auth.User) { u.Banned = true })
		defer app.updateUser(t, "ada@example.com", func(u *auth.User) { u.Banned = false })
		rec := app.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
			"email": "ada@example.com", "password": testPassword,
		}})
		expectError(t, rec, http.StatusForbidden, msgUserBanned)
	})
}

func TestSignOut(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	other := app.signIn(t, "ada@example.com", testPassword)

	rec := app.do(t, req{method: http.MethodPost, path: "/signout", token: token})
	expectStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Fatalf("204 with a body: %q", rec.Body)
	}

	// The token no longer works; the other session does.
	expectError(t, app.do(t, req{method: http.MethodGet, path: "/me", token: token}), http.StatusUnauthorized, msgSessionNotFound)
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", token: other}), http.StatusOK)

	// Signing out again needs a valid session.
	expectError(t, app.do(t, req{method: http.MethodPost, path: "/signout", token: token}), http.StatusUnauthorized, msgSessionNotFound)
	expectError(t, app.do(t, req{method: http.MethodPost, path: "/signout"}), http.StatusUnauthorized, msgMissingToken)
}

func TestGetSession(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token

	rec := app.do(t, req{method: http.MethodGet, path: "/session", token: token, headers: map[string]string{"User-Agent": "ua"}})
	expectStatus(t, rec, http.StatusOK)
	var out struct {
		User    userResult `json:"user"`
		Session struct {
			ID        string    `json:"id"`
			UserID    string    `json:"user_id"`
			ExpiresAt time.Time `json:"expires_at"`
			Current   bool      `json:"current"`
		} `json:"session"`
	}
	decode(t, rec, &out)
	if out.User.Email != "ada@example.com" || out.Session.UserID != out.User.ID || out.Session.ID == "" || !out.Session.Current {
		t.Fatalf("unexpected session response %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("the session response must not contain token material: %s", rec.Body)
	}
}

func TestListSessions(t *testing.T) {
	app := newTestApp(t, appOptions{})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	second := app.signIn(t, "ada@example.com", testPassword)
	app.signUp(t, "Bob", "bob@example.com", testPassword)

	rec := app.do(t, req{method: http.MethodGet, path: "/sessions", token: second})
	expectStatus(t, rec, http.StatusOK)
	var out struct {
		Sessions []struct {
			ID      string `json:"id"`
			UserID  string `json:"user_id"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}
	decode(t, rec, &out)
	if len(out.Sessions) != 2 {
		t.Fatalf("got %d sessions, want 2: %s", len(out.Sessions), rec.Body)
	}
	current := 0
	adaID := app.userIDByEmail(t, "ada@example.com")
	for _, s := range out.Sessions {
		if s.UserID != adaID {
			t.Fatalf("session of another user listed: %+v", s)
		}
		if s.Current {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("%d sessions marked current, want exactly 1", current)
	}
	// After revoking everything only a fresh session is listed.
	expectStatus(t, app.do(t, req{method: http.MethodDelete, path: "/sessions", token: second}), http.StatusNoContent)
	fresh := app.signIn(t, "ada@example.com", testPassword)
	rec = app.do(t, req{method: http.MethodGet, path: "/sessions", token: fresh})
	decode(t, rec, &out)
	if len(out.Sessions) != 1 || !out.Sessions[0].Current {
		t.Fatalf("after revoke-all only the fresh session remains: %s", rec.Body)
	}
}

func TestRevokeAllSessions(t *testing.T) {
	app := newTestApp(t, appOptions{})
	first := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	second := app.signIn(t, "ada@example.com", testPassword)
	bob := app.signUp(t, "Bob", "bob@example.com", testPassword).Token

	expectStatus(t, app.do(t, req{method: http.MethodDelete, path: "/sessions", token: first}), http.StatusNoContent)
	for _, token := range []string{first, second} {
		expectError(t, app.do(t, req{method: http.MethodGet, path: "/me", token: token}), http.StatusUnauthorized, msgSessionNotFound)
	}
	// Other users keep their sessions.
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", token: bob}), http.StatusOK)
}

func TestRevokeSession(t *testing.T) {
	app := newTestApp(t, appOptions{})
	current := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	other := app.signIn(t, "ada@example.com", testPassword)
	bob := app.signUp(t, "Bob", "bob@example.com", testPassword).Token

	sessionID := func(token string) string {
		s, _, err := app.service.GetSession(t.Context(), token)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		return s.ID
	}
	otherID, bobID := sessionID(other), sessionID(bob)

	t.Run("foreign session is 404", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodDelete, path: "/sessions/" + bobID, token: current})
		expectError(t, rec, http.StatusNotFound, msgSessionResourceNotFound)
		expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", token: bob}), http.StatusOK)
	})
	t.Run("unknown session is 404", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodDelete, path: "/sessions/does-not-exist", token: current})
		expectError(t, rec, http.StatusNotFound, msgSessionResourceNotFound)
	})
	t.Run("own other session", func(t *testing.T) {
		expectStatus(t, app.do(t, req{method: http.MethodDelete, path: "/sessions/" + otherID, token: current}), http.StatusNoContent)
		expectError(t, app.do(t, req{method: http.MethodGet, path: "/me", token: other}), http.StatusUnauthorized, msgSessionNotFound)
		expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", token: current}), http.StatusOK)
	})
	t.Run("already revoked is 404", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodDelete, path: "/sessions/" + otherID, token: current})
		expectError(t, rec, http.StatusNotFound, msgSessionResourceNotFound)
	})
	t.Run("current session", func(t *testing.T) {
		expectStatus(t, app.do(t, req{method: http.MethodDelete, path: "/sessions/" + sessionID(current), token: current}), http.StatusNoContent)
		expectError(t, app.do(t, req{method: http.MethodGet, path: "/me", token: current}), http.StatusUnauthorized, msgSessionNotFound)
	})
}

func TestUserAgentIsTruncated(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want int // expected byte length
	}{
		{"short", "curl/8", 6},
		{"exact limit", strings.Repeat("a", maxUserAgentBytes), maxUserAgentBytes},
		{"over limit", strings.Repeat("a", 4*maxUserAgentBytes), maxUserAgentBytes},
		// 'é' is two bytes; a cut in the middle of the rune drops it.
		{"multibyte boundary", strings.Repeat("a", maxUserAgentBytes-1) + "é" + "tail", maxUserAgentBytes - 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, appOptions{})
			app.signUp(t, "Ada", "ada@example.com", testPassword)
			rec := app.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
				"email": "ada@example.com", "password": testPassword,
			}, headers: map[string]string{"User-Agent": tt.ua}})
			expectStatus(t, rec, http.StatusOK)
			var out signUpResult
			decode(t, rec, &out)
			session, _, err := app.service.GetSession(t.Context(), out.Token)
			if err != nil {
				t.Fatalf("GetSession: %v", err)
			}
			if len(session.UserAgent) != tt.want || !strings.HasPrefix(tt.ua, session.UserAgent) {
				t.Fatalf("stored user agent has %d bytes, want %d", len(session.UserAgent), tt.want)
			}
		})
	}
}
