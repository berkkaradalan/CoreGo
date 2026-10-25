package authhttp

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/web"
)

// rateLimitedRoutes are the routes documented as RL.
var rateLimitedRoutes = []web.Route{
	{Method: http.MethodPost, Path: "/signup"},
	{Method: http.MethodPost, Path: "/signin"},
	{Method: http.MethodPost, Path: "/verify-email/request"},
	{Method: http.MethodPost, Path: "/verify-email"},
	{Method: http.MethodGet, Path: "/verify-email"},
	{Method: http.MethodPost, Path: "/forgot-password"},
	{Method: http.MethodPost, Path: "/reset-password"},
	{Method: http.MethodPost, Path: "/change-password"},
	{Method: http.MethodPost, Path: "/change-email"},
	{Method: http.MethodDelete, Path: "/me"},
}

// isRateLimited reports whether r is in rateLimitedRoutes.
func isRateLimited(r web.Route) bool {
	for _, rl := range rateLimitedRoutes {
		if rl == r {
			return true
		}
	}
	return false
}

// hitCounter makes the email address of every hit unique.
var hitCounter atomic.Uint64

// hit sends one request to route from the given address. Bodies carry
// nothing but a fresh email address, so the per address limiters never
// interfere with the per client tests; the per client limiter runs
// before anything else, so the handler's answer does not matter.
func hit(t *testing.T, app *testApp, route web.Route, remote string, headers map[string]string) int {
	t.Helper()
	body := fmt.Sprintf(`{"email":"rl-%d@example.com"}`, hitCounter.Add(1))
	return app.do(t, req{method: route.Method, path: pathFor(route), raw: body, ctype: "application/json", remote: remote, headers: headers}).Code
}

func TestRateLimitEveryDocumentedRoute(t *testing.T) {
	for _, route := range expectedRoutes {
		limited := isRateLimited(route)
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			app := newTestApp(t, appOptions{})
			const ip = "198.51.100.1:1000"
			for i := 1; i <= DefaultRateLimit; i++ {
				if code := hit(t, app, route, ip, nil); code == http.StatusTooManyRequests {
					t.Fatalf("request %d was rate limited", i)
				}
			}
			rec := app.do(t, req{method: route.Method, path: pathFor(route), raw: "{}", ctype: "application/json", remote: ip})
			if !limited {
				if rec.Code == http.StatusTooManyRequests {
					t.Fatal("route is not documented as rate limited")
				}
				return
			}
			expectError(t, rec, http.StatusTooManyRequests, web.DefaultRateLimitMessage)
			retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
			// Sliding window: a full window must partly slide out, so the
			// wait lies between one and two windows.
			if err != nil || retry < 60 || retry > 120 {
				t.Fatalf("Retry-After = %q, want 60..120 seconds", rec.Header().Get("Retry-After"))
			}
			if rec.Header().Get("RateLimit-Limit") != "10" || rec.Header().Get("RateLimit-Remaining") != "0" {
				t.Fatalf("RateLimit headers: limit %q remaining %q", rec.Header().Get("RateLimit-Limit"), rec.Header().Get("RateLimit-Remaining"))
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("429 responses must not be cached either")
			}
			// Another client is not affected.
			if code := hit(t, app, route, "198.51.100.2:1000", nil); code == http.StatusTooManyRequests {
				t.Fatal("another IP was rate limited")
			}
		})
	}
}

// pathFor turns a route pattern into a concrete path.
func pathFor(r web.Route) string {
	if r.Path == "/sessions/:id" {
		return "/sessions/x"
	}
	return r.Path
}

func TestRateLimitedProtectedRoutesLimitBeforeAuthenticating(t *testing.T) {
	app := newTestApp(t, appOptions{})
	route := web.Route{Method: http.MethodPost, Path: "/change-password"}
	const ip = "198.51.100.9:1"
	for i := 0; i < DefaultRateLimit; i++ {
		if code := hit(t, app, route, ip, nil); code != http.StatusUnauthorized {
			t.Fatalf("request %d: status %d, want 401", i+1, code)
		}
	}
	if code := hit(t, app, route, ip, nil); code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429: unauthenticated attempts count too", code)
	}
}

func TestRateLimitersAreIndependentPerRoute(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 2}}})
	const ip = "198.51.100.3:1"
	signin := web.Route{Method: http.MethodPost, Path: "/signin"}
	for i := 0; i < 2; i++ {
		hit(t, app, signin, ip, nil)
	}
	if code := hit(t, app, signin, ip, nil); code != http.StatusTooManyRequests {
		t.Fatalf("third sign-in: %d, want 429", code)
	}
	for _, other := range rateLimitedRoutes {
		if other == signin {
			continue
		}
		if code := hit(t, app, other, ip, nil); code == http.StatusTooManyRequests {
			t.Fatalf("%s %s shares the sign-in limiter", other.Method, other.Path)
		}
	}
	// GET and POST /verify-email have separate limiters as well.
	for i := 0; i < 2; i++ {
		hit(t, app, web.Route{Method: http.MethodPost, Path: "/verify-email"}, "198.51.100.4:1", nil)
	}
	if code := hit(t, app, web.Route{Method: http.MethodGet, Path: "/verify-email"}, "198.51.100.4:1", nil); code == http.StatusTooManyRequests {
		t.Fatal("GET /verify-email shares the POST limiter")
	}
}

func TestRateLimitDisabled(t *testing.T) {
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Disabled: true, Limit: 1}}})
	for _, route := range rateLimitedRoutes {
		for i := 0; i < 3*DefaultRateLimit; i++ {
			rec := app.do(t, req{method: route.Method, path: route.Path, raw: "{}", ctype: "application/json", remote: "198.51.100.5:1"})
			if rec.Code == http.StatusTooManyRequests || rec.Header().Get("RateLimit-Limit") != "" {
				t.Fatalf("%s %s limited although rate limiting is disabled", route.Method, route.Path)
			}
		}
	}
}

func TestRateLimitWindowResets(t *testing.T) {
	const window = 500 * time.Millisecond
	app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 1, Window: window}}})
	// GET /verify-email without parameters answers instantly (no bcrypt),
	// so both requests land in the same window even under the race
	// detector.
	route := web.Route{Method: http.MethodGet, Path: "/verify-email"}
	const ip = "198.51.100.6:1"
	if code := hit(t, app, route, ip, nil); code == http.StatusTooManyRequests {
		t.Fatal("first request limited")
	}
	if code := hit(t, app, route, ip, nil); code != http.StatusTooManyRequests {
		t.Fatalf("second request: %d, want 429", code)
	}
	time.Sleep(3 * window)
	if code := hit(t, app, route, ip, nil); code == http.StatusTooManyRequests {
		t.Fatal("still limited after the window passed")
	}
}

func TestRateLimitKeyUsesRemoteIP(t *testing.T) {
	route := web.Route{Method: http.MethodPost, Path: "/signin"}
	xff := func(v string) map[string]string { return map[string]string{"X-Forwarded-For": v} }

	t.Run("proxy headers untrusted", func(t *testing.T) {
		app := newTestApp(t, appOptions{httpCfg: Config{RateLimit: RateLimitConfig{Limit: 1}}})
		const proxy = "192.0.2.10:1"
		hit(t, app, route, proxy, xff("203.0.113.1"))
		// A spoofed header does not buy a fresh bucket.
		if code := hit(t, app, route, proxy, xff("203.0.113.2")); code != http.StatusTooManyRequests {
			t.Fatalf("status %d, want 429: X-Forwarded-For must be ignored", code)
		}
	})

	t.Run("proxy headers trusted", func(t *testing.T) {
		app := newTestApp(t, appOptions{
			httpCfg: Config{RateLimit: RateLimitConfig{Limit: 1}},
			server:  func(s *web.Server) { s.TrustProxyHeaders = true },
		})
		const proxy = "192.0.2.10:1"
		hit(t, app, route, proxy, xff("203.0.113.1"))
		// Different clients behind the same proxy have separate buckets.
		if code := hit(t, app, route, proxy, xff("203.0.113.2")); code == http.StatusTooManyRequests {
			t.Fatal("a different client behind the proxy was limited")
		}
		// The rightmost entry is the trusted one: a client-supplied
		// leftmost entry does not change the bucket.
		if code := hit(t, app, route, proxy, xff("1.2.3.4, 203.0.113.1")); code != http.StatusTooManyRequests {
			t.Fatalf("status %d, want 429: the leftmost entry must not be used", code)
		}
	})
}

func TestEachMountHasItsOwnLimiters(t *testing.T) {
	service := newService(t)
	h := New(service, Config{RateLimit: RateLimitConfig{Limit: 1}})
	first, second := web.New(), web.New()
	h.Mount(first)
	h.Mount(second)
	route := web.Route{Method: http.MethodPost, Path: "/signin"}
	a := &testApp{server: first, base: "/auth"}
	b := &testApp{server: second, base: "/auth"}
	hit(t, a, route, "198.51.100.7:1", nil)
	if code := hit(t, b, route, "198.51.100.7:1", nil); code == http.StatusTooManyRequests {
		t.Fatal("two mounts share a limiter")
	}
}
