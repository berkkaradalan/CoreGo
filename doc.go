// Package corego assembles the CoreGo building blocks - environment
// configuration, database connections, the authentication service, its
// HTTP routes and the web server - behind one facade.
//
// A complete application with sign-up, sign-in and a protected route:
//
//	core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
//	if err != nil {
//		log.Fatal(err)
//	}
//	app := core.Server()
//	api := app.Group("/api", core.RequireAuth())
//	api.Get("/hello", func(c *web.Context) error {
//		user := auth.MustUser(c)
//		return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
//	})
//	if err := core.Run(); err != nil {
//		log.Fatal(err)
//	}
//
// Every piece is optional. New loads the environment (./.env when present
// plus the process environment; Config.Env supplies a snapshot the
// application loaded itself), connects to MongoDB and PostgreSQL only
// when they are configured in Config or discovered in the environment, and
// starts the auth service only when Config.Auth is set. Server builds the
// web server once, with request logging, optional CORS and the auth routes
// already mounted; Run serves it on PORT until SIGINT or SIGTERM and then
// releases everything with Close. RequireAuth protects routes;
// OptionalAuth serves signed-in and anonymous visitors alike.
//
// # Environment
//
// The facade reads these variables (see docs/environment.md for details).
// Values configured in code win over the environment, except that the
// boolean switches can only turn a feature on (code OR environment).
// Invalid values of the COREGO_* auth variables make New return an error.
//
//	APP_URL                           public base URL, used in email links
//	PORT                              listen port of Run (default 8080)
//	MONGODB_CONNECTION_URL            connect to MongoDB when Config.Mongo is nil
//	MONGODB_DATABASE                  MongoDB database name when Config.Mongo is nil
//	POSTGRES_CONNECTION_URL           connect to PostgreSQL when Config.Postgres is nil
//	COREGO_AUTH_STORE                 memory | mongo | postgres when AuthOptions.Store is empty
//	COREGO_AUTH_TABLE_PREFIX          auth table prefix when AuthOptions.TablePrefix is empty
//	COREGO_REQUIRE_VERIFIED_EMAIL     true turns auth.Config.RequireVerifiedEmail on
//	COREGO_AUTH_COOKIE                true turns the session cookie on
//	COREGO_MAIL                       auth email delivery: "" (auto) | smtp | log | none
//	SMTP_HOST, SMTP_PORT,             SMTP delivery of auth emails when no mailer
//	SMTP_USERNAME, SMTP_PASSWORD,     or mail sender is configured in code
//	SMTP_FROM
//	COREGO_RESET_PASSWORD_URL         page the password reset email links to
//	COREGO_VERIFY_EMAIL_REDIRECT_URL  redirect target after email verification
//	CORS_ALLOWED_ORIGINS              CORS origins when ServerOptions.CORS is nil ("*": any origin, without cookies)
//	TRUST_PROXY_HEADERS               trust X-Forwarded-For when ServerOptions.TrustProxyHeaders is nil
package corego
