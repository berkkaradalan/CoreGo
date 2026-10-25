package corego

import (
	"os"
	"slices"

	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/internal/console"
	"github.com/berkkaradalan/CoreGo/web"
)

// corsWildcard is the CORS origin that allows every origin.
const corsWildcard = "*"

// ServerOptions configures the web server returned by Core.Server.
type ServerOptions struct {
	// CORS enables Cross-Origin Resource Sharing with this
	// configuration. When nil, the CORS_ALLOWED_ORIGINS environment
	// variable (a comma separated list of origins) enables it for those
	// origins with credentials (cookies) allowed and the Content-Type
	// and Authorization request headers; when that is unset too, CORS
	// stays off. A CORS_ALLOWED_ORIGINS containing "*" allows every
	// origin WITHOUT credentials instead (browsers never send cookies
	// cross-origin to a wildcard, so only the Authorization header
	// works) and New logs a warning.
	//
	// The explicit origins (every entry except "*") of this
	// configuration or of CORS_ALLOWED_ORIGINS also become the auth
	// routes' trusted origins for cookie-authenticated requests, unless
	// AuthOptions.HTTP.TrustedOrigins is set; see AuthOptions.HTTP.
	//
	// web.CORS panics on AllowCredentials combined with the "*" origin,
	// so such a configuration makes Server panic.
	CORS *web.CORSConfig

	// TrustProxyHeaders sets web.Server.TrustProxyHeaders: the client IP
	// used for logging and rate limiting is read from X-Forwarded-For.
	// Enable it only behind a reverse proxy you control. nil reads the
	// TRUST_PROXY_HEADERS environment variable (default false); an
	// invalid value there makes New return an error.
	TrustProxyHeaders *bool

	// DisableRequestLog turns off the one-line-per-request log.
	DisableRequestLog bool

	// DisableBanner turns off the startup summary Run prints: the
	// address, the connected services and every registered route. By
	// default the banner is printed when standard error is a terminal;
	// the COREGO_BANNER environment variable forces it on (true) or off
	// (false).
	DisableBanner bool
}

// Server returns the application's web server, building it on the first
// call; later calls return the same *web.Server, so it is safe to call
// Server from anywhere. Register routes on it, then start it with Run
// (or serve it yourself: it is an http.Handler).
//
// The server comes with, in this order of global middleware:
//
//  1. web.RequestLogger (unless ServerOptions.DisableRequestLog), so
//     every request is logged, including CORS preflights and the
//     framework's 404, 405 and OPTIONS answers;
//  2. web.CORS when CORS is configured (see ServerOptions.CORS), so
//     preflights are answered before any route middleware runs;
//
// and, when auth is configured, the auth routes mounted under
// AuthOptions.HTTP.BasePath ("/auth" by default). Its logs go to
// Config.Logger.
func (c *Core) Server() *web.Server {
	c.serverOnce.Do(func() {
		c.server = c.buildServer()
	})
	return c.server
}

// buildServer assembles the server returned by Server.
func (c *Core) buildServer() *web.Server {
	s := web.New()
	s.Logger = c.logger()

	s.TrustProxyHeaders = c.trustProxy

	opts := c.cfg.Server
	if !opts.DisableRequestLog {
		s.Use(web.RequestLogger(c.logger()))
	}
	if c.cors != nil {
		s.Use(web.CORS(*c.cors))
	}

	if c.authHandlers != nil {
		c.authHandlers.Mount(s)
	}
	return s
}

// corsConfig returns a copy of ServerOptions.CORS, or the configuration
// derived from CORS_ALLOWED_ORIGINS, or nil when CORS is off. New calls
// it once; a "*" in CORS_ALLOWED_ORIGINS selects a wildcard
// configuration without credentials and logs a warning.
func (c *Core) corsConfig() *web.CORSConfig {
	if c.cfg.Server.CORS != nil {
		cors := *c.cfg.Server.CORS
		return &cors
	}
	origins := c.Env.Strings("CORS_ALLOWED_ORIGINS")
	if len(origins) == 0 {
		return nil
	}
	headers := []string{"Content-Type", "Authorization"}
	if slices.Contains(origins, corsWildcard) {
		c.logger().Warn("corego: CORS_ALLOWED_ORIGINS contains \"*\": every origin may call the API, " +
			"but without credentials, so browsers on other origins cannot use the session cookie " +
			"(the Authorization header still works); list the allowed origins instead to allow cookies")
		return &web.CORSConfig{
			AllowedOrigins: []string{corsWildcard},
			AllowedHeaders: headers,
		}
	}
	return &web.CORSConfig{
		AllowedOrigins:   origins,
		AllowedHeaders:   headers,
		AllowCredentials: true,
	}
}

// RequireAuth returns the middleware that only lets requests with a
// valid session through and stores the signed-in user in the request
// context, where handlers read it with auth.MustUser(c). It is the same
// check the mounted auth routes use: 401 for a missing, unknown or
// expired session, 403 for a banned (or, with RequireVerifiedEmail, an
// unverified) user.
//
//	api := core.Server().Group("/api", core.RequireAuth())
//
// RequireAuth panics when auth is not configured (Config.Auth is nil):
// protecting routes without an auth service is a programming error.
func (c *Core) RequireAuth() web.MiddlewareFunc {
	if c == nil || c.authHandlers == nil {
		panic("corego: RequireAuth needs the auth service; enable it with corego.Config{Auth: &corego.AuthOptions{}}")
	}
	return c.authHandlers.RequireAuth()
}

// OptionalAuth returns the middleware for routes that serve signed-in
// and anonymous visitors alike: with a valid session it stores the user
// in the request context exactly like RequireAuth, otherwise (no token,
// an unknown or expired session, a banned or unverified user, or a
// cookie request that fails the cross-site check) the request continues
// anonymously, never with a 401. Handlers read the user with
// auth.UserFromContext:
//
//	app.Get("/", func(c *web.Context) error {
//		if user, ok := auth.UserFromContext(c); ok {
//			return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
//		}
//		return c.JSON(http.StatusOK, map[string]string{"hello": "guest"})
//	}, core.OptionalAuth())
//
// Store failures still answer 500. OptionalAuth panics when auth is not
// configured (Config.Auth is nil), like RequireAuth.
func (c *Core) OptionalAuth() web.MiddlewareFunc {
	if c == nil || c.authHandlers == nil {
		panic("corego: OptionalAuth needs the auth service; enable it with corego.Config{Auth: &corego.AuthOptions{}}")
	}
	return c.authHandlers.OptionalAuth()
}

// AuthHandlers returns the HTTP layer of the auth service, the same
// instance whose routes Server mounts, for advanced use such as
// protecting plain net/http handlers with its Middleware or reading the
// session token with Token. It returns nil when auth is not configured.
func (c *Core) AuthHandlers() *authhttp.Handlers {
	if c == nil {
		return nil
	}
	return c.authHandlers
}

// Run serves Server on ":" + PORT (Env.Port) until the process receives
// SIGINT or SIGTERM, shuts the server down gracefully and then calls
// Close. Before serving it prints the startup banner (the address, the
// connected services and every registered route, see
// ServerOptions.DisableBanner), or logs one "corego: starting server"
// line when the banner is off. It returns the error that stopped the server (for example a
// port already in use) or, when the server stopped cleanly, the error of
// Close - which means nil after a clean Ctrl+C, so check the error
// before passing it to log.Fatal:
//
//	if err := core.Run(); err != nil {
//		log.Fatal(err)
//	}
func (c *Core) Run() error {
	addr := ":" + c.Env.Port
	if c.showBanner() {
		c.writeBanner(os.Stderr, console.NewPalette(console.ColorEnabled(os.Stderr, envLookup(c.Env))), addr)
	} else {
		c.logger().Info("corego: starting server", "addr", addr, "url", c.Env.AppURL)
	}
	runErr := c.Server().Run(addr)
	closeErr := c.Close()
	if runErr != nil {
		if closeErr != nil {
			c.logger().Error("corego: close after the server stopped", "error", closeErr)
		}
		return runErr
	}
	return closeErr
}
