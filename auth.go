package corego

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/auth/mongostore"
	"github.com/berkkaradalan/CoreGo/auth/pgstore"
	"github.com/berkkaradalan/CoreGo/mail"
)

// Auth store names accepted by AuthOptions.Store and the
// COREGO_AUTH_STORE environment variable.
const (
	// StoreMemory keeps auth records in process memory. Development and
	// tests only: the data does not survive a restart.
	StoreMemory = "memory"
	// StoreMongo persists auth records in the connected MongoDB.
	StoreMongo = "mongo"
	// StorePostgres persists auth records in the connected PostgreSQL.
	StorePostgres = "postgres"
)

// storeSetupTimeout bounds the index creation (MongoDB) or schema
// migration (PostgreSQL) New runs for a database backed auth store.
const storeSetupTimeout = 30 * time.Second

// AuthOptions enables and configures the authentication service and its
// HTTP routes. The zero value is ready to use.
type AuthOptions struct {
	// Service configures the auth service: session and token lifetimes,
	// the password policy, mandatory email verification and the mailer.
	// Service.Logger defaults to Config.Logger.
	//
	// Service.RequireVerifiedEmail is also turned on by
	// COREGO_REQUIRE_VERIFIED_EMAIL=true: the effective setting is the
	// code value OR the environment value, so the environment can turn
	// it on but never off.
	//
	// When Service.Mailer is nil, the facade builds an
	// auth.DefaultMailer: over MailSender when it is set, otherwise over
	// the sender the COREGO_MAIL environment variable selects:
	//
	//   - empty (automatic): SMTP when SMTP_HOST is set, otherwise no
	//     email and a startup warning;
	//   - "smtp": SMTP; New returns an error when SMTP_HOST is not set;
	//   - "log": development mode, a mail.LogSender on Config.Logger
	//     that logs every email instead of sending it, with its links
	//     at Info level (New logs a warning: the links contain live
	//     tokens);
	//   - "none": no email and no warning.
	//
	// The DefaultMailer's verification link is APP_URL + the auth base
	// path + "/verify-email" and its password reset link is
	// COREGO_RESET_PASSWORD_URL, or APP_URL + "/reset-password".
	Service auth.Config

	// MailSender delivers the auth emails through the facade's
	// auth.DefaultMailer, with the links derived as described for
	// Service. Use it to send through a non-SMTP backend, or to read the
	// emails locally with mail.NewLogSender (development only: it logs
	// live tokens at Debug level). It is ignored when Service.Mailer is
	// set and takes precedence over COREGO_MAIL and SMTP_HOST.
	MailSender mail.Sender

	// Store selects where users, sessions and tokens are kept:
	// StoreMemory, StoreMongo or StorePostgres. Empty reads the
	// COREGO_AUTH_STORE environment variable; when that is empty too,
	// the first connected database is used (MongoDB, then PostgreSQL)
	// and, without a database, the in-memory store with a logged
	// warning. MongoDB indexes and PostgreSQL migrations are applied by
	// New. Ignored when CustomStore is set.
	Store string

	// TablePrefix is the prefix of the auth tables (PostgreSQL) or
	// collections (MongoDB) of the store New creates, passed on as
	// pgstore.Options.TablePrefix or mongostore.Options.CollectionPrefix.
	// Empty reads the COREGO_AUTH_TABLE_PREFIX environment variable;
	// when that is empty too, the stores' default "corego_" applies.
	// Only lowercase ASCII letters, digits and underscores are allowed;
	// New returns the store's error for anything else. The memory store
	// and CustomStore ignore it.
	TablePrefix string

	// CustomStore, when non-nil, is used as the auth store as-is and
	// takes precedence over Store, COREGO_AUTH_STORE and the automatic
	// selection (TablePrefix is ignored too). Use it for your own
	// auth.Store implementation or a store built with non-default
	// options. The caller prepares it: New neither creates MongoDB
	// indexes (mongostore.Store.EnsureIndexes) nor runs PostgreSQL
	// migrations (pgstore.Store.Migrate) for a custom store.
	CustomStore auth.Store

	// HTTP configures the auth routes mounted by Core.Server and the
	// middleware returned by Core.RequireAuth and Core.OptionalAuth.
	// The facade fills some fields from the environment:
	//
	//   - COREGO_AUTH_COOKIE=true turns Cookie.Enabled on (code OR
	//     environment, like COREGO_REQUIRE_VERIFIED_EMAIL);
	//   - with Cookie.Enabled and an APP_URL starting with "https://",
	//     Cookie.Secure is forced to true; with cookies enabled but not
	//     Secure, New logs a warning unless APP_URL's host is localhost,
	//     127.0.0.1 or ::1;
	//   - an empty VerifyEmailRedirectURL reads
	//     COREGO_VERIFY_EMAIL_REDIRECT_URL;
	//   - an empty TrustedOrigins takes the explicit origins of the CORS
	//     configuration (ServerOptions.CORS, or CORS_ALLOWED_ORIGINS
	//     when that is nil), skipping "*" and any entry that is not an
	//     http(s) origin (logged), so a frontend allowed by CORS can also
	//     send cookie-authenticated requests.
	HTTP authhttp.Config
}

// initAuth starts the auth service, prepares its HTTP layer and logs the
// "corego: auth ready" summary.
func (c *Core) initAuth(opts *AuthOptions) error {
	envOpts, err := readAuthEnv(c.Env)
	if err != nil {
		return err
	}

	// Part of the HTTP configuration may come from the environment, so
	// an invalid value is reported as an error instead of authhttp.New's
	// panic.
	httpCfg := opts.HTTP
	httpCfg.Cookie.Enabled = httpCfg.Cookie.Enabled || envOpts.cookie
	httpCfg = c.authHTTPConfig(httpCfg)
	if err := httpCfg.Validate(); err != nil {
		return fmt.Errorf("corego: invalid AuthOptions.HTTP: %w", err)
	}
	// Validated above, so the base path resolves.
	basePath, _ := httpCfg.ResolvedBasePath()
	c.warnInsecureCookie(httpCfg.Cookie)

	svcCfg := opts.Service
	if svcCfg.Logger == nil {
		svcCfg.Logger = c.logger()
	}
	svcCfg.RequireVerifiedEmail = svcCfg.RequireVerifiedEmail || envOpts.requireVerifiedEmail
	mailKind := mailKindCustom
	if svcCfg.Mailer == nil {
		sender, kind, err := c.authMailSender(opts.MailSender, envOpts.mailMode)
		if err != nil {
			return err
		}
		mailKind = kind
		if sender != nil {
			svcCfg.Mailer = auth.NewDefaultMailer(sender, c.defaultMailerConfig(basePath))
		}
	}

	store, err := c.authStore(opts, envOpts.tablePrefix)
	if err != nil {
		return err
	}

	service, err := auth.New(svcCfg, store.store)
	if err != nil {
		return fmt.Errorf("corego: start auth service: %w", err)
	}
	c.Auth = service
	c.authHandlers = authhttp.New(service, httpCfg)
	c.logAuthReady(store, basePath, mailKind, svcCfg.RequireVerifiedEmail, httpCfg.Cookie.Enabled)
	return nil
}

// Store kinds reported in the "corego: auth ready" startup log line,
// besides StoreMemory, StoreMongo and StorePostgres.
const storeKindCustom = "custom"

// resolvedStore is the auth store selected by authStore, with what the
// startup log reports about it.
type resolvedStore struct {
	store auth.Store
	// kind is StoreMemory, StoreMongo, StorePostgres or storeKindCustom.
	kind string
	// prefix is the effective table or collection prefix of a database
	// store; empty for the memory and custom stores.
	prefix string
}

// logAuthReady logs the resolved auth setup once New has succeeded, so
// the startup log shows which store, mailer and transports are in use.
func (c *Core) logAuthReady(store resolvedStore, basePath, mailKind string, requireVerifiedEmail, cookie bool) {
	if basePath == "" {
		basePath = "/"
	}
	attrs := []any{"store", store.kind}
	if store.prefix != "" {
		attrs = append(attrs, "table_prefix", store.prefix)
	}
	attrs = append(attrs,
		"base_path", basePath,
		"mail", mailKind,
		"require_verified_email", requireVerifiedEmail,
		"cookie", cookie,
	)
	c.logger().Info("corego: auth ready", attrs...)
	c.authInfo = &authSummary{
		store:                store.kind,
		prefix:               store.prefix,
		basePath:             basePath,
		mail:                 mailKind,
		requireVerifiedEmail: requireVerifiedEmail,
		cookie:               cookie,
	}
}

// authHTTPConfig applies the environment driven defaults to the HTTP
// configuration of the auth routes.
func (c *Core) authHTTPConfig(cfg authhttp.Config) authhttp.Config {
	if cfg.Cookie.Enabled && strings.HasPrefix(strings.ToLower(c.Env.AppURL), "https://") {
		cfg.Cookie.Secure = true
	}
	if cfg.VerifyEmailRedirectURL == "" {
		cfg.VerifyEmailRedirectURL = c.Env.GetOrDefault("COREGO_VERIFY_EMAIL_REDIRECT_URL", "")
	}
	if len(cfg.TrustedOrigins) == 0 {
		cfg.TrustedOrigins = c.trustedOriginsFromCORS()
	}
	return cfg
}

// trustedOriginsFromCORS returns the explicit origins of the resolved
// CORS configuration that are valid authhttp trusted origins. "*" is
// skipped silently; other unusable entries (such as "null") are skipped
// with a warning.
func (c *Core) trustedOriginsFromCORS() []string {
	if c.cors == nil {
		return nil
	}
	var trusted []string
	for _, origin := range c.cors.AllowedOrigins {
		if origin == corsWildcard {
			continue
		}
		if err := (authhttp.Config{TrustedOrigins: []string{origin}}).Validate(); err != nil {
			c.logger().Warn("corego: CORS origin not trusted for cookie-authenticated auth requests; "+
				"set AuthOptions.HTTP.TrustedOrigins to choose the trusted origins explicitly",
				"origin", origin, "reason", err)
			continue
		}
		trusted = append(trusted, origin)
	}
	return trusted
}

// warnInsecureCookie logs a warning when the session cookie is enabled
// without the Secure attribute outside local development: browsers then
// send the session token over plain HTTP.
func (c *Core) warnInsecureCookie(cookie authhttp.CookieConfig) {
	if !cookie.Enabled || cookie.Secure || isLocalURL(c.Env.AppURL) {
		return
	}
	c.logger().Warn("corego: the session cookie is not Secure, so browsers also send it over plain HTTP; "+
		"use an https APP_URL or set AuthOptions.HTTP.Cookie.Secure in production",
		"app_url", c.Env.AppURL)
}

// isLocalURL reports whether raw is a URL whose host is localhost,
// 127.0.0.1 or ::1.
func isLocalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// defaultMailerConfig derives the links of the DefaultMailer emails from
// APP_URL, the resolved auth base path (authhttp.Handlers.BasePath: ""
// at the root, otherwise for example "/auth") and
// COREGO_RESET_PASSWORD_URL.
func (c *Core) defaultMailerConfig(basePath string) auth.DefaultMailerConfig {
	return auth.DefaultMailerConfig{
		VerifyEmailURL:   c.Env.AppURL + basePath + "/verify-email",
		ResetPasswordURL: c.Env.GetOrDefault("COREGO_RESET_PASSWORD_URL", c.Env.AppURL+"/reset-password"),
	}
}

// authStore resolves and prepares the auth store: CustomStore, then the
// explicit name, then COREGO_AUTH_STORE, then the first connected
// database, then memory. envPrefix (COREGO_AUTH_TABLE_PREFIX) is the
// table prefix when AuthOptions.TablePrefix is empty.
func (c *Core) authStore(opts *AuthOptions, envPrefix string) (resolvedStore, error) {
	if opts.CustomStore != nil {
		return resolvedStore{store: opts.CustomStore, kind: storeKindCustom}, nil
	}
	prefix, prefixSource := opts.TablePrefix, "AuthOptions.TablePrefix"
	if prefix == "" {
		prefix, prefixSource = envPrefix, envAuthTablePrefix
	}

	name := opts.Store
	source := "AuthOptions.Store"
	if strings.TrimSpace(name) == "" {
		name = c.Env.GetOrDefault("COREGO_AUTH_STORE", "")
		source = "COREGO_AUTH_STORE"
	}
	name = strings.ToLower(strings.TrimSpace(name))

	if name == "" {
		switch {
		case c.Mongo != nil:
			name = StoreMongo
		case c.Postgres != nil:
			name = StorePostgres
		default:
			c.logger().Warn("corego: no database is connected, so auth uses the in-memory store; " +
				"users and sessions are lost on restart (configure MongoDB or PostgreSQL for production)")
			return resolvedStore{store: auth.NewMemoryStore(), kind: StoreMemory}, nil
		}
	}

	switch name {
	case StoreMemory:
		return resolvedStore{store: auth.NewMemoryStore(), kind: StoreMemory}, nil
	case StoreMongo:
		return c.mongoStore(prefix, prefixSource)
	case StorePostgres:
		return c.postgresStore(prefix, prefixSource)
	default:
		return resolvedStore{}, fmt.Errorf("corego: unknown auth store %q in %s: valid values are %q, %q and %q",
			name, source, StoreMemory, StoreMongo, StorePostgres)
	}
}

// mongoStore creates the MongoDB auth store with the given collection
// prefix (taken from prefixSource) and its indexes.
func (c *Core) mongoStore(prefix, prefixSource string) (resolvedStore, error) {
	if c.Mongo == nil {
		return resolvedStore{}, fmt.Errorf("corego: auth store %q needs a MongoDB connection: set MONGODB_CONNECTION_URL or Config.Mongo", StoreMongo)
	}
	store, err := mongostore.NewStore(c.Mongo, mongostore.Options{CollectionPrefix: prefix})
	if err != nil {
		return resolvedStore{}, storeCreateError("MongoDB", prefix, prefixSource, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeSetupTimeout)
	defer cancel()
	if err := store.EnsureIndexes(ctx); err != nil {
		return resolvedStore{}, fmt.Errorf("corego: create MongoDB auth indexes: %w", err)
	}
	if prefix == "" {
		prefix = mongostore.DefaultCollectionPrefix
	}
	return resolvedStore{store: store, kind: StoreMongo, prefix: prefix}, nil
}

// postgresStore creates the PostgreSQL auth store with the given table
// prefix (taken from prefixSource) and migrates its schema.
func (c *Core) postgresStore(prefix, prefixSource string) (resolvedStore, error) {
	if c.Postgres == nil {
		return resolvedStore{}, fmt.Errorf("corego: auth store %q needs a PostgreSQL connection: set POSTGRES_CONNECTION_URL or Config.Postgres", StorePostgres)
	}
	store, err := pgstore.NewStore(c.Postgres.Pool(), pgstore.Options{TablePrefix: prefix})
	if err != nil {
		return resolvedStore{}, storeCreateError("PostgreSQL", prefix, prefixSource, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeSetupTimeout)
	defer cancel()
	if err := store.Migrate(ctx); err != nil {
		return resolvedStore{}, fmt.Errorf("corego: migrate PostgreSQL auth schema: %w", err)
	}
	return resolvedStore{store: store, kind: StorePostgres, prefix: store.TablePrefix()}, nil
}

// storeCreateError wraps the error of a store constructor. For a
// non-empty prefix it names where the prefix came from
// (AuthOptions.TablePrefix or COREGO_AUTH_TABLE_PREFIX), because an
// invalid prefix is the constructor's usual failure.
func storeCreateError(database, prefix, prefixSource string, err error) error {
	if prefix == "" {
		return fmt.Errorf("corego: create %s auth store: %w", database, err)
	}
	return fmt.Errorf("corego: create %s auth store with the table prefix from %s: %w", database, prefixSource, err)
}
