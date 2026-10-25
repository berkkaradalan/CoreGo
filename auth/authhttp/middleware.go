package authhttp

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// Token returns the session token carried by r. The Authorization header
// wins: when it holds "Bearer <token>" (scheme matched
// case-insensitively) that token is returned, even if a cookie is
// present too. Otherwise, when the cookie transport is enabled, the
// configured cookie is read. Token reports false when the request
// carries no token.
//
// Token does not apply the cross-site request forgery check that
// RequireAuth, OptionalAuth and Middleware run for cookie-authenticated
// requests.
func (h *Handlers) Token(r *http.Request) (string, bool) {
	token, _, ok := h.token(r)
	return token, ok
}

// token is Token that also reports whether the token came from the
// session cookie (and therefore needs the cross-site check).
func (h *Handlers) token(r *http.Request) (token string, fromCookie, ok bool) {
	if token, ok := bearerToken(r.Header.Get("Authorization")); ok {
		return token, false, true
	}
	if h.cfg.Cookie.Enabled {
		if cookie, err := r.Cookie(h.cfg.Cookie.Name); err == nil && cookie.Value != "" {
			return cookie.Value, true, true
		}
	}
	return "", false, false
}

// bearerToken extracts the token from an Authorization header value of
// the form "Bearer <token>".
func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}

// authenticate resolves the request's session. A token taken from the
// cookie must pass the cross-site check first. Errors are mapped
// *web.HTTPError values for every known failure.
func (h *Handlers) authenticate(r *http.Request) (*auth.Session, *auth.User, error) {
	token, fromCookie, ok := h.token(r)
	if !ok {
		return nil, nil, errMissingToken()
	}
	if fromCookie {
		if reason := h.crossSiteRejection(r); reason != "" {
			logCrossSiteRejection(r, reason)
			return nil, nil, errCrossSite()
		}
	}
	session, user, err := h.service.GetSession(r.Context(), token)
	if err != nil {
		return nil, nil, mapError(err)
	}
	return session, user, nil
}

// RequireAuth returns web middleware that only lets requests with a
// valid session through. It resolves the token with Token, validates it
// with auth.Auth.GetSession and stores the session and user in the
// request context (auth.ContextWithSession), so handlers read them with
// auth.MustUser(c), auth.UserFromContext(c) or auth.SessionFromContext(c):
//
//	api := server.Group("/api", h.RequireAuth())
//	api.Get("/hello", func(c *web.Context) error {
//		user := auth.MustUser(c)
//		return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
//	})
//
// Failures abort the chain with a *web.HTTPError rendered as
// {"error": "..."}: 401 for a missing, unknown or expired token (with a
// "WWW-Authenticate: Bearer" header), 403 for a banned user or, when the
// service requires verified addresses, an unverified one, and 403
// "Cross-site request blocked." for a cookie-authenticated request with
// a method other than GET, HEAD and OPTIONS that fails the cross-site
// request forgery check (see Config.TrustedOrigins). Unknown service
// errors are returned unchanged, so the web error handler logs them and
// answers 500.
func (h *Handlers) RequireAuth() web.MiddlewareFunc {
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(c *web.Context) error {
			session, user, err := h.authenticate(c.Request())
			if err != nil {
				if isUnauthorized(err) {
					c.SetHeader("WWW-Authenticate", "Bearer")
				}
				return err
			}
			r := c.Request()
			c.SetRequest(r.WithContext(auth.ContextWithSession(r.Context(), session, user)))
			return next(c)
		}
	}
}

// Middleware is RequireAuth for plain net/http handlers and routers
// other than the CoreGo web framework:
//
//	mux := http.NewServeMux()
//	mux.Handle("/api/", h.Middleware(apiHandler))
//
// It performs the same checks (the cross-site check for cookie
// authenticated requests included) and stores the same values in the
// request context, so handlers call auth.MustUser(r.Context()). Failures are
// answered with exactly the status codes, headers and JSON bodies of the
// web framework's default error handler; unknown errors are logged with
// slog.Default() and answered 500.
func (h *Handlers) Middleware(next http.Handler) http.Handler {
	if next == nil {
		panic("authhttp: Middleware requires a non-nil http.Handler")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, user, err := h.authenticate(r)
		if err != nil {
			writeHTTPError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.ContextWithSession(r.Context(), session, user)))
	})
}

// OptionalAuth returns web middleware for routes that serve signed-in
// and anonymous visitors alike. When the request carries a valid session
// token it stores the session and user exactly like RequireAuth, so
// handlers read them with auth.UserFromContext(c) and
// auth.SessionFromContext(c):
//
//	server.Get("/", func(c *web.Context) error {
//		if user, ok := auth.UserFromContext(c); ok {
//			return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
//		}
//		return c.JSON(http.StatusOK, map[string]string{"hello": "guest"})
//	}, h.OptionalAuth())
//
// When the token is missing, unknown or expired, belongs to a banned
// (or, when the service requires verified addresses, an unverified)
// user, or comes from the cookie on a request that fails the cross-site
// check RequireAuth applies, the request continues anonymously: nothing
// is stored and no error is returned. The cookie is left untouched.
// Unknown service errors (for example an unreachable store) are
// returned unchanged, so the web error handler logs them and answers
// 500 instead of silently treating a signed-in visitor as anonymous.
func (h *Handlers) OptionalAuth() web.MiddlewareFunc {
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(c *web.Context) error {
			session, user, err := h.authenticate(c.Request())
			if err != nil {
				var he *web.HTTPError
				if errors.As(err, &he) {
					return next(c)
				}
				return err
			}
			r := c.Request()
			c.SetRequest(r.WithContext(auth.ContextWithSession(r.Context(), session, user)))
			return next(c)
		}
	}
}

// isUnauthorized reports whether err renders as a 401.
func isUnauthorized(err error) bool {
	var he *web.HTTPError
	return errors.As(err, &he) && he.Code == http.StatusUnauthorized
}

// currentAuth returns the session and user RequireAuth stored for the
// request. Protected handlers only run behind RequireAuth, so a missing
// value is a wiring bug and reported as an internal error.
func currentAuth(c *web.Context) (*auth.Session, *auth.User, error) {
	session, ok := auth.SessionFromContext(c)
	if !ok {
		return nil, nil, errors.New("authhttp: no session in the request context; the route must be wrapped in RequireAuth")
	}
	return session, auth.MustUser(c), nil
}

// setSessionCookie writes the session token as the configured cookie,
// expiring together with the session. It is a no-op when cookies are
// disabled.
func (h *Handlers) setSessionCookie(c *web.Context, token string, expires time.Time) {
	if !h.cfg.Cookie.Enabled {
		return
	}
	c.SetCookie(h.cookie(token, expires, 0))
}

// clearSessionCookie overwrites the session cookie with an empty value
// that expires immediately, so browsers drop it. It is a no-op when
// cookies are disabled.
func (h *Handlers) clearSessionCookie(c *web.Context) {
	if !h.cfg.Cookie.Enabled {
		return
	}
	c.SetCookie(h.cookie("", time.Unix(0, 0), -1))
}

// cookie builds the configured session cookie. HttpOnly is always set.
func (h *Handlers) cookie(value string, expires time.Time, maxAge int) *http.Cookie {
	cc := h.cfg.Cookie
	return &http.Cookie{
		Name:     cc.Name,
		Value:    value,
		Path:     cc.Path,
		Domain:   cc.Domain,
		Expires:  expires,
		MaxAge:   maxAge,
		Secure:   cc.Secure,
		HttpOnly: true,
		SameSite: cc.SameSite,
	}
}
