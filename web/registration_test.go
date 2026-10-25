package web

import (
	"net/http"
	"strings"
	"testing"
)

// mustPanic runs fn and fails unless it panics with a string message
// starting with "web: " and containing wantSubstr.
func mustPanic(t *testing.T, wantSubstr string, fn func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("did not panic, want panic containing %q", wantSubstr)
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value %T (%v), want a string message", r, r)
		}
		if !strings.HasPrefix(msg, "web: ") {
			t.Errorf("panic message %q does not start with %q", msg, "web: ")
		}
		if !strings.Contains(msg, wantSubstr) {
			t.Errorf("panic message %q does not contain %q", msg, wantSubstr)
		}
	}()
	fn()
}

func passThrough(next HandlerFunc) HandlerFunc { return next }

func TestRegistrationPanics(t *testing.T) {
	tests := []struct {
		name       string
		register   func(s *Server)
		wantSubstr string
	}{
		{name: "empty path", wantSubstr: "path must not be empty", register: func(s *Server) {
			s.Get("", noopHandler)
		}},
		{name: "path without leading slash", wantSubstr: `"users" for GET: must start with '/'`, register: func(s *Server) {
			s.Get("users", noopHandler)
		}},
		{name: "nil handler", wantSubstr: `handler for GET "/x" must not be nil`, register: func(s *Server) {
			s.Get("/x", nil)
		}},
		{name: "nil handler lowercase method", wantSubstr: `handler for POST "/x" must not be nil`, register: func(s *Server) {
			s.Handle("post", "/x", nil)
		}},
		{name: "nil route middleware", wantSubstr: "route GET /x: middleware at index 1 is nil", register: func(s *Server) {
			s.Get("/x", noopHandler, passThrough, nil)
		}},
		{name: "empty method", wantSubstr: "method must not be empty", register: func(s *Server) {
			s.Handle("", "/x", noopHandler)
		}},
		{name: "method with space", wantSubstr: `invalid HTTP method "GE T"`, register: func(s *Server) {
			s.Handle("GE T", "/x", noopHandler)
		}},
		{name: "duplicate route", wantSubstr: "route GET /dup is already registered", register: func(s *Server) {
			s.Get("/dup", noopHandler)
			s.Get("/dup", noopHandler)
		}},
		{name: "duplicate via trailing slash", wantSubstr: "already registered", register: func(s *Server) {
			s.Get("/dup", noopHandler)
			s.Get("/dup/", noopHandler)
		}},
		{name: "duplicate across method case", wantSubstr: "already registered", register: func(s *Server) {
			s.Handle("get", "/dup", noopHandler)
			s.Get("/dup", noopHandler)
		}},
		{name: "duplicate through group", wantSubstr: "already registered", register: func(s *Server) {
			s.Get("/api/users", noopHandler)
			s.Group("/api").Get("/users", noopHandler)
		}},
		{name: "conflicting parameter names", wantSubstr: `parameter name "uid" conflicts with existing parameter "id"`, register: func(s *Server) {
			s.Get("/users/:id", noopHandler)
			s.Get("/users/:uid/posts", noopHandler)
		}},
		{name: "conflicting wildcard names", wantSubstr: `wildcard name "b" conflicts with existing wildcard "a"`, register: func(s *Server) {
			s.Get("/f/*a", noopHandler)
			s.Get("/f/*b/", noopHandler)
		}},
		{name: "wildcard not last", wantSubstr: `wildcard "w" must be the last segment`, register: func(s *Server) {
			s.Get("/a/*w/b", noopHandler)
		}},
		{name: "empty interior segment", wantSubstr: "empty path segment", register: func(s *Server) {
			s.Get("/a//b", noopHandler)
		}},
		{name: "unnamed parameter", wantSubstr: "parameter segment must have a name", register: func(s *Server) {
			s.Get("/users/:", noopHandler)
		}},
		{name: "malformed escape", wantSubstr: "malformed percent-encoding", register: func(s *Server) {
			s.Get("/a/%zz", noopHandler)
		}},
		{name: "group prefix without slash", wantSubstr: `invalid group prefix "api": must start with '/'`, register: func(s *Server) {
			s.Group("api")
		}},
		{name: "group prefix with empty segment", wantSubstr: `invalid group prefix "/a//b"`, register: func(s *Server) {
			s.Group("/a//b")
		}},
		{name: "group prefix with wildcard", wantSubstr: "wildcard segment can only end a route", register: func(s *Server) {
			s.Group("/files/*path")
		}},
		{name: "nested group prefix invalid", wantSubstr: `invalid group prefix "v1"`, register: func(s *Server) {
			s.Group("/api").Group("v1")
		}},
		{name: "nil group middleware", wantSubstr: "Group: middleware at index 0 is nil", register: func(s *Server) {
			s.Group("/api", nil)
		}},
		{name: "nil global middleware", wantSubstr: "Use: middleware at index 1 is nil", register: func(s *Server) {
			s.Use(passThrough, nil)
		}},
		{name: "nil group Use middleware", wantSubstr: "Use: middleware at index 0 is nil", register: func(s *Server) {
			s.Group("/api").Use(nil)
		}},
		{name: "group route path without slash", wantSubstr: "must start with '/'", register: func(s *Server) {
			s.Group("/api").Get("users", noopHandler)
		}},
		{name: "group route nil handler", wantSubstr: "must not be nil", register: func(s *Server) {
			s.Group("/api").Post("/x", nil)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			mustPanic(t, tt.wantSubstr, func() { tt.register(s) })
		})
	}
}

func TestRegistrationPanicLeavesServerUsable(t *testing.T) {
	s := New()
	s.Get("/ok", text("ok"))
	mustPanic(t, "already registered", func() { s.Get("/ok", noopHandler) })
	// The lock is released and the original route is intact.
	if rec := serveRequest(t, s, http.MethodGet, "/ok", nil); rec.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", rec.Body.String())
	}
	s.Get("/other", text("other"))
	if rec := serveRequest(t, s, http.MethodGet, "/other", nil); rec.Body.String() != "other" {
		t.Errorf("body = %q, want other", rec.Body.String())
	}
}

func TestRegistrationValidVariants(t *testing.T) {
	// None of these may panic.
	s := New()
	s.Get("/", noopHandler)
	s.Get("/users/:id", noopHandler)
	s.Get("/users/:id/posts/:pid", noopHandler)
	s.Delete("/users/:id", noopHandler) // same pattern, other method
	s.Get("/files/*path", noopHandler)
	s.Handle("PROPFIND", "/dav", noopHandler) // extension method
	s.Group("").Get("/root-group", noopHandler)
	s.Group("/").Get("/slash-group", noopHandler)
	s.Group("/api/").Get("/trimmed", noopHandler)
	s.Group("/orgs/:org").Get("/repos", noopHandler)
	if got := len(s.Routes()); got != 10 {
		t.Errorf("len(Routes()) = %d, want 10: %v", got, s.Routes())
	}
	for _, path := range []string{"/root-group", "/slash-group", "/api/trimmed", "/orgs/acme/repos"} {
		if rec := serveRequest(t, s, http.MethodGet, path, nil); rec.Code != 200 {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}
