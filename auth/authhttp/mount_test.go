package authhttp

import (
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/berkkaradalan/CoreGo/web"
)

// expectedRoutes is the documented route table, relative to BasePath.
var expectedRoutes = []web.Route{
	{Method: http.MethodPost, Path: "/signup"},
	{Method: http.MethodPost, Path: "/signin"},
	{Method: http.MethodPost, Path: "/signout"},
	{Method: http.MethodGet, Path: "/session"},
	{Method: http.MethodGet, Path: "/sessions"},
	{Method: http.MethodDelete, Path: "/sessions"},
	{Method: http.MethodDelete, Path: "/sessions/:id"},
	{Method: http.MethodPost, Path: "/verify-email/request"},
	{Method: http.MethodPost, Path: "/verify-email"},
	{Method: http.MethodGet, Path: "/verify-email"},
	{Method: http.MethodPost, Path: "/forgot-password"},
	{Method: http.MethodPost, Path: "/reset-password"},
	{Method: http.MethodPost, Path: "/change-password"},
	{Method: http.MethodPost, Path: "/change-email"},
	{Method: http.MethodGet, Path: "/me"},
	{Method: http.MethodPatch, Path: "/me"},
	{Method: http.MethodDelete, Path: "/me"},
}

// routesUnder returns expectedRoutes below prefix, sorted like
// web.Server.Routes.
func routesUnder(prefix string) []web.Route {
	out := make([]web.Route, 0, len(expectedRoutes))
	for _, r := range expectedRoutes {
		out = append(out, web.Route{Method: r.Method, Path: prefix + r.Path})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func TestMountRegistersEveryRoute(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		group    string
		want     string
	}{
		{name: "default", want: "/auth"},
		{name: "root", basePath: "/", want: ""},
		{name: "custom", basePath: "/accounts/", want: "/accounts"},
		{name: "nested custom", basePath: "/api/v1/auth", want: "/api/v1/auth"},
		{name: "group prefix", group: "/api/v1", want: "/api/v1/auth"},
		{name: "group prefix with root base", group: "/api", basePath: "/", want: "/api"},
		{name: "dynamic group prefix", group: "/tenants/:tenant", want: "/tenants/:tenant/auth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(newService(t), Config{BasePath: tt.basePath})
			if tt.group == "" && h.BasePath() != tt.want {
				t.Errorf("BasePath() = %q, want %q", h.BasePath(), tt.want)
			}
			s := web.New()
			if tt.group != "" {
				h.Mount(s.Group(tt.group))
			} else {
				h.Mount(s)
			}
			if got, want := s.Routes(), routesUnder(tt.want); !reflect.DeepEqual(got, want) {
				t.Fatalf("routes:\n got %v\nwant %v", got, want)
			}
		})
	}
	if len(expectedRoutes) != 17 {
		t.Fatalf("the documented table has 17 routes, the test lists %d", len(expectedRoutes))
	}
}

func TestMountServesRequestsUnderPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		group    string
		path     string
	}{
		{name: "default", path: "/auth/signup"},
		{name: "root", basePath: "/", path: "/signup"},
		{name: "custom trailing slash", basePath: "/accounts/", path: "/accounts/signup"},
		{name: "group", group: "/api/v1", path: "/api/v1/auth/signup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, appOptions{httpCfg: Config{BasePath: tt.basePath}})
			if tt.group != "" {
				// Remount on a fresh server below the group.
				app.server = web.New()
				app.handlers.Mount(app.server.Group(tt.group))
			}
			rec := app.do(t, req{method: http.MethodPost, path: tt.path, absolute: true, body: map[string]string{
				"name": "Ada", "email": "ada@example.com", "password": testPassword,
			}})
			expectStatus(t, rec, http.StatusCreated)

			var out signUpResult
			decode(t, rec, &out)
			me := app.do(t, req{method: http.MethodGet, path: tt.path[:len(tt.path)-len("/signup")] + "/me", absolute: true, token: out.Token})
			expectStatus(t, me, http.StatusOK)
		})
	}
}

func TestMountGroupMiddlewareRunsOutsideAuth(t *testing.T) {
	h := New(newService(t), Config{})
	s := web.New()
	var seen []string
	g := s.Group("/api", func(next web.HandlerFunc) web.HandlerFunc {
		return func(c *web.Context) error {
			seen = append(seen, c.Path())
			return next(c)
		}
	})
	h.Mount(g)

	app := &testApp{server: s, base: "/api/auth"}
	rec := app.do(t, req{method: http.MethodGet, path: "/me"})
	expectError(t, rec, http.StatusUnauthorized, msgMissingToken)
	if len(seen) != 1 || seen[0] != "/api/auth/me" {
		t.Fatalf("group middleware saw %v, want one call for /api/auth/me", seen)
	}
}

func TestMountPanicsOnConflicts(t *testing.T) {
	t.Run("mounted twice", func(t *testing.T) {
		h := New(newService(t), Config{})
		s := web.New()
		h.Mount(s)
		expectPanic(t, "web: route POST /auth/signup is already registered", func() { h.Mount(s) })
	})
	t.Run("application route conflicts", func(t *testing.T) {
		h := New(newService(t), Config{})
		s := web.New()
		s.Get("/auth/me", func(c *web.Context) error { return nil })
		expectPanic(t, "web: route GET /auth/me is already registered", func() { h.Mount(s) })
	})
	t.Run("conflicting parameter name", func(t *testing.T) {
		h := New(newService(t), Config{})
		s := web.New()
		s.Delete("/auth/sessions/:sessionID", func(c *web.Context) error { return nil })
		expectPanic(t, "web: ", func() { h.Mount(s) })
	})
	t.Run("two handlers on distinct base paths coexist", func(t *testing.T) {
		service := newService(t)
		s := web.New()
		New(service, Config{}).Mount(s)
		New(service, Config{BasePath: "/v2/auth"}).Mount(s)
		if got := len(s.Routes()); got != 2*len(expectedRoutes) {
			t.Fatalf("got %d routes, want %d", got, 2*len(expectedRoutes))
		}
	})
}

func TestMountPanicsOnNilRouter(t *testing.T) {
	h := New(newService(t), Config{})
	expectPanic(t, "authhttp: Mount requires a non-nil Router", func() { h.Mount(nil) })
}

func TestEveryRouteSetsCacheControlNoStore(t *testing.T) {
	app := newTestApp(t, appOptions{})
	for _, r := range expectedRoutes {
		t.Run(r.Method+" "+r.Path, func(t *testing.T) {
			path := r.Path
			if path == "/sessions/:id" {
				path = "/sessions/some-id"
			}
			// Even failing requests (no body, no token) must not be
			// cached.
			rec := app.do(t, req{method: r.Method, path: path})
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store (status %d)", got, rec.Code)
			}
		})
	}
}

func TestHandlersRoutes(t *testing.T) {
	// Routes that need a session token, relative to BasePath.
	protected := map[web.Route]bool{
		{Method: http.MethodPost, Path: "/signout"}:         true,
		{Method: http.MethodGet, Path: "/session"}:          true,
		{Method: http.MethodGet, Path: "/sessions"}:         true,
		{Method: http.MethodDelete, Path: "/sessions"}:      true,
		{Method: http.MethodDelete, Path: "/sessions/:id"}:  true,
		{Method: http.MethodPost, Path: "/change-password"}: true,
		{Method: http.MethodPost, Path: "/change-email"}:    true,
		{Method: http.MethodGet, Path: "/me"}:               true,
		{Method: http.MethodPatch, Path: "/me"}:             true,
		{Method: http.MethodDelete, Path: "/me"}:            true,
	}
	tests := []struct {
		basePath string
		prefix   string
	}{
		{basePath: "", prefix: "/auth"},
		{basePath: "/accounts/", prefix: "/accounts"},
		{basePath: "/", prefix: ""},
	}
	for _, tt := range tests {
		t.Run(tt.basePath, func(t *testing.T) {
			h := New(newService(t), Config{BasePath: tt.basePath})
			got := h.Routes()
			if len(got) != len(expectedRoutes) {
				t.Fatalf("Routes returned %d routes, want %d", len(got), len(expectedRoutes))
			}
			// Registration order, sign-up first, with full paths.
			for i, want := range expectedRoutes {
				r := got[i]
				if r.Method != want.Method || r.Path != tt.prefix+want.Path {
					t.Fatalf("route %d = %s %s, want %s %s", i, r.Method, r.Path, want.Method, tt.prefix+want.Path)
				}
				if r.RequiresSession != protected[want] {
					t.Errorf("%s %s RequiresSession = %v, want %v", r.Method, r.Path, r.RequiresSession, protected[want])
				}
			}

			// Routes describes exactly what Mount registers.
			s := web.New()
			h.Mount(s)
			mounted := map[web.Route]bool{}
			for _, r := range s.Routes() {
				mounted[r] = true
			}
			for _, r := range got {
				if !mounted[web.Route{Method: r.Method, Path: r.Path}] {
					t.Errorf("%s %s is listed but not mounted", r.Method, r.Path)
				}
			}
		})
	}
}
