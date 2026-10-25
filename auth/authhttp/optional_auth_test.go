package authhttp

import (
	"net/http"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// anonymous is the body the OptionalAuth test route renders without a
// signed-in user.
const anonymous = "anonymous"

// mountOptionalRoute registers GET and POST /api/maybe behind
// OptionalAuth; they answer with the signed-in user's email and session
// owner, or "anonymous".
func mountOptionalRoute(app *testApp) {
	handler := func(c *web.Context) error {
		user, ok := auth.UserFromContext(c)
		if !ok {
			if _, hasSession := auth.SessionFromContext(c); hasSession {
				return c.String(http.StatusInternalServerError, "session without user")
			}
			return c.String(http.StatusOK, anonymous)
		}
		session, ok := auth.SessionFromContext(c)
		if !ok || session.UserID != user.ID {
			return c.String(http.StatusInternalServerError, "user without matching session")
		}
		return c.String(http.StatusOK, user.Email)
	}
	app.server.Get("/api/maybe", handler, app.handlers.OptionalAuth())
	app.server.Post("/api/maybe", handler, app.handlers.OptionalAuth())
}

func TestOptionalAuthFailuresContinueAnonymously(t *testing.T) {
	for _, tt := range authFailureCases() {
		t.Run(tt.name, func(t *testing.T) {
			app := newAuthFailureApp(t)
			mountOptionalRoute(app)
			r := tt.prepare(t, app)
			r.method, r.path, r.absolute = http.MethodGet, "/api/maybe", true
			rec := app.do(t, r)
			if tt.code == http.StatusInternalServerError {
				// Store outages are not silently treated as anonymous.
				expectError(t, rec, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			expectStatus(t, rec, http.StatusOK)
			if rec.Body.String() != anonymous {
				t.Fatalf("body = %q, want %q", rec.Body, anonymous)
			}
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Fatal("OptionalAuth must never challenge")
			}
		})
	}
}

func TestOptionalAuthLoadsTheSession(t *testing.T) {
	app, cookie := newCookieApp(t, http.SameSiteLaxMode)
	mountOptionalRoute(app)
	tests := []struct {
		name string
		r    req
		want string
	}{
		{name: "bearer", r: req{method: http.MethodGet, token: cookie.Value}, want: "ada@example.com"},
		{name: "cookie", r: req{method: http.MethodGet, cookie: cookie}, want: "ada@example.com"},
		{name: "no token", r: req{method: http.MethodGet}, want: anonymous},
		{name: "same-site cookie POST", r: req{method: http.MethodPost, cookie: cookie,
			headers: map[string]string{"Sec-Fetch-Site": "same-origin"}}, want: "ada@example.com"},
		{name: "cross-site cookie POST is anonymous", r: req{method: http.MethodPost, cookie: cookie,
			headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}}, want: anonymous},
		{name: "cross-site bearer POST", r: req{method: http.MethodPost, token: cookie.Value,
			headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}}, want: "ada@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.path, tt.r.absolute = "/api/maybe", true
			rec := app.do(t, tt.r)
			expectStatus(t, rec, http.StatusOK)
			if rec.Body.String() != tt.want {
				t.Fatalf("body = %q, want %q", rec.Body, tt.want)
			}
		})
	}
}

func TestOptionalAuthLeavesAnInvalidCookieAlone(t *testing.T) {
	app, cookie := newCookieApp(t, http.SameSiteLaxMode)
	mountOptionalRoute(app)
	expectStatus(t, app.do(t, req{method: http.MethodPost, path: "/signout", cookie: cookie}), http.StatusNoContent)

	rec := app.do(t, req{method: http.MethodGet, path: "/api/maybe", absolute: true, cookie: cookie})
	expectStatus(t, rec, http.StatusOK)
	if rec.Body.String() != anonymous {
		t.Fatalf("body = %q, want %q", rec.Body, anonymous)
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("OptionalAuth must not touch cookies, got Set-Cookie %v", got)
	}
}

func TestOptionalAuthOnGroup(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	feed := app.server.Group("/feed", app.handlers.OptionalAuth())
	feed.Get("/", func(c *web.Context) error {
		if user, ok := auth.UserFromContext(c); ok {
			return c.String(http.StatusOK, "hello "+user.Name)
		}
		return c.String(http.StatusOK, "hello guest")
	})
	if rec := app.do(t, req{method: http.MethodGet, path: "/feed", absolute: true, token: token}); rec.Body.String() != "hello Ada" {
		t.Fatalf("signed in: %q", rec.Body)
	}
	if rec := app.do(t, req{method: http.MethodGet, path: "/feed", absolute: true}); rec.Body.String() != "hello guest" {
		t.Fatalf("anonymous: %q", rec.Body)
	}
}
