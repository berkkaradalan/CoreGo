package authhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

func TestToken(t *testing.T) {
	tests := []struct {
		name      string
		cookies   bool
		header    string
		cookie    *http.Cookie
		wantToken string
		wantOK    bool
	}{
		{name: "none", wantOK: false},
		{name: "bearer", header: "Bearer abc", wantToken: "abc", wantOK: true},
		{name: "bearer lowercase scheme", header: "bearer abc", wantToken: "abc", wantOK: true},
		{name: "bearer extra spaces", header: "  Bearer   abc  ", wantToken: "abc", wantOK: true},
		{name: "bearer without token", header: "Bearer", wantOK: false},
		{name: "bearer with two tokens", header: "Bearer a b", wantOK: false},
		{name: "basic scheme", header: "Basic dXNlcjpwYXNz", wantOK: false},
		{name: "cookie ignored when disabled", cookie: &http.Cookie{Name: DefaultCookieName, Value: "c"}, wantOK: false},
		{name: "cookie when enabled", cookies: true, cookie: &http.Cookie{Name: DefaultCookieName, Value: "c"}, wantToken: "c", wantOK: true},
		{name: "empty cookie", cookies: true, cookie: &http.Cookie{Name: DefaultCookieName, Value: ""}, wantOK: false},
		{name: "other cookie name", cookies: true, cookie: &http.Cookie{Name: "other", Value: "c"}, wantOK: false},
		{name: "bearer wins over cookie", cookies: true, header: "Bearer b", cookie: &http.Cookie{Name: DefaultCookieName, Value: "c"}, wantToken: "b", wantOK: true},
		{name: "non-bearer header falls back to cookie", cookies: true, header: "Basic x", cookie: &http.Cookie{Name: DefaultCookieName, Value: "c"}, wantToken: "c", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(newService(t), Config{Cookie: CookieConfig{Enabled: tt.cookies}})
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			if tt.cookie != nil {
				r.AddCookie(tt.cookie)
			}
			token, ok := h.Token(r)
			if token != tt.wantToken || ok != tt.wantOK {
				t.Fatalf("Token() = %q, %v; want %q, %v", token, ok, tt.wantToken, tt.wantOK)
			}
		})
	}
}

// authFailureCase is one way a protected request can fail.
type authFailureCase struct {
	name    string
	prepare func(t *testing.T, app *testApp) req // returns the request to send
	code    int
	message string
}

// authFailureCases lists every failure RequireAuth and Middleware map.
// The app must have been built with newAuthFailureApp.
func authFailureCases() []authFailureCase {
	return []authFailureCase{
		{
			name:    "missing token",
			prepare: func(t *testing.T, app *testApp) req { return req{} },
			code:    http.StatusUnauthorized, message: msgMissingToken,
		},
		{
			name: "malformed header",
			prepare: func(t *testing.T, app *testApp) req {
				return req{headers: map[string]string{"Authorization": "Token abc"}}
			},
			code: http.StatusUnauthorized, message: msgMissingToken,
		},
		{
			name:    "unknown token",
			prepare: func(t *testing.T, app *testApp) req { return req{token: "not-a-real-token"} },
			code:    http.StatusUnauthorized, message: msgSessionNotFound,
		},
		{
			name: "expired session",
			prepare: func(t *testing.T, app *testApp) req {
				token := app.signIn(t, "ada@example.com", testPassword)
				app.expireSession(t, token)
				return req{token: token}
			},
			code: http.StatusUnauthorized, message: msgSessionExpired,
		},
		{
			name: "banned user",
			prepare: func(t *testing.T, app *testApp) req {
				token := app.signIn(t, "ban@example.com", testPassword)
				app.updateUser(t, "ban@example.com", func(u *auth.User) { u.Banned = true })
				return req{token: token}
			},
			code: http.StatusForbidden, message: msgUserBanned,
		},
		{
			name: "unverified user",
			prepare: func(t *testing.T, app *testApp) req {
				token := app.signIn(t, "unverified@example.com", testPassword)
				app.updateUser(t, "unverified@example.com", func(u *auth.User) { u.EmailVerified = false })
				return req{token: token}
			},
			code: http.StatusForbidden, message: msgEmailNotVerified,
		},
		{
			name: "store failure",
			prepare: func(t *testing.T, app *testApp) req {
				token := app.signIn(t, "ada@example.com", testPassword)
				app.store.(*failingStore).failGetSession.Store(true)
				t.Cleanup(func() { app.store.(*failingStore).failGetSession.Store(false) })
				return req{token: token}
			},
			code: http.StatusInternalServerError, message: "Internal Server Error",
		},
	}
}

// newAuthFailureApp builds an app with RequireVerifiedEmail enabled, a
// failing store wrapper, a protected application route at /api/whoami and
// three verified users (ada, ban, unverified).
func newAuthFailureApp(t *testing.T) *testApp {
	t.Helper()
	store := &failingStore{MemoryStore: auth.NewMemoryStore()}
	app := newTestApp(t, appOptions{
		store:   store,
		authCfg: func(c *auth.Config) { c.RequireVerifiedEmail = true },
	})
	app.server.Get("/api/whoami", func(c *web.Context) error {
		return c.String(http.StatusOK, auth.MustUser(c).Email)
	}, app.handlers.RequireAuth())
	for _, email := range []string{"ada@example.com", "ban@example.com", "unverified@example.com"} {
		app.signUp(t, "User", email, testPassword)
		app.updateUser(t, email, func(u *auth.User) { u.EmailVerified = true })
	}
	return app
}

func TestRequireAuthFailures(t *testing.T) {
	for _, tt := range authFailureCases() {
		t.Run(tt.name, func(t *testing.T) {
			app := newAuthFailureApp(t)
			r := tt.prepare(t, app)
			r.method, r.path, r.absolute = http.MethodGet, "/api/whoami", true
			rec := app.do(t, r)
			expectError(t, rec, tt.code, tt.message)
			wantChallenge := ""
			if tt.code == http.StatusUnauthorized {
				wantChallenge = "Bearer"
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != wantChallenge {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, wantChallenge)
			}
		})
	}
}

func TestRequireAuthExposesUserAndSession(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token

	var gotUser *auth.User
	var gotSession *auth.Session
	app.server.Get("/api/me", func(c *web.Context) error {
		gotUser = auth.MustUser(c)
		gotSession, _ = auth.SessionFromContext(c)
		// The values are on the request context, visible to any code
		// that only receives the *http.Request.
		if u, ok := auth.UserFromContext(c.Request().Context()); !ok || u.ID != gotUser.ID {
			t.Error("the user is not on the request context")
		}
		return c.String(http.StatusOK, gotUser.Name)
	}, app.handlers.RequireAuth())

	rec := app.do(t, req{method: http.MethodGet, path: "/api/me", absolute: true, token: token})
	expectStatus(t, rec, http.StatusOK)
	if rec.Body.String() != "Ada" {
		t.Fatalf("body = %q", rec.Body)
	}
	if gotUser == nil || gotUser.Email != "ada@example.com" || gotSession == nil || gotSession.UserID != gotUser.ID {
		t.Fatalf("user %+v / session %+v not exposed", gotUser, gotSession)
	}
}

func TestRequireAuthOnGroupAndUnprotectedRoutes(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token

	api := app.server.Group("/api", app.handlers.RequireAuth())
	api.Get("/hello", func(c *web.Context) error {
		return c.String(http.StatusOK, "hello "+auth.MustUser(c).Name)
	})
	app.server.Get("/public", func(c *web.Context) error {
		if _, ok := auth.UserFromContext(c); ok {
			return c.String(http.StatusOK, "signed in")
		}
		return c.String(http.StatusOK, "anonymous")
	})
	// MustUser without RequireAuth is a programming error: recovered
	// into a 500 by the web framework.
	app.server.Get("/forgot-to-protect", func(c *web.Context) error {
		return c.String(http.StatusOK, auth.MustUser(c).Name)
	})

	expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/api/hello", absolute: true, token: token}), http.StatusOK)
	expectError(t, app.do(t, req{method: http.MethodGet, path: "/api/hello", absolute: true}), http.StatusUnauthorized, msgMissingToken)

	rec := app.do(t, req{method: http.MethodGet, path: "/public", absolute: true, token: token})
	if rec.Body.String() != "anonymous" {
		t.Fatalf("an unprotected route must not resolve the session: %q", rec.Body)
	}
	expectError(t, app.do(t, req{method: http.MethodGet, path: "/forgot-to-protect", absolute: true, token: token}),
		http.StatusInternalServerError, "Internal Server Error")
}

func TestRequireAuthErrorsKeepTheSentinel(t *testing.T) {
	app := newTestApp(t, appOptions{})
	var captured error
	app.server.OnError(func(c *web.Context, err error) {
		captured = err
		_ = c.Status(http.StatusTeapot)
	})
	app.server.Get("/api/x", func(c *web.Context) error { return nil }, app.handlers.RequireAuth())
	rec := app.do(t, req{method: http.MethodGet, path: "/api/x", absolute: true, token: "unknown"})
	expectStatus(t, rec, http.StatusTeapot)
	if !errors.Is(captured, auth.ErrSessionNotFound) {
		t.Fatalf("custom error handlers must see the auth sentinel, got %v", captured)
	}
}

// netHTTPHandler mounts h.Middleware in front of a plain net/http handler.
func netHTTPHandler(h *Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/whoami", h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(auth.MustUser(r.Context()).Email))
	})))
	return mux
}

func TestMiddlewareParityWithRequireAuth(t *testing.T) {
	for _, tt := range authFailureCases() {
		t.Run(tt.name, func(t *testing.T) {
			// Two identical apps: one serves the request through the web
			// framework, the other through net/http.
			webApp := newAuthFailureApp(t)
			r := tt.prepare(t, webApp)
			r.method, r.path, r.absolute = http.MethodGet, "/api/whoami", true
			webRec := webApp.do(t, r)

			plainApp := newAuthFailureApp(t)
			r2 := tt.prepare(t, plainApp)
			r2.method, r2.path, r2.absolute = http.MethodGet, "/api/whoami", true
			plainHTTP := netHTTPHandler(plainApp.handlers)
			plainRec := serveWith(t, plainApp, plainHTTP, r2)

			if webRec.Code != plainRec.Code || webRec.Body.String() != plainRec.Body.String() {
				t.Fatalf("web %d %q vs net/http %d %q", webRec.Code, webRec.Body, plainRec.Code, plainRec.Body)
			}
			for _, header := range []string{"Content-Type", "WWW-Authenticate"} {
				if webRec.Header().Get(header) != plainRec.Header().Get(header) {
					t.Fatalf("%s: web %q vs net/http %q", header, webRec.Header().Get(header), plainRec.Header().Get(header))
				}
			}
			expectError(t, plainRec, tt.code, tt.message)
		})
	}
}

// serveWith sends r to handler instead of the app's web server.
func serveWith(t *testing.T, app *testApp, handler http.Handler, r req) *httptest.ResponseRecorder {
	t.Helper()
	original := app.server
	defer func() { app.server = original }()
	s := web.New()
	s.Get("/*any", web.WrapHandler(handler))
	app.server = s
	return app.do(t, r)
}

func TestMiddlewareSuccess(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{Cookie: CookieConfig{Enabled: true}}})
	signup := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": testPassword,
	}})
	expectStatus(t, signup, http.StatusCreated)
	cookie := responseCookie(signup, DefaultCookieName)

	handler := netHTTPHandler(app.handlers)
	for _, tc := range []struct {
		name  string
		setup func(*http.Request)
	}{
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+cookie.Value) }},
		{"cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
			tc.setup(r)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK || rec.Body.String() != "ada@example.com" {
				t.Fatalf("got %d %q", rec.Code, rec.Body)
			}
		})
	}
}

func TestMiddlewarePanicsOnNilHandler(t *testing.T) {
	h := New(newService(t), Config{})
	expectPanic(t, "authhttp: Middleware requires a non-nil http.Handler", func() { h.Middleware(nil) })
}

func TestTransportPrecedence(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{Cookie: CookieConfig{Enabled: true}}})
	ada := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	bob := app.signUp(t, "Bob", "bob@example.com", testPassword).Token
	cookieFor := func(token string) *http.Cookie { return &http.Cookie{Name: DefaultCookieName, Value: token} }

	tests := []struct {
		name      string
		r         req
		wantEmail string
		wantCode  int
	}{
		{name: "bearer only", r: req{token: ada}, wantEmail: "ada@example.com", wantCode: 200},
		{name: "cookie only", r: req{cookie: cookieFor(bob)}, wantEmail: "bob@example.com", wantCode: 200},
		{name: "bearer wins over cookie", r: req{token: ada, cookie: cookieFor(bob)}, wantEmail: "ada@example.com", wantCode: 200},
		{name: "invalid bearer does not fall back to a valid cookie", r: req{token: "invalid", cookie: cookieFor(bob)}, wantCode: 401},
		{name: "invalid cookie", r: req{cookie: cookieFor("invalid")}, wantCode: 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.method, tt.r.path = http.MethodGet, "/me"
			rec := app.do(t, tt.r)
			expectStatus(t, rec, tt.wantCode)
			if tt.wantCode == http.StatusOK {
				var out struct {
					User userResult `json:"user"`
				}
				decode(t, rec, &out)
				if out.User.Email != tt.wantEmail {
					t.Fatalf("authenticated as %q, want %q", out.User.Email, tt.wantEmail)
				}
			}
		})
	}

	t.Run("cookie ignored when the transport is disabled", func(t *testing.T) {
		plain := newTestApp(t, appOptions{})
		token := plain.signUp(t, "Ada", "ada@example.com", testPassword).Token
		rec := plain.do(t, req{method: http.MethodGet, path: "/me", cookie: cookieFor(token)})
		expectError(t, rec, http.StatusUnauthorized, msgMissingToken)
	})
}

func TestCookieAttributes(t *testing.T) {
	tests := []struct {
		name   string
		cookie CookieConfig
		check  func(t *testing.T, c *http.Cookie)
	}{
		{
			name:   "defaults",
			cookie: CookieConfig{Enabled: true},
			check: func(t *testing.T, c *http.Cookie) {
				if c.Name != DefaultCookieName || c.Path != "/" || c.Domain != "" || c.Secure || c.SameSite != http.SameSiteLaxMode {
					t.Fatalf("unexpected default cookie %+v", c)
				}
			},
		},
		{
			name:   "custom",
			cookie: CookieConfig{Enabled: true, Name: "sid", Path: "/app", Domain: "example.com", Secure: true, SameSite: http.SameSiteStrictMode},
			check: func(t *testing.T, c *http.Cookie) {
				if c.Name != "sid" || c.Path != "/app" || c.Domain != "example.com" || !c.Secure || c.SameSite != http.SameSiteStrictMode {
					t.Fatalf("unexpected custom cookie %+v", c)
				}
			},
		},
		{
			name:   "cross site",
			cookie: CookieConfig{Enabled: true, Secure: true, SameSite: http.SameSiteNoneMode},
			check: func(t *testing.T, c *http.Cookie) {
				if c.SameSite != http.SameSiteNoneMode || !c.Secure {
					t.Fatalf("unexpected cross-site cookie %+v", c)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, appOptions{httpCfg: Config{Cookie: tt.cookie}})
			name := app.handlers.cfg.Cookie.Name
			for _, path := range []string{"/signup", "/signin"} {
				body := map[string]any{"name": "Ada", "email": "ada@example.com", "password": testPassword}
				rec := app.do(t, req{method: http.MethodPost, path: path, body: body})
				if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
					t.Fatalf("%s: status %d", path, rec.Code)
				}
				var out signUpResult
				decode(t, rec, &out)
				c := responseCookie(rec, name)
				if c == nil {
					t.Fatalf("%s set no %s cookie", path, name)
				}
				if c.Value != out.Token {
					t.Fatalf("%s: cookie value differs from the token", path)
				}
				if !c.HttpOnly {
					t.Fatalf("%s: the session cookie must always be HttpOnly", path)
				}
				if d := c.Expires.Sub(*out.ExpiresAt); d < -time.Second || d > time.Second {
					t.Fatalf("%s: cookie expires %v, session expires %v", path, c.Expires, out.ExpiresAt)
				}
				tt.check(t, c)
			}
		})
	}
}

func TestCookieIsClearedWhenTheSessionEnds(t *testing.T) {
	cookieCfg := CookieConfig{Enabled: true, Name: "sid", Path: "/app", Domain: "example.com", Secure: true, SameSite: http.SameSiteStrictMode}
	tests := []struct {
		name string
		do   func(t *testing.T, app *testApp, token string) *httptest.ResponseRecorder
	}{
		{"signout", func(t *testing.T, app *testApp, token string) *httptest.ResponseRecorder {
			return app.do(t, req{method: http.MethodPost, path: "/signout", token: token})
		}},
		{"revoke all sessions", func(t *testing.T, app *testApp, token string) *httptest.ResponseRecorder {
			return app.do(t, req{method: http.MethodDelete, path: "/sessions", token: token})
		}},
		{"revoke the current session", func(t *testing.T, app *testApp, token string) *httptest.ResponseRecorder {
			s, _, err := app.service.GetSession(t.Context(), token)
			if err != nil {
				t.Fatal(err)
			}
			return app.do(t, req{method: http.MethodDelete, path: "/sessions/" + s.ID, token: token})
		}},
		{"change password", func(t *testing.T, app *testApp, token string) *httptest.ResponseRecorder {
			return app.do(t, req{method: http.MethodPost, path: "/change-password", token: token, body: map[string]any{
				"current_password": testPassword, "new_password": "a-brand-new-password",
			}})
		}},
		{"delete account", func(t *testing.T, app *testApp, token string) *httptest.ResponseRecorder {
			return app.do(t, req{method: http.MethodDelete, path: "/me", token: token, body: map[string]any{"password": testPassword}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, appOptions{httpCfg: Config{Cookie: cookieCfg}})
			token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
			rec := tt.do(t, app, token)
			expectStatus(t, rec, http.StatusNoContent)
			expectClearedCookie(t, rec, "sid")
			c := responseCookie(rec, "sid")
			// The clearing cookie must match the attributes of the original,
			// or browsers keep the original.
			if c.Path != "/app" || c.Domain != "example.com" || !c.Secure || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("clearing cookie attributes differ: %+v", c)
			}
		})
	}

	t.Run("revoking another session keeps the cookie", func(t *testing.T) {
		app := newTestApp(t, appOptions{httpCfg: Config{Cookie: cookieCfg}})
		token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
		other := app.signIn(t, "ada@example.com", testPassword)
		s, _, _ := app.service.GetSession(t.Context(), other)
		rec := app.do(t, req{method: http.MethodDelete, path: "/sessions/" + s.ID, token: token})
		expectStatus(t, rec, http.StatusNoContent)
		if len(rec.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("unexpected Set-Cookie %v", rec.Header().Values("Set-Cookie"))
		}
	})

	t.Run("nothing is cleared while cookies are disabled", func(t *testing.T) {
		app := newTestApp(t, appOptions{})
		token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
		rec := app.do(t, req{method: http.MethodPost, path: "/signout", token: token})
		expectStatus(t, rec, http.StatusNoContent)
		if len(rec.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("unexpected Set-Cookie %v", rec.Header().Values("Set-Cookie"))
		}
	})
}
