package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestGroupPrefixJoining(t *testing.T) {
	s := New()
	api := s.Group("/api")
	api.Get("/users", text("api users"))
	v1 := api.Group("/v1")
	v1.Get("/posts", text("v1 posts"))
	// A route path of "/" registers the prefix itself.
	api.Get("/", text("api root"))
	orgs := s.Group("/orgs/:org")
	orgs.Get("/repos/:repo", func(c *Context) error {
		return c.String(200, c.Param("org")+"/"+c.Param("repo"))
	})
	tests := []struct {
		target string
		want   string
	}{
		{target: "/api/users", want: "api users"},
		{target: "/api/users/", want: "api users"}, // trailing slash normalized
		{target: "/api/v1/posts", want: "v1 posts"},
		{target: "/api", want: "api root"},
		{target: "/api/", want: "api root"},
		{target: "/orgs/acme/repos/web", want: "acme/web"},
	}
	for _, tt := range tests {
		rec := serveRequest(t, s, http.MethodGet, tt.target, nil)
		if rec.Code != 200 || rec.Body.String() != tt.want {
			t.Errorf("GET %s = %d %q, want 200 %q", tt.target, rec.Code, rec.Body.String(), tt.want)
		}
	}
}

func TestGroupPrefixNormalization(t *testing.T) {
	s := New()
	tests := []struct {
		prefix string
		want   string
	}{
		{prefix: "/api/", want: "/api"},
		{prefix: "/api", want: "/api"},
		{prefix: "/", want: ""},
		{prefix: "", want: ""},
		{prefix: "/a/:id/", want: "/a/:id"},
	}
	for _, tt := range tests {
		if got := s.Group(tt.prefix).prefix; got != tt.want {
			t.Errorf("Group(%q).prefix = %q, want %q", tt.prefix, got, tt.want)
		}
	}
	if got := s.Group("/a/").Group("/b/").prefix; got != "/a/b" {
		t.Errorf("nested prefix = %q, want /a/b", got)
	}
}

func TestGroupMiddlewareShortCircuit(t *testing.T) {
	s := New()
	handlerReached := false
	api := s.Group("/api", func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			// Stop the chain: write a response, do not call next.
			return c.String(http.StatusForbidden, "denied by group")
		}
	})
	api.Get("/open", func(c *Context) error {
		handlerReached = true
		return nil
	})
	rec := serveRequest(t, s, http.MethodGet, "/api/open", nil)
	if rec.Code != http.StatusForbidden || rec.Body.String() != "denied by group" {
		t.Errorf("code/body = %d/%q", rec.Code, rec.Body.String())
	}
	if handlerReached {
		t.Error("handler ran despite short-circuit")
	}
}

func TestGroupMiddlewareDoesNotLeak(t *testing.T) {
	s := New()
	var trace []string
	api := s.Group("/api", record(&trace, "api"))
	api.Get("/x", text("x"))
	s.Get("/outside", text("o"))
	other := s.Group("/other")
	other.Get("/y", text("y"))

	for _, target := range []string{"/outside", "/other/y", "/missing"} {
		trace = nil
		serveRequest(t, s, http.MethodGet, target, nil)
		if len(trace) != 0 {
			t.Errorf("GET %s ran group middleware: %v", target, trace)
		}
	}
	trace = nil
	serveRequest(t, s, http.MethodGet, "/api/x", nil)
	if strings.Join(trace, ",") != "api" {
		t.Errorf("trace = %v, want [api]", trace)
	}
}

func TestGroupConvenienceMethods(t *testing.T) {
	s := New()
	var trace []string
	g := s.Group("/g")
	registrations := []struct {
		register func(path string, h HandlerFunc, mw ...MiddlewareFunc)
		method   string
	}{
		{g.Get, http.MethodGet},
		{g.Post, http.MethodPost},
		{g.Put, http.MethodPut},
		{g.Patch, http.MethodPatch},
		{g.Delete, http.MethodDelete},
		{g.Head, http.MethodHead},
		{g.Options, http.MethodOptions},
	}
	for _, r := range registrations {
		method := r.method
		r.register("/x", func(c *Context) error {
			c.SetHeader("X-Method", method)
			return c.String(200, method)
		}, record(&trace, "route-"+method))
		trace = nil
		rec := serveRequest(t, s, method, "/g/x", nil)
		if rec.Code != 200 || rec.Header().Get("X-Method") != method {
			t.Errorf("%s code/X-Method = %d/%q", method, rec.Code, rec.Header().Get("X-Method"))
		}
		if strings.Join(trace, ",") != "route-"+method {
			t.Errorf("%s per-route middleware trace = %v", method, trace)
		}
	}
	g.Handle("PURGE", "/x", text("purged"))
	if rec := serveRequest(t, s, "PURGE", "/g/x", nil); rec.Body.String() != "purged" {
		t.Errorf("PURGE body = %q", rec.Body.String())
	}
}

func TestGroupSamePathInDifferentGroups(t *testing.T) {
	s := New()
	s.Group("/a").Get("/shared", text("from a"))
	s.Group("/b").Get("/shared", text("from b"))
	if rec := serveRequest(t, s, http.MethodGet, "/a/shared", nil); rec.Body.String() != "from a" {
		t.Errorf("body = %q, want from a", rec.Body.String())
	}
	if rec := serveRequest(t, s, http.MethodGet, "/b/shared", nil); rec.Body.String() != "from b" {
		t.Errorf("body = %q, want from b", rec.Body.String())
	}
}

func TestGroupSharesPrefixWithoutSharingMiddleware(t *testing.T) {
	// Two groups with the same prefix keep independent middleware.
	s := New()
	var trace []string
	s.Group("/api", record(&trace, "first")).Get("/one", text("1"))
	s.Group("/api", record(&trace, "second")).Get("/two", text("2"))
	serveRequest(t, s, http.MethodGet, "/api/one", nil)
	serveRequest(t, s, http.MethodGet, "/api/two", nil)
	if got := strings.Join(trace, ","); got != "first,second" {
		t.Errorf("trace = %q, want first,second", got)
	}
}

// TestGroupEmptyRoutePath covers g.Get("", h): the empty path registers the
// group's own path, exactly like "/".
func TestGroupEmptyRoutePath(t *testing.T) {
	tests := []struct {
		name      string
		group     func(s *Server) *Group
		wantRoute string
		targets   []string
		wantBody  string
	}{
		{name: "static prefix", group: func(s *Server) *Group { return s.Group("/users") },
			wantRoute: "/users", targets: []string{"/users", "/users/"}, wantBody: "own path"},
		{name: "prefix with trailing slash", group: func(s *Server) *Group { return s.Group("/users/") },
			wantRoute: "/users", targets: []string{"/users", "/users/"}, wantBody: "own path"},
		{name: "nested groups", group: func(s *Server) *Group { return s.Group("/api").Group("/v1") },
			wantRoute: "/api/v1", targets: []string{"/api/v1", "/api/v1/"}, wantBody: "own path"},
		{name: "parameter prefix", group: func(s *Server) *Group { return s.Group("/orgs/:org") },
			wantRoute: "/orgs/:org", targets: []string{"/orgs/acme", "/orgs/acme/"}, wantBody: "own path"},
		{name: "empty prefix is the root", group: func(s *Server) *Group { return s.Group("") },
			wantRoute: "/", targets: []string{"/"}, wantBody: "own path"},
		{name: "slash prefix is the root", group: func(s *Server) *Group { return s.Group("/") },
			wantRoute: "/", targets: []string{"/"}, wantBody: "own path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			tt.group(s).Get("", text(tt.wantBody))
			routes := s.Routes()
			if len(routes) != 1 || routes[0] != (Route{Method: http.MethodGet, Path: tt.wantRoute}) {
				t.Fatalf("Routes() = %v, want [GET %s]", routes, tt.wantRoute)
			}
			for _, target := range tt.targets {
				rec := serveRequest(t, s, http.MethodGet, target, nil)
				if rec.Code != http.StatusOK || rec.Body.String() != tt.wantBody {
					t.Errorf("GET %s = %d %q, want 200 %q", target, rec.Code, rec.Body.String(), tt.wantBody)
				}
			}
		})
	}
}

func TestGroupEmptyRoutePathEveryMethod(t *testing.T) {
	s := New()
	var order []string
	g := s.Group("/items", func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			order = append(order, "group mw")
			return next(c)
		}
	})
	register := map[string]func(string, HandlerFunc, ...MiddlewareFunc){
		http.MethodGet:     g.Get,
		http.MethodPost:    g.Post,
		http.MethodPut:     g.Put,
		http.MethodPatch:   g.Patch,
		http.MethodDelete:  g.Delete,
		http.MethodHead:    g.Head,
		http.MethodOptions: g.Options,
	}
	for method, fn := range register {
		fn("", text(method))
	}
	g.Handle("PROPFIND", "", text("PROPFIND"))

	for method := range register {
		rec := serveRequest(t, s, method, "/items", nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s /items = %d, want 200", method, rec.Code)
		}
		if method != http.MethodHead && rec.Body.String() != method {
			t.Errorf("%s /items body = %q, want %q", method, rec.Body.String(), method)
		}
	}
	if rec := serveRequest(t, s, "PROPFIND", "/items", nil); rec.Body.String() != "PROPFIND" {
		t.Errorf("PROPFIND /items body = %q", rec.Body.String())
	}
	if len(order) != len(register)+1 {
		t.Errorf("group middleware ran %d times, want %d", len(order), len(register)+1)
	}
}

func TestGroupEmptyAndSlashPathAreTheSameRoute(t *testing.T) {
	for _, paths := range [][2]string{{"", "/"}, {"/", ""}, {"", ""}} {
		t.Run(paths[0]+" then "+paths[1], func(t *testing.T) {
			s := New()
			g := s.Group("/users")
			g.Get(paths[0], noopHandler)
			mustPanic(t, "already registered", func() { g.Get(paths[1], noopHandler) })
		})
	}
}

func TestServerEmptyRoutePathStillPanics(t *testing.T) {
	register := map[string]func(s *Server){
		"Get":    func(s *Server) { s.Get("", noopHandler) },
		"Post":   func(s *Server) { s.Post("", noopHandler) },
		"Handle": func(s *Server) { s.Handle(http.MethodPut, "", noopHandler) },
	}
	for name, fn := range register {
		t.Run(name, func(t *testing.T) {
			mustPanic(t, "path must not be empty", func() { fn(New()) })
		})
	}
}

func TestGroupEmptyRoutePathStillValidatesTheRest(t *testing.T) {
	s := New()
	g := s.Group("/users")
	mustPanic(t, `handler for GET "/" must not be nil`, func() { g.Get("", nil) })
	mustPanic(t, "middleware at index 0 is nil", func() { g.Get("", noopHandler, nil) })
	mustPanic(t, "method must not be empty", func() { g.Handle("", "", noopHandler) })
}
