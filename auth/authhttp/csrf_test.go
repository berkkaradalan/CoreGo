package authhttp

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
)

// trustedFrontend is the TrustedOrigins entry of the CSRF tests.
const trustedFrontend = "https://app.example.com"

// newCookieApp builds an app with the cookie transport and
// trustedFrontend trusted, signs Ada up and returns her session cookie.
func newCookieApp(t *testing.T, sameSite http.SameSite) (*testApp, *http.Cookie) {
	t.Helper()
	app := newTestApp(t, appOptions{httpCfg: Config{
		Cookie:         CookieConfig{Enabled: true, Secure: true, SameSite: sameSite},
		TrustedOrigins: []string{trustedFrontend + "/"},
	}})
	rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": testPassword,
	}})
	expectStatus(t, rec, http.StatusCreated)
	cookie := responseCookie(rec, DefaultCookieName)
	if cookie == nil {
		t.Fatal("signup set no session cookie")
	}
	return app, &http.Cookie{Name: cookie.Name, Value: cookie.Value}
}

func TestCrossSiteCheckOnCookieRequests(t *testing.T) {
	// httptest requests target Host "example.com".
	tests := []struct {
		name    string
		method  string
		bearer  bool // send the token as a Bearer header instead of the cookie
		both    bool // send the Bearer header and the cookie
		site    string
		origin  string
		blocked bool
		reason  string // the reason logged at Debug for a blocked request
	}{
		{name: "no browser headers", method: http.MethodPatch},
		{name: "same-origin", method: http.MethodPatch, site: "same-origin", origin: "https://evil.example"},
		{name: "same-site", method: http.MethodPatch, site: "same-site"},
		{name: "user initiated", method: http.MethodPatch, site: "none"},
		{name: "fetch metadata is case insensitive", method: http.MethodPatch, site: "Same-Origin"},
		{name: "cross-site from an untrusted origin", method: http.MethodPatch, site: "cross-site", origin: "https://evil.example", blocked: true, reason: reasonUntrustedCrossSite},
		{name: "cross-site from a trusted origin", method: http.MethodPatch, site: "cross-site", origin: trustedFrontend},
		{name: "trusted origins compare case-insensitively", method: http.MethodPatch, site: "cross-site", origin: "HTTPS://App.Example.COM"},
		{name: "cross-site without an origin", method: http.MethodPatch, site: "cross-site", blocked: true, reason: reasonUntrustedCrossSite},
		{name: "unknown fetch metadata value", method: http.MethodPatch, site: "cross-origin", origin: "http://example.com", blocked: true, reason: reasonUnknownFetchSite},
		{name: "origin matches the host", method: http.MethodPatch, origin: "http://example.com"},
		{name: "origin host compares case-insensitively", method: http.MethodPatch, origin: "https://EXAMPLE.com"},
		{name: "origin on another port", method: http.MethodPatch, origin: "http://example.com:8080", blocked: true, reason: reasonOriginMismatch},
		{name: "origin of another site", method: http.MethodPatch, origin: "https://evil.example", blocked: true, reason: reasonOriginMismatch},
		{name: "origin of a subdomain", method: http.MethodPatch, origin: "https://evil.example.com", blocked: true, reason: reasonOriginMismatch},
		{name: "trusted origin without fetch metadata", method: http.MethodPatch, origin: trustedFrontend},
		{name: "opaque null origin", method: http.MethodPatch, origin: "null", blocked: true, reason: reasonOriginMismatch},
		{name: "malformed origin", method: http.MethodPatch, origin: "::not a url", blocked: true, reason: reasonOriginMismatch},
		{name: "lowercase unsafe method", method: "patch", site: "cross-site", origin: "https://evil.example", blocked: true, reason: reasonUntrustedCrossSite},
		{name: "GET is never checked", method: http.MethodGet, site: "cross-site", origin: "https://evil.example"},
		{name: "lowercase get is never checked", method: "get", site: "cross-site", origin: "https://evil.example"},
		{name: "bearer tokens are never checked", method: http.MethodPatch, bearer: true, site: "cross-site", origin: "https://evil.example"},
		{name: "a bearer token wins over the cookie", method: http.MethodPatch, both: true, site: "cross-site", origin: "https://evil.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, cookie := newCookieApp(t, http.SameSiteNoneMode)
			r := req{method: tt.method, path: "/me", headers: map[string]string{}}
			if !strings.EqualFold(tt.method, http.MethodGet) {
				r.body = map[string]any{"name": "Changed"}
			}
			switch {
			case tt.bearer:
				r.token = cookie.Value
			case tt.both:
				r.token = cookie.Value
				r.cookie = cookie
			default:
				r.cookie = cookie
			}
			if tt.site != "" {
				r.headers["Sec-Fetch-Site"] = tt.site
			}
			if tt.origin != "" {
				r.headers["Origin"] = tt.origin
			}
			logs := captureDefaultLogger(t)
			rec := app.do(t, r)
			blockedLogs := logs.withMessage(logMsgCrossSiteRejection)
			if tt.blocked {
				expectError(t, rec, http.StatusForbidden, msgCrossSiteBlocked)
				if len(blockedLogs) != 1 {
					t.Fatalf("logged %d cross-site rejections, want 1", len(blockedLogs))
				}
				entry := blockedLogs[0]
				want := map[string]string{
					"method":         tt.method,
					"path":           app.base + "/me",
					"origin":         tt.origin,
					"sec_fetch_site": tt.site,
					"reason":         tt.reason,
				}
				if entry.level != slog.LevelDebug {
					t.Errorf("rejection logged at %v, want Debug", entry.level)
				}
				if len(entry.attrs) != len(want) {
					t.Errorf("rejection attrs = %v, want exactly %v", entry.attrs, want)
				}
				for key, value := range want {
					if entry.attrs[key] != value {
						t.Errorf("rejection attr %s = %q, want %q", key, entry.attrs[key], value)
					}
				}
				for key, value := range entry.attrs {
					if strings.Contains(value, cookie.Value) {
						t.Fatalf("rejection attr %s leaks the session token", key)
					}
				}
				if rec.Header().Get("WWW-Authenticate") != "" {
					t.Fatal("a blocked request is not an authentication challenge")
				}
				// Nothing changed.
				user, err := app.store.GetUserByEmail(t.Context(), "ada@example.com")
				if err != nil || user.Name != "Ada" {
					t.Fatalf("user after a blocked request: %+v, %v", user, err)
				}
				return
			}
			expectStatus(t, rec, http.StatusOK)
			if len(blockedLogs) != 0 {
				t.Fatalf("an allowed request logged a rejection: %+v", blockedLogs)
			}
		})
	}
}

// logEntry is one record captured by logCapture, with every attribute
// rendered as a string.
type logEntry struct {
	level   slog.Level
	message string
	attrs   map[string]string
}

// logCapture is a slog.Handler that records every enabled record.
type logCapture struct {
	mu      sync.Mutex
	entries []logEntry
}

func (h *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *logCapture) Handle(_ context.Context, r slog.Record) error {
	entry := logEntry{level: r.Level, message: r.Message, attrs: make(map[string]string)}
	r.Attrs(func(a slog.Attr) bool {
		entry.attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, entry)
	return nil
}

func (h *logCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logCapture) WithGroup(string) slog.Handler      { return h }

// withMessage returns the captured entries with the given message.
func (h *logCapture) withMessage(message string) []logEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []logEntry
	for _, e := range h.entries {
		if e.message == message {
			out = append(out, e)
		}
	}
	return out
}

// captureDefaultLogger installs a logCapture as slog.Default() for the
// rest of the test.
func captureDefaultLogger(t *testing.T) *logCapture {
	t.Helper()
	capture := &logCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return capture
}

// TestSignoutFormCSRF is the regression test of the cross-site form
// POST that signed users out (the token travels in the cookie, the form
// sends no JSON and the browser adds Origin).
func TestSignoutFormCSRF(t *testing.T) {
	app, cookie := newCookieApp(t, http.SameSiteNoneMode)
	logs := captureDefaultLogger(t)
	rec := app.do(t, req{method: http.MethodPost, path: "/signout", cookie: cookie, ctype: "application/x-www-form-urlencoded",
		headers: map[string]string{"Origin": "https://evil.example"}})
	expectError(t, rec, http.StatusForbidden, msgCrossSiteBlocked)
	if got := logs.withMessage(logMsgCrossSiteRejection); len(got) != 1 || got[0].attrs["path"] != app.base+"/signout" || got[0].attrs["reason"] != reasonOriginMismatch {
		t.Fatalf("sign-out rejection logs = %+v", got)
	}
	if responseCookie(rec, DefaultCookieName) != nil {
		t.Fatal("a blocked sign-out must not touch the cookie")
	}
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", cookie: cookie}), http.StatusOK)

	// The same request from the application's own page works.
	rec = app.do(t, req{method: http.MethodPost, path: "/signout", cookie: cookie,
		headers: map[string]string{"Origin": "http://example.com", "Sec-Fetch-Site": "same-origin"}})
	expectStatus(t, rec, http.StatusNoContent)
	expectClearedCookie(t, rec, DefaultCookieName)
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", cookie: cookie}), http.StatusUnauthorized)
}

func TestCrossSiteCheckOnEveryCookieProtectedRoute(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodPost, "/signout"},
		{http.MethodDelete, "/sessions"},
		{http.MethodDelete, "/sessions/x"},
		{http.MethodPost, "/change-password"},
		{http.MethodPost, "/change-email"},
		{http.MethodPatch, "/me"},
		{http.MethodDelete, "/me"},
	}
	app, cookie := newCookieApp(t, http.SameSiteLaxMode)
	for _, rt := range routes {
		rec := app.do(t, req{method: rt.method, path: rt.path, cookie: cookie, body: map[string]any{},
			headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403", rt.method, rt.path, rec.Code)
		}
	}
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", cookie: cookie}), http.StatusOK)
}

func TestMiddlewareCrossSiteCheck(t *testing.T) {
	app, cookie := newCookieApp(t, http.SameSiteNoneMode)
	handler := app.handlers.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(auth.MustUser(r.Context()).Email))
	}))
	send := func(method string, bearer bool, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/action", nil)
		if bearer {
			r.Header.Set("Authorization", "Bearer "+cookie.Value)
		} else {
			r.AddCookie(cookie)
		}
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	logs := captureDefaultLogger(t)
	expectError(t, send(http.MethodPost, false, "https://evil.example"), http.StatusForbidden, msgCrossSiteBlocked)
	if got := logs.withMessage(logMsgCrossSiteRejection); len(got) != 1 || got[0].attrs["path"] != "/api/action" ||
		got[0].attrs["origin"] != "https://evil.example" || got[0].attrs["sec_fetch_site"] != "cross-site" {
		t.Fatalf("Middleware rejection logs = %+v", got)
	}
	for _, rec := range []*httptest.ResponseRecorder{
		send(http.MethodPost, false, trustedFrontend),
		send(http.MethodPost, true, "https://evil.example"),
		send(http.MethodGet, false, "https://evil.example"),
	} {
		if rec.Code != http.StatusOK || rec.Body.String() != "ada@example.com" {
			t.Fatalf("got %d %q, want 200", rec.Code, rec.Body)
		}
	}
}

func TestCrossSiteCheckWithoutTrustedOrigins(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{Cookie: CookieConfig{Enabled: true}}})
	rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": testPassword,
	}})
	cookie := responseCookie(rec, DefaultCookieName)
	for _, origin := range []string{trustedFrontend, "https://evil.example"} {
		rec := app.do(t, req{method: http.MethodPatch, path: "/me", cookie: &http.Cookie{Name: cookie.Name, Value: cookie.Value},
			body: map[string]any{"name": "X"}, headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": origin}})
		expectError(t, rec, http.StatusForbidden, msgCrossSiteBlocked)
	}
}

func TestOriginMatchesHost(t *testing.T) {
	tests := []struct {
		origin, host string
		want         bool
	}{
		{"https://api.example.com", "api.example.com", true},
		{"http://localhost:8080", "localhost:8080", true},
		{"http://localhost:8080", "localhost", false},
		{"https://API.example.com", "api.EXAMPLE.com", true},
		{"https://[::1]:8080", "[::1]:8080", true},
		{"null", "null", false},
		{"", "example.com", false},
		{"https://example.com", "", false},
		{"%zz", "example.com", false},
	}
	for _, tt := range tests {
		if got := originMatchesHost(tt.origin, tt.host); got != tt.want {
			t.Errorf("originMatchesHost(%q, %q) = %v, want %v", tt.origin, tt.host, got, tt.want)
		}
	}
}
