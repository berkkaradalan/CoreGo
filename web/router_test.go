package web

import (
	"strings"
	"testing"
)

func TestRouterAddValidation(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		pattern    string
		handler    HandlerFunc
		wantSubstr string
	}{
		{name: "empty method", method: "", pattern: "/a", handler: noopHandler, wantSubstr: "method must not be empty"},
		{name: "nil handler", method: "GET", pattern: "/a", handler: nil, wantSubstr: "must not be nil"},
		{name: "empty path", method: "GET", pattern: "", handler: noopHandler, wantSubstr: "must not be empty"},
		{name: "no leading slash", method: "GET", pattern: "a", handler: noopHandler, wantSubstr: "must start with"},
		{name: "wildcard not last", method: "GET", pattern: "/a/*w/b", handler: noopHandler, wantSubstr: "must be the last segment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newRouter()
			err := rt.add(tt.method, tt.pattern, tt.handler)
			if err == nil {
				t.Fatalf("add(%q, %q) succeeded, want error", tt.method, tt.pattern)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("add error = %q, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}

func TestRouterAddDuplicate(t *testing.T) {
	rt := newRouter()
	if err := rt.add("GET", "/users/:id", noopHandler); err != nil {
		t.Fatal(err)
	}
	err := rt.add("GET", "/users/:id", noopHandler)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate add error = %v, want 'already registered'", err)
	}
	// Same path under a different method is fine.
	if err := rt.add("DELETE", "/users/:id", noopHandler); err != nil {
		t.Errorf("same path other method: %v", err)
	}
	// Trailing slash counts as the same path.
	if err := rt.add("POST", "/users/:id/", noopHandler); err != nil {
		t.Fatal(err)
	}
	if err := rt.add("POST", "/users/:id", noopHandler); err == nil {
		t.Error("duplicate add via trailing slash succeeded, want error")
	}
}

func TestRouterAddParamConflict(t *testing.T) {
	rt := newRouter()
	if err := rt.add("GET", "/users/:id", noopHandler); err != nil {
		t.Fatal(err)
	}
	err := rt.add("GET", "/users/:uid", noopHandler)
	if err == nil || !strings.Contains(err.Error(), "conflicts with existing parameter") {
		t.Fatalf("param conflict error = %v, want conflict message", err)
	}
	// Conflict detection also works through deeper shared prefixes.
	if err := rt.add("GET", "/users/:id/posts", noopHandler); err != nil {
		t.Fatal(err)
	}
	if err := rt.add("GET", "/users/:uid/settings", noopHandler); err == nil {
		t.Error("deep param conflict succeeded, want error")
	}
}

func TestRouterAddMethodCaseNormalized(t *testing.T) {
	rt := newRouter()
	if err := rt.add("get", "/x", noopHandler); err != nil {
		t.Fatal(err)
	}
	h, _ := rt.resolve("GET", "/x")
	if h == nil {
		t.Fatal("resolve(GET) after add(get) = nil, want handler")
	}
	if err := rt.add("GET", "/x", noopHandler); err == nil {
		t.Error("duplicate across method casing not detected")
	}
}

func TestRouterResolveMethodIsolation(t *testing.T) {
	rt := newRouter()
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		if err := rt.add(m, "/thing", noopHandler); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		h, params := rt.resolve(m, "/thing")
		if h == nil {
			t.Errorf("resolve(%s) = nil, want handler", m)
		}
		if params == nil {
			t.Errorf("resolve(%s) params = nil, want map", m)
		}
	}
	for _, m := range []string{"HEAD", "OPTIONS", "TRACE"} {
		h, _ := rt.resolve(m, "/thing")
		if h != nil {
			t.Errorf("resolve(%s) = handler, want nil", m)
		}
	}
}

func TestRouterResolveUnmatchedPaths(t *testing.T) {
	rt := newRouter()
	if err := rt.add("GET", "/users/:id", noopHandler); err != nil {
		t.Fatal(err)
	}
	unmatched := []string{
		"/users",         // param needs a non-empty segment
		"/users/",        // trailing slash normalizes to /users, still no handler
		"/users//x",      // interior double slash
		"/users/1/extra", // too deep
		"/other/1",       // different static prefix
	}
	for _, p := range unmatched {
		h, _ := rt.resolve("GET", p)
		if h != nil {
			t.Errorf("resolve(GET, %q) = handler, want nil", p)
		}
	}
	matched, params := rt.resolve("GET", "/users/7")
	if matched == nil {
		t.Fatal("resolve(GET, /users/7) = nil, want handler")
	}
	if params["id"] != "7" {
		t.Errorf("params[id] = %q, want 7", params["id"])
	}
}

func TestRouterMethodsFor(t *testing.T) {
	rt := newRouter()
	for _, m := range []string{"POST", "GET", "DELETE"} {
		if err := rt.add(m, "/res", noopHandler); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.add("GET", "/res/nested", noopHandler); err != nil {
		t.Fatal(err)
	}
	got := rt.methodsFor("/res")
	want := "DELETE,GET,POST"
	if strings.Join(got, ",") != want {
		t.Errorf("methodsFor(/res) = %v, want %v", got, want)
	}
	if got := rt.methodsFor("/res/nested"); strings.Join(got, ",") != "GET" {
		t.Errorf("methodsFor(/res/nested) = %v, want [GET]", got)
	}
	if got := rt.methodsFor("/missing"); len(got) != 0 {
		t.Errorf("methodsFor(/missing) = %v, want empty", got)
	}
	if got := rt.methodsFor("/res//bad"); len(got) != 0 {
		t.Errorf("methodsFor with double slash = %v, want empty", got)
	}
	// Wildcard empty tail still counts as a matching method.
	rt2 := newRouter()
	if err := rt2.add("GET", "/files/*path", noopHandler); err != nil {
		t.Fatal(err)
	}
	if got := rt2.methodsFor("/files"); strings.Join(got, ",") != "GET" {
		t.Errorf("methodsFor(/files) = %v, want [GET]", got)
	}
}

func TestIsToken(t *testing.T) {
	tests := map[string]bool{
		"GET":             true,
		"get":             true,
		"PROPFIND":        true,
		"M-SEARCH":        true,
		"X_CUSTOM1":       true,
		"!#$%&'*+-.^_`|~": true,
		"":                false,
		"GE T":            false,
		"GET\n":           false,
		"GET/1":           false,
		"(GET)":           false,
		"GÉT":             false,
	}
	for in, want := range tests {
		if got := isToken(in); got != want {
			t.Errorf("isToken(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRouterAddInvalidMethod(t *testing.T) {
	rt := newRouter()
	err := rt.add("GE T", "/a", noopHandler)
	if err == nil || !strings.Contains(err.Error(), `invalid HTTP method "GE T"`) {
		t.Fatalf("add error = %v, want invalid method error", err)
	}
	if len(rt.trees) != 0 || len(rt.list()) != 0 {
		t.Error("failed add left state behind")
	}
}

func TestRouterList(t *testing.T) {
	rt := newRouter()
	for _, r := range []Route{{"POST", "/b"}, {"GET", "/b/"}, {"GET", "/a/:id"}, {"DELETE", "/a/:id"}, {"GET", "/"}} {
		if err := rt.add(r.Method, r.Path, noopHandler); err != nil {
			t.Fatal(err)
		}
	}
	// A failed duplicate add is not listed.
	if err := rt.add("GET", "/b", noopHandler); err == nil {
		t.Fatal("duplicate add succeeded")
	}
	got := rt.list()
	want := []Route{{"GET", "/"}, {"DELETE", "/a/:id"}, {"GET", "/a/:id"}, {"GET", "/b"}, {"POST", "/b"}}
	if len(got) != len(want) {
		t.Fatalf("list() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("list()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
