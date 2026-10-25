package authhttp

import (
	"net/http"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// Handlers is the HTTP layer around an auth.Auth service. It maps the
// service operations onto JSON routes (see the package documentation for
// the full table), translates the service's sentinel errors into HTTP
// responses, rate limits the sensitive routes and optionally transports
// the session token as a cookie.
//
// Create it with New and register the routes with Mount. RequireAuth
// and Middleware protect the application's own routes with the same
// session check; OptionalAuth loads the session on routes that also
// serve anonymous visitors. A Handlers value is safe for concurrent use.
type Handlers struct {
	service *auth.Auth
	cfg     Config

	// trustedOrigins holds the normalized Config.TrustedOrigins.
	trustedOrigins map[string]struct{}
}

// Router is what Mount registers routes on. Both *web.Server and
// *web.Group implement it, so the auth routes can live at the root of a
// server or below any group prefix (with the group's middleware running
// outside the auth middleware).
type Router interface {
	Handle(method, path string, h web.HandlerFunc, mw ...web.MiddlewareFunc)
}

// Compile-time checks that both web types satisfy Router.
var (
	_ Router = (*web.Server)(nil)
	_ Router = (*web.Group)(nil)
)

// New returns Handlers that expose service over HTTP with the given
// configuration. Zero-value Config fields are replaced by the package
// defaults (see the Default* constants).
//
// New panics when service is nil or cfg is invalid: a BasePath that does
// not start with '/' or contains empty, parameter or wildcard segments, a
// negative rate limit, a VerifyEmailRedirectURL that is neither an
// absolute http(s) URL nor an absolute path, or (with cookies enabled) a
// cookie name, path or domain net/http rejects, SameSite=None without
// Secure, Cookie.OmitTokenFromBody without Cookie.Enabled, or a
// TrustedOrigins entry that is not an absolute http(s)
// origin. All of them are programming errors; Config.Validate reports
// them as an error instead.
func New(service *auth.Auth, cfg Config) *Handlers {
	if service == nil {
		panic("authhttp: New requires a non-nil *auth.Auth service")
	}
	h := &Handlers{service: service, cfg: cfg.withDefaults()}
	h.trustedOrigins = make(map[string]struct{}, len(h.cfg.TrustedOrigins))
	for _, origin := range h.cfg.TrustedOrigins {
		h.trustedOrigins[origin] = struct{}{}
	}
	return h
}

// BasePath returns the resolved prefix Mount registers the routes under:
// "" when they are mounted at the root (Config.BasePath "/"), otherwise
// the normalized Config.BasePath without a trailing slash, for example
// DefaultBasePath ("/auth") when Config.BasePath is empty. The prefix of
// a web.Group the routes are mounted on is not included. Use it to build
// links to the routes, such as BasePath() + "/verify-email".
func (h *Handlers) BasePath() string {
	return h.cfg.BasePath
}

// RouteInfo describes one auth route, for listings such as the startup
// banner of the corego facade.
type RouteInfo struct {
	// Method is the HTTP method, for example "POST".
	Method string
	// Path is the full path under BasePath, for example "/auth/signup".
	// The prefix of a web.Group the routes are mounted on is not
	// included.
	Path string
	// RequiresSession reports whether the route needs a valid session
	// token (it is wrapped in RequireAuth).
	RequiresSession bool
}

// Routes returns every auth route in registration order, sign-up first,
// with its full path under BasePath.
func (h *Handlers) Routes() []RouteInfo {
	routes := h.routes(emailLimiters{})
	out := make([]RouteInfo, 0, len(routes))
	for _, rt := range routes {
		out = append(out, RouteInfo{Method: rt.method, Path: h.cfg.BasePath + rt.path, RequiresSession: rt.protected})
	}
	return out
}

// route describes one endpoint, relative to BasePath.
type route struct {
	method      string
	path        string
	protected   bool // wrapped in RequireAuth
	rateLimited bool // wrapped in a dedicated per client IP limiter
	handler     web.HandlerFunc
}

// emailLimiters holds the per email address limiters of one Mount call,
// one per route limited by address. All fields are nil when rate
// limiting is disabled.
type emailLimiters struct {
	signIn             *web.RateLimiter
	forgotPassword     *web.RateLimiter
	verifyEmailRequest *web.RateLimiter
}

// newEmailLimiters returns fresh, independent per address limiters, or
// nil limiters when rate limiting is disabled.
func (h *Handlers) newEmailLimiters() emailLimiters {
	if h.cfg.RateLimit.Disabled {
		return emailLimiters{}
	}
	limiter := func() *web.RateLimiter {
		return web.NewRateLimiter(web.RateLimitConfig{
			Limit:  h.cfg.RateLimit.Limit,
			Window: h.cfg.RateLimit.Window,
		})
	}
	return emailLimiters{
		signIn:             limiter(),
		forgotPassword:     limiter(),
		verifyEmailRequest: limiter(),
	}
}

// routes lists every endpoint of the package in registration order. The
// routes limited per email address use the limiters in limits.
func (h *Handlers) routes(limits emailLimiters) []route {
	return []route{
		{http.MethodPost, "/signup", false, true, h.signUp},
		{http.MethodPost, "/signin", false, true, h.signIn(limits.signIn)},
		{http.MethodPost, "/signout", true, false, h.signOut},
		{http.MethodGet, "/session", true, false, h.currentSession},
		{http.MethodGet, "/sessions", true, false, h.listSessions},
		{http.MethodDelete, "/sessions", true, false, h.revokeAllSessions},
		{http.MethodDelete, "/sessions/:id", true, false, h.revokeSession},
		{http.MethodPost, "/verify-email/request", false, true, h.requestEmailVerification(limits.verifyEmailRequest)},
		{http.MethodPost, "/verify-email", false, true, h.verifyEmail},
		{http.MethodGet, "/verify-email", false, true, h.verifyEmailLink},
		{http.MethodPost, "/forgot-password", false, true, h.forgotPassword(limits.forgotPassword)},
		{http.MethodPost, "/reset-password", false, true, h.resetPassword},
		{http.MethodPost, "/change-password", true, true, h.changePassword},
		{http.MethodPost, "/change-email", true, true, h.changeEmail},
		{http.MethodGet, "/me", true, false, h.getMe},
		{http.MethodPatch, "/me", true, false, h.updateMe},
		{http.MethodDelete, "/me", true, true, h.deleteMe},
	}
}

// Mount registers every auth route on r under Config.BasePath (a group's
// prefix comes first). Each route's middleware runs in this order:
// Cache-Control: no-store, then the route's own rate limiter (unless
// RateLimit.Disabled), then RequireAuth on protected routes.
//
// Every Mount call creates fresh, independent rate limiters. The per
// client limiters are keyed by web.ClientKey: the client IP, with IPv6
// addresses grouped by their /64 prefix (enable
// web.Server.TrustProxyHeaders behind a reverse proxy so the real client
// address is used). POST /signin, /forgot-password and
// /verify-email/request are also limited per email address; see
// RateLimitConfig.
//
// Mount panics like web registration does: when r is nil or a route is
// already registered (for example when Mount is called twice on the
// same router).
func (h *Handlers) Mount(r Router) {
	if r == nil {
		panic("authhttp: Mount requires a non-nil Router")
	}
	requireAuth := h.RequireAuth()
	for _, rt := range h.routes(h.newEmailLimiters()) {
		mw := []web.MiddlewareFunc{noStore}
		if rt.rateLimited && !h.cfg.RateLimit.Disabled {
			mw = append(mw, web.RateLimit(web.RateLimitConfig{
				Limit:  h.cfg.RateLimit.Limit,
				Window: h.cfg.RateLimit.Window,
				Key:    web.ClientKey,
			}))
		}
		if rt.protected {
			mw = append(mw, requireAuth)
		}
		path := h.cfg.BasePath + rt.path
		r.Handle(rt.method, path, rt.handler, mw...)
	}
}

// noStore marks every auth response as not cacheable: responses carry
// session tokens, personal data or one-time links.
func noStore(next web.HandlerFunc) web.HandlerFunc {
	return func(c *web.Context) error {
		c.SetHeader("Cache-Control", "no-store")
		return next(c)
	}
}
