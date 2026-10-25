package corego

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/web"
)

// wildcardWarning is part of the warning New logs for a "*" in
// CORS_ALLOWED_ORIGINS.
const wildcardWarning = `CORS_ALLOWED_ORIGINS contains \"*\"`

// TestCORSWildcardFromEnvHasNoCredentials is the regression test of the
// configuration that reflected any origin together with
// Access-Control-Allow-Credentials (and later panicked).
func TestCORSWildcardFromEnvHasNoCredentials(t *testing.T) {
	for _, origins := range []string{"*", " * ", "https://app.example.com, *"} {
		t.Run(origins, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", origins)
			logger, logs := newTestLogger()
			core := newCore(t, &Config{
				Auth:   &AuthOptions{HTTP: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}}},
				Logger: logger,
			})
			if !strings.Contains(logs.String(), wildcardWarning) {
				t.Errorf("no wildcard warning; logs:\n%s", logs)
			}

			w := do(t, core.Server(), request{method: http.MethodGet, path: "/auth/me",
				headers: map[string]string{"Origin": "https://evil.example"}})
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("Allow-Origin = %q, want *", got)
			}
			if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
				t.Errorf("Allow-Credentials = %q, want none", got)
			}

			preflight := do(t, core.Server(), request{method: http.MethodOptions, path: "/auth/signin", headers: map[string]string{
				"Origin": "https://evil.example", "Access-Control-Request-Method": http.MethodPost,
				"Access-Control-Request-Headers": "Content-Type, Authorization",
			}})
			if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Errorf("preflight: %d, credentials %q", preflight.Code, preflight.Header().Get("Access-Control-Allow-Credentials"))
			}
			if missing := missingSubstrings(preflight.Header().Get("Access-Control-Allow-Headers"), "Content-Type", "Authorization"); len(missing) > 0 {
				t.Errorf("Allow-Headers missing %v", missing)
			}
		})
	}
}

func TestTrustedOriginsFromCORS(t *testing.T) {
	tests := []struct {
		name        string
		env         string
		cors        *web.CORSConfig
		explicit    []string
		want        []string
		wantWarning string
	}{
		{name: "no CORS"},
		{name: "origins from the environment", env: "https://app.example.com, http://localhost:5173",
			want: []string{"https://app.example.com", "http://localhost:5173"}},
		{name: "wildcard from the environment", env: "*"},
		{name: "explicit origins from code", cors: &web.CORSConfig{AllowedOrigins: []string{"https://app.example.com", "https://admin.example.com/"}},
			want: []string{"https://app.example.com", "https://admin.example.com/"}},
		{name: "wildcard in code is skipped", cors: &web.CORSConfig{AllowedOrigins: []string{"*", "https://app.example.com"}},
			want: []string{"https://app.example.com"}},
		{name: "unusable origins are skipped with a warning", cors: &web.CORSConfig{AllowedOrigins: []string{"null", "https://app.example.com", "app.example.com"}},
			want: []string{"https://app.example.com"}, wantWarning: "CORS origin not trusted"},
		{name: "code configuration wins over the environment", env: "https://env.example.com",
			cors: &web.CORSConfig{AllowedOrigins: []string{"https://app.example.com"}}, want: []string{"https://app.example.com"}},
		{name: "explicit trusted origins win", env: "https://app.example.com",
			explicit: []string{"https://other.example.com"}, want: []string{"https://other.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", tt.env)
			logger, logs := newTestLogger()
			core := newCore(t, &Config{
				Auth:   &AuthOptions{HTTP: authhttp.Config{TrustedOrigins: tt.explicit}},
				Server: ServerOptions{CORS: tt.cors},
				Logger: logger,
			})
			got := core.authHTTPConfig(authhttp.Config{TrustedOrigins: tt.explicit}).TrustedOrigins
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("TrustedOrigins = %q, want %q", got, tt.want)
			}
			if tt.wantWarning != "" && !strings.Contains(logs.String(), tt.wantWarning) {
				t.Errorf("no %q warning; logs:\n%s", tt.wantWarning, logs)
			}
			if tt.wantWarning == "" && strings.Contains(logs.String(), "CORS origin not trusted") {
				t.Errorf("unexpected warning; logs:\n%s", logs)
			}
		})
	}
}

func TestCORSOriginsAreTrustedForCookieRequests(t *testing.T) {
	isolateEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	core := newCore(t, &Config{Auth: &AuthOptions{HTTP: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}}}})
	app := core.Server()

	w := do(t, app, request{method: http.MethodPost, path: "/auth/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": "analytical-engine",
	}})
	var cookie string
	for _, c := range w.Result().Cookies() {
		if c.Name == authhttp.DefaultCookieName {
			cookie = c.Name + "=" + c.Value
		}
	}
	if cookie == "" {
		t.Fatalf("signup set no session cookie: %d %s", w.Code, w.Body.String())
	}

	tests := []struct {
		origin string
		want   int
	}{
		{origin: "https://app.example.com", want: http.StatusOK},
		{origin: "https://evil.example", want: http.StatusForbidden},
	}
	for _, tt := range tests {
		w := do(t, app, request{method: http.MethodPatch, path: "/auth/me", body: map[string]any{"name": "Grace"},
			headers: map[string]string{"Cookie": cookie, "Origin": tt.origin, "Sec-Fetch-Site": "cross-site"}})
		if w.Code != tt.want {
			t.Errorf("PATCH /auth/me from %s: status = %d, want %d (body %s)", tt.origin, w.Code, tt.want, w.Body.String())
		}
	}
}

func TestInsecureCookieWarning(t *testing.T) {
	const warning = "the session cookie is not Secure"
	tests := []struct {
		name     string
		appURL   string
		cookie   authhttp.CookieConfig
		wantWarn bool
	}{
		{name: "cookies off", appURL: "http://api.example.com"},
		{name: "default APP_URL is local", cookie: authhttp.CookieConfig{Enabled: true}},
		{name: "localhost", appURL: "http://localhost:8080", cookie: authhttp.CookieConfig{Enabled: true}},
		{name: "localhost in capitals", appURL: "http://LOCALHOST:8080", cookie: authhttp.CookieConfig{Enabled: true}},
		{name: "IPv4 loopback", appURL: "http://127.0.0.1:3000", cookie: authhttp.CookieConfig{Enabled: true}},
		{name: "IPv6 loopback", appURL: "http://[::1]:8080", cookie: authhttp.CookieConfig{Enabled: true}},
		{name: "https APP_URL forces Secure", appURL: "https://api.example.com", cookie: authhttp.CookieConfig{Enabled: true}},
		{name: "explicit Secure", appURL: "http://api.example.com", cookie: authhttp.CookieConfig{Enabled: true, Secure: true}},
		{name: "public http APP_URL", appURL: "http://api.example.com", cookie: authhttp.CookieConfig{Enabled: true}, wantWarn: true},
		{name: "look-alike host", appURL: "http://localhost.example.com", cookie: authhttp.CookieConfig{Enabled: true}, wantWarn: true},
		{name: "other private address", appURL: "http://10.0.0.5:8080", cookie: authhttp.CookieConfig{Enabled: true}, wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("APP_URL", tt.appURL)
			logger, logs := newTestLogger()
			newCore(t, &Config{Auth: &AuthOptions{HTTP: authhttp.Config{Cookie: tt.cookie}}, Logger: logger})
			if got := strings.Contains(logs.String(), warning); got != tt.wantWarn {
				t.Errorf("warning logged = %v, want %v; logs:\n%s", got, tt.wantWarn, logs)
			}
		})
	}
}

func TestIsLocalURL(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"http://localhost", true},
		{"http://localhost:8080", true},
		{"https://127.0.0.1", true},
		{"http://[::1]:3000", true},
		{"http://api.example.com", false},
		{"http://127.0.0.2", false},
		{"", false},
		{"localhost:8080", false},
		{"http://%zz", false},
	}
	for _, tt := range tests {
		if got := isLocalURL(tt.raw); got != tt.want {
			t.Errorf("isLocalURL(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}
