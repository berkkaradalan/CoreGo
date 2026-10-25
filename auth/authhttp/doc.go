// Package authhttp exposes the CoreGo authentication service (package
// auth) as JSON routes on the CoreGo web framework, and provides the
// middleware that protects an application's own routes.
//
// The facade mounts everything automatically (corego.Core.Server). On a
// plain web.Server it takes three lines:
//
//	h := authhttp.New(service, authhttp.Config{})
//	server := web.New()
//	h.Mount(server) // routes under /auth
//
// Protect your own routes with RequireAuth and read the signed-in user
// with auth.MustUser:
//
//	api := server.Group("/api", h.RequireAuth())
//	api.Get("/hello", func(c *web.Context) error {
//		user := auth.MustUser(c)
//		return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
//	})
//
// Routes that serve anonymous visitors too use OptionalAuth and read the
// user with auth.UserFromContext. For net/http handlers and other
// routers use Middleware instead of RequireAuth.
//
// # Routes
//
// Paths are relative to Config.BasePath ("/auth" by default). "auth"
// marks routes behind RequireAuth, "RL" routes that are rate limited per
// client IP (10 requests per minute by default, one independent limiter
// per route, IPv6 clients grouped by /64). POST /signin,
// /forgot-password and /verify-email/request are additionally limited
// per email address with the same limit, for registered and unknown
// addresses alike (see RateLimitConfig):
//
//	POST   /signup                RL    {name, email, password, image?, remember_me?}
//	                                    -> 201 {user, token?, expires_at?, verification_required}
//	POST   /signin                RL    {email, password, remember_me?} -> 200 {user, token?, expires_at}
//	POST   /signout               auth  -> 204 (clears the cookie)
//	GET    /session               auth  -> 200 {user, session}
//	GET    /sessions              auth  -> 200 {sessions: [...]}
//	DELETE /sessions              auth  -> 204 (revokes all, clears the cookie)
//	DELETE /sessions/:id          auth  -> 204, 404 for unknown or foreign IDs
//	POST   /verify-email/request  RL    {email} -> 202 {message} (always)
//	POST   /verify-email          RL    {email, token} -> 204
//	GET    /verify-email          RL    ?email&token -> 303 to VerifyEmailRedirectURL, or an HTML page
//	POST   /forgot-password       RL    {email} -> 202 {message} (always)
//	POST   /reset-password        RL    {email, token, new_password} -> 204
//	POST   /change-password       auth RL {current_password, new_password} -> 204 (clears the cookie)
//	POST   /change-email          auth RL {new_email, current_password} -> 200 {user}
//	GET    /me                    auth  -> 200 {user}
//	PATCH  /me                    auth  {name?, image?} -> 200 {user}
//	DELETE /me                    auth RL {password} -> 204 (clears the cookie)
//
// Token and expires_at are omitted from the sign-up response when the
// service requires a verified email address (auth.Config
// RequireVerifiedEmail); verification_required is then true and no
// session or cookie is created. Token alone is omitted from both
// responses when CookieConfig.OmitTokenFromBody is set. Every auth response carries
// "Cache-Control: no-store".
//
// # Session tokens
//
// The raw session token is returned exactly once, in the token field of
// the sign-up and sign-in responses. Clients send it as
// "Authorization: Bearer <token>". With Config.Cookie enabled the token
// is also set as an HttpOnly cookie (SameSite=Lax by default) and read
// when no Authorization header is present; see Handlers.Token. The JSON
// body still carries the token then, so scripts in the page can read
// it; set CookieConfig.OmitTokenFromBody to keep it in the cookie only.
//
// Browsers attach cookies to requests other sites trigger, so
// cookie-authenticated requests with a method other than GET, HEAD and
// OPTIONS must come from the same site (judged by the Sec-Fetch-Site and
// Origin headers) or from one of Config.TrustedOrigins; others are
// answered 403. Requests with an Authorization header are never
// affected.
//
// # Errors
//
// Errors are JSON bodies of the form {"error": "<message>"}. The auth
// service's sentinel errors map to stable statuses and messages: 401 for
// invalid credentials and missing, unknown or expired sessions, 403 for
// banned or unverified accounts and blocked cross-site requests, 409
// for conflicts (including an account modified concurrently), 400 for
// validation (such as an over-long name or an image that is not an
// http(s) URL) and token errors, 400/413/415 for malformed, oversized or non-JSON
// bodies and 429 when a rate limit is exceeded (with Retry-After).
// Unknown errors propagate to the web error handler, which logs them and
// answers 500.
package authhttp
