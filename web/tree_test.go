package web

import (
	"strings"
	"testing"
)

func noopHandler(c *Context) error { return nil }

func TestParsePatternValid(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    []segment
	}{
		{name: "root", pattern: "/", want: []segment{}},
		{name: "double slash collapses to root", pattern: "//", want: []segment{}},
		{name: "static only", pattern: "/users", want: []segment{{kindStatic, "users"}}},
		{name: "multiple static", pattern: "/a/b/c", want: []segment{
			{kindStatic, "a"}, {kindStatic, "b"}, {kindStatic, "c"},
		}},
		{name: "trailing slash ignored", pattern: "/a/", want: []segment{{kindStatic, "a"}}},
		{name: "param", pattern: "/users/:id", want: []segment{
			{kindStatic, "users"}, {kindParam, "id"},
		}},
		{name: "wildcard last", pattern: "/files/*path", want: []segment{
			{kindStatic, "files"}, {kindWildcard, "path"},
		}},
		{name: "wildcard alone at root", pattern: "/*all", want: []segment{{kindWildcard, "all"}}},
		{name: "static percent-decoded", pattern: "/caf%C3%A9", want: []segment{{kindStatic, "café"}}},
		{name: "trailing slash after wildcard", pattern: "/f/*rest/", want: []segment{
			{kindStatic, "f"}, {kindWildcard, "rest"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePattern(tt.pattern)
			if err != nil {
				t.Fatalf("parsePattern(%q) error: %v", tt.pattern, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parsePattern(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("segment %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParsePatternInvalid(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		wantSubstr string
	}{
		{name: "empty path", pattern: "", wantSubstr: "must not be empty"},
		{name: "missing leading slash", pattern: "users", wantSubstr: "must start with '/'"},
		{name: "interior double slash", pattern: "/a//b", wantSubstr: "empty path segment"},
		{name: "leading double slash", pattern: "//a", wantSubstr: "empty path segment"},
		{name: "trailing double slash", pattern: "/a//", wantSubstr: "empty path segment"},
		{name: "wildcard not last", pattern: "/a/*rest/b", wantSubstr: "must be the last segment"},
		{name: "wildcard in middle multi", pattern: "/*a/b/*c", wantSubstr: "must be the last segment"},
		{name: "empty param name", pattern: "/users/:", wantSubstr: "parameter segment must have a name"},
		{name: "empty wildcard name", pattern: "/files/*", wantSubstr: "wildcard segment must have a name"},
		{name: "malformed escape", pattern: "/a/%zz", wantSubstr: "malformed percent-encoding"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePattern(tt.pattern)
			if err == nil {
				t.Fatalf("parsePattern(%q) succeeded, want error", tt.pattern)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("parsePattern(%q) error = %q, want substring %q", tt.pattern, err, tt.wantSubstr)
			}
		})
	}
}

func buildTree(t *testing.T, patterns ...string) *node {
	t.Helper()
	root := &node{kind: kindStatic}
	for _, p := range patterns {
		segs, err := parsePattern(p)
		if err != nil {
			t.Fatalf("parsePattern(%q): %v", p, err)
		}
		if err := root.insert("TEST "+p, segs, noopHandler); err != nil {
			t.Fatalf("insert(%q): %v", p, err)
		}
	}
	return root
}

func TestNodeInsertDuplicate(t *testing.T) {
	root := buildTree(t, "/a")
	segs, err := parsePattern("/a")
	if err != nil {
		t.Fatal(err)
	}
	err = root.insert("TEST /a", segs, noopHandler)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate insert error = %v, want 'already registered'", err)
	}
}

func TestNodeInsertParamNameConflict(t *testing.T) {
	root := buildTree(t, "/users/:id")
	segs, err := parsePattern("/users/:uid")
	if err != nil {
		t.Fatal(err)
	}
	err = root.insert("TEST /users/:uid", segs, noopHandler)
	if err == nil || !strings.Contains(err.Error(), "conflicts with existing parameter") {
		t.Fatalf("param conflict error = %v, want conflict message", err)
	}
}

func TestNodeInsertSameParamNameAllowed(t *testing.T) {
	root := buildTree(t, "/users/:id", "/users/:id/posts", "/users/:id/posts/:pid")
	// Same names at shared positions must compose into one tree.
	if root.static["users"].param.name != "id" {
		t.Error("expected shared param node :id under /users")
	}
}

func TestNodeInsertWildcardNameConflict(t *testing.T) {
	root := buildTree(t, "/files/*rest")
	segs, err := parsePattern("/files/*other")
	if err != nil {
		t.Fatal(err)
	}
	err = root.insert("TEST /files/*other", segs, noopHandler)
	if err == nil || !strings.Contains(err.Error(), "conflicts with existing wildcard") {
		t.Fatalf("wildcard conflict error = %v, want conflict message", err)
	}
}

func TestNodeSearchPrecedenceAndBacktracking(t *testing.T) {
	root := buildTree(t,
		"/",             // root
		"/users",        // static
		"/users/new",    // static beats param
		"/users/:id",    // param
		"/a/:p/b",       // param behind static
		"/a/x/c",        // static behind static (forces backtracking)
		"/a/x/:q/deep",  // deeper param
		"/files/report", // static beats wildcard
		"/files/*path",  // wildcard
		"/x/:p",         // param with handler
		"/x/*w",         // wildcard sibling (param wins when it matches)
	)
	tests := []struct {
		name       string
		segs       []string
		wantFound  bool
		wantParams map[string]string
	}{
		{name: "root", segs: []string{}, wantFound: true, wantParams: map[string]string{}},
		{name: "static", segs: []string{"users"}, wantFound: true, wantParams: map[string]string{}},
		{name: "static beats param", segs: []string{"users", "new"}, wantFound: true, wantParams: map[string]string{}},
		{name: "param", segs: []string{"users", "42"}, wantFound: true, wantParams: map[string]string{"id": "42"}},
		{name: "param empty segment never matches", segs: []string{"users", ""}, wantFound: false},
		{name: "backtrack from static into param", segs: []string{"a", "x", "b"}, wantFound: true, wantParams: map[string]string{"p": "x"}},
		{name: "static deep", segs: []string{"a", "x", "c"}, wantFound: true, wantParams: map[string]string{}},
		{name: "deep param", segs: []string{"a", "x", "zz", "deep"}, wantFound: true, wantParams: map[string]string{"q": "zz"}},
		{name: "dead end no match", segs: []string{"a", "y", "z"}, wantFound: false},
		{name: "static beats wildcard", segs: []string{"files", "report"}, wantFound: true, wantParams: map[string]string{}},
		{name: "wildcard captures rest", segs: []string{"files", "a", "b", "c.txt"}, wantFound: true, wantParams: map[string]string{"path": "a/b/c.txt"}},
		{name: "wildcard single segment", segs: []string{"files", "x"}, wantFound: true, wantParams: map[string]string{"path": "x"}},
		{name: "wildcard empty tail", segs: []string{"files"}, wantFound: true, wantParams: map[string]string{"path": ""}},
		{name: "param beats wildcard", segs: []string{"x", "abc"}, wantFound: true, wantParams: map[string]string{"p": "abc"}},
		{name: "wildcard when param has no handler", segs: []string{"users", "42", "zzz"}, wantFound: false},
		{name: "unknown first segment", segs: []string{"nope"}, wantFound: false},
		{name: "too deep", segs: []string{"a", "x", "c", "d", "e"}, wantFound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			h := root.search(tt.segs, params)
			if (h != nil) != tt.wantFound {
				t.Fatalf("search(%v) found = %v, want %v", tt.segs, h != nil, tt.wantFound)
			}
			if !tt.wantFound {
				if len(params) != 0 {
					t.Errorf("failed search polluted params: %v", params)
				}
				return
			}
			if len(params) != len(tt.wantParams) {
				t.Fatalf("params = %v, want %v", params, tt.wantParams)
			}
			for k, v := range tt.wantParams {
				if params[k] != v {
					t.Errorf("params[%q] = %q, want %q", k, params[k], v)
				}
			}
		})
	}
}

func TestSplitRequestPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantOK  bool
		wantSeg []string
	}{
		{name: "root", path: "/", wantOK: true, wantSeg: []string{}},
		{name: "empty treated as root", path: "", wantOK: true, wantSeg: []string{}},
		{name: "simple", path: "/a/b", wantOK: true, wantSeg: []string{"a", "b"}},
		{name: "trailing slash normalized", path: "/a/", wantOK: true, wantSeg: []string{"a"}},
		{name: "root double slash keeps root", path: "//", wantOK: true, wantSeg: []string{}},
		{name: "interior double slash rejected", path: "/a//b", wantOK: false},
		{name: "trailing double slash rejected", path: "/a//", wantOK: false},
		{name: "no leading slash rejected", path: "a/b", wantOK: false},
		{name: "percent decoded", path: "/users/jos%C3%A9", wantOK: true, wantSeg: []string{"users", "josé"}},
		{name: "percent decoded slash stays in segment", path: "/a%2Fb", wantOK: true, wantSeg: []string{"a/b"}},
		{name: "malformed escape kept raw", path: "/a%zz", wantOK: true, wantSeg: []string{"a%zz"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segs, ok := splitRequestPath(tt.path)
			if ok != tt.wantOK {
				t.Fatalf("splitRequestPath(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if len(segs) != len(tt.wantSeg) {
				t.Fatalf("splitRequestPath(%q) = %v, want %v", tt.path, segs, tt.wantSeg)
			}
			for i := range segs {
				if segs[i] != tt.wantSeg[i] {
					t.Errorf("segment %d = %q, want %q", i, segs[i], tt.wantSeg[i])
				}
			}
		})
	}
}
