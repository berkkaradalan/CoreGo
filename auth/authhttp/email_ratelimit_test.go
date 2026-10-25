package authhttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/web"
)

// emailLimitedRoutes are the routes limited per email address as well
// as per client.
var emailLimitedRoutes = []web.Route{
	{Method: http.MethodPost, Path: "/signin"},
	{Method: http.MethodPost, Path: "/forgot-password"},
	{Method: http.MethodPost, Path: "/verify-email/request"},
}

// emailHit sends one request for email to route from a fresh client IP,
// so only the per address limiter can reject it.
func emailHit(t *testing.T, app *testApp, route web.Route, email string) *httptest.ResponseRecorder {
	t.Helper()
	return app.do(t, req{method: route.Method, path: route.Path, body: map[string]any{
		"email": email, "password": "wrong-password",
	}})
}

func TestEmailRateLimitAcrossClients(t *testing.T) {
	for _, route := range emailLimitedRoutes {
		t.Run(route.Path, func(t *testing.T) {
			app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 2}}})
			app.signUp(t, "Ada", "ada@example.com", testPassword)

			// Spelling variants of one address share the bucket.
			for i, email := range []string{"ada@example.com", "  ADA@Example.COM "} {
				if rec := emailHit(t, app, route, email); rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d was rate limited", i+1)
				}
			}
			rec := emailHit(t, app, route, "Ada@example.com")
			expectError(t, rec, http.StatusTooManyRequests, web.DefaultRateLimitMessage)
			retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
			if err != nil || retry < 60 || retry > 120 {
				t.Fatalf("Retry-After = %q, want 60..120 seconds", rec.Header().Get("Retry-After"))
			}
			if rec.Header().Get("RateLimit-Limit") != "2" || rec.Header().Get("RateLimit-Remaining") != "0" {
				t.Fatalf("RateLimit headers: limit %q remaining %q", rec.Header().Get("RateLimit-Limit"), rec.Header().Get("RateLimit-Remaining"))
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("429 responses must not be cached")
			}
			// Another address is not affected.
			if rec := emailHit(t, app, route, "grace@example.com"); rec.Code == http.StatusTooManyRequests {
				t.Fatal("another address was rate limited")
			}
		})
	}
}

func TestEmailRateLimitDoesNotRevealRegistration(t *testing.T) {
	for _, route := range emailLimitedRoutes[1:] {
		t.Run(route.Path, func(t *testing.T) {
			app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 3}}})
			app.signUp(t, "Ada", "ada@example.com", testPassword)
			app.waitEmails(t)

			sequence := func(email string) []string {
				var out []string
				for i := 0; i < 5; i++ {
					rec := emailHit(t, app, route, email)
					out = append(out, fmt.Sprintf("%d %s %s", rec.Code, rec.Header().Get("RateLimit-Remaining"), rec.Body))
				}
				return out
			}
			registered := sequence("ada@example.com")
			unknown := sequence("nobody@example.com")
			for i := range registered {
				if registered[i] != unknown[i] {
					t.Fatalf("response %d differs: registered %q, unknown %q", i+1, registered[i], unknown[i])
				}
			}
			if want := http.StatusTooManyRequests; registered[4][:3] != strconv.Itoa(want) {
				t.Fatalf("the fifth request was not limited: %q", registered[4])
			}
		})
	}
}

func TestEmailRateLimitersAreIndependentPerRoute(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 1}}})
	signin := emailLimitedRoutes[0]
	emailHit(t, app, signin, "ada@example.com")
	if rec := emailHit(t, app, signin, "ada@example.com"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second sign-in for the address: %d, want 429", rec.Code)
	}
	for _, other := range emailLimitedRoutes[1:] {
		if rec := emailHit(t, app, other, "ada@example.com"); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("%s shares the sign-in address limiter", other.Path)
		}
	}
}

func TestEmailRateLimitOnlyOnDocumentedRoutes(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 1}}})
	for _, route := range []web.Route{
		{Method: http.MethodPost, Path: "/signup"},
		{Method: http.MethodPost, Path: "/verify-email"},
		{Method: http.MethodPost, Path: "/reset-password"},
	} {
		for i := 0; i < 3; i++ {
			if rec := emailHit(t, app, route, "ada@example.com"); rec.Code == http.StatusTooManyRequests {
				t.Fatalf("%s is limited per address", route.Path)
			}
		}
	}
}

func TestEmailRateLimitDisabled(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Disabled: true, Limit: 1}}})
	for _, route := range emailLimitedRoutes {
		for i := 0; i < 3; i++ {
			rec := emailHit(t, app, route, "ada@example.com")
			if rec.Code == http.StatusTooManyRequests || rec.Header().Get("RateLimit-Limit") != "" {
				t.Fatalf("%s limited although rate limiting is disabled", route.Path)
			}
		}
	}
}

func TestEmailRateLimitRemainingIsTheStricterLimit(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 5}}})
	route := emailLimitedRoutes[1]
	// Three requests for the address from different clients ...
	for i := 0; i < 3; i++ {
		emailHit(t, app, route, "ada@example.com")
	}
	// ... leave a fresh client with 4 requests but the address with 1.
	rec := emailHit(t, app, route, "ada@example.com")
	expectStatus(t, rec, http.StatusAccepted)
	if got := rec.Header().Get("RateLimit-Remaining"); got != "1" {
		t.Fatalf("RateLimit-Remaining = %q, want 1 (the address allowance)", got)
	}
	// A fresh address keeps reporting the client allowance.
	const ip = "198.51.100.20:1"
	for i := 0; i < 2; i++ {
		app.do(t, req{method: route.Method, path: route.Path, remote: ip, body: map[string]any{"email": fmt.Sprintf("x%d@example.com", i)}})
	}
	rec = app.do(t, req{method: route.Method, path: route.Path, remote: ip, body: map[string]any{"email": "fresh@example.com"}})
	if got := rec.Header().Get("RateLimit-Remaining"); got != "2" {
		t.Fatalf("RateLimit-Remaining = %q, want 2 (the client allowance)", got)
	}
}

func TestEmailRateLimitIgnoresMalformedBodies(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 1}}})
	route := emailLimitedRoutes[0]
	for i := 0; i < 3; i++ {
		rec := app.do(t, req{method: route.Method, path: route.Path, raw: `{"email":`, ctype: "application/json"})
		expectStatus(t, rec, http.StatusBadRequest)
	}
	// Bodies that never decoded did not count against any address.
	if rec := emailHit(t, app, route, ""); rec.Code == http.StatusTooManyRequests {
		t.Fatal("malformed bodies counted against the address limiter")
	}
}

func TestEmailRateLimitWindowResets(t *testing.T) {
	const window = 300 * time.Millisecond
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 1, Window: window}}})
	route := emailLimitedRoutes[2]
	emailHit(t, app, route, "ada@example.com")
	if rec := emailHit(t, app, route, "ada@example.com"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: %d, want 429", rec.Code)
	}
	time.Sleep(3 * window)
	if rec := emailHit(t, app, route, "ada@example.com"); rec.Code == http.StatusTooManyRequests {
		t.Fatal("still limited after the window passed")
	}
}

func TestEmailRateKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ada@example.com", "ada@example.com"},
		{"  Ada@Example.COM\t", "ada@example.com"},
		{"", ""},
		{"not an address", "not an address"},
	}
	for _, tt := range tests {
		if got := emailRateKey(tt.in); got != tt.want {
			t.Errorf("emailRateKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
