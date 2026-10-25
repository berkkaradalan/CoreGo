package corego

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

func TestServerIsBuiltOnce(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, &Config{Auth: &AuthOptions{}})

	first := core.Server()
	if first == nil {
		t.Fatal("Server returned nil")
	}
	for range 3 {
		if core.Server() != first {
			t.Fatal("Server returned a different instance")
		}
	}

	// Concurrent first calls agree as well.
	other := newCore(t, &Config{Auth: &AuthOptions{}})
	got := make(chan *web.Server, 8)
	for range 8 {
		go func() { got <- other.Server() }()
	}
	want := <-got
	for range 7 {
		if s := <-got; s != want {
			t.Fatal("concurrent Server calls returned different instances")
		}
	}
}

func TestServerMountsAuthRoutes(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, &Config{Auth: &AuthOptions{}})

	routes := core.Server().Routes()
	for _, want := range []web.Route{
		{Method: http.MethodPost, Path: "/auth/signup"},
		{Method: http.MethodPost, Path: "/auth/signin"},
		{Method: http.MethodGet, Path: "/auth/me"},
		{Method: http.MethodGet, Path: "/auth/verify-email"},
		{Method: http.MethodDelete, Path: "/auth/me"},
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("route %s %s not mounted", want.Method, want.Path)
		}
	}
	if len(routes) != 17 {
		t.Errorf("mounted %d routes, want the 17 auth routes", len(routes))
	}
}

func TestServerWithoutAuthHasNoRoutes(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, nil)
	if routes := core.Server().Routes(); len(routes) != 0 {
		t.Fatalf("routes = %v, want none", routes)
	}
	w := do(t, core.Server(), request{method: http.MethodPost, path: "/auth/signup"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("POST /auth/signup without auth = %d, want 404", w.Code)
	}
}

// TestHelloWorld is the package documentation example, served through
// httptest: sign up over the mounted routes, then call a route protected
// by core.RequireAuth that reads the user with auth.MustUser.
func TestHelloWorld(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, &Config{Auth: &AuthOptions{}})

	app := core.Server()
	api := app.Group("/api", core.RequireAuth())
	api.Get("/hello", func(c *web.Context) error {
		user := auth.MustUser(c)
		return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
	})

	w := do(t, app, request{method: http.MethodGet, path: "/api/hello"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("without a token: status = %d, want 401", w.Code)
	}

	token := signUp(t, app, "ada@example.com")
	w = do(t, app, request{method: http.MethodGet, path: "/api/hello", token: token})
	if w.Code != http.StatusOK {
		t.Fatalf("with a token: status = %d, body %s", w.Code, w.Body.String())
	}
	var out map[string]string
	decode(t, w, &out)
	if out["hello"] != "Ada Lovelace" {
		t.Fatalf("body = %v", out)
	}

	// RequireAuth shares the mounted routes' session check: signing out
	// through /auth/signout invalidates the token for /api too.
	if w := do(t, app, request{method: http.MethodPost, path: "/auth/signout", token: token}); w.Code != http.StatusNoContent {
		t.Fatalf("signout status = %d", w.Code)
	}
	if w := do(t, app, request{method: http.MethodGet, path: "/api/hello", token: token}); w.Code != http.StatusUnauthorized {
		t.Fatalf("after signout: status = %d, want 401", w.Code)
	}
}

func TestAuthMiddlewareWithoutAuthPanics(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, nil)
	var nilCore *Core

	middleware := map[string]func(*Core) web.MiddlewareFunc{
		"RequireAuth":  (*Core).RequireAuth,
		"OptionalAuth": (*Core).OptionalAuth,
	}
	for mwName, get := range middleware {
		for name, c := range map[string]*Core{"core without auth": core, "nil core": nilCore} {
			t.Run(mwName+"/"+name, func(t *testing.T) {
				defer func() {
					r := recover()
					msg, _ := r.(string)
					if !strings.Contains(msg, "corego: "+mwName+" needs the auth service") {
						t.Fatalf("panic = %v, want the %s message", r, mwName)
					}
				}()
				get(c)
			})
		}
	}
}

func TestOptionalAuth(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, &Config{Auth: &AuthOptions{}})
	app := core.Server()
	app.Get("/greeting", func(c *web.Context) error {
		if user, ok := auth.UserFromContext(c); ok {
			return c.String(http.StatusOK, "hello "+user.Name)
		}
		return c.String(http.StatusOK, "hello guest")
	}, core.OptionalAuth())

	token := signUp(t, app, "ada@example.com")
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{name: "signed in", token: token, want: "hello Ada Lovelace"},
		{name: "anonymous", want: "hello guest"},
		{name: "unknown token", token: "not-a-session", want: "hello guest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := do(t, app, request{method: http.MethodGet, path: "/greeting", token: tt.token})
			if w.Code != http.StatusOK || w.Body.String() != tt.want {
				t.Fatalf("got %d %q, want 200 %q", w.Code, w.Body.String(), tt.want)
			}
		})
	}
}

func TestAuthHandlersSharedWithServer(t *testing.T) {
	isolateEnv(t)
	core := newCore(t, &Config{Auth: &AuthOptions{}})
	h := core.AuthHandlers()
	if h == nil {
		t.Fatal("AuthHandlers = nil with auth configured")
	}
	if core.AuthHandlers() != h {
		t.Fatal("AuthHandlers returned a different instance")
	}
	var nilCore *Core
	if nilCore.AuthHandlers() != nil {
		t.Fatal("nil Core AuthHandlers must be nil")
	}

	// The net/http middleware accepts a token issued by the mounted routes.
	token := signUp(t, core.Server(), "ada@example.com")
	protected := h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, auth.MustUser(r.Context()).Email)
	}))
	w := do(t, protected, request{method: http.MethodGet, path: "/plain", token: token})
	if w.Code != http.StatusOK || w.Body.String() != "ada@example.com" {
		t.Fatalf("net/http middleware: status = %d, body %q", w.Code, w.Body.String())
	}
}

func TestServerCORS(t *testing.T) {
	const allowed = "https://app.example.com"
	preflight := func(origin string) request {
		return request{method: http.MethodOptions, path: "/auth/signin", headers: map[string]string{
			"Origin":                         origin,
			"Access-Control-Request-Method":  http.MethodPost,
			"Access-Control-Request-Headers": "Content-Type, Authorization",
		}}
	}
	tests := []struct {
		name            string
		env             string
		cors            *web.CORSConfig
		origin          string
		wantStatus      int
		wantAllowOrigin string
		wantCredentials bool
	}{
		{name: "off without configuration", origin: allowed, wantStatus: http.StatusNoContent},
		{name: "allowed origin from the environment", env: "https://app.example.com, http://localhost:5173", origin: allowed,
			wantStatus: http.StatusNoContent, wantAllowOrigin: allowed, wantCredentials: true},
		{name: "second origin from the environment", env: "https://app.example.com, http://localhost:5173", origin: "http://localhost:5173",
			wantStatus: http.StatusNoContent, wantAllowOrigin: "http://localhost:5173", wantCredentials: true},
		{name: "disallowed origin is rejected", env: allowed, origin: "https://evil.example.com", wantStatus: http.StatusForbidden},
		{name: "explicit configuration wins over the environment", env: "https://env.example.com",
			cors: &web.CORSConfig{AllowedOrigins: []string{allowed}}, origin: allowed,
			wantStatus: http.StatusNoContent, wantAllowOrigin: allowed},
		{name: "environment origin ignored when configured in code", env: "https://env.example.com",
			cors: &web.CORSConfig{AllowedOrigins: []string{allowed}}, origin: "https://env.example.com",
			wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", tt.env)
			logger, logs := newTestLogger()
			core := newCore(t, &Config{
				Auth:   &AuthOptions{},
				Server: ServerOptions{CORS: tt.cors},
				Logger: logger,
			})

			w := do(t, core.Server(), preflight(tt.origin))
			if w.Code != tt.wantStatus {
				t.Fatalf("preflight status = %d, want %d", w.Code, tt.wantStatus)
			}
			h := w.Header()
			if got := h.Get("Access-Control-Allow-Origin"); got != tt.wantAllowOrigin {
				t.Errorf("Allow-Origin = %q, want %q", got, tt.wantAllowOrigin)
			}
			if got := h.Get("Access-Control-Allow-Credentials") == "true"; got != tt.wantCredentials {
				t.Errorf("Allow-Credentials = %v, want %v", got, tt.wantCredentials)
			}
			if tt.wantAllowOrigin != "" && tt.env != "" && tt.cors == nil {
				allowHeaders := h.Get("Access-Control-Allow-Headers")
				if missing := missingSubstrings(allowHeaders, "Content-Type", "Authorization"); len(missing) > 0 {
					t.Errorf("Allow-Headers = %q, missing %v", allowHeaders, missing)
				}
			}
			// The request logger is outermost, so preflights are logged
			// with the status CORS answered.
			wantLog := fmt.Sprintf("method=OPTIONS path=/auth/signin status=%d", tt.wantStatus)
			if !strings.Contains(logs.String(), wantLog) {
				t.Errorf("preflight not logged as %q; logs:\n%s", wantLog, logs)
			}
		})
	}
}

func TestServerCORSSimpleRequest(t *testing.T) {
	isolateEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	core := newCore(t, &Config{Auth: &AuthOptions{}})

	w := do(t, core.Server(), request{
		method: http.MethodPost, path: "/auth/signup",
		body:    map[string]any{"name": "Ada", "email": "ada@example.com", "password": "analytical-engine"},
		headers: map[string]string{"Origin": "https://app.example.com"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q", got)
	}
}

func TestServerTrustProxyHeadersInvalidEnv(t *testing.T) {
	for _, value := range []string{"yes", "on", "2", "ture"} {
		t.Run(value, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("TRUST_PROXY_HEADERS", value)
			core, err := New(nil)
			if err == nil {
				core.Close()
				t.Fatal("New succeeded, want an error for an invalid TRUST_PROXY_HEADERS")
			}
			if !strings.Contains(err.Error(), "TRUST_PROXY_HEADERS") {
				t.Fatalf("error %q does not name the variable", err)
			}
			// An explicit option wins and the variable is not parsed.
			yes := true
			core = newCore(t, &Config{Server: ServerOptions{TrustProxyHeaders: &yes}})
			if !core.Server().TrustProxyHeaders {
				t.Fatal("TrustProxyHeaders option was not applied")
			}
		})
	}
}

func TestServerTrustProxyHeaders(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name   string
		env    string
		option *bool
		want   bool
	}{
		{name: "default off", want: false},
		{name: "environment true", env: "true", want: true},
		{name: "environment 1", env: "1", want: true},
		{name: "option true", option: &yes, want: true},
		{name: "option false wins over the environment", env: "true", option: &no, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("TRUST_PROXY_HEADERS", tt.env)
			core := newCore(t, &Config{Server: ServerOptions{TrustProxyHeaders: tt.option}})
			app := core.Server()
			if app.TrustProxyHeaders != tt.want {
				t.Fatalf("TrustProxyHeaders = %v, want %v", app.TrustProxyHeaders, tt.want)
			}

			app.Get("/ip", func(c *web.Context) error { return c.String(http.StatusOK, c.RemoteIP()) })
			w := do(t, app, request{method: http.MethodGet, path: "/ip", headers: map[string]string{"X-Forwarded-For": "203.0.113.7"}})
			wantIP := "192.0.2.1" // httptest's RemoteAddr
			if tt.want {
				wantIP = "203.0.113.7"
			}
			if w.Body.String() != wantIP {
				t.Errorf("RemoteIP = %q, want %q", w.Body.String(), wantIP)
			}
		})
	}
}

func TestServerRequestLog(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("DisableRequestLog=%v", disabled), func(t *testing.T) {
			isolateEnv(t)
			logger, logs := newTestLogger()
			core := newCore(t, &Config{Server: ServerOptions{DisableRequestLog: disabled}, Logger: logger})
			app := core.Server()
			if app.Logger != logger {
				t.Error("web.Server.Logger is not Config.Logger")
			}

			do(t, app, request{method: http.MethodGet, path: "/missing"})
			logged := strings.Contains(logs.String(), "path=/missing status=404")
			if logged == disabled {
				t.Errorf("request logged = %v with DisableRequestLog = %v; logs:\n%s", logged, disabled, logs)
			}
		})
	}
}

func TestRunServesUntilShutdownThenCloses(t *testing.T) {
	isolateEnv(t)
	port := freePort(t)
	t.Setenv("PORT", port)
	logger, logs := newTestLogger()

	core, err := New(&Config{Auth: &AuthOptions{}, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	core.Server().Get("/health", func(c *web.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	done := make(chan error, 1)
	go func() { done <- core.Run() }()

	url := "http://127.0.0.1:" + port + "/health"
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /health = %d", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never answered: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := core.Server().Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil after a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Shutdown")
	}
	if !strings.Contains(logs.String(), "corego: starting server") || !strings.Contains(logs.String(), "addr=:"+port) {
		t.Errorf("startup not logged; logs:\n%s", logs)
	}
}

func TestRunReturnsListenError(t *testing.T) {
	isolateEnv(t)
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	t.Setenv("PORT", port)

	core, err := New(&Config{Auth: &AuthOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	err = core.Run()
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("Run = %v, want the listen error", err)
	}
	// Run already closed the Core; closing again is a no-op.
	if err := core.Close(); err != nil {
		t.Fatalf("Close after Run: %v", err)
	}
}

// freePort returns a TCP port that was free a moment ago.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}
