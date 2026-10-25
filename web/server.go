package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultMaxBodyBytes is the default request body limit enforced by
// Context.Body, BindJSON and BindJSONStrict (1 MiB).
const DefaultMaxBodyBytes int64 = 1 << 20

// Default values New assigns to the Server timeout fields.
const (
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 30 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultShutdownTimeout   = 10 * time.Second
)

// DefaultClientIPHeader is the default value of Server.ClientIPHeader.
const DefaultClientIPHeader = "X-Forwarded-For"

// errorEnvelope is the JSON body shape used for framework-generated error
// responses: {"error": "..."}.
type errorEnvelope struct {
	Error string `json:"error"`
}

// Server is CoreGo's web server. It implements http.Handler, so it can be
// mounted anywhere net/http accepts a handler, and it can also run and shut
// itself down (see Run, Serve, ListenAndServe and Shutdown).
//
// New sets every exported field to its default. Override fields after New
// and before serving starts; changing them while requests are being served
// is a data race.
//
// Registration methods (Handle, Get, Group, Use, ...) never return errors:
// an invalid registration is a programmer error and panics immediately with
// a message starting with "web: ", like net/http.ServeMux. Registration is
// safe for concurrent use, also while serving.
type Server struct {
	// MaxBodyBytes is the largest request body Context.Body,
	// Context.BindJSON and Context.BindJSONStrict will read. Default
	// DefaultMaxBodyBytes (1 MiB); 0 means unlimited.
	MaxBodyBytes int64

	// TrustProxyHeaders makes Context.RemoteIP read the client address
	// from ClientIPHeader. Enable it only behind a reverse proxy that sets
	// that header; otherwise clients can spoof their address. Default false.
	//
	// While it is false, the first request carrying X-Forwarded-For or
	// ClientIPHeader makes the server log a one-time Warn: behind a proxy,
	// every client then appears with the proxy's address and shares its
	// rate limits.
	TrustProxyHeaders bool

	// ClientIPHeader is the header consulted by Context.RemoteIP when
	// TrustProxyHeaders is true. Default "X-Forwarded-For", whose rightmost
	// entry is used; any other header (e.g. "X-Real-IP",
	// "CF-Connecting-IP") is used whole. Empty means "X-Forwarded-For".
	ClientIPHeader string

	// Timeouts passed to the underlying http.Server when serving starts.
	// A value of 0 means no timeout, exactly as in net/http.
	ReadHeaderTimeout time.Duration // default 10s
	ReadTimeout       time.Duration // default 30s
	WriteTimeout      time.Duration // default 30s
	IdleTimeout       time.Duration // default 120s

	// ShutdownTimeout bounds how long Run waits for in-flight requests
	// after SIGINT or SIGTERM before closing remaining connections.
	// Default 10s; 0 means wait indefinitely.
	ShutdownTimeout time.Duration

	// Logger receives framework logs (recovered panics, unhandled errors,
	// net/http server errors at Error level). nil means slog.Default(),
	// resolved when a log is written (for the net/http error log: when
	// serving starts).
	Logger *slog.Logger

	// regMu serializes registration (routes, global middleware, error
	// handler, group middleware) against request dispatch.
	regMu   sync.RWMutex
	router  *router
	mw      []MiddlewareFunc
	global  HandlerFunc // composed global chain; nil until first needed
	onError ErrorHandler

	// proxyWarned records that the untrusted proxy header warning has
	// been logged.
	proxyWarned atomic.Bool

	// runMu guards httpServer, the server currently serving, if any.
	runMu      sync.Mutex
	httpServer *http.Server
}

// New creates an empty Server with every field set to its default: a 1 MiB
// body limit, proxy headers untrusted, X-Forwarded-For as the client IP
// header, the default timeouts, slog.Default() logging and the default
// JSON error handler.
func New() *Server {
	return &Server{
		MaxBodyBytes:      DefaultMaxBodyBytes,
		ClientIPHeader:    DefaultClientIPHeader,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       DefaultReadTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		ShutdownTimeout:   DefaultShutdownTimeout,
		router:            newRouter(),
	}
}

// logger resolves the Logger field.
func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Use appends global middleware. Global middleware runs for every request,
// including the framework's own 404, 405 and automatic OPTIONS responses,
// which makes it the right place for CORS and request logging. The first
// middleware registered runs outermost. Use panics if any middleware is
// nil.
//
// The global chain is composed once, on the first request after the last
// Use call, so each middleware's outer function runs once rather than per
// request (or per Use call).
func (s *Server) Use(mw ...MiddlewareFunc) {
	mustMiddleware("Use", mw)
	s.regMu.Lock()
	defer s.regMu.Unlock()
	s.mw = append(s.mw, mw...)
	s.global = nil
}

// globalChain returns the composed global chain, composing it on first
// use after a Use call.
func (s *Server) globalChain() HandlerFunc {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	if s.global == nil {
		s.global = chain(dispatchTarget, s.mw...)
	}
	return s.global
}

// OnError installs fn as the error handler invoked when a handler or
// middleware returns an error. Calling it with nil restores the default
// handler, which renders *HTTPError as its code and message, maps the
// sentinel errors ErrBodyTooLarge, ErrUnsupportedMediaType and
// ErrInvalidJSON to 413, 415 and 400 with descriptive messages (see
// AsHTTPError, which custom handlers can reuse), and logs anything else
// and answers 500. In every case the body is JSON: {"error": "..."}. When the response
// has already been written, errors are only logged.
func (s *Server) OnError(fn ErrorHandler) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	s.onError = fn
}

// Routes returns every registered route, sorted by path and then method.
// Automatic HEAD and OPTIONS responses are not listed.
func (s *Server) Routes() []Route {
	s.regMu.RLock()
	defer s.regMu.RUnlock()
	return s.router.list()
}

// Group creates a route group with the given prefix and middleware. The
// prefix must start with '/' ("" and "/" mean no prefix); it may contain
// ":name" parameters but no wildcard. Group panics on an invalid prefix or
// a nil middleware.
func (s *Server) Group(prefix string, mw ...MiddlewareFunc) *Group {
	return newGroup(s, nil, prefix, mw)
}

// Handle registers h for requests with the given method and path, with
// optional per-route middleware mw (first = outermost).
//
// Paths must start with '/', may contain ":name" parameter segments and a
// final "*name" wildcard segment. Handle panics with a "web: " message when
// the method is empty or not a valid HTTP token, the path is invalid, h or
// any middleware is nil, the method+path pair is already registered, or a
// parameter or wildcard name conflicts with one already registered at the
// same position ("/users/:id" and "/users/:uid").
func (s *Server) Handle(method, path string, h HandlerFunc, mw ...MiddlewareFunc) {
	s.handle(nil, method, path, h, mw)
}

// Get registers h for GET requests. HEAD requests without an explicit HEAD
// route are served by the GET route with the body suppressed.
func (s *Server) Get(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	s.handle(nil, http.MethodGet, path, h, mw)
}

// Post registers h for POST requests.
func (s *Server) Post(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	s.handle(nil, http.MethodPost, path, h, mw)
}

// Put registers h for PUT requests.
func (s *Server) Put(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	s.handle(nil, http.MethodPut, path, h, mw)
}

// Patch registers h for PATCH requests.
func (s *Server) Patch(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	s.handle(nil, http.MethodPatch, path, h, mw)
}

// Delete registers h for DELETE requests.
func (s *Server) Delete(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	s.handle(nil, http.MethodDelete, path, h, mw)
}

// Head registers h for HEAD requests. The body is always suppressed for
// HEAD requests.
func (s *Server) Head(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	s.handle(nil, http.MethodHead, path, h, mw)
}

// Options registers h for OPTIONS requests. Without an explicit OPTIONS
// route the server answers OPTIONS automatically with 204 and an Allow
// header.
func (s *Server) Options(path string, h HandlerFunc, mw ...MiddlewareFunc) {
	s.handle(nil, http.MethodOptions, path, h, mw)
}

// handle validates and registers one route. g is nil for routes
// registered on the Server itself, where an empty path panics; on a group
// "" means the group's own path, exactly like "/". The stored handler is
// h wrapped, from the outside in, by the group's middleware (parents
// first), the per-route middleware and the built-in panic recovery.
// Global middleware is applied at dispatch time.
func (s *Server) handle(g *Group, method, path string, h HandlerFunc, mw []MiddlewareFunc) {
	if path == "" {
		if g == nil {
			panic(fmt.Sprintf("web: invalid route path for %s: path must not be empty (use \"/\" for the root)", method))
		}
		// A group's own path: g.Get("", h) is g.Get("/", h).
		path = "/"
	}
	if path[0] != '/' {
		panic(fmt.Sprintf("web: invalid route path %q for %s: must start with '/'", path, method))
	}
	if h == nil {
		panic(fmt.Sprintf("web: handler for %s %q must not be nil", strings.ToUpper(method), path))
	}
	mustMiddleware("route "+method+" "+path, mw)

	s.regMu.Lock()
	defer s.regMu.Unlock()
	full := path
	var stack []MiddlewareFunc
	if g != nil {
		full = g.prefix + path
		stack = g.middleware()
	}
	stack = append(stack, mw...)
	stack = append(stack, Recovery())
	if err := s.router.add(method, full, chain(h, stack...)); err != nil {
		panic(err.Error())
	}
}

// ServeHTTP implements http.Handler. It resolves the request to a route
// handler, or to one of the framework fallbacks (404 Not Found, 405 Method
// Not Allowed with an Allow header, automatic 204 for OPTIONS), runs it
// inside the global middleware chain and turns any returned error into a
// response through the error handler. HEAD requests fall back to the GET
// route and always have their body suppressed.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	escaped := r.URL.EscapedPath()
	if escaped == "" {
		escaped = "/"
	}
	method := strings.ToUpper(r.Method)

	var hw *headWriter
	if method == http.MethodHead {
		hw = newHeadWriter(w)
		w = hw
	}

	s.regMu.RLock()
	target, params := s.router.resolve(method, escaped)
	if target == nil && method == http.MethodHead {
		target, params = s.router.resolve(http.MethodGet, escaped)
	}
	if target == nil {
		target = fallback(method, s.router.methodsFor(escaped))
	}
	global, onError := s.global, s.onError
	s.regMu.RUnlock()
	if global == nil {
		global = s.globalChain()
	}

	c := newContext(w, r, s)
	if !c.trustProxy && !s.proxyWarned.Load() {
		s.warnUntrustedProxyHeader(r, c.clientIPHeader)
	}
	c.params = params
	c.target = target
	c.onError = onError
	s.dispatch(c, global)

	if hw != nil {
		hw.finish()
	}
}

// warnUntrustedProxyHeader logs, once per Server, that a request carries
// a client IP header from a proxy while TrustProxyHeaders is false. The
// header value is not logged.
func (s *Server) warnUntrustedProxyHeader(r *http.Request, configured string) {
	name := ""
	switch {
	case r.Header.Get(DefaultClientIPHeader) != "":
		name = DefaultClientIPHeader
	case configured != "" && r.Header.Get(configured) != "":
		name = http.CanonicalHeaderKey(configured)
	default:
		return
	}
	if !s.proxyWarned.CompareAndSwap(false, true) {
		return
	}
	s.logger().Warn("web: request carries a proxy client IP header but Server.TrustProxyHeaders is false; "+
		"client IPs (and rate limits) see the proxy address. Behind a trusted reverse proxy that sets this header, "+
		"enable TrustProxyHeaders; if the server is exposed directly, clients sent the header themselves and this can be ignored",
		"header", name,
	)
}

// dispatchTarget is the innermost link of the global chain: it runs the
// handler ServeHTTP selected for this request.
func dispatchTarget(c *Context) error { return c.target(c) }

// fallback returns the framework handler for a request that matched no
// route: an automatic 204 for OPTIONS on a known path, 405 with an Allow
// header for other methods on a known path, and 404 otherwise.
func fallback(method string, allowed []string) HandlerFunc {
	if len(allowed) == 0 {
		return func(c *Context) error { return NotFound("") }
	}
	if method == http.MethodOptions {
		allow := append(allowed, http.MethodOptions)
		sort.Strings(allow)
		header := strings.Join(allow, ", ")
		return func(c *Context) error {
			c.SetHeader("Allow", header)
			return c.NoContent()
		}
	}
	header := strings.Join(allowed, ", ")
	return func(c *Context) error {
		c.SetHeader("Allow", header)
		return NewHTTPError(http.StatusMethodNotAllowed, "")
	}
}

// dispatch runs the global chain and routes a returned error to the error
// handler. A deferred recover is the safety net for panics raised in
// global middleware (handlers are already wrapped in Recovery): the panic
// is logged and rendered as a 500 through the error handler, so the server
// always keeps running.
func (s *Server) dispatch(c *Context, global HandlerFunc) {
	defer func() {
		if r := recover(); r != nil {
			if isAbortPanic(r) {
				panic(r)
			}
			s.logger().Error("web: panic recovered in middleware",
				"method", c.Method(),
				"path", c.Path(),
				"panic", r,
				"stack", string(debug.Stack()),
			)
			c.handleError(NewHTTPError(http.StatusInternalServerError, ""))
		}
	}()
	if err := global(c); err != nil {
		c.handleError(err)
	}
}

// defaultOnError maps handler errors to responses through AsHTTPError
// (*HTTPError first, then the body sentinels) and answers everything else
// with a logged 500.
func defaultOnError(c *Context, err error) {
	if httpErr, ok := AsHTTPError(err); ok {
		_ = c.JSON(httpErr.Code, errorEnvelope{Error: statusText(httpErr.Code, httpErr.Message)})
		return
	}
	c.logger().Error("web: request handling error",
		"method", c.Method(),
		"path", c.Path(),
		"error", err,
	)
	code := http.StatusInternalServerError
	_ = c.JSON(code, errorEnvelope{Error: statusText(code, "")})
}
