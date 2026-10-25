package web

import (
	"fmt"
	"net/http"
	"strings"
)

// Group bundles a path prefix with middleware so that routes registered
// through it share both. Groups nest: a child group extends its parent's
// prefix, and the parent's middleware always runs before (outside) the
// child's.
//
// Group middleware is captured when a route is registered: middleware added
// later with Use applies to routes registered afterwards, on the group and
// on any of its child groups, but never to routes that already exist.
type Group struct {
	server *Server
	parent *Group
	prefix string // full prefix, including every ancestor's
	mw     []MiddlewareFunc
}

// newGroup builds a group under parent (nil for a top-level group),
// panicking on an invalid prefix or a nil middleware.
func newGroup(s *Server, parent *Group, prefix string, mw []MiddlewareFunc) *Group {
	mustMiddleware("Group", mw)
	g := &Group{server: s, parent: parent, mw: append([]MiddlewareFunc(nil), mw...)}
	if parent != nil {
		g.prefix = parent.prefix
	}
	g.prefix += mustPrefix(prefix)
	return g
}

// mustPrefix validates a group prefix and returns it normalized: "" and "/"
// become "" (no prefix) and a trailing slash is dropped. A prefix must start
// with '/', must not contain empty segments, unnamed parameters or malformed
// escapes, and must not contain a wildcard (a wildcard can only end a route).
func mustPrefix(prefix string) string {
	if prefix == "" || prefix == "/" {
		return ""
	}
	if prefix[0] != '/' {
		panic(fmt.Sprintf("web: invalid group prefix %q: must start with '/'", prefix))
	}
	segs, err := parsePattern(prefix)
	if err != nil {
		panic(fmt.Sprintf("web: invalid group prefix %q: %v", prefix, strings.TrimPrefix(err.Error(), "web: ")))
	}
	for _, seg := range segs {
		if seg.kind == kindWildcard {
			panic(fmt.Sprintf("web: invalid group prefix %q: a wildcard segment can only end a route, not a group prefix", prefix))
		}
	}
	return strings.TrimSuffix(prefix, "/")
}

// middleware returns the group's effective middleware: every ancestor's
// (outermost first) followed by its own.
func (g *Group) middleware() []MiddlewareFunc {
	if g.parent == nil {
		return append([]MiddlewareFunc(nil), g.mw...)
	}
	return append(g.parent.middleware(), g.mw...)
}

// Use appends middleware to the group. It applies to routes registered on
// g, or on any of its child groups, after the call; routes registered
// before the call are not affected. It panics if any middleware is nil.
func (g *Group) Use(mw ...MiddlewareFunc) {
	mustMiddleware("Use", mw)
	g.server.regMu.Lock()
	defer g.server.regMu.Unlock()
	g.mw = append(g.mw, mw...)
}

// Group creates a nested group under g. The child's prefix is appended to
// g's, and the child's middleware runs after (inside) g's. It panics on an
// invalid prefix (see Server.Group).
func (g *Group) Group(prefix string, mw ...MiddlewareFunc) *Group {
	return newGroup(g.server, g, prefix, mw)
}

// Handle registers h for method and path under the group's prefix. The
// group's middleware (parent groups first) runs before any per-route
// middleware mw. It panics on invalid registrations, exactly like
// Server.Handle, with one difference: the empty path is allowed and
// registers the group's own path, exactly like "/":
//
//	users := server.Group("/users")
//	users.Get("", listUsers)     // GET /users
//	users.Get("/:id", showUser)  // GET /users/:id
//
// On the Server itself an empty path panics.
func (g *Group) Handle(method, path string, h HandlerFunc, mw ...MiddlewareFunc) {
	g.server.handle(g, method, path, h, mw)
}

// Get registers h for GET requests under the group's prefix.
func (g *Group) Get(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	g.server.handle(g, http.MethodGet, path, h, mw)
}

// Post registers h for POST requests under the group's prefix.
func (g *Group) Post(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	g.server.handle(g, http.MethodPost, path, h, mw)
}

// Put registers h for PUT requests under the group's prefix.
func (g *Group) Put(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	g.server.handle(g, http.MethodPut, path, h, mw)
}

// Patch registers h for PATCH requests under the group's prefix.
func (g *Group) Patch(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	g.server.handle(g, http.MethodPatch, path, h, mw)
}

// Delete registers h for DELETE requests under the group's prefix.
func (g *Group) Delete(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	g.server.handle(g, http.MethodDelete, path, h, mw)
}

// Head registers h for HEAD requests under the group's prefix.
func (g *Group) Head(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	g.server.handle(g, http.MethodHead, path, h, mw)
}

// Options registers h for OPTIONS requests under the group's prefix.
func (g *Group) Options(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	g.server.handle(g, http.MethodOptions, path, h, mw)
}

// mustMiddleware panics when any middleware in mw is nil; where names the
// API being called, for the message.
func mustMiddleware(where string, mw []MiddlewareFunc) {
	for i, m := range mw {
		if m == nil {
			panic(fmt.Sprintf("web: %s: middleware at index %d is nil", where, i))
		}
	}
}
