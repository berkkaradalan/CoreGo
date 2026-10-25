package corego

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/database"
	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/web"
)

// closeTimeout bounds how long Close waits for auth emails that are
// still being delivered in the background before it closes the database
// connections.
const closeTimeout = 10 * time.Second

// Config configures a Core. Every field is optional; a nil *Config is
// the same as an empty one.
type Config struct {
	// Env is the environment snapshot New reads its settings from. When
	// nil, New loads one with env.Load (./.env when present plus the
	// process environment). Set it when the application loads the
	// environment itself, for example to read its own settings before
	// building this Config, so .env is read only once:
	//
	//	e, err := env.Load()
	//	if err != nil {
	//		log.Fatal(err)
	//	}
	//	core, err := corego.New(&corego.Config{Env: e, Auth: &corego.AuthOptions{}})
	//
	// Core.Env is this same snapshot.
	Env *env.Env

	// Mongo connects to MongoDB. When nil, the facade connects when the
	// MONGODB_CONNECTION_URL environment variable is set, using
	// MONGODB_DATABASE (when set) as the database name.
	Mongo *database.MongoConfig

	// Postgres connects to PostgreSQL. When nil, the facade connects
	// when the POSTGRES_CONNECTION_URL environment variable is set.
	Postgres *database.PostgresConfig

	// Auth enables the authentication service and its HTTP routes. nil
	// leaves authentication off; &AuthOptions{} turns it on with every
	// default.
	Auth *AuthOptions

	// Server configures the web server returned by Core.Server.
	Server ServerOptions

	// Logger receives the facade's startup log (warnings and the
	// "corego: auth ready" summary), the request log, the web server's
	// logs, the emails of COREGO_MAIL=log and, unless
	// AuthOptions.Service.Logger is set, the auth service's logs.
	//
	// nil selects a compact, colored console logger when standard error
	// is a terminal and slog's default handler has not been replaced
	// (NO_COLOR turns the colors off, FORCE_COLOR turns them on), and
	// slog.Default() otherwise, so production log pipelines keep their
	// format.
	Logger *slog.Logger
}

// Core is the root handle of a CoreGo application. It owns the loaded
// environment, the database connections and, when configured, the
// authentication service. Close (or Run, which calls it) releases
// everything.
//
// The exported fields are set by New and must be treated as read-only.
type Core struct {
	// Env is the environment snapshot New read its settings from:
	// Config.Env, or the one New loaded when that is nil.
	Env *env.Env
	// Mongo is the MongoDB connection; nil when MongoDB is not
	// configured.
	Mongo *database.MongoDB
	// Postgres is the PostgreSQL connection; nil when PostgreSQL is not
	// configured.
	Postgres *database.PostgresDB
	// Auth is the authentication service; nil when Config.Auth is nil.
	Auth *auth.Auth

	cfg          Config
	log          *slog.Logger    // resolved by New, see Config.Logger
	cors         *web.CORSConfig // resolved by New; nil when CORS is off
	trustProxy   bool            // resolved by New
	banner       bannerMode      // resolved by New, see ServerOptions.DisableBanner
	authHandlers *authhttp.Handlers
	authInfo     *authSummary // set by initAuth; shown in the startup banner
	mongoURL     string       // the URL MongoDB was connected with

	serverOnce sync.Once
	server     *web.Server

	closeOnce sync.Once
	closeErr  error
}

// New loads the environment (unless Config.Env provides it), connects
// the configured databases and, when Config.Auth is set, starts the
// authentication service on the selected store and logs one
// "corego: auth ready" line at Info level that summarizes the resolved
// auth setup. A nil cfg is valid.
//
// New never connects to anything that was neither configured in cfg nor
// discovered in the environment. It returns an error for a malformed
// .env file, an unreachable database, an unknown or unavailable auth
// store, an invalid AuthOptions.TablePrefix or COREGO_AUTH_TABLE_PREFIX,
// an invalid COREGO_REQUIRE_VERIFIED_EMAIL, COREGO_AUTH_COOKIE or
// COREGO_MAIL value, COREGO_MAIL=smtp without SMTP_HOST, a failed index
// or schema migration and an invalid AuthOptions.HTTP configuration
// (including trusted origins). On error every connection opened so far
// is closed again.
func New(cfg *Config) (*Core, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	e := cfg.Env
	if e == nil {
		loaded, err := env.Load()
		if err != nil {
			return nil, fmt.Errorf("corego: load environment: %w", err)
		}
		e = loaded
	}

	core := &Core{Env: e, cfg: *cfg}
	core.log = resolveLogger(cfg.Logger, e)
	if err := core.init(); err != nil {
		if closeErr := core.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return nil, err
	}
	return core, nil
}

// init resolves the server settings, connects the databases and starts
// the auth service.
func (c *Core) init() error {
	banner, err := readBannerMode(c.Env)
	if err != nil {
		return err
	}
	c.banner = banner
	if c.cfg.Server.DisableBanner {
		c.banner = bannerOff
	}
	c.cors = c.corsConfig()
	if opt := c.cfg.Server.TrustProxyHeaders; opt != nil {
		c.trustProxy = *opt
	} else {
		trust, err := envBool(c.Env, "TRUST_PROXY_HEADERS")
		if err != nil {
			return err
		}
		c.trustProxy = trust
	}
	if err := c.connectMongo(); err != nil {
		return err
	}
	if err := c.connectPostgres(); err != nil {
		return err
	}
	if c.cfg.Auth != nil {
		if err := c.initAuth(c.cfg.Auth); err != nil {
			return err
		}
	}
	return nil
}

// connectMongo connects to MongoDB when Config.Mongo is set or the
// environment provides MONGODB_CONNECTION_URL.
func (c *Core) connectMongo() error {
	mongoCfg := c.cfg.Mongo
	if mongoCfg == nil {
		if c.Env.MongoURL == "" {
			return nil
		}
		mongoCfg = &database.MongoConfig{URL: c.Env.MongoURL, Database: c.Env.MongoDatabase}
	}
	db, err := database.NewMongoDB(*mongoCfg)
	if err != nil {
		return fmt.Errorf("corego: connect to MongoDB: %w", err)
	}
	c.Mongo = db
	c.mongoURL = mongoCfg.URL
	return nil
}

// connectPostgres connects to PostgreSQL when Config.Postgres is set or
// the environment provides POSTGRES_CONNECTION_URL.
func (c *Core) connectPostgres() error {
	pgCfg := c.cfg.Postgres
	if pgCfg == nil {
		if c.Env.PostgresURL == "" {
			return nil
		}
		pgCfg = &database.PostgresConfig{URL: c.Env.PostgresURL}
	}
	db, err := database.NewPostgresDB(*pgCfg)
	if err != nil {
		return fmt.Errorf("corego: connect to PostgreSQL: %w", err)
	}
	c.Postgres = db
	return nil
}

// Close releases everything the Core owns: it waits up to
// 10 seconds for auth emails still being delivered in the
// background, then closes the MongoDB and PostgreSQL connections.
// Errors are joined.
//
// Close is idempotent (later calls return the result of the first) and
// safe on a nil *Core. Stop serving requests before calling it; Run does
// this for you.
func (c *Core) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		var errs []error
		if c.Auth != nil {
			ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
			if err := c.Auth.Shutdown(ctx); err != nil {
				errs = append(errs, fmt.Errorf("corego: wait for auth email deliveries: %w", err))
			}
			cancel()
		}
		if err := c.Mongo.Close(); err != nil {
			errs = append(errs, fmt.Errorf("corego: close MongoDB: %w", err))
		}
		if err := c.Postgres.Close(); err != nil {
			errs = append(errs, fmt.Errorf("corego: close PostgreSQL: %w", err))
		}
		c.closeErr = errors.Join(errs...)
	})
	return c.closeErr
}

// logger returns the logger resolved by New (see Config.Logger).
func (c *Core) logger() *slog.Logger {
	if c.log != nil {
		return c.log
	}
	if c.cfg.Logger != nil {
		return c.cfg.Logger
	}
	return slog.Default()
}
