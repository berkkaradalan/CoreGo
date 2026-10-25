# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Before 1.0.0, minor releases may contain breaking changes; they are always
listed here.

## [0.2.0] - Unreleased

A complete rewrite. Almost every public API changed; the
[migration guide](docs/migrating-from-0.1.md) shows the before and after
code for each of them.

### Added

- `web`: a standard-library-only web framework. Trie router with
  `:param` and `*wildcard` segments, 404/405 with `Allow`, automatic
  `OPTIONS` and `HEAD`, route groups, middleware (global, group,
  per-route), `Context` (which is a `context.Context`), JSON binding with a
  body size limit, `HTTPError` helpers, a replaceable error handler, panic
  recovery, `CORS`, `RateLimit`, `RequestLogger`, `WrapHandler`,
  `WrapMiddleware`, `Run` with graceful shutdown, `Serve`, `Shutdown`.
- `web`: `RateLimiter`, `NewRateLimiter` and `RateLimiter.Allow`, the
  limiter behind `RateLimit`, for limits keyed by anything (for example an
  email address); `RateLimitConfig.MaxKeys` (default
  `DefaultRateLimitMaxKeys`, 100000) bounds the tracked keys, with one
  shared overflow bucket beyond it; `ClientKey`, the default `RateLimit`
  key (the client IP, IPv6 addresses grouped by their /64 prefix). `CORS`
  answers the `"*"` origin with the literal `Access-Control-Allow-Origin:
  *`.
- `auth`: a store-agnostic authentication service (`auth.New(Config,
  Store)`): sign-up, sign-in, opaque server-side sessions with
  remember-me, session listing and revocation, email verification
  (`SendVerificationEmail`, `RequestEmailVerification`, `VerifyEmail`),
  password reset (`RequestPasswordReset`, `ResetPassword`),
  `ChangePassword`, `ChangeEmail`, `CheckPassword`, profile updates,
  `DeleteUser`, `ListAccounts`, `ListUsers`, `CountUsers`, `BanUser` /
  `UnbanUser` with optional expiry, `CleanupExpired` and `Shutdown`.
- `auth`: optimistic concurrency for users and accounts: `User.Version`
  and `Account.Version` (managed by the stores) and the store sentinel
  `ErrConflict`. The `Store` contract: `CreateUser`/`CreateAccount` set
  `Version` to 1, `UpdateUser`/`UpdateAccount` write only when the stored
  `Version` matches and return `ErrConflict` otherwise. The service
  retries a conflicting change up to 5 times and then returns
  `ErrConflict`.
- `auth`: `ErrNameTooLong` (names longer than 256 characters) and
  `ErrInvalidImageURL` (images that are not empty or an absolute
  `http`/`https` URL of at most 2048 bytes), returned by `SignUp` and
  `UpdateUser`.
- `auth`: `Config.MaxPendingEmails` (default `DefaultMaxPendingEmails`,
  100) bounds the background email jobs in flight; further jobs are
  dropped and logged at Warn level.
- `auth`: snake_case JSON tags on `User`, `Session`, `Account` and
  `VerificationToken`. Password hashes, token hashes and `Version` are
  never marshalled; `User` includes the ban fields.
- `auth`: request-context helpers `MustUser`, `UserFromContext`,
  `SessionFromContext` and `ContextWithSession`.
- `auth`: the `Store` interface, `MemoryStore`, the `Mailer` interface and
  `DefaultMailer` (plain text and HTML emails with verification and reset
  links).
- `auth/authhttp`: the HTTP layer of the auth service. 17 JSON routes under
  a configurable base path (`/auth` by default), bearer-token and optional
  HttpOnly-cookie transport, `RequireAuth` middleware, `OptionalAuth`
  middleware for routes that also serve anonymous visitors, `Middleware`
  for plain `net/http`, `Token` and `BasePath` helpers, `Config.Validate`
  and `Config.ResolvedBasePath`, `Config.TrustedOrigins` for the
  cross-site request forgery check of cookie-authenticated requests,
  per-email rate limits on sign-in, forgot-password and verification
  requests, stable status codes and
  error messages, and a `GET /verify-email` route that serves the link in
  verification emails (redirect or a built-in page).
- `auth/mongostore`: `auth.Store` on MongoDB with `EnsureIndexes` (unique
  and TTL indexes), a configurable collection prefix and an int64
  `version` field on users and accounts.
- `auth/pgstore`: `auth.Store` on PostgreSQL with `Migrate`, a versioned,
  advisory-locked migration runner, a configurable table prefix and a
  `version BIGINT NOT NULL DEFAULT 1` column on users and accounts
  (migration 1). Development databases migrated by a pre-release build of
  0.2.0 lack that column and must be dropped and migrated again (see
  [auth-storage.md](docs/auth-storage.md#migrations)).
- `mail`: the `Sender` interface with `SMTPSender` (STARTTLS, implicit TLS
  or plaintext for local servers), `LogSender`, `CaptureSender` and
  `SenderFunc`; `Message.Validate`; quoted-printable bodies and RFC 2047
  encoded headers.
- `corego`: `Core.Server` (the web server with request logging, optional
  CORS and the auth routes mounted), `Core.RequireAuth`,
  `Core.OptionalAuth`, `Core.Run`, `Core.AuthHandlers`, `AuthOptions`
  (service config, `MailSender`, store selection, `TablePrefix`,
  `CustomStore`, HTTP config), `ServerOptions` (CORS, proxy headers,
  request log), `Config.Logger` and the `StoreMemory`, `StoreMongo`,
  `StorePostgres` constants.
- `corego`: the explicit CORS origins (`ServerOptions.CORS` or
  `CORS_ALLOWED_ORIGINS`) become the auth routes' trusted origins unless
  `AuthOptions.HTTP.TrustedOrigins` is set; startup warnings for a
  wildcard `CORS_ALLOWED_ORIGINS` and for a session cookie without
  `Secure` outside localhost.
- `corego`: automatic auth store selection (`AuthOptions.Store`, then
  `COREGO_AUTH_STORE`, then the first connected database, then memory with
  a warning), automatic indexes and migrations, and an SMTP mailer built
  from the `SMTP_*` variables (port 465 uses implicit TLS, other ports
  STARTTLS).
- `env`: `Load`, `LoadFile`, the typed getters `Get`, `GetOrDefault`,
  `Int`, `Bool`, `Duration` and `Strings`, and the constants `DefaultFile`
  and `DefaultPort`.
- `database`: every helper takes a `context.Context`; `ErrNotFound`;
  `MongoDB.Count`, `Name`, `Database`, `Ping`; `PostgresDB.QueryMaps`,
  `WithTx`, `Ping`; the generic `QueryAll[T]` and `QueryOne[T]` with the
  `Querier` interface; `DefaultMongoDatabase`; the MongoDB database name
  is also read from the connection URL.
- Environment variables `APP_URL`, `MONGODB_DATABASE`,
  `COREGO_AUTH_STORE`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`,
  `SMTP_PASSWORD`, `SMTP_FROM`, `CORS_ALLOWED_ORIGINS`,
  `TRUST_PROXY_HEADERS`, `COREGO_RESET_PASSWORD_URL` and
  `COREGO_VERIFY_EMAIL_REDIRECT_URL`, and a commented `.env.example`.
- Environment switches for the common auth settings, so an application can
  be configured from `.env` alone: `COREGO_REQUIRE_VERIFIED_EMAIL`,
  `COREGO_AUTH_COOKIE`, `COREGO_AUTH_TABLE_PREFIX` and `COREGO_MAIL`
  (`smtp`, `log` for development, `none`, or empty for automatic
  selection). Invalid values, including an invalid `TRUST_PROXY_HEADERS`,
  make `corego.New` return an error instead of being ignored.
- `corego.Config.Env`, to hand `New` an environment the application
  already loaded, and one `corego: auth ready` Info line at startup that
  summarizes the store, table prefix, base path, mailer and switches.
- `web.AsHTTPError` (the default error mapping, reusable in custom error
  handlers) and `web.Context.BindJSONStrict`, which rejects unknown
  fields. `Group` routes registered with an empty path serve the group's
  own path.
- `mail.LogSender` logs one Info line per message with the links found in
  it, so development emails can be followed without Debug logging.
- `authhttp.CookieConfig.OmitTokenFromBody`, to keep the session token out
  of the JSON body when the cookie transport is used, and a Debug log line
  with the reason for every blocked cross-site cookie request.
- A startup banner printed by `Core.Run` when the output is a terminal:
  the address, the services in use (auth with its store and mailer,
  MongoDB and PostgreSQL with their database and host, CORS) and every
  registered route, the auth routes marked when they need a session.
  `ServerOptions.DisableBanner` and `COREGO_BANNER` control it.
- A compact, colored console logger the facade uses by default when
  standard error is a terminal and slog's default handler has not been
  replaced (`NO_COLOR` and `FORCE_COLOR` are honored); `Config.Logger`
  and production log pipelines are unaffected. `corego.Version`.
- `authhttp.Handlers.Routes`, listing the auth routes in registration
  order with whether they need a session.
- `examples/quickstart`, a runnable reference application, and a complete
  documentation set under `docs/`.

### Changed

- The `web` default error handler answers the body errors with the same
  descriptive messages as the auth routes ("Request body is not valid
  JSON.", "Content-Type must be application/json.", "Request body exceeds
  the configured size limit.") instead of the bare status text.
- **Breaking:** authentication uses opaque, server-side sessions instead
  of JWTs. Sessions can be listed and revoked, and sign-out takes effect
  immediately. No signing secret is needed any more.
- **Breaking:** `auth.Manager` is replaced by `auth.Auth`;
  `auth.New(*auth.Config, *database.MongoDB)` became
  `auth.New(auth.Config, auth.Store)`. `Signup` / `Login` became
  `SignUp` / `SignIn` with `context.Context`, input structs and outputs
  carrying the user, the session token and the session. A display name is
  required at sign-up.
- **Breaking:** `auth.User` lost `Password` (hashes now live on
  `auth.Account`) and `Custom`; it gained `Name`, `EmailVerified`,
  `Image`, ban fields and `UpdatedAt`. Store application-specific user
  data in your own table or collection keyed by `User.ID`.
- **Breaking:** auth data lives in `corego_users`, `corego_accounts`,
  `corego_sessions` and `corego_verification_tokens` (prefix
  configurable) instead of a `users` collection, and works on MongoDB,
  PostgreSQL or memory instead of MongoDB only.
- **Breaking:** `corego.Config.Auth` is a `*corego.AuthOptions` instead of
  a `*auth.Config`, and `corego.Core.Auth` is an `*auth.Auth`.
- **Breaking:** `corego.Core.Close` now also closes PostgreSQL and first
  waits (up to 10 s) for auth emails still being delivered.
- **Breaking:** the environment is loaded with `env.Load()` (see
  Removed), which returns an error for a malformed `.env` file. The `Env`
  fields were renamed
  (`URL` → `AppURL`, `PORT` → `Port`, `MONGODB_CONNECTION_URL` →
  `MongoURL`, `POSTGRES_CONNECTION_URL` → `PostgresURL`) and are plain
  strings instead of pointers. `APP_URL` replaces `URL` (still read as a
  fallback), and the default application URL now includes the port
  (`http://localhost:8080`).
- **Breaking:** loading the environment no longer writes `.env` values
  into the process environment; read them through `Env`. A missing `.env`
  file is no longer logged.
- **Breaking:** `database.NewMongoDB` and `database.NewPostgresDB` take
  their config by value and never modify it; the accessors are `Client`,
  `Pool` and `Close` (see Removed).
- **Breaking:** every `database` helper takes the caller's
  `context.Context` as its first argument; the hidden 5 second timeout per
  operation is gone.
- **Breaking:** `MongoDB.InsertOne` returns the inserted `_id` as the
  driver reports it (`any`, usually a `bson.ObjectID`) instead of a hex
  string; `MongoDB.Find` decodes into a slice pointer and accepts driver
  find options instead of returning `[]map[string]any`;
  `UpdateOne` and `DeleteOne` return `ErrNotFound` when nothing matched;
  `UpdateMany` and `DeleteMany` return the affected count; the write
  helpers reject a nil filter.
- **Breaking:** `PostgresDB.Query` returns the raw `pgx.Rows`; the old
  map-returning behavior is `QueryMaps`. `PostgresConfig.Database` was
  removed (the database is part of the URL).
- **Breaking:** the MongoDB driver moved from
  `go.mongodb.org/mongo-driver` (v1) to `go.mongodb.org/mongo-driver/v2`;
  use `bson.ObjectID` instead of `primitive.ObjectID`.
- Error responses are JSON objects of the form `{"error": "<message>"}`
  with stable, user-presentable messages and documented status codes,
  including `409` for `auth.ErrConflict`, `400` for `auth.ErrNameTooLong`
  and `auth.ErrInvalidImageURL`, and `403 Cross-site request blocked.`
- All auth timestamps are stored and returned in UTC.

### Removed

- **Breaking:** the Gin integration: `Manager.SignupHandler`,
  `LoginHandler`, `GetProfileHandler`, `UpdateProfileHandler`,
  `ChangePasswordHandler`, `DeleteAccountHandler` and
  `Manager.Middleware`. The routes are served by `auth/authhttp`, mounted
  by `Core.Server`; Gin is no longer a dependency.
- **Breaking:** `auth.Config.Secret`, `TokenExpiry` and `DatabaseName`;
  `Manager.GenerateToken` and `ValidateToken` (use `Auth.GetSession`);
  `Manager.GetUserByEmail`, `GetUserByID` (use `Auth.GetUser`),
  `UpdateProfile` (use `Auth.UpdateUser`), `DeleteAccount` (use
  `Auth.DeleteUser`); the types `SignupRequest`, `LoginRequest`,
  `AuthResponse`, `UpdateProfileRequest` and `ChangePasswordRequest`.
- **Breaking:** `env.LoadEnv` (use `env.Load`) and the `Env.MANUAL` map
  (use `Env.Get` and the typed getters).
- **Breaking:** `MongoDB.GetClient` (use `Client`), `PostgresDB.GetPool`
  (use `Pool`) and `Disconnect` on both (use `Close`).
- The `github.com/golang-jwt/jwt/v5` and `github.com/gin-gonic/gin`
  dependencies and the separate `test/` example module (replaced by
  `examples/quickstart`).

### Fixed

- `POSTGRES_CONNECTION_URL` now connects PostgreSQL automatically; it was
  never read before.
- `corego.New` closes every connection it opened when a later step fails,
  and the database constructors close the client or pool when the initial
  ping fails, so nothing leaks.

### Security

- **Token purpose separation:** email verification and password reset
  tokens are stored under different purpose-prefixed identifiers, so a
  verification token can never reset a password, and the reverse.
- **No stale tokens after an email change:** changing the email address
  deletes every outstanding token of the old address, deleting an account
  deletes the user's tokens, a successful reset or password change deletes
  all outstanding reset tokens, and tokens issued before the current owner
  of an address registered are rejected.
- **`RequireVerifiedEmail` is enforced everywhere:** sign-up creates no
  session, and sign-in and every session lookup (`GetSession`,
  `RequireAuth`) reject unverified users, including sessions created
  before an email change.
- **Enumeration-safe and timing-safe email flows:** the public "resend
  verification" and "forgot password" flows answer identically (`202`)
  for registered, unknown, verified and malformed addresses and when no
  mailer is configured, and emails are delivered in the background so
  neither SMTP latency nor SMTP failures are observable. Sign-in failures
  are uniform and unknown addresses still pay for a bcrypt comparison.
- **Password-confirmed sensitive changes:** changing the email address and
  deleting the account over HTTP require the current password; changing
  the password requires it too and revokes every session.
- **Rate limiting:** sign-up, sign-in, the email and password flows, email
  change and account deletion are rate limited per client IP (10 requests
  per minute per route by default).
- **No lost updates:** users and accounts are written with optimistic
  concurrency, so concurrent changes (for example a profile update racing
  a ban) can never overwrite each other or silently revert a ban.
- **No address squatting through stale links:** a verification link only
  verifies the address it was sent to, never an address the user changed
  to afterwards, and the credential account always follows the user's
  current address.
- **No token double-spend:** `VerifyEmail` and `ResetPassword` consume
  the token before applying the change, so two simultaneous requests can
  never both redeem one token (a failure after consumption needs a new
  token; a rejected new password does not consume a reset token).
- **Bounded background work:** email jobs are capped by
  `auth.Config.MaxPendingEmails` (extra jobs are dropped and logged)
  instead of one unbounded goroutine per request, so a slow or hanging
  mail server cannot exhaust memory.
- **Forgot-password timing:** `RequestPasswordReset` and
  `RequestEmailVerification` do only the user lookup synchronously; the
  token write runs in the background job with the delivery, so the
  response time does not tell registered and unknown addresses apart.
- **No recipient addresses in logs:** `mail` errors identify addresses by
  field and index (`mail: to[1]: invalid address`, `RCPT TO for cc[0]`)
  and redact addresses quoted in SMTP replies; the reply stays available
  as a `*textproto.Error` through `errors.As`.
- **CSRF protection for cookie authentication:** cookie-authenticated
  requests with a method other than `GET`, `HEAD` and `OPTIONS` must be
  same-site (per `Sec-Fetch-Site`/`Origin`) or come from
  `authhttp.Config.TrustedOrigins`; others get `403`. Bearer tokens are
  unaffected.
- **No CORS wildcard with credentials:** `web.CORS` panics when
  `AllowCredentials` is combined with `"*"`, and `CORS_ALLOWED_ORIGINS=*`
  allows every origin without credentials (with a startup warning), so no
  website can make cookie-authenticated requests on a visitor's behalf.
- **IPv6 rate-limit bypass:** clients are keyed by their IPv6 /64 prefix,
  so rotating addresses within one subscriber's network never yields a
  fresh budget.
- **Bounded limiter memory:** rate limiters track at most `MaxKeys` keys
  and share one overflow bucket beyond that, so key rotation can neither
  exhaust memory nor escape the limit.
- **Per-email rate limits:** sign-in, forgot-password and verification
  requests are also limited per email address (uniformly for unknown
  addresses), which stops distributed password guessing against one
  account and inbox flooding.
- **`HEAD` in any letter case:** `HEAD` requests to `GET /verify-email`
  (sent by link previewers), whatever the letter case of the method, never
  consume the verification token.
- **Profile field validation:** names are limited to 256 characters and
  images to absolute `http(s)` URLs of at most 2048 bytes, so profiles
  cannot carry `javascript:` URLs or unbounded text.
- **Proxy misconfiguration warning:** the web server logs a one-time
  warning when requests carry `X-Forwarded-For` while
  `TrustProxyHeaders` is off, the setup in which every client shares the
  proxy's rate limit.
- **Server timeouts:** the web server sets `ReadHeaderTimeout` (10 s),
  `ReadTimeout` (30 s), `WriteTimeout` (30 s) and `IdleTimeout` (120 s) by
  default, so slow clients cannot hold connections open, and limits
  request bodies to 1 MiB.
- **Spoof-resistant client IP:** proxy headers are ignored unless
  `TrustProxyHeaders` (`TRUST_PROXY_HEADERS`) is enabled, and then the
  rightmost `X-Forwarded-For` entry (the one your proxy appended) is used,
  so clients cannot choose their own IP for logging or rate limiting.
- **Prefixed tables and collections:** the auth stores only touch their
  own `corego_`-prefixed tables and collections, never an application's
  `users` or `sessions`.
- Session, verification and reset tokens carry 256 bits of entropy and are
  stored only as SHA-256 hashes; session cookies are always `HttpOnly` and
  become `Secure` automatically with an `https://` `APP_URL`; auth
  responses carry `Cache-Control: no-store`.
- Email headers are protected against header injection, and SMTP
  credentials are only ever sent over TLS.

## [0.1.0] - 2025-11-28

Initial release: JWT authentication on MongoDB with Gin handlers and
middleware, a MongoDB wrapper and `.env` loading.

[0.2.0]: https://github.com/berkkaradalan/CoreGo/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/berkkaradalan/CoreGo/releases/tag/v0.1.0
