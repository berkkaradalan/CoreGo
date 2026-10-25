package web

import (
	"fmt"
	"sort"
	"strings"
)

// Route describes one registered route: its HTTP method and its path
// pattern (with any group prefix applied and a trailing slash removed).
type Route struct {
	Method string
	Path   string
}

// router dispatches requests across one routing tree per HTTP method. It
// performs no locking itself; the Server serializes registration against
// lookups.
type router struct {
	trees  map[string]*node
	routes []Route
}

func newRouter() *router {
	return &router{trees: make(map[string]*node)}
}

// add registers h for method and pattern, validating both. The method is
// normalized to upper case. Every returned error carries the "web: "
// prefix and describes the offending registration.
func (rt *router) add(method, pattern string, h HandlerFunc) error {
	if method == "" {
		return fmt.Errorf("web: method must not be empty (route %q)", pattern)
	}
	if !isToken(method) {
		return fmt.Errorf("web: invalid HTTP method %q (route %q)", method, pattern)
	}
	normalized := strings.ToUpper(method)
	if h == nil {
		return fmt.Errorf("web: handler for %s %q must not be nil", normalized, pattern)
	}
	segs, err := parsePattern(pattern)
	if err != nil {
		return err
	}
	root := rt.trees[normalized]
	if root == nil {
		root = &node{kind: kindStatic}
	}
	if err := root.insert(normalized+" "+pattern, segs, h); err != nil {
		return err
	}
	rt.trees[normalized] = root
	rt.routes = append(rt.routes, Route{Method: normalized, Path: canonicalPattern(pattern)})
	return nil
}

// resolve returns the handler (and captured parameters) registered for
// method at escapedPath, or nil when no route matches.
func (rt *router) resolve(method, escapedPath string) (HandlerFunc, map[string]string) {
	segs, ok := splitRequestPath(escapedPath)
	if !ok {
		return nil, nil
	}
	root := rt.trees[method]
	if root == nil {
		return nil, nil
	}
	params := make(map[string]string)
	h := root.search(segs, params)
	if h == nil {
		return nil, nil
	}
	return h, params
}

// methodsFor returns the sorted list of HTTP methods that have a handler
// matching escapedPath, or nil when the path matches no method.
func (rt *router) methodsFor(escapedPath string) []string {
	segs, ok := splitRequestPath(escapedPath)
	if !ok {
		return nil
	}
	methods := make([]string, 0, len(rt.trees))
	for method, root := range rt.trees {
		if root.search(segs, make(map[string]string)) != nil {
			methods = append(methods, method)
		}
	}
	sort.Strings(methods)
	return methods
}

// list returns a copy of the registered routes sorted by path, then method.
func (rt *router) list() []Route {
	out := make([]Route, len(rt.routes))
	copy(out, rt.routes)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// canonicalPattern strips the trailing slash of a validated pattern (the
// router treats "/a/" and "/a" as the same route); the root stays "/".
func canonicalPattern(pattern string) string {
	if len(pattern) > 1 && strings.HasSuffix(pattern, "/") {
		return pattern[:len(pattern)-1]
	}
	return pattern
}

// isToken reports whether s is a valid HTTP token (RFC 9110 section 5.6.2),
// the syntax required for request methods.
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", ch) >= 0:
		default:
			return false
		}
	}
	return true
}
