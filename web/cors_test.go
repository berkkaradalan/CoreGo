package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// corsRequest sends method target with the given headers through a server
// that has cfg installed globally and one GET+POST route at /api/data.
func corsRequest(t *testing.T, cfg CORSConfig, method, target string, headers map[string]string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	s := New()
	s.Use(CORS(cfg))
	reached := false
	handler := func(c *Context) error {
		reached = true
		c.SetHeader("X-Request-Id", "abc")
		return c.String(http.StatusOK, "data")
	}
	s.Get("/api/data", handler)
	s.Post("/api/data", handler)
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec, reached
}

func TestCORSRequestsWithoutOriginUntouched(t *testing.T) {
	cfg := CORSConfig{AllowedOrigins: []string{"https://app.example.com"}, AllowCredentials: true, ExposedHeaders: []string{"X-Request-Id"}}
	for _, method := range []string{"GET", "POST", "OPTIONS"} {
		rec, _ := corsRequest(t, cfg, method, "/api/data", map[string]string{"Access-Control-Request-Method": "POST"})
		for name := range rec.Header() {
			if strings.HasPrefix(name, "Access-Control-") || name == "Vary" {
				t.Errorf("%s without Origin got header %s", method, name)
			}
		}
	}
	// OPTIONS without Origin is the router's automatic answer.
	rec, _ := corsRequest(t, cfg, "OPTIONS", "/api/data", nil)
	if rec.Code != 204 || rec.Header().Get("Allow") != "GET, OPTIONS, POST" {
		t.Errorf("plain OPTIONS = %d Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestCORSSimpleRequests(t *testing.T) {
	const app = "https://app.example.com"
	tests := []struct {
		name            string
		cfg             CORSConfig
		origin          string
		wantAllowOrigin string
		wantCredentials string
		wantExpose      string
	}{
		{name: "exact origin allowed", cfg: CORSConfig{AllowedOrigins: []string{app}}, origin: app, wantAllowOrigin: app},
		{name: "disallowed origin gets no cors headers", cfg: CORSConfig{AllowedOrigins: []string{app}}, origin: "https://evil.example.com"},
		{name: "configured origin normalized", cfg: CORSConfig{AllowedOrigins: []string{" HTTPS://App.Example.com/ "}}, origin: app, wantAllowOrigin: app},
		{name: "request origin echoed as sent", cfg: CORSConfig{AllowedOrigins: []string{app}}, origin: "https://APP.example.com", wantAllowOrigin: "https://APP.example.com"},
		{name: "scheme matters", cfg: CORSConfig{AllowedOrigins: []string{app}}, origin: "http://app.example.com"},
		{name: "port matters", cfg: CORSConfig{AllowedOrigins: []string{app}}, origin: "https://app.example.com:8443"},
		{name: "subdomain not implied", cfg: CORSConfig{AllowedOrigins: []string{app}}, origin: "https://x.app.example.com"},
		{name: "wildcard literal without credentials", cfg: CORSConfig{AllowedOrigins: []string{"*"}}, origin: "https://any.example", wantAllowOrigin: "*"},
		{name: "wildcard literal for explicitly listed origin too", cfg: CORSConfig{AllowedOrigins: []string{app, "*"}}, origin: app, wantAllowOrigin: "*"},
		{name: "origin func with credentials echoes origin", cfg: CORSConfig{AllowOriginFunc: func(o string) bool { return strings.HasSuffix(o, ".example") }, AllowCredentials: true}, origin: "https://any.example", wantAllowOrigin: "https://any.example", wantCredentials: "true"},
		{name: "wildcard does not allow null origin", cfg: CORSConfig{AllowedOrigins: []string{"*"}}, origin: "null"},
		{name: "explicit null origin allowed", cfg: CORSConfig{AllowedOrigins: []string{"null"}}, origin: "null", wantAllowOrigin: "null"},
		{name: "credentials with exact origin", cfg: CORSConfig{AllowedOrigins: []string{app}, AllowCredentials: true}, origin: app, wantAllowOrigin: app, wantCredentials: "true"},
		{name: "exposed headers", cfg: CORSConfig{AllowedOrigins: []string{app}, ExposedHeaders: []string{"X-Request-Id", " ", "X-Total"}}, origin: app, wantAllowOrigin: app, wantExpose: "X-Request-Id, X-Total"},
		{name: "exposed headers not sent to disallowed origin", cfg: CORSConfig{AllowedOrigins: []string{app}, ExposedHeaders: []string{"X-Request-Id"}}, origin: "https://evil.example"},
		{name: "origin func allows", cfg: CORSConfig{AllowOriginFunc: func(o string) bool { return strings.HasSuffix(o, ".preview.example") }}, origin: "https://pr-1.preview.example", wantAllowOrigin: "https://pr-1.preview.example"},
		{name: "origin func rejects", cfg: CORSConfig{AllowOriginFunc: func(o string) bool { return false }}, origin: app},
		{name: "list match skips origin func", cfg: CORSConfig{AllowedOrigins: []string{app}, AllowOriginFunc: func(string) bool { panic("must not be called") }}, origin: app, wantAllowOrigin: app},
		{name: "empty config allows nothing", cfg: CORSConfig{}, origin: app},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, reached := corsRequest(t, tt.cfg, "GET", "/api/data", map[string]string{"Origin": tt.origin})
			if !reached || rec.Code != 200 || rec.Body.String() != "data" {
				t.Fatalf("simple request did not pass through: reached=%v code=%d", reached, rec.Code)
			}
			h := rec.Header()
			if got := h.Get("Access-Control-Allow-Origin"); got != tt.wantAllowOrigin {
				t.Errorf("Allow-Origin = %q, want %q", got, tt.wantAllowOrigin)
			}
			if got := h.Get("Access-Control-Allow-Credentials"); got != tt.wantCredentials {
				t.Errorf("Allow-Credentials = %q, want %q", got, tt.wantCredentials)
			}
			if got := h.Get("Access-Control-Expose-Headers"); got != tt.wantExpose {
				t.Errorf("Expose-Headers = %q, want %q", got, tt.wantExpose)
			}
			if got := h.Values("Vary"); len(got) != 1 || got[0] != "Origin" {
				t.Errorf("Vary = %v, want [Origin]", got)
			}
			if h.Get("Access-Control-Allow-Methods") != "" || h.Get("Access-Control-Max-Age") != "" {
				t.Error("preflight-only headers on a simple request")
			}
			if tt.wantCredentials == "" && h.Get("Access-Control-Allow-Origin") == "*" && h.Get("Access-Control-Allow-Credentials") != "" {
				t.Error("literal * combined with credentials")
			}
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	const app = "https://app.example.com"
	preflight := func(origin string, extra map[string]string) map[string]string {
		h := map[string]string{"Origin": origin, "Access-Control-Request-Method": "POST"}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}
	tests := []struct {
		name        string
		cfg         CORSConfig
		target      string
		headers     map[string]string
		wantCode    int
		wantOrigin  string
		wantMethods string
		wantHeaders string
		wantMaxAge  string
		wantCreds   string
		wantVary    []string
	}{
		{
			name: "allowed with defaults", cfg: CORSConfig{AllowedOrigins: []string{app}}, target: "/api/data",
			headers: preflight(app, nil), wantCode: 204, wantOrigin: app,
			wantMethods: "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS", wantHeaders: "Content-Type, Authorization",
			wantMaxAge: "600", wantVary: []string{"Origin"},
		},
		{
			name: "custom lists normalized", target: "/api/data", headers: preflight(app, nil), wantCode: 204, wantOrigin: app,
			cfg:         CORSConfig{AllowedOrigins: []string{app}, AllowedMethods: []string{"get", " post ", ""}, AllowedHeaders: []string{"X-Api-Key", ""}, MaxAge: 90 * time.Second},
			wantMethods: "GET, POST", wantHeaders: "X-Api-Key", wantMaxAge: "90", wantVary: []string{"Origin"},
		},
		{
			name: "credentials echo the listed origin", target: "/api/data", headers: preflight("https://x.example", nil), wantCode: 204,
			cfg:        CORSConfig{AllowedOrigins: []string{"https://x.example"}, AllowCredentials: true},
			wantOrigin: "https://x.example", wantCreds: "true", wantMethods: "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS",
			wantHeaders: "Content-Type, Authorization", wantMaxAge: "600", wantVary: []string{"Origin"},
		},
		{
			name: "wildcard headers echo the request", target: "/api/data", wantCode: 204, wantOrigin: "*",
			cfg:         CORSConfig{AllowedOrigins: []string{"*"}, AllowedHeaders: []string{"*"}},
			headers:     preflight(app, map[string]string{"Access-Control-Request-Headers": "x-one, x-two"}),
			wantMethods: "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS", wantHeaders: "x-one, x-two", wantMaxAge: "600",
			wantVary: []string{"Origin", "Access-Control-Request-Headers"},
		},
		{
			name: "negative max age omits header", target: "/api/data", headers: preflight(app, nil), wantCode: 204, wantOrigin: app,
			cfg:         CORSConfig{AllowedOrigins: []string{app}, MaxAge: -1},
			wantMethods: "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS", wantHeaders: "Content-Type, Authorization", wantVary: []string{"Origin"},
		},
		{
			name: "preflight for unknown path still answered", cfg: CORSConfig{AllowedOrigins: []string{app}}, target: "/no/such/route",
			headers: preflight(app, nil), wantCode: 204, wantOrigin: app,
			wantMethods: "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS", wantHeaders: "Content-Type, Authorization",
			wantMaxAge: "600", wantVary: []string{"Origin"},
		},
		{
			name: "disallowed origin 403", cfg: CORSConfig{AllowedOrigins: []string{app}}, target: "/api/data",
			headers: preflight("https://evil.example", nil), wantCode: 403, wantVary: []string{"Origin"},
		},
		{
			name: "origin func rejection 403", cfg: CORSConfig{AllowOriginFunc: func(string) bool { return false }}, target: "/api/data",
			headers: preflight(app, nil), wantCode: 403, wantVary: []string{"Origin"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, reached := corsRequest(t, tt.cfg, http.MethodOptions, tt.target, tt.headers)
			if reached {
				t.Error("preflight reached the route handler; it must short-circuit")
			}
			if rec.Code != tt.wantCode {
				t.Fatalf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			h := rec.Header()
			checks := map[string]string{
				"Access-Control-Allow-Origin":      tt.wantOrigin,
				"Access-Control-Allow-Methods":     tt.wantMethods,
				"Access-Control-Allow-Headers":     tt.wantHeaders,
				"Access-Control-Max-Age":           tt.wantMaxAge,
				"Access-Control-Allow-Credentials": tt.wantCreds,
				"Access-Control-Expose-Headers":    "",
				"Allow":                            "",
			}
			for name, want := range checks {
				if got := h.Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if got := h.Values("Vary"); strings.Join(got, "|") != strings.Join(tt.wantVary, "|") {
				t.Errorf("Vary = %v, want %v", got, tt.wantVary)
			}
			if tt.wantCode == 204 && rec.Body.Len() != 0 {
				t.Errorf("preflight body = %q, want empty", rec.Body.String())
			}
			if tt.wantCode == 403 && rec.Body.String() != `{"error":"Forbidden"}` {
				t.Errorf("403 body = %q", rec.Body.String())
			}
		})
	}
}

func TestCORSOptionsWithoutRequestMethodIsNotPreflight(t *testing.T) {
	const app = "https://app.example.com"
	rec, _ := corsRequest(t, CORSConfig{AllowedOrigins: []string{app}}, http.MethodOptions, "/api/data", map[string]string{"Origin": app})
	if rec.Code != 204 || rec.Header().Get("Allow") != "GET, OPTIONS, POST" {
		t.Errorf("code/Allow = %d/%q, want the router's automatic OPTIONS answer", rec.Code, rec.Header().Get("Allow"))
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != app {
		t.Error("allowed origin missing on a non-preflight OPTIONS")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Error("preflight headers on a non-preflight OPTIONS")
	}
}

func TestCORSHeadersSurviveErrorResponses(t *testing.T) {
	const app = "https://app.example.com"
	s := New()
	s.Use(CORS(CORSConfig{AllowedOrigins: []string{app}, AllowCredentials: true}))
	s.Get("/fail", func(c *Context) error { return Unauthorized("") })
	for _, target := range []string{"/fail", "/missing"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Origin", app)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Header().Get("Access-Control-Allow-Origin") != app || rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("GET %s (%d) lost CORS headers: %v", target, rec.Code, rec.Header())
		}
	}
}

func TestAddVary(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		token    string
		want     []string
	}{
		{name: "empty", token: "Origin", want: []string{"Origin"}},
		{name: "already present", existing: []string{"Origin"}, token: "Origin", want: []string{"Origin"}},
		{name: "present case-insensitively in a list", existing: []string{"Accept-Encoding, origin"}, token: "Origin", want: []string{"Accept-Encoding, origin"}},
		{name: "appended", existing: []string{"Accept-Encoding"}, token: "Origin", want: []string{"Accept-Encoding", "Origin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for _, v := range tt.existing {
				h.Add("Vary", v)
			}
			addVary(h, tt.token)
			if got := h.Values("Vary"); strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("Vary = %v, want %v", got, tt.want)
			}
		})
	}
}

// Regression: AllowCredentials with the "*" origin used to echo every
// origin together with Access-Control-Allow-Credentials: true, letting
// any website make authenticated requests with the visitor's cookies.
func TestCORSCredentialsWithWildcardPanics(t *testing.T) {
	const want = `web: CORS: AllowCredentials cannot be combined with the "*" origin; list the allowed origins or use AllowOriginFunc`
	tests := []struct {
		name    string
		origins []string
	}{
		{name: "only wildcard", origins: []string{"*"}},
		{name: "wildcard with spaces", origins: []string{" * "}},
		{name: "wildcard among explicit origins", origins: []string{"https://app.example.com", "*"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r != want {
					t.Fatalf("panic = %v, want %q", r, want)
				}
			}()
			CORS(CORSConfig{AllowedOrigins: tt.origins, AllowCredentials: true, AllowOriginFunc: func(string) bool { return false }})
		})
	}

	// The same origins without credentials, and credentials without "*",
	// are valid.
	for _, cfg := range []CORSConfig{
		{AllowedOrigins: []string{"*"}},
		{AllowedOrigins: []string{"https://app.example.com"}, AllowCredentials: true},
		{AllowOriginFunc: func(string) bool { return true }, AllowCredentials: true},
		{AllowedOrigins: []string{"", " "}, AllowCredentials: true},
	} {
		if CORS(cfg) == nil {
			t.Errorf("CORS(%+v) = nil", cfg)
		}
	}
}
