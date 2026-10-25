package authhttp

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// newService returns a memory-backed service with discarded logs.
func newService(t *testing.T) *auth.Auth {
	t.Helper()
	service, err := auth.New(auth.Config{Logger: slog.New(slog.DiscardHandler)}, auth.NewMemoryStore())
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return service
}

func TestNewAppliesDefaults(t *testing.T) {
	h := New(newService(t), Config{})
	cfg := h.cfg
	if cfg.BasePath != DefaultBasePath {
		t.Errorf("BasePath = %q, want %q", cfg.BasePath, DefaultBasePath)
	}
	if cfg.Cookie.Enabled {
		t.Error("cookies must be disabled by default")
	}
	if cfg.Cookie.Name != DefaultCookieName || cfg.Cookie.Path != DefaultCookiePath {
		t.Errorf("cookie name/path = %q/%q, want %q/%q", cfg.Cookie.Name, cfg.Cookie.Path, DefaultCookieName, DefaultCookiePath)
	}
	if cfg.Cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cfg.Cookie.SameSite)
	}
	if cfg.RateLimit.Disabled || cfg.RateLimit.Limit != DefaultRateLimit || cfg.RateLimit.Window != DefaultRateLimitWindow {
		t.Errorf("RateLimit = %+v, want enabled with %d per %v", cfg.RateLimit, DefaultRateLimit, DefaultRateLimitWindow)
	}
	if DefaultRateLimit != 10 || DefaultRateLimitWindow != time.Minute {
		t.Errorf("documented defaults changed: %d per %v", DefaultRateLimit, DefaultRateLimitWindow)
	}
}

func TestNewKeepsExplicitValues(t *testing.T) {
	h := New(newService(t), Config{
		BasePath: "/accounts/",
		Cookie: CookieConfig{
			Enabled:  true,
			Name:     "sid",
			Path:     "/app",
			Domain:   "example.com",
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		},
		RateLimit:              RateLimitConfig{Limit: 3, Window: time.Second},
		VerifyEmailRedirectURL: "https://app.example.com/verified",
	})
	cfg := h.cfg
	if cfg.BasePath != "/accounts" {
		t.Errorf("BasePath = %q, want /accounts", cfg.BasePath)
	}
	want := CookieConfig{Enabled: true, Name: "sid", Path: "/app", Domain: "example.com", Secure: true, SameSite: http.SameSiteNoneMode}
	if cfg.Cookie != want {
		t.Errorf("Cookie = %+v, want %+v", cfg.Cookie, want)
	}
	if cfg.RateLimit.Limit != 3 || cfg.RateLimit.Window != time.Second {
		t.Errorf("RateLimit = %+v", cfg.RateLimit)
	}
}

func TestNewPanicsOnNilService(t *testing.T) {
	expectPanic(t, "authhttp: New requires a non-nil *auth.Auth service", func() {
		New(nil, Config{})
	})
}

func TestNormalizeBasePath(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr string
	}{
		{raw: "", want: "/auth"},
		{raw: "/", want: ""},
		{raw: "/auth", want: "/auth"},
		{raw: "/auth/", want: "/auth"},
		{raw: "/api/v1/auth", want: "/api/v1/auth"},
		{raw: "/caf%C3%A9", want: "/caf%C3%A9"},
		{raw: "auth", wantErr: "must start with '/'"},
		{raw: " /auth", wantErr: "must start with '/'"},
		{raw: "//", wantErr: "empty segments"},
		{raw: "/auth//", wantErr: "empty segments"},
		{raw: "/a//b", wantErr: "empty segments"},
		{raw: "/tenants/:id", wantErr: "parameter or wildcard"},
		{raw: "/files/*rest", wantErr: "parameter or wildcard"},
		{raw: "/auth?x=1", wantErr: "query or fragment"},
		{raw: "/auth#top", wantErr: "query or fragment"},
		{raw: "/my auth", wantErr: "whitespace or control"},
		{raw: "/auth\t", wantErr: "whitespace or control"},
		{raw: "/auth\x00", wantErr: "whitespace or control"},
		{raw: "/%zz", wantErr: "malformed percent-encoding"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := normalizeBasePath(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("normalizeBasePath(%q) = %q, want an error", tt.raw, got)
				}
				if msg := err.Error(); !strings.Contains(msg, tt.wantErr) || !strings.Contains(msg, "authhttp: invalid BasePath") {
					t.Fatalf("error %q does not mention %q", msg, tt.wantErr)
				}
				expectPanic(t, tt.wantErr, func() { New(newService(t), Config{BasePath: tt.raw}) })
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("normalizeBasePath(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
			}
		})
	}
}

func TestHandlersBasePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		want     string
	}{
		{name: "empty selects the default", basePath: "", want: DefaultBasePath},
		{name: "root", basePath: "/", want: ""},
		{name: "custom", basePath: "/accounts", want: "/accounts"},
		{name: "trailing slash dropped", basePath: "/accounts/", want: "/accounts"},
		{name: "nested", basePath: "/api/v1/auth/", want: "/api/v1/auth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{BasePath: tt.basePath}
			resolved, err := cfg.ResolvedBasePath()
			if err != nil || resolved != tt.want {
				t.Fatalf("ResolvedBasePath() = %q, %v; want %q", resolved, err, tt.want)
			}
			h := New(newService(t), cfg)
			if got := h.BasePath(); got != tt.want {
				t.Fatalf("BasePath() = %q, want %q", got, tt.want)
			}
			// Mounting on a group does not change the resolved prefix.
			h.Mount(web.New().Group("/api"))
			if got := h.BasePath(); got != tt.want {
				t.Fatalf("BasePath() after Mount on a group = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateRedirectURL(t *testing.T) {
	tests := []struct {
		raw     string
		wantErr string
	}{
		{raw: ""},
		{raw: "https://app.example.com/verified"},
		{raw: "http://localhost:3000/verified?lang=en#top"},
		{raw: "/email-verified"},
		{raw: "/"},
		{raw: "https:///verified", wantErr: "needs a host"},
		{raw: "javascript:alert(1)", wantErr: "only http and https"},
		{raw: "ftp://example.com/x", wantErr: "only http and https"},
		{raw: "//evil.example/verified", wantErr: "protocol-relative"},
		{raw: "/\\evil.example", wantErr: "protocol-relative"},
		{raw: "verified", wantErr: "path starting with '/'"},
		{raw: "https://exa mple.com/", wantErr: "not a valid URL"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			err := validateRedirectURL(tt.raw)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateRedirectURL(%q) = %v, want nil", tt.raw, err)
				}
				New(newService(t), Config{VerifyEmailRedirectURL: tt.raw})
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateRedirectURL(%q) = %v, want an error containing %q", tt.raw, err, tt.wantErr)
			}
			expectPanic(t, "authhttp: invalid VerifyEmailRedirectURL", func() {
				New(newService(t), Config{VerifyEmailRedirectURL: tt.raw})
			})
		})
	}
}

func TestNewValidatesCookieOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cookie  CookieConfig
		wantErr string
	}{
		{name: "defaults", cookie: CookieConfig{Enabled: true}},
		{name: "none with secure", cookie: CookieConfig{Enabled: true, SameSite: http.SameSiteNoneMode, Secure: true}},
		{name: "strict", cookie: CookieConfig{Enabled: true, SameSite: http.SameSiteStrictMode}},
		{name: "invalid name", cookie: CookieConfig{Enabled: true, Name: "bad name"}, wantErr: "invalid Cookie configuration"},
		{name: "invalid path", cookie: CookieConfig{Enabled: true, Path: "/a;b"}, wantErr: "invalid Cookie configuration"},
		{name: "invalid domain", cookie: CookieConfig{Enabled: true, Domain: "exa mple.com"}, wantErr: "invalid Cookie configuration"},
		{name: "none without secure", cookie: CookieConfig{Enabled: true, SameSite: http.SameSiteNoneMode}, wantErr: "SameSite=None requires Secure=true"},
		{name: "disabled invalid name is ignored", cookie: CookieConfig{Name: "bad name"}},
		{name: "omit token from body", cookie: CookieConfig{Enabled: true, OmitTokenFromBody: true}},
		{name: "omit token without cookies", cookie: CookieConfig{OmitTokenFromBody: true}, wantErr: "OmitTokenFromBody requires Enabled=true"},
		{name: "omit token without cookies and an invalid name", cookie: CookieConfig{Name: "bad name", OmitTokenFromBody: true}, wantErr: "OmitTokenFromBody requires Enabled=true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == "" {
				New(newService(t), Config{Cookie: tt.cookie})
				return
			}
			expectPanic(t, tt.wantErr, func() { New(newService(t), Config{Cookie: tt.cookie}) })
		})
	}
}

func TestConfigValidateMatchesNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{name: "zero value", cfg: Config{}},
		{name: "fully customized", cfg: Config{
			BasePath:               "/accounts/",
			Cookie:                 CookieConfig{Enabled: true, Name: "sid", Secure: true, SameSite: http.SameSiteNoneMode},
			RateLimit:              RateLimitConfig{Limit: 3, Window: time.Second},
			VerifyEmailRedirectURL: "https://app.example.com/verified",
		}},
		{name: "invalid base path", cfg: Config{BasePath: "auth"}, wantErr: "authhttp: invalid BasePath"},
		{name: "invalid cookie", cfg: Config{Cookie: CookieConfig{Enabled: true, SameSite: http.SameSiteNoneMode}}, wantErr: "SameSite=None requires Secure=true"},
		{name: "negative limit", cfg: Config{RateLimit: RateLimitConfig{Limit: -1}}, wantErr: "RateLimit.Limit must not be negative"},
		{name: "negative window", cfg: Config{RateLimit: RateLimitConfig{Window: -time.Second}}, wantErr: "RateLimit.Window must not be negative"},
		{name: "invalid redirect", cfg: Config{VerifyEmailRedirectURL: "//evil.example"}, wantErr: "authhttp: invalid VerifyEmailRedirectURL"},
		{name: "trusted origins", cfg: Config{TrustedOrigins: []string{"https://app.example.com", "http://localhost:5173/"}}},
		{name: "invalid trusted origin", cfg: Config{TrustedOrigins: []string{"https://app.example.com", "*"}}, wantErr: "authhttp: invalid TrustedOrigins entry \"*\""},
		{name: "omit token with cookies", cfg: Config{Cookie: CookieConfig{Enabled: true, OmitTokenFromBody: true}}},
		{name: "omit token without cookies", cfg: Config{Cookie: CookieConfig{OmitTokenFromBody: true}}, wantErr: "OmitTokenFromBody requires Enabled=true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				New(newService(t), tt.cfg)
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
			// New panics with exactly the error Validate reports.
			expectPanic(t, err.Error(), func() { New(newService(t), tt.cfg) })
		})
	}
}

func TestResolvedBasePathRejectsInvalid(t *testing.T) {
	got, err := Config{BasePath: "/tenants/:id"}.ResolvedBasePath()
	if err == nil || !strings.Contains(err.Error(), "authhttp: invalid BasePath") {
		t.Fatalf("ResolvedBasePath() = %q, %v; want an invalid BasePath error", got, err)
	}
}

func TestNewRejectsNegativeRateLimits(t *testing.T) {
	expectPanic(t, "RateLimit.Limit must not be negative", func() {
		New(newService(t), Config{RateLimit: RateLimitConfig{Limit: -1}})
	})
	expectPanic(t, "RateLimit.Window must not be negative", func() {
		New(newService(t), Config{RateLimit: RateLimitConfig{Window: -time.Second}})
	})
	// Disabled limits are still validated: a negative value is a bug.
	expectPanic(t, "RateLimit.Limit must not be negative", func() {
		New(newService(t), Config{RateLimit: RateLimitConfig{Disabled: true, Limit: -5}})
	})
}

func TestTrustedOriginsValidation(t *testing.T) {
	tests := []struct {
		origin  string
		wantErr string
	}{
		{origin: "https://app.example.com"},
		{origin: "http://localhost:5173"},
		{origin: "https://app.example.com/"},
		{origin: "HTTPS://APP.EXAMPLE.COM"},
		{origin: "http://127.0.0.1:3000"},
		{origin: "http://[::1]:3000"},
		{origin: "*", wantErr: "wildcards are not allowed"},
		{origin: "", wantErr: "absolute http or https origin"},
		{origin: "null", wantErr: "absolute http or https origin"},
		{origin: "app.example.com", wantErr: "absolute http or https origin"},
		{origin: "ftp://app.example.com", wantErr: "absolute http or https origin"},
		{origin: "//app.example.com", wantErr: "absolute http or https origin"},
		{origin: "https://", wantErr: "must contain a host"},
		{origin: "https://:8080", wantErr: "must contain a host"},
		{origin: "https://user:pw@app.example.com", wantErr: "user information"},
		{origin: "https://app.example.com/path", wantErr: "without a path, query or fragment"},
		{origin: "https://app.example.com?x=1", wantErr: "without a path, query or fragment"},
		{origin: "https://app.example.com?", wantErr: "without a path, query or fragment"},
		{origin: "https://app.example.com#top", wantErr: "without a path, query or fragment"},
		{origin: "https://app.example.com#", wantErr: "without a path, query or fragment"},
		{origin: " https://app.example.com", wantErr: "whitespace or control"},
		{origin: "https://app.example.com\n", wantErr: "whitespace or control"},
		{origin: "https://app.example.com:port", wantErr: "not a valid URL"},
	}
	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			cfg := Config{TrustedOrigins: []string{tt.origin}}
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.HasPrefix(err.Error(), "authhttp: invalid TrustedOrigins entry") {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
			expectPanic(t, tt.wantErr, func() { New(newService(t), cfg) })
		})
	}
}

func TestTrustedOriginsAreNormalizedWithoutTouchingTheCaller(t *testing.T) {
	origins := []string{"HTTPS://App.Example.com/", "http://localhost:5173"}
	h := New(newService(t), Config{TrustedOrigins: origins})
	if origins[0] != "HTTPS://App.Example.com/" {
		t.Fatal("New modified the caller's slice")
	}
	want := []string{"https://app.example.com", "http://localhost:5173"}
	if len(h.cfg.TrustedOrigins) != len(want) {
		t.Fatalf("TrustedOrigins = %v, want %v", h.cfg.TrustedOrigins, want)
	}
	for i := range want {
		if h.cfg.TrustedOrigins[i] != want[i] {
			t.Fatalf("TrustedOrigins = %v, want %v", h.cfg.TrustedOrigins, want)
		}
		if !h.isTrustedOrigin(want[i]) {
			t.Fatalf("%s is not trusted", want[i])
		}
	}
	if h.isTrustedOrigin("") || h.isTrustedOrigin("https://evil.example") {
		t.Fatal("an unlisted origin is trusted")
	}
}
