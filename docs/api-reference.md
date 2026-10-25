# API Reference

Every exported identifier of every CoreGo package, generated from the
package documentation (`go doc -all`): signatures are exact, each entry
carries the first sentence of its doc comment. Run `go doc` on any
identifier for the full text, and see the linked guide of each package
for explanations and examples.

Packages:

- [corego](#corego)
- [web](#web)
- [auth](#auth)
- [auth/authhttp](#authauthhttp)
- [auth/mongostore](#authmongostore)
- [auth/pgstore](#authpgstore)
- [database](#database)
- [mail](#mail)
- [env](#env)

## corego

`import "github.com/berkkaradalan/CoreGo"`

Package corego assembles the CoreGo building blocks - environment configuration, database connections, the authentication service, its HTTP routes and the web server - behind one facade. Guide: [getting-started.md](getting-started.md).

### Constants

| Name | Definition | Description |
| --- | --- | --- |
| `StoreMemory` | `= "memory"` | StoreMemory keeps auth records in process memory. |
| `StoreMongo` | `= "mongo"` | StoreMongo persists auth records in the connected MongoDB. |
| `StorePostgres` | `= "postgres"` | StorePostgres persists auth records in the connected PostgreSQL. |
| `Version` | `= "0.2.0"` | Version is the CoreGo release this code belongs to. |

### Types

#### AuthOptions

AuthOptions enables and configures the authentication service and its HTTP routes.

| Field | Type | Description |
| --- | --- | --- |
| `Service` | `auth.Config` | Service configures the auth service. |
| `MailSender` | `mail.Sender` | MailSender delivers the auth emails through the facade's auth.DefaultMailer, with the links derived as described for Service. |
| `Store` | `string` | Store selects where users, sessions and tokens are kept. |
| `TablePrefix` | `string` | TablePrefix is the prefix of the auth tables (PostgreSQL) or collections (MongoDB) of the store New creates, passed on as pgstore.Options.TablePrefix or mongostore.Options.CollectionPrefix. |
| `CustomStore` | `auth.Store` | CustomStore, when non-nil, is used as the auth store as-is and takes precedence over Store, COREGO_AUTH_STORE and the automatic selection (TablePrefix is ignored too). |
| `HTTP` | `authhttp.Config` | HTTP configures the auth routes mounted by Core.Server and the middleware returned by Core.RequireAuth and Core.OptionalAuth. |

#### Config

Config configures a Core.

| Field | Type | Description |
| --- | --- | --- |
| `Env` | `*env.Env` | Env is the environment snapshot New reads its settings from. |
| `Mongo` | `*database.MongoConfig` | Mongo connects to MongoDB. |
| `Postgres` | `*database.PostgresConfig` | Postgres connects to PostgreSQL. |
| `Auth` | `*AuthOptions` | Auth enables the authentication service and its HTTP routes. |
| `Server` | `ServerOptions` | Server configures the web server returned by Core.Server. |
| `Logger` | `*slog.Logger` | Logger receives the facade's startup log (warnings and the "corego: auth ready" summary), the request log, the web server's logs, the emails of COREGO_MAIL=log and, unless AuthOptions.Service.Logger is set, the auth service's logs; nil selects a colored console logger in a terminal and slog.Default() otherwise. |

#### Core

Core is the root handle of a CoreGo application.

| Field | Type | Description |
| --- | --- | --- |
| `Env` | `*env.Env` | Env is the environment snapshot New read its settings from. |
| `Mongo` | `*database.MongoDB` | Mongo is the MongoDB connection; nil when MongoDB is not configured. |
| `Postgres` | `*database.PostgresDB` | Postgres is the PostgreSQL connection; nil when PostgreSQL is not configured. |
| `Auth` | `*auth.Auth` | Auth is the authentication service; nil when Config.Auth is nil. |

| Function / method | Description |
| --- | --- |
| `func New(cfg *Config) (*Core, error)` | New loads the environment (unless Config.Env provides it), connects the configured databases and, when Config.Auth is set, starts the authentication service on the selected store and logs one "corego: auth ready" line at Info level that summarizes the resolved auth setup. |
| `func (c *Core) AuthHandlers() *authhttp.Handlers` | AuthHandlers returns the HTTP layer of the auth service, the same instance whose routes Server mounts, for advanced use such as protecting plain net/http handlers with its Middleware or reading the session token with Token. |
| `func (c *Core) Close() error` | Close releases everything the Core owns. |
| `func (c *Core) OptionalAuth() web.MiddlewareFunc` | OptionalAuth returns the middleware for routes that serve signed-in and anonymous visitors alike. |
| `func (c *Core) RequireAuth() web.MiddlewareFunc` | RequireAuth returns the middleware that only lets requests with a valid session through and stores the signed-in user in the request context, where handlers read it with auth.MustUser(c). |
| `func (c *Core) Run() error` | Run serves Server on ":" + PORT (Env.Port) until the process receives SIGINT or SIGTERM, shuts the server down gracefully and then calls Close. |
| `func (c *Core) Server() *web.Server` | Server returns the application's web server, building it on the first call; later calls return the same *web.Server, so it is safe to call Server from anywhere. |

#### ServerOptions

ServerOptions configures the web server returned by Core.Server.

| Field | Type | Description |
| --- | --- | --- |
| `CORS` | `*web.CORSConfig` | CORS enables Cross-Origin Resource Sharing with this configuration. |
| `TrustProxyHeaders` | `*bool` | TrustProxyHeaders sets web.Server.TrustProxyHeaders. |
| `DisableRequestLog` | `bool` | DisableRequestLog turns off the one-line-per-request log. |
| `DisableBanner` | `bool` | DisableBanner turns off the startup summary Run prints: the address, the connected services and every registered route. |

## web

`import "github.com/berkkaradalan/CoreGo/web"`

Package web is CoreGo's HTTP framework, built only on the standard library. Guide: [web-framework.md](web-framework.md).

### Constants

| Name | Definition | Description |
| --- | --- | --- |
| `DefaultReadHeaderTimeout` | `= 10 * time.Second` | Default values New assigns to the Server timeout fields. |
| `DefaultReadTimeout` | `= 30 * time.Second` | Default values New assigns to the Server timeout fields. |
| `DefaultWriteTimeout` | `= 30 * time.Second` | Default values New assigns to the Server timeout fields. |
| `DefaultIdleTimeout` | `= 120 * time.Second` | Default values New assigns to the Server timeout fields. |
| `DefaultShutdownTimeout` | `= 10 * time.Second` | Default values New assigns to the Server timeout fields. |
| `DefaultClientIPHeader` | `= "X-Forwarded-For"` | DefaultClientIPHeader is the default value of Server.ClientIPHeader. |
| `DefaultMaxBodyBytes` | `int64 = 1 << 20` | DefaultMaxBodyBytes is the default request body limit enforced by Context.Body, BindJSON and BindJSONStrict (1 MiB). |
| `DefaultRateLimitMaxKeys` | `= 100000` | DefaultRateLimitMaxKeys is the number of keys a rate limiter tracks individually when RateLimitConfig.MaxKeys is zero. |
| `DefaultRateLimitMessage` | `= "Too many requests. Please try again later."` | DefaultRateLimitMessage is the 429 error message used when RateLimitConfig.Message is empty. |

### Variables

| Name | Definition | Description |
| --- | --- | --- |
| `ErrBodyTooLarge` | `= errors.New("web: request body exceeds the configured limit")` | Sentinel errors returned by Context methods. |
| `ErrUnsupportedMediaType` | `= errors.New("web: unsupported media type")` | Sentinel errors returned by Context methods. |
| `ErrInvalidJSON` | `= errors.New("web: invalid JSON body")` | Sentinel errors returned by Context methods. |

### Functions

| Signature | Description |
| --- | --- |
| `func ClientKey(c *Context) string` | ClientKey returns a key identifying the client of c for per-client limits such as RateLimit (its default key). |

### Types

#### CORSConfig

CORSConfig configures the CORS middleware.

| Field | Type | Description |
| --- | --- | --- |
| `AllowedOrigins` | `[]string` | AllowedOrigins lists the origins allowed to make cross-origin requests, exactly as browsers send them ("https://app.example.com", scheme and host compared case-insensitively, a trailing slash ignored). |
| `AllowOriginFunc` | `func(origin string) bool` | AllowOriginFunc, when set, is consulted for origins that AllowedOrigins does not match; returning true allows the origin. |
| `AllowedMethods` | `[]string` | AllowedMethods is sent in Access-Control-Allow-Methods. |
| `AllowedHeaders` | `[]string` | AllowedHeaders is sent in Access-Control-Allow-Headers. |
| `ExposedHeaders` | `[]string` | ExposedHeaders lists response headers browsers may expose to the calling script (Access-Control-Expose-Headers). |
| `AllowCredentials` | `bool` | AllowCredentials allows cookies and HTTP authentication on cross-origin requests (Access-Control-Allow-Credentials: true). |
| `MaxAge` | `time.Duration` | MaxAge is how long browsers may cache a preflight result (Access-Control-Max-Age, whole seconds). |

#### Context

Context carries per-request state through the middleware and handler chain.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func (c *Context) BindJSON(v any) error` | BindJSON decodes the request body into v. |
| `func (c *Context) BindJSONStrict(v any) error` | BindJSONStrict is BindJSON for clients that must send exactly the expected shape. |
| `func (c *Context) Body() ([]byte, error)` | Body reads and returns the request body. |
| `func (c *Context) Cookie(name string) (string, error)` | Cookie returns the value of the named request cookie. |
| `func (c *Context) Data(code int, contentType string, b []byte) error` | Data writes raw bytes with the given status code and Content-Type. |
| `func (c *Context) Deadline() (time.Time, bool)` | Deadline implements context.Context by delegating to the request context. |
| `func (c *Context) DefaultQuery(name, def string) string` | DefaultQuery returns the first URL query parameter with the given name, or def when the parameter is absent. |
| `func (c *Context) Done() <-chan struct{}` | Done implements context.Context by delegating to the request context. |
| `func (c *Context) Err() error` | Err implements context.Context by delegating to the request context. |
| `func (c *Context) Get(key string) (any, bool)` | Get returns the per-request value stored under key with Set and whether it was present. |
| `func (c *Context) GetHeader(name string) string` | GetHeader returns the first request header with the given (case-insensitive) name, or the empty string when absent. |
| `func (c *Context) HTML(code int, s string) error` | HTML writes s with the given status code and a "text/html; charset=utf-8" Content-Type. |
| `func (c *Context) JSON(code int, v any) error` | JSON serializes v as JSON and writes it with the given status code and a "application/json; charset=utf-8" Content-Type. |
| `func (c *Context) Method() string` | Method returns the request's HTTP method. |
| `func (c *Context) NoContent() error` | NoContent writes a 204 No Content status with no body. |
| `func (c *Context) Param(name string) string` | Param returns the value of the named path parameter (":name" or "*name" segments), or the empty string when no such parameter was captured. |
| `func (c *Context) Path() string` | Path returns the decoded request path, without the query string. |
| `func (c *Context) Queries() url.Values` | Queries returns all URL query parameters, keyed by name. |
| `func (c *Context) Query(name string) string` | Query returns the first URL query parameter with the given name, or the empty string when absent. |
| `func (c *Context) Redirect(code int, location string) error` | Redirect writes a redirection response with the given 3xx status code (commonly 301, 302, 303, 307 or 308) and Location header. |
| `func (c *Context) RemoteIP() string` | RemoteIP returns the client IP address. |
| `func (c *Context) Request() *http.Request` | Request returns the current *http.Request. |
| `func (c *Context) Response() http.ResponseWriter` | Response returns the http.ResponseWriter for the current request. |
| `func (c *Context) Set(key string, value any)` | Set stores a per-request value for communication between middleware and handlers. |
| `func (c *Context) SetCookie(cookie *http.Cookie)` | SetCookie adds a Set-Cookie header to the response. |
| `func (c *Context) SetHeader(name, value string)` | SetHeader sets a response header, replacing any existing value. |
| `func (c *Context) SetRequest(r *http.Request)` | SetRequest replaces the current request, typically to attach values to its context so downstream handlers (and Value) see them. |
| `func (c *Context) Status(code int) error` | Status writes the response status code without a body. |
| `func (c *Context) String(code int, s string) error` | String writes s with the given status code and a "text/plain; charset=utf-8" Content-Type. |
| `func (c *Context) Value(key any) any` | Value implements context.Context by delegating to the request context, so values attached with SetRequest(r.WithContext(...)) are visible. |
| `func (c *Context) Written() bool` | Written reports whether the response has been started (a status code or body bytes were written, through the Context writers or Response()). |

#### ErrorHandler

ErrorHandler turns an error returned by a handler or middleware into a response.

```go
type ErrorHandler func(*Context, error)
```

#### Group

Group bundles a path prefix with middleware so that routes registered through it share both.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func (g *Group) Delete(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Delete registers h for DELETE requests under the group's prefix. |
| `func (g *Group) Get(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Get registers h for GET requests under the group's prefix. |
| `func (g *Group) Group(prefix string, mw ...MiddlewareFunc) *Group` | Group creates a nested group under g. |
| `func (g *Group) Handle(method, path string, h HandlerFunc, mw ...MiddlewareFunc)` | Handle registers h for method and path under the group's prefix. |
| `func (g *Group) Head(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Head registers h for HEAD requests under the group's prefix. |
| `func (g *Group) Options(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Options registers h for OPTIONS requests under the group's prefix. |
| `func (g *Group) Patch(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Patch registers h for PATCH requests under the group's prefix. |
| `func (g *Group) Post(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Post registers h for POST requests under the group's prefix. |
| `func (g *Group) Put(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Put registers h for PUT requests under the group's prefix. |
| `func (g *Group) Use(mw ...MiddlewareFunc)` | Use appends middleware to the group. |

#### HTTPError

HTTPError is an error carrying an HTTP status code and a user-presentable message.

| Field | Type | Description |
| --- | --- | --- |
| `Code` | `int` |  |
| `Message` | `string` |  |
| `Err` | `error` |  |

| Function / method | Description |
| --- | --- |
| `func AsHTTPError(err error) (*HTTPError, bool)` | AsHTTPError returns the *HTTPError the default error handler renders for err, and reports whether there is one. |
| `func BadRequest(message string) *HTTPError` | BadRequest returns a 400 *HTTPError. |
| `func Conflict(message string) *HTTPError` | Conflict returns a 409 *HTTPError. |
| `func Forbidden(message string) *HTTPError` | Forbidden returns a 403 *HTTPError. |
| `func NewHTTPError(code int, message string) *HTTPError` | NewHTTPError returns an *HTTPError with the given status code and message. |
| `func NotFound(message string) *HTTPError` | NotFound returns a 404 *HTTPError. |
| `func TooManyRequests(message string) *HTTPError` | TooManyRequests returns a 429 *HTTPError. |
| `func Unauthorized(message string) *HTTPError` | Unauthorized returns a 401 *HTTPError. |
| `func (e *HTTPError) Error() string` | Error implements the error interface. |
| `func (e *HTTPError) Unwrap() error` | Unwrap exposes the wrapped cause, if any. |

#### HandlerFunc

HandlerFunc is a single request handler.

```go
type HandlerFunc func(*Context) error
```

| Function / method | Description |
| --- | --- |
| `func WrapHandler(h http.Handler) HandlerFunc` | WrapHandler adapts a standard http.Handler into a HandlerFunc, for mounting existing net/http handlers (file servers, metrics endpoints, pprof, ...) on CoreGo routes. |

#### MiddlewareFunc

MiddlewareFunc wraps a HandlerFunc with additional behavior.

```go
type MiddlewareFunc func(HandlerFunc) HandlerFunc
```

| Function / method | Description |
| --- | --- |
| `func CORS(cfg CORSConfig) MiddlewareFunc` | CORS returns a middleware implementing Cross-Origin Resource Sharing. |
| `func RateLimit(cfg RateLimitConfig) MiddlewareFunc` | RateLimit returns an in-memory rate limiting middleware built on a RateLimiter. |
| `func Recovery() MiddlewareFunc` | Recovery returns a middleware that converts panics raised downstream into a logged 500 response and keeps the server running. |
| `func RequestLogger(logger *slog.Logger) MiddlewareFunc` | RequestLogger returns a middleware that logs one line per request with the attributes method, path, status, duration and remote_ip. |
| `func WrapMiddleware(mw func(http.Handler) http.Handler) MiddlewareFunc` | WrapMiddleware adapts a standard net/http middleware into a MiddlewareFunc. |

#### RateLimitConfig

RateLimitConfig configures the RateLimit middleware and NewRateLimiter.

| Field | Type | Description |
| --- | --- | --- |
| `Limit` | `int` | Limit is the maximum number of requests per Window for one key. |
| `Window` | `time.Duration` | Window is the length of the rate window. |
| `Key` | `func(*Context) string` | Key derives the bucket a request counts against. |
| `Message` | `string` | Message is the error message of the 429 response. |
| `MaxKeys` | `int` | MaxKeys bounds the number of keys tracked individually. |

#### RateLimiter

RateLimiter is an in-memory, per-key rate limiter.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewRateLimiter(cfg RateLimitConfig) *RateLimiter` | NewRateLimiter returns a RateLimiter enforcing cfg.Limit requests per cfg.Window for each key, tracking at most cfg.MaxKeys keys. |
| `func (l *RateLimiter) Allow(key string) (allowed bool, retryAfter time.Duration, remaining int)` | Allow records one request for key if it fits in the limit. |

#### Route

Route describes one registered route.

| Field | Type | Description |
| --- | --- | --- |
| `Method` | `string` |  |
| `Path` | `string` |  |

#### Server

Server is CoreGo's web server.

| Field | Type | Description |
| --- | --- | --- |
| `MaxBodyBytes` | `int64` | MaxBodyBytes is the largest request body Context.Body, Context.BindJSON and Context.BindJSONStrict will read. |
| `TrustProxyHeaders` | `bool` | TrustProxyHeaders makes Context.RemoteIP read the client address from ClientIPHeader. |
| `ClientIPHeader` | `string` | ClientIPHeader is the header consulted by Context.RemoteIP when TrustProxyHeaders is true. |
| `ReadHeaderTimeout` | `time.Duration` | Timeouts passed to the underlying http.Server when serving starts. |
| `ReadTimeout` | `time.Duration` | default 30s |
| `WriteTimeout` | `time.Duration` | default 30s |
| `IdleTimeout` | `time.Duration` | default 120s |
| `ShutdownTimeout` | `time.Duration` | ShutdownTimeout bounds how long Run waits for in-flight requests after SIGINT or SIGTERM before closing remaining connections. |
| `Logger` | `*slog.Logger` | Logger receives framework logs (recovered panics, unhandled errors, net/http server errors at Error level). |

| Function / method | Description |
| --- | --- |
| `func New() *Server` | New creates an empty Server with every field set to its default. |
| `func (s *Server) Delete(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Delete registers h for DELETE requests. |
| `func (s *Server) Get(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Get registers h for GET requests. |
| `func (s *Server) Group(prefix string, mw ...MiddlewareFunc) *Group` | Group creates a route group with the given prefix and middleware. |
| `func (s *Server) Handle(method, path string, h HandlerFunc, mw ...MiddlewareFunc)` | Handle registers h for requests with the given method and path, with optional per-route middleware mw (first = outermost). |
| `func (s *Server) Head(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Head registers h for HEAD requests. |
| `func (s *Server) ListenAndServe(addr string) error` | ListenAndServe listens on the TCP address addr (for example ":8080") and serves requests until Shutdown is called, then returns nil. |
| `func (s *Server) OnError(fn ErrorHandler)` | OnError installs fn as the error handler invoked when a handler or middleware returns an error. |
| `func (s *Server) Options(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Options registers h for OPTIONS requests. |
| `func (s *Server) Patch(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Patch registers h for PATCH requests. |
| `func (s *Server) Post(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Post registers h for POST requests. |
| `func (s *Server) Put(path string, h HandlerFunc, mw ...MiddlewareFunc)` | Put registers h for PUT requests. |
| `func (s *Server) Routes() []Route` | Routes returns every registered route, sorted by path and then method. |
| `func (s *Server) Run(addr string) error` | Run listens on addr and serves requests until the process receives SIGINT or SIGTERM, then shuts down gracefully. |
| `func (s *Server) Serve(ln net.Listener) error` | Serve accepts connections on ln and serves requests until Shutdown is called, then returns nil. |
| `func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request)` | ServeHTTP implements http.Handler. |
| `func (s *Server) Shutdown(ctx context.Context) error` | Shutdown gracefully shuts down the running server. |
| `func (s *Server) Use(mw ...MiddlewareFunc)` | Use appends global middleware. |

## auth

`import "github.com/berkkaradalan/CoreGo/auth"`

Package auth provides authentication and session management for applications built with CoreGo. Guide: [authentication.md](authentication.md).

### Constants

| Name | Definition | Description |
| --- | --- | --- |
| `DefaultSessionTTL` | `= 7 * 24 * time.Hour` | DefaultSessionTTL is the lifetime of a regular session. |
| `DefaultRememberMeTTL` | `= 30 * 24 * time.Hour` | DefaultRememberMeTTL is the lifetime of a session created with the remember me flag set. |
| `DefaultEmailVerificationTokenTTL` | `= 24 * time.Hour` | DefaultEmailVerificationTokenTTL is the lifetime of an email verification token. |
| `DefaultPasswordResetTokenTTL` | `= time.Hour` | DefaultPasswordResetTokenTTL is the lifetime of a password reset token. |
| `DefaultMinPasswordLength` | `= 8` | DefaultMinPasswordLength is the minimum password length in bytes enforced by the default password policy. |
| `DefaultMaxPendingEmails` | `= 100` | DefaultMaxPendingEmails is the maximum number of background email jobs (token creation and delivery) in flight at once. |
| `CredentialProvider` | `= "credential"` | CredentialProvider is the provider name used for email and password (credential) accounts. |

### Variables

| Name | Definition | Description |
| --- | --- | --- |
| `ErrInvalidConfig` | `= errors.New("auth: invalid configuration")` | ErrInvalidConfig is returned by New when the configuration or the store is not usable. |
| `ErrNameRequired` | `= errors.New("auth: name is required")` | ErrNameRequired is returned when a signup omits the display name. |
| `ErrNameTooLong` | `= errors.New("auth: name is too long")` | ErrNameTooLong is returned by SignUp and UpdateUser when the display name is longer than 256 characters (Unicode code points, counted after trimming surrounding whitespace). |
| `ErrInvalidImageURL` | `= errors.New("auth: image must be an absolute http or https URL")` | ErrInvalidImageURL is returned by SignUp and UpdateUser when the image is neither empty nor an absolute http or https URL (with a host) of at most 2048 bytes, measured after trimming surrounding whitespace. |
| `ErrEmailRequired` | `= errors.New("auth: email is required")` | ErrEmailRequired is returned when an email address is required but missing. |
| `ErrInvalidEmail` | `= errors.New("auth: invalid email address")` | ErrInvalidEmail is returned when an email address is malformed. |
| `ErrPasswordRequired` | `= errors.New("auth: password is required")` | ErrPasswordRequired is returned when a password is required but missing. |
| `ErrWeakPassword` | `= errors.New("auth: password does not meet the requirements")` | ErrWeakPassword is returned when a password does not satisfy the configured password policy. |
| `ErrUserAlreadyExists` | `= errors.New("auth: user with this email already exists")` | ErrUserAlreadyExists is returned when a user with the same email already exists. |
| `ErrUserNotFound` | `= errors.New("auth: user not found")` | ErrUserNotFound is returned when the requested user does not exist. |
| `ErrAccountNotFound` | `= errors.New("auth: account not found")` | ErrAccountNotFound is returned when the requested account does not exist. |
| `ErrInvalidCredentials` | `= errors.New("auth: invalid email or password")` | ErrInvalidCredentials is returned when email or password do not match an account. |
| `ErrInvalidCurrentPassword` | `= errors.New("auth: current password is incorrect")` | ErrInvalidCurrentPassword is returned by ChangePassword, ChangeEmail and CheckPassword when the supplied password does not match the user's credential account. |
| `ErrEmailNotVerified` | `= errors.New("auth: email address is not verified")` | ErrEmailNotVerified is returned by SignIn and GetSession when Config.RequireVerifiedEmail is enabled and the user's email address has not been verified yet. |
| `ErrEmailAlreadyVerified` | `= errors.New("auth: email address is already verified")` | ErrEmailAlreadyVerified is returned by SendVerificationEmail when the user's address is already verified. |
| `ErrUserBanned` | `= errors.New("auth: this account has been banned")` | ErrUserBanned is returned when the account is currently banned. |
| `ErrSessionNotFound` | `= errors.New("auth: session not found")` | ErrSessionNotFound is returned when the requested session does not exist. |
| `ErrSessionExpired` | `= errors.New("auth: session has expired")` | ErrSessionExpired is returned when a session token is valid but the session has expired. |
| `ErrInvalidToken` | `= errors.New("auth: invalid token")` | ErrInvalidToken is returned when a verification or reset token is unknown or malformed. |
| `ErrTokenExpired` | `= errors.New("auth: token has expired")` | ErrTokenExpired is returned when a verification or reset token is recognized but has expired. |
| `ErrMailerNotConfigured` | `= errors.New("auth: no mailer configured")` | ErrMailerNotConfigured is returned by SendVerificationEmail when no mailer is configured. |
| `ErrEmailDeliveryFailed` | `= errors.New("auth: email delivery failed")` | ErrEmailDeliveryFailed wraps mailer errors returned by the synchronous SendVerificationEmail so callers can detect delivery failures with errors.Is while still seeing the cause. |
| `ErrNotFound` | `= errors.New("auth: record not found")` | ErrNotFound is returned when the requested record does not exist. |
| `ErrDuplicate` | `= errors.New("auth: duplicate record")` | ErrDuplicate is returned when a create or update violates a unique constraint, for example a second user with the same email. |
| `ErrConflict` | `= errors.New("auth: record was modified concurrently")` | ErrConflict is returned by UpdateUser and UpdateAccount when the stored record's Version no longer matches the Version of the record passed in. |

### Functions

| Signature | Description |
| --- | --- |
| `func ContextWithSession(ctx context.Context, session *Session, user *User) context.Context` | ContextWithSession returns a copy of ctx that carries the authenticated session and user. |
| `func HashPassword(password string) (string, error)` | HashPassword hashes password with bcrypt at bcrypt.DefaultCost. |
| `func HashToken(token string) string` | HashToken returns the SHA-256 hex digest of token. |
| `func NewToken() (string, error)` | NewToken returns a new opaque high-entropy token. |
| `func VerifyPassword(hash, password string) bool` | VerifyPassword reports whether password matches the bcrypt hash. |

### Types

#### Account

Account links a user to an authentication provider.

| Field | Type | JSON | Description |
| --- | --- | --- | --- |
| `ID` | `string` | `id` |  |
| `UserID` | `string` | `user_id` |  |
| `Provider` | `string` | `provider` | "credential", "google", "apple", "facebook", ... |
| `ProviderAccountID` | `string` | `provider_account_id` | provider specific user id; the email for credential accounts |
| `Password` | `string` | never | bcrypt hash; empty for OAuth accounts |
| `CreatedAt` | `time.Time` | `created_at` |  |
| `UpdatedAt` | `time.Time` | `updated_at` |  |
| `Version` | `int64` | never | Version is the optimistic concurrency counter managed by the store. |

#### Auth

Auth is the CoreGo authentication service.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func New(cfg Config, store Store) (*Auth, error)` | New creates an Auth service backed by store. |
| `func (a *Auth) BanUser(ctx context.Context, userID, reason string, expires *time.Time) error` | BanUser marks the user with the given ID as banned, records the reason and optional expiry, and immediately revokes every session so the user is signed out everywhere. |
| `func (a *Auth) ChangeEmail(ctx context.Context, userID, newEmail, currentPassword string) (*User, error)` | ChangeEmail moves the user's login address to newEmail after verifying currentPassword, and returns the updated user. |
| `func (a *Auth) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error` | ChangePassword replaces the password of the user's credential account after verifying the current password, then revokes all of the user's sessions so every client must sign in again, and invalidates any outstanding password reset tokens. |
| `func (a *Auth) CheckPassword(ctx context.Context, userID, password string) error` | CheckPassword reports whether password is the current password of the user's credential account. |
| `func (a *Auth) CleanupExpired(ctx context.Context) error` | CleanupExpired removes every verification and password reset token and every session whose expiry lies before now. |
| `func (a *Auth) CountUsers(ctx context.Context) (int64, error)` | CountUsers returns the total number of stored users. |
| `func (a *Auth) DeleteUser(ctx context.Context, userID string) error` | DeleteUser removes the user with the given ID together with everything that belongs to it. |
| `func (a *Auth) GetSession(ctx context.Context, sessionToken string) (*Session, *User, error)` | GetSession resolves sessionToken to its session and user. |
| `func (a *Auth) GetUser(ctx context.Context, userID string) (*User, error)` | GetUser returns the user with the given ID. |
| `func (a *Auth) ListAccounts(ctx context.Context, userID string) ([]*Account, error)` | ListAccounts returns all provider accounts linked to the user, oldest first. |
| `func (a *Auth) ListSessions(ctx context.Context, userID string) ([]*Session, error)` | ListSessions returns all sessions of userID, newest first. |
| `func (a *Auth) ListUsers(ctx context.Context, opts ListOptions) ([]*User, error)` | ListUsers returns users page by page, newest first. |
| `func (a *Auth) RequestEmailVerification(ctx context.Context, email string) error` | RequestEmailVerification is the public, unauthenticated "send me a new verification link" flow, keyed by email address. |
| `func (a *Auth) RequestPasswordReset(ctx context.Context, email string) error` | RequestPasswordReset starts the password reset flow for the given email address. |
| `func (a *Auth) ResetPassword(ctx context.Context, email, token, newPassword string) error` | ResetPassword consumes a password reset token and sets a new password for the matching credential account. |
| `func (a *Auth) RevokeAllSessions(ctx context.Context, userID string) error` | RevokeAllSessions deletes every session of userID. |
| `func (a *Auth) RevokeOtherSessions(ctx context.Context, userID, keepSessionID string) error` | RevokeOtherSessions deletes every session of userID except the one with the given ID. |
| `func (a *Auth) RevokeSession(ctx context.Context, userID, sessionID string) error` | RevokeSession deletes the session with the given ID, but only when it belongs to userID. |
| `func (a *Auth) SendVerificationEmail(ctx context.Context, userID string) error` | SendVerificationEmail creates a fresh verification token for the user with the given ID and emails it synchronously. |
| `func (a *Auth) Shutdown(ctx context.Context) error` | Shutdown waits until every email that is being delivered in the background has been handed to the mailer (successfully or not). |
| `func (a *Auth) SignIn(ctx context.Context, in SignInInput) (*SignInOutput, error)` | SignIn authenticates a user with an email address and a password and starts a new session. |
| `func (a *Auth) SignOut(ctx context.Context, sessionToken string) error` | SignOut invalidates the session identified by sessionToken. |
| `func (a *Auth) SignUp(ctx context.Context, in SignUpInput) (*SignUpOutput, error)` | SignUp registers a user with a credential account and, unless Config.RequireVerifiedEmail is enabled, starts a session. |
| `func (a *Auth) UnbanUser(ctx context.Context, userID string) error` | UnbanUser clears every ban field (Banned, BanReason, BanExpires) of the user with the given ID. |
| `func (a *Auth) UpdateUser(ctx context.Context, userID string, in UpdateUserInput) (*User, error)` | UpdateUser applies the non-nil fields of in to the user with the given ID and returns the updated record. |
| `func (a *Auth) VerifyEmail(ctx context.Context, email, token string) error` | VerifyEmail consumes an email verification token. |

#### Config

Config configures an Auth service.

| Field | Type | Description |
| --- | --- | --- |
| `SessionTTL` | `time.Duration` | SessionTTL is how long a session stays valid. |
| `RememberMeTTL` | `time.Duration` | RememberMeTTL is how long a session created with RememberMe set stays valid. |
| `EmailVerificationTokenTTL` | `time.Duration` | EmailVerificationTokenTTL is how long an email verification token stays valid. |
| `PasswordResetTokenTTL` | `time.Duration` | PasswordResetTokenTTL is how long a password reset token stays valid. |
| `RequireVerifiedEmail` | `bool` | RequireVerifiedEmail makes a verified email address mandatory for being signed in. |
| `MinPasswordLength` | `int` | MinPasswordLength is the minimum password length in bytes. |
| `Mailer` | `Mailer` | Mailer sends verification and password reset emails. |
| `MaxPendingEmails` | `int` | MaxPendingEmails bounds the background email jobs in flight at once (verification and password reset emails, including the token creation of the public request flows). |
| `Logger` | `*slog.Logger` | Logger receives background email delivery failures and configuration warnings. |

#### DefaultMailer

DefaultMailer is the built-in Mailer.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewDefaultMailer(sender mail.Sender, cfg DefaultMailerConfig) *DefaultMailer` | NewDefaultMailer returns a DefaultMailer that delivers through sender using the links and wording from cfg. |
| `func (m *DefaultMailer) SendPasswordResetEmail(ctx context.Context, to, token string) error` | SendPasswordResetEmail implements Mailer. |
| `func (m *DefaultMailer) SendVerificationEmail(ctx context.Context, to, token string) error` | SendVerificationEmail implements Mailer. |

#### DefaultMailerConfig

DefaultMailerConfig configures the links and wording of the DefaultMailer emails.

| Field | Type | Description |
| --- | --- | --- |
| `AppName` | `string` | AppName is shown in subjects and bodies, for example "Verify your email for Acme". |
| `VerifyEmailURL` | `string` | VerifyEmailURL is the absolute http(s) URL the verification link points at; "token" and "email" query parameters are added to it and any existing query parameters are kept. |
| `ResetPasswordURL` | `string` | ResetPasswordURL is the absolute http(s) URL of the application's "choose a new password" page; the link carries the same "token" and "email" query parameters. |

#### ListOptions

ListOptions controls pagination for list operations.

| Field | Type | Description |
| --- | --- | --- |
| `Limit` | `int` | Limit is the maximum number of records returned. |
| `Offset` | `int` | Offset skips the first Offset records. |

#### Mailer

Mailer sends the transactional emails of the auth flows.

```go
type Mailer interface {
	// SendVerificationEmail delivers an email verification token to the
	// user with the given address.
	SendVerificationEmail(ctx context.Context, to, token string) error

	// SendPasswordResetEmail delivers a password reset token to the
	// user with the given address.
	SendPasswordResetEmail(ctx context.Context, to, token string) error
}
```

#### MemoryStore

MemoryStore is a complete in-memory Store implementation intended for development, examples and tests.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewMemoryStore() *MemoryStore` | NewMemoryStore returns an empty MemoryStore ready for use. |
| `func (s *MemoryStore) CountUsers(_ context.Context) (int64, error)` | CountUsers implements Store. |
| `func (s *MemoryStore) CreateAccount(_ context.Context, account *Account) error` | CreateAccount implements Store. |
| `func (s *MemoryStore) CreateSession(_ context.Context, session *Session) error` | CreateSession implements Store. |
| `func (s *MemoryStore) CreateUser(_ context.Context, user *User) error` | CreateUser implements Store. |
| `func (s *MemoryStore) CreateVerificationToken(_ context.Context, token *VerificationToken) error` | CreateVerificationToken implements Store. |
| `func (s *MemoryStore) DeleteAccount(_ context.Context, id string) error` | DeleteAccount implements Store. |
| `func (s *MemoryStore) DeleteExpiredSessions(_ context.Context, before time.Time) error` | DeleteExpiredSessions implements Store. |
| `func (s *MemoryStore) DeleteExpiredVerificationTokens(_ context.Context, before time.Time) error` | DeleteExpiredVerificationTokens implements Store. |
| `func (s *MemoryStore) DeleteSession(_ context.Context, id string) error` | DeleteSession implements Store. |
| `func (s *MemoryStore) DeleteSessionsByUser(_ context.Context, userID string) error` | DeleteSessionsByUser implements Store. |
| `func (s *MemoryStore) DeleteUser(_ context.Context, id string) error` | DeleteUser implements Store. |
| `func (s *MemoryStore) DeleteVerificationToken(_ context.Context, id string) error` | DeleteVerificationToken implements Store. |
| `func (s *MemoryStore) DeleteVerificationTokensByIdentifier(_ context.Context, identifier string) error` | DeleteVerificationTokensByIdentifier implements Store. |
| `func (s *MemoryStore) GetAccount(_ context.Context, provider, providerAccountID string) (*Account, error)` | GetAccount implements Store. |
| `func (s *MemoryStore) GetAccountByUser(_ context.Context, userID, provider string) (*Account, error)` | GetAccountByUser implements Store. |
| `func (s *MemoryStore) GetSession(_ context.Context, id string) (*Session, error)` | GetSession implements Store. |
| `func (s *MemoryStore) GetSessionByTokenHash(_ context.Context, tokenHash string) (*Session, error)` | GetSessionByTokenHash implements Store. |
| `func (s *MemoryStore) GetUser(_ context.Context, id string) (*User, error)` | GetUser implements Store. |
| `func (s *MemoryStore) GetUserByEmail(_ context.Context, email string) (*User, error)` | GetUserByEmail implements Store. |
| `func (s *MemoryStore) GetVerificationToken(_ context.Context, identifier, valueHash string) (*VerificationToken, error)` | GetVerificationToken implements Store. |
| `func (s *MemoryStore) ListAccountsByUser(_ context.Context, userID string) ([]*Account, error)` | ListAccountsByUser implements Store. |
| `func (s *MemoryStore) ListSessionsByUser(_ context.Context, userID string) ([]*Session, error)` | ListSessionsByUser implements Store. |
| `func (s *MemoryStore) ListUsers(_ context.Context, opts ListOptions) ([]*User, error)` | ListUsers implements Store. |
| `func (s *MemoryStore) UpdateAccount(_ context.Context, account *Account) error` | UpdateAccount implements Store. |
| `func (s *MemoryStore) UpdateSession(_ context.Context, session *Session) error` | UpdateSession implements Store. |
| `func (s *MemoryStore) UpdateUser(_ context.Context, user *User) error` | UpdateUser implements Store. |

#### Session

Session represents a server side session.

| Field | Type | JSON | Description |
| --- | --- | --- | --- |
| `ID` | `string` | `id` |  |
| `UserID` | `string` | `user_id` |  |
| `TokenHash` | `string` | never |  |
| `ExpiresAt` | `time.Time` | `expires_at` |  |
| `IPAddress` | `string` | `ip_address` |  |
| `UserAgent` | `string` | `user_agent` |  |
| `CreatedAt` | `time.Time` | `created_at` |  |
| `UpdatedAt` | `time.Time` | `updated_at` |  |

| Function / method | Description |
| --- | --- |
| `func SessionFromContext(ctx context.Context) (*Session, bool)` | SessionFromContext returns the authenticated session stored by ContextWithSession and whether one is present. |

#### SignInInput

SignInInput holds the data required to authenticate with an email address and a password.

| Field | Type | Description |
| --- | --- | --- |
| `Email` | `string` | Email is the login address; it is normalized before lookup. |
| `Password` | `string` | Password is the account password. |
| `RememberMe` | `bool` | RememberMe extends the created session's lifetime from SessionTTL to RememberMeTTL. |
| `IPAddress` | `string` | IPAddress and UserAgent are recorded on the session for auditing. |
| `UserAgent` | `string` |  |

#### SignInOutput

SignInOutput is the result of a successful sign-in.

| Field | Type | Description |
| --- | --- | --- |
| `User` | `*User` | User is the authenticated user record. |
| `SessionToken` | `string` | SessionToken is the raw opaque session token, returned exactly once. |
| `Session` | `*Session` | Session is the stored session record. |

#### SignUpInput

SignUpInput holds the data required to register a new user with an email address and a password.

| Field | Type | Description |
| --- | --- | --- |
| `Name` | `string` | Name is the display name. |
| `Email` | `string` | Email is the login address. |
| `Password` | `string` | Password must satisfy the configured password policy. |
| `Image` | `string` | Image is an optional avatar URL stored on the user. |
| `RememberMe` | `bool` | RememberMe extends the created session's lifetime from SessionTTL to RememberMeTTL. |
| `IPAddress` | `string` | IPAddress and UserAgent are recorded on the session for auditing. |
| `UserAgent` | `string` |  |

#### SignUpOutput

SignUpOutput is the result of a successful registration.

| Field | Type | Description |
| --- | --- | --- |
| `User` | `*User` | User is the created user record. |
| `SessionToken` | `string` | SessionToken is the raw opaque session token. |
| `Session` | `*Session` | Session is the stored session record; it does not contain the raw token. |

#### Store

Store persists authentication records.

```go
type Store interface {
	// User records.
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	ListUsers(ctx context.Context, opts ListOptions) ([]*User, error)
	CountUsers(ctx context.Context) (int64, error)
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, id string) error

	// Account records.
	CreateAccount(ctx context.Context, account *Account) error
	GetAccount(ctx context.Context, provider, providerAccountID string) (*Account, error)
	GetAccountByUser(ctx context.Context, userID, provider string) (*Account, error)
	ListAccountsByUser(ctx context.Context, userID string) ([]*Account, error)
	UpdateAccount(ctx context.Context, account *Account) error
	DeleteAccount(ctx context.Context, id string) error

	// Session records.
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	ListSessionsByUser(ctx context.Context, userID string) ([]*Session, error)
	UpdateSession(ctx context.Context, session *Session) error
	DeleteSession(ctx context.Context, id string) error
	DeleteSessionsByUser(ctx context.Context, userID string) error
	DeleteExpiredSessions(ctx context.Context, before time.Time) error

	// VerificationToken records.
	CreateVerificationToken(ctx context.Context, token *VerificationToken) error
	GetVerificationToken(ctx context.Context, identifier, valueHash string) (*VerificationToken, error)
	DeleteVerificationToken(ctx context.Context, id string) error
	DeleteVerificationTokensByIdentifier(ctx context.Context, identifier string) error
	DeleteExpiredVerificationTokens(ctx context.Context, before time.Time) error
}
```

#### UpdateUserInput

UpdateUserInput describes changes to a user profile.

| Field | Type | Description |
| --- | --- | --- |
| `Name` | `*string` | Name replaces the display name. |
| `Image` | `*string` | Image replaces the avatar URL. |

#### User

User is the primary identity record.

| Field | Type | JSON | Description |
| --- | --- | --- | --- |
| `ID` | `string` | `id` |  |
| `Name` | `string` | `name` |  |
| `Email` | `string` | `email` |  |
| `EmailVerified` | `bool` | `email_verified` |  |
| `Image` | `string` | `image` |  |
| `Banned` | `bool` | `banned` |  |
| `BanReason` | `string` | `ban_reason` |  |
| `BanExpires` | `*time.Time` | `ban_expires` (omitempty) |  |
| `CreatedAt` | `time.Time` | `created_at` |  |
| `UpdatedAt` | `time.Time` | `updated_at` |  |
| `Version` | `int64` | never | Version is the optimistic concurrency counter managed by the store. |

| Function / method | Description |
| --- | --- |
| `func MustUser(ctx context.Context) *User` | MustUser returns the authenticated user stored in ctx. |
| `func UserFromContext(ctx context.Context) (*User, bool)` | UserFromContext returns the authenticated user stored by ContextWithSession and whether one is present. |
| `func (u *User) IsBanned() bool` | IsBanned reports whether the user is banned right now according to the wall clock. |
| `func (u *User) IsBannedAt(t time.Time) bool` | IsBannedAt reports whether the user is banned at the instant t. |

#### VerificationToken

VerificationToken is a single use token used for email verification and password reset.

| Field | Type | JSON | Description |
| --- | --- | --- | --- |
| `ID` | `string` | `id` |  |
| `Identifier` | `string` | `identifier` |  |
| `ValueHash` | `string` | never |  |
| `ExpiresAt` | `time.Time` | `expires_at` |  |
| `CreatedAt` | `time.Time` | `created_at` |  |

## auth/authhttp

`import "github.com/berkkaradalan/CoreGo/auth/authhttp"`

Package authhttp exposes the CoreGo authentication service (package auth) as JSON routes on the CoreGo web framework, and provides the middleware that protects an application's own routes. Guide: [http-api.md](http-api.md).

### Constants

| Name | Definition | Description |
| --- | --- | --- |
| `DefaultBasePath` | `= "/auth"` | DefaultBasePath prefixes every mounted route when Config.BasePath is empty. |
| `DefaultCookieName` | `= "corego_session"` | DefaultCookieName is the session cookie name used when CookieConfig.Name is empty. |
| `DefaultCookiePath` | `= "/"` | DefaultCookiePath is the session cookie path used when CookieConfig.Path is empty. |
| `DefaultRateLimit` | `= 10` | DefaultRateLimit is the number of requests one client (and, on the routes limited per email address, one address) may send to one rate limited route per window when RateLimitConfig.Limit is zero. |
| `DefaultRateLimitWindow` | `= time.Minute` | DefaultRateLimitWindow is the rate limit window used when RateLimitConfig.Window is zero. |

### Types

#### Config

Config configures the HTTP layer.

| Field | Type | Description |
| --- | --- | --- |
| `BasePath` | `string` | BasePath prefixes every mounted route. |
| `Cookie` | `CookieConfig` | Cookie, when enabled, additionally transports the session token as an HttpOnly cookie for browser clients. |
| `RateLimit` | `RateLimitConfig` | RateLimit limits how often one client IP may call each sensitive route (sign-up, sign-in, the email and password flows, the email change and account deletion) and how often one email address may be used on sign-in, forgot-password and verify-email/request. |
| `TrustedOrigins` | `[]string` | TrustedOrigins lists the origins of other sites (typically your frontend on another host, such as "https://app.example.com") that may send state-changing requests authenticated with the session cookie. |
| `VerifyEmailRedirectURL` | `string` | VerifyEmailRedirectURL is where GET {BasePath}/verify-email (the link in verification emails) sends the browser after verifying, with a "status" query parameter set to "verified", "invalid" or "expired"; existing query parameters and the fragment are kept. |

| Function / method | Description |
| --- | --- |
| `func (cfg Config) ResolvedBasePath() (string, error)` | ResolvedBasePath returns the prefix Mount registers the routes under for this configuration. |
| `func (cfg Config) Validate() error` | Validate reports the first invalid value in cfg as an error starting with "authhttp: ", or nil when New would accept cfg. |

#### CookieConfig

CookieConfig describes the optional session cookie.

| Field | Type | Description |
| --- | --- | --- |
| `Enabled` | `bool` | Enabled turns the cookie transport on. |
| `Name` | `string` | Name is the cookie name. |
| `Path` | `string` | Path is the cookie path. |
| `Domain` | `string` | Domain is the cookie domain. |
| `Secure` | `bool` | Secure restricts the cookie to HTTPS. |
| `SameSite` | `http.SameSite` | SameSite is the cookie's SameSite policy. |
| `OmitTokenFromBody` | `bool` | OmitTokenFromBody leaves the "token" field out of the sign-up and sign-in responses (expires_at, user and verification_required stay), so the session token only ever travels in the HttpOnly cookie and scripts in the page, including injected ones, can never read it. |

#### Handlers

Handlers is the HTTP layer around an auth.Auth service.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func New(service *auth.Auth, cfg Config) *Handlers` | New returns Handlers that expose service over HTTP with the given configuration. |
| `func (h *Handlers) BasePath() string` | BasePath returns the resolved prefix Mount registers the routes under. |
| `func (h *Handlers) Middleware(next http.Handler) http.Handler` | Middleware is RequireAuth for plain net/http handlers and routers other than the CoreGo web framework. |
| `func (h *Handlers) Mount(r Router)` | Mount registers every auth route on r under Config.BasePath (a group's prefix comes first). |
| `func (h *Handlers) OptionalAuth() web.MiddlewareFunc` | OptionalAuth returns web middleware for routes that serve signed-in and anonymous visitors alike. |
| `func (h *Handlers) RequireAuth() web.MiddlewareFunc` | RequireAuth returns web middleware that only lets requests with a valid session through. |
| `func (h *Handlers) Routes() []RouteInfo` | Routes returns every auth route in registration order, sign-up first, with its full path under BasePath. |
| `func (h *Handlers) Token(r *http.Request) (string, bool)` | Token returns the session token carried by r. |

#### RateLimitConfig

RateLimitConfig configures the rate limits of the sensitive routes.

| Field | Type | Description |
| --- | --- | --- |
| `Disabled` | `bool` | Disabled turns rate limiting off for every route, for example when a reverse proxy or API gateway already enforces limits. |
| `Limit` | `int` | Limit is the maximum number of requests per Window from one client IP to one route, and for one email address to one of the routes limited per address. |
| `Window` | `time.Duration` | Window is the length of the rate window. |

#### RouteInfo

RouteInfo describes one auth route, for listings such as the startup banner of the corego facade.

| Field | Type | Description |
| --- | --- | --- |
| `Method` | `string` | Method is the HTTP method, for example "POST". |
| `Path` | `string` | Path is the full path under BasePath, for example "/auth/signup". |
| `RequiresSession` | `bool` | RequiresSession reports whether the route needs a valid session token (it is wrapped in RequireAuth). |

#### Router

Router is what Mount registers routes on.

```go
type Router interface {
	Handle(method, path string, h web.HandlerFunc, mw ...web.MiddlewareFunc)
}
```

## auth/mongostore

`import "github.com/berkkaradalan/CoreGo/auth/mongostore"`

Package mongostore implements the auth.Store interface on MongoDB (driver go.mongodb.org/mongo-driver/v2), on top of the CoreGo database.MongoDB wrapper. Guide: [auth-storage.md](auth-storage.md#mongodb-store).

### Constants

| Name | Definition | Description |
| --- | --- | --- |
| `DefaultCollectionPrefix` | `= "corego_"` | DefaultCollectionPrefix is the collection name prefix used when Options.CollectionPrefix is empty. |

### Types

#### Options

Options configures a Store.

| Field | Type | Description |
| --- | --- | --- |
| `CollectionPrefix` | `string` | CollectionPrefix is prepended to every collection name the store uses. |

#### Store

Store implements auth.Store on MongoDB.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewStore(db *database.MongoDB, opts Options) (*Store, error)` | NewStore returns an auth.Store that keeps its records in the database db is bound to (see database.MongoConfig.Database). |
| `func (s *Store) CountUsers(ctx context.Context) (int64, error)` | CountUsers returns the number of stored users. |
| `func (s *Store) CreateAccount(ctx context.Context, account *auth.Account) error` | CreateAccount inserts account, filling in ID and timestamps on the passed struct when the caller left them zero and setting Version to 1 (also on the passed struct). |
| `func (s *Store) CreateSession(ctx context.Context, session *auth.Session) error` | CreateSession inserts session, filling in ID and timestamps on the passed struct when the caller left them zero. |
| `func (s *Store) CreateUser(ctx context.Context, user *auth.User) error` | CreateUser inserts user. |
| `func (s *Store) CreateVerificationToken(ctx context.Context, token *auth.VerificationToken) error` | CreateVerificationToken inserts token, filling in ID and CreatedAt on the passed struct when the caller left them zero; a CreatedAt set by the caller (the auth service always sets one) is persisted as is, at millisecond precision. |
| `func (s *Store) DeleteAccount(ctx context.Context, id string) error` | DeleteAccount removes an account by ID. |
| `func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) error` | DeleteExpiredSessions removes every session whose ExpiresAt is strictly before the given instant and succeeds when none matches. |
| `func (s *Store) DeleteExpiredVerificationTokens(ctx context.Context, before time.Time) error` | DeleteExpiredVerificationTokens removes every token whose ExpiresAt is strictly before the given instant and succeeds when none matches. |
| `func (s *Store) DeleteSession(ctx context.Context, id string) error` | DeleteSession removes a session by ID. |
| `func (s *Store) DeleteSessionsByUser(ctx context.Context, userID string) error` | DeleteSessionsByUser removes every session of a user. |
| `func (s *Store) DeleteUser(ctx context.Context, id string) error` | DeleteUser removes the user and cascades to its accounts and sessions. |
| `func (s *Store) DeleteVerificationToken(ctx context.Context, id string) error` | DeleteVerificationToken removes a token by ID. |
| `func (s *Store) DeleteVerificationTokensByIdentifier(ctx context.Context, identifier string) error` | DeleteVerificationTokensByIdentifier removes every token whose identifier matches exactly (no prefix or case-insensitive matching) and succeeds when none matches. |
| `func (s *Store) EnsureIndexes(ctx context.Context) error` | EnsureIndexes creates the indexes the store relies on. |
| `func (s *Store) GetAccount(ctx context.Context, provider, providerAccountID string) (*auth.Account, error)` | GetAccount fetches an account by provider and provider specific account ID. |
| `func (s *Store) GetAccountByUser(ctx context.Context, userID, provider string) (*auth.Account, error)` | GetAccountByUser fetches the account of provider owned by userID. |
| `func (s *Store) GetSession(ctx context.Context, id string) (*auth.Session, error)` | GetSession fetches a session by ID. |
| `func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*auth.Session, error)` | GetSessionByTokenHash resolves a session token hash. |
| `func (s *Store) GetUser(ctx context.Context, id string) (*auth.User, error)` | GetUser fetches a user by ID. |
| `func (s *Store) GetUserByEmail(ctx context.Context, email string) (*auth.User, error)` | GetUserByEmail fetches a user by exact email match. |
| `func (s *Store) GetVerificationToken(ctx context.Context, identifier, valueHash string) (*auth.VerificationToken, error)` | GetVerificationToken fetches a token by its exact identifier and value hash. |
| `func (s *Store) ListAccountsByUser(ctx context.Context, userID string) ([]*auth.Account, error)` | ListAccountsByUser returns the user's accounts oldest first (created_at ascending, the ID ascending as tie-breaker). |
| `func (s *Store) ListSessionsByUser(ctx context.Context, userID string) ([]*auth.Session, error)` | ListSessionsByUser returns the user's sessions newest first (created_at descending, the ID descending as tie-breaker). |
| `func (s *Store) ListUsers(ctx context.Context, opts auth.ListOptions) ([]*auth.User, error)` | ListUsers returns users newest first (created_at descending, the ID descending as tie-breaker) with pagination; see normalizeListOptions for how Limit and Offset are interpreted. |
| `func (s *Store) UpdateAccount(ctx context.Context, account *auth.Account) error` | UpdateAccount replaces the full account record with optimistic concurrency, exactly like UpdateUser. |
| `func (s *Store) UpdateSession(ctx context.Context, session *auth.Session) error` | UpdateSession replaces the full session record and sets UpdatedAt to the current time (also on the passed struct). |
| `func (s *Store) UpdateUser(ctx context.Context, user *auth.User) error` | UpdateUser replaces the full user record with optimistic concurrency. |

## auth/pgstore

`import "github.com/berkkaradalan/CoreGo/auth/pgstore"`

Package pgstore implements auth.Store on PostgreSQL using the pgx driver and its connection pool. Guide: [auth-storage.md](auth-storage.md#postgresql-store).

### Constants

| Name | Definition | Description |
| --- | --- | --- |
| `DefaultTablePrefix` | `= "corego_"` | DefaultTablePrefix is the table name prefix used when Options.TablePrefix is empty. |
| `MaxTablePrefixLength` | `= 24` | MaxTablePrefixLength is the longest accepted Options.TablePrefix in bytes. |

### Variables

| Name | Definition | Description |
| --- | --- | --- |
| `ErrNilPool` | `= errors.New("pgstore: pool must not be nil")` | ErrNilPool is returned by NewStore when the pool is nil. |
| `ErrInvalidTablePrefix` | `= errors.New("pgstore: invalid table prefix")` | ErrInvalidTablePrefix is returned by NewStore when Options.TablePrefix contains characters other than lowercase ASCII letters, digits and underscores, or is longer than MaxTablePrefixLength. |

### Types

#### Options

Options configures a Store.

| Field | Type | Description |
| --- | --- | --- |
| `TablePrefix` | `string` | TablePrefix is prepended verbatim to every table name, so "myapp_" yields myapp_users, myapp_accounts and so on. |

#### Store

Store implements auth.Store on PostgreSQL.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewStore(pool *pgxpool.Pool, opts Options) (*Store, error)` | NewStore returns a Store that issues its queries through pool, a connected pool such as the one returned by database.PostgresDB.Pool. |
| `func (s *Store) CountUsers(ctx context.Context) (int64, error)` | CountUsers implements auth.Store. |
| `func (s *Store) CreateAccount(ctx context.Context, account *auth.Account) error` | CreateAccount implements auth.Store. |
| `func (s *Store) CreateSession(ctx context.Context, session *auth.Session) error` | CreateSession implements auth.Store. |
| `func (s *Store) CreateUser(ctx context.Context, user *auth.User) error` | CreateUser implements auth.Store. |
| `func (s *Store) CreateVerificationToken(ctx context.Context, token *auth.VerificationToken) error` | CreateVerificationToken implements auth.Store. |
| `func (s *Store) DeleteAccount(ctx context.Context, id string) error` | DeleteAccount implements auth.Store. |
| `func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) error` | DeleteExpiredSessions implements auth.Store. |
| `func (s *Store) DeleteExpiredVerificationTokens(ctx context.Context, before time.Time) error` | DeleteExpiredVerificationTokens implements auth.Store. |
| `func (s *Store) DeleteSession(ctx context.Context, id string) error` | DeleteSession implements auth.Store. |
| `func (s *Store) DeleteSessionsByUser(ctx context.Context, userID string) error` | DeleteSessionsByUser implements auth.Store. |
| `func (s *Store) DeleteUser(ctx context.Context, id string) error` | DeleteUser implements auth.Store. |
| `func (s *Store) DeleteVerificationToken(ctx context.Context, id string) error` | DeleteVerificationToken implements auth.Store. |
| `func (s *Store) DeleteVerificationTokensByIdentifier(ctx context.Context, identifier string) error` | DeleteVerificationTokensByIdentifier implements auth.Store. |
| `func (s *Store) GetAccount(ctx context.Context, provider, providerAccountID string) (*auth.Account, error)` | GetAccount implements auth.Store. |
| `func (s *Store) GetAccountByUser(ctx context.Context, userID, provider string) (*auth.Account, error)` | GetAccountByUser implements auth.Store. |
| `func (s *Store) GetSession(ctx context.Context, id string) (*auth.Session, error)` | GetSession implements auth.Store. |
| `func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*auth.Session, error)` | GetSessionByTokenHash implements auth.Store. |
| `func (s *Store) GetUser(ctx context.Context, id string) (*auth.User, error)` | GetUser implements auth.Store. |
| `func (s *Store) GetUserByEmail(ctx context.Context, email string) (*auth.User, error)` | GetUserByEmail implements auth.Store. |
| `func (s *Store) GetVerificationToken(ctx context.Context, identifier, valueHash string) (*auth.VerificationToken, error)` | GetVerificationToken implements auth.Store. |
| `func (s *Store) ListAccountsByUser(ctx context.Context, userID string) ([]*auth.Account, error)` | ListAccountsByUser implements auth.Store. |
| `func (s *Store) ListSessionsByUser(ctx context.Context, userID string) ([]*auth.Session, error)` | ListSessionsByUser implements auth.Store. |
| `func (s *Store) ListUsers(ctx context.Context, opts auth.ListOptions) ([]*auth.User, error)` | ListUsers implements auth.Store. |
| `func (s *Store) Migrate(ctx context.Context) (err error)` | Migrate brings the store's tables up to date. |
| `func (s *Store) TablePrefix() string` | TablePrefix returns the effective table prefix of the store, for example "corego_" when Options.TablePrefix was left empty. |
| `func (s *Store) UpdateAccount(ctx context.Context, account *auth.Account) error` | UpdateAccount implements auth.Store with optimistic concurrency, exactly like UpdateUser. |
| `func (s *Store) UpdateSession(ctx context.Context, session *auth.Session) error` | UpdateSession implements auth.Store. |
| `func (s *Store) UpdateUser(ctx context.Context, user *auth.User) error` | UpdateUser implements auth.Store with optimistic concurrency. |

## database

`import "github.com/berkkaradalan/CoreGo/database"`

Package database connects CoreGo applications to MongoDB and PostgreSQL and offers small, context-aware helpers on top of the official drivers (go.mongodb.org/mongo-driver/v2 and github.com/jackc/pgx/v5). Guide: [database.md](database.md).

### Constants

| Name | Definition | Description |
| --- | --- | --- |
| `DefaultMongoDatabase` | `= "corego"` | DefaultMongoDatabase is the database name used when neither MongoConfig.Database nor the connection URL names one. |

### Variables

| Name | Definition | Description |
| --- | --- | --- |
| `ErrNotFound` | `= errors.New("database: not found")` | ErrNotFound reports that a lookup, update or delete matched nothing. |

### Functions

| Signature | Description |
| --- | --- |
| `func QueryAll[T any](ctx context.Context, q Querier, sql string, args ...any) ([]T, error)` | QueryAll runs sql on q and scans every row into a T. |
| `func QueryOne[T any](ctx context.Context, q Querier, sql string, args ...any) (T, error)` | QueryOne runs sql on q and scans the first row into a T, using the same mapping rules as QueryAll. |

### Types

#### MongoConfig

MongoConfig configures a MongoDB connection.

| Field | Type | Description |
| --- | --- | --- |
| `URL` | `string` | URL is the MongoDB connection string, for example "mongodb://localhost:27017/myapp" or "mongodb+srv://user:pass@cluster.example.net/myapp?retryWrites=true". |
| `Database` | `string` | Database names the database the helpers operate on. |

#### MongoDB

MongoDB is a connected MongoDB client bound to one database.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewMongoDB(cfg MongoConfig) (*MongoDB, error)` | NewMongoDB connects to MongoDB, pings the server and returns a handle bound to the database resolved from cfg (see MongoConfig.Database). |
| `func (m *MongoDB) Client() *mongo.Client` | Client returns the underlying driver client, for example to start a session or a multi-document transaction. |
| `func (m *MongoDB) Close() error` | Close disconnects the client, waiting at most 10 seconds for in-flight operations. |
| `func (m *MongoDB) Collection(name string) *mongo.Collection` | Collection returns the driver handle of a collection in the configured database. |
| `func (m *MongoDB) Count(ctx context.Context, collection string, filter any) (int64, error)` | Count returns the number of documents matching filter. |
| `func (m *MongoDB) Database() *mongo.Database` | Database returns the driver handle of the configured database. |
| `func (m *MongoDB) DeleteMany(ctx context.Context, collection string, filter any) (int64, error)` | DeleteMany deletes every document matching filter and returns how many were deleted. |
| `func (m *MongoDB) DeleteOne(ctx context.Context, collection string, filter any) error` | DeleteOne deletes the first document matching filter. |
| `func (m *MongoDB) Find(ctx context.Context, collection string, filter any, out any, opts ...options.Lister[options.FindOptions]) error` | Find decodes every document matching filter into out, which must be a non-nil pointer to a slice (for example *[]Task). |
| `func (m *MongoDB) FindOne(ctx context.Context, collection string, filter any, out any) error` | FindOne decodes the first document matching filter into out, which must be a non-nil pointer (to a struct, a bson.M, ...). |
| `func (m *MongoDB) InsertOne(ctx context.Context, collection string, document any) (any, error)` | InsertOne inserts document into collection and returns the inserted _id exactly as the driver reports it. |
| `func (m *MongoDB) Name() string` | Name returns the resolved database name. |
| `func (m *MongoDB) Ping(ctx context.Context) error` | Ping checks that the server is reachable. |
| `func (m *MongoDB) UpdateMany(ctx context.Context, collection string, filter, update any) (int64, error)` | UpdateMany applies update to every document matching filter and returns how many documents matched. |
| `func (m *MongoDB) UpdateOne(ctx context.Context, collection string, filter, update any) error` | UpdateOne applies update (for example bson.M{"$set": ...}) to the first document matching filter. |

#### PostgresConfig

PostgresConfig configures a PostgreSQL connection.

| Field | Type | Description |
| --- | --- | --- |
| `URL` | `string` | URL is the PostgreSQL connection string, for example "postgres://user:pass@localhost:5432/myapp?sslmode=disable". |

#### PostgresDB

PostgresDB is a connected PostgreSQL connection pool.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewPostgresDB(cfg PostgresConfig) (*PostgresDB, error)` | NewPostgresDB creates a connection pool, pings the server and returns the handle. |
| `func (p *PostgresDB) Close() error` | Close closes every connection in the pool, waiting for acquired connections to be released. |
| `func (p *PostgresDB) Exec(ctx context.Context, sql string, args ...any) (int64, error)` | Exec runs a statement that returns no rows (INSERT, UPDATE, DELETE, DDL) and returns the number of affected rows. |
| `func (p *PostgresDB) Ping(ctx context.Context) error` | Ping checks that the server is reachable. |
| `func (p *PostgresDB) Pool() *pgxpool.Pool` | Pool returns the underlying pgx connection pool. |
| `func (p *PostgresDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)` | Query runs a statement that returns rows and hands back the raw pgx.Rows. |
| `func (p *PostgresDB) QueryMaps(ctx context.Context, sql string, args ...any) ([]map[string]any, error)` | QueryMaps runs a statement that returns rows and returns them as maps keyed by column name, for ad-hoc queries where declaring a struct is not worth it. |
| `func (p *PostgresDB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error` | WithTx runs fn inside a transaction. |

#### Querier

Querier is anything that can run a query returning rows.

```go
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
```

## mail

`import "github.com/berkkaradalan/CoreGo/mail"`

Package mail sends email through pluggable backends. Guide: [mail.md](mail.md).

### Types

#### CaptureSender

CaptureSender is a Sender that records every message it receives in memory.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewCaptureSender() *CaptureSender` | NewCaptureSender returns an empty CaptureSender. |
| `func (c *CaptureSender) Last() (Message, bool)` | Last returns a copy of the most recently captured message and whether any message has been captured at all. |
| `func (c *CaptureSender) Messages() []Message` | Messages returns a deep copy of all captured messages in the order they were sent. |
| `func (c *CaptureSender) Reset()` | Reset discards all captured messages. |
| `func (c *CaptureSender) Send(_ context.Context, msg Message) error` | Send implements Sender. |

#### LogSender

LogSender is a Sender that logs messages instead of delivering them.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewLogSender(logger *slog.Logger) *LogSender` | NewLogSender returns a LogSender that logs to logger. |
| `func (l *LogSender) Send(ctx context.Context, msg Message) error` | Send implements Sender. |

#### Message

Message represents an outgoing email message.

| Field | Type | Description |
| --- | --- | --- |
| `From` | `string` |  |
| `To` | `[]string` |  |
| `Cc` | `[]string` |  |
| `Bcc` | `[]string` |  |
| `ReplyTo` | `string` |  |
| `Subject` | `string` |  |
| `Text` | `string` |  |
| `HTML` | `string` |  |

| Function / method | Description |
| --- | --- |
| `func (m Message) Validate() error` | Validate reports whether the message satisfies the minimum rules for sending. |

#### SMTPConfig

SMTPConfig configures an SMTPSender.

| Field | Type | Description |
| --- | --- | --- |
| `Host` | `string` | Host is the SMTP server hostname or IP address. |
| `Port` | `int` | Port is the SMTP server port. |
| `Username` | `string` | Username and Password enable SMTP authentication (AUTH PLAIN). |
| `Password` | `string` |  |
| `From` | `string` | From is the envelope sender (MAIL FROM) and the default value for Message.From when a message does not set its own From. |
| `TLSMode` | `TLSMode` | TLSMode selects STARTTLS (default), implicit TLS or plaintext. |
| `Timeout` | `time.Duration` | Timeout bounds both the connection dial and every individual SMTP command exchange. |

#### SMTPSender

SMTPSender delivers messages over SMTP using net/smtp.

Opaque: no exported fields.

| Function / method | Description |
| --- | --- |
| `func NewSMTPSender(cfg SMTPConfig) *SMTPSender` | NewSMTPSender returns an SMTPSender with defaults applied. |
| `func (s *SMTPSender) Send(ctx context.Context, msg Message) error` | Send implements Sender. |

#### Sender

Sender delivers email messages.

```go
type Sender interface {
	Send(ctx context.Context, msg Message) error
}
```

#### SenderFunc

SenderFunc adapts a plain function to the Sender interface.

```go
type SenderFunc func(ctx context.Context, msg Message) error
```

| Function / method | Description |
| --- | --- |
| `func (f SenderFunc) Send(ctx context.Context, msg Message) error` | Send implements Sender. |

#### TLSMode

TLSMode selects how an SMTPSender secures its connection.

```go
type TLSMode int
```

| Constant | Definition | Description |
| --- | --- | --- |
| `TLSStartTLS` | `TLSMode = iota` | TLSStartTLS connects over plaintext and upgrades the session with the STARTTLS extension. |
| `TLSImplicit` |  | TLSImplicit establishes TLS before the SMTP dialog starts (formerly "SMTPS", typically served on port 465). |
| `TLSDisabled` |  | TLSDisabled speaks plaintext SMTP for the whole dialog. |

| Function / method | Description |
| --- | --- |
| `func (m TLSMode) String() string` | String returns a human-readable name for the mode. |

## env

`import "github.com/berkkaradalan/CoreGo/env"`

Package env loads application configuration from the process environment and an optional .env file. Guide: [environment.md](environment.md).

### Constants

| Name | Definition | Description |
| --- | --- | --- |
| `DefaultFile` | `= ".env"` | DefaultFile is the file Load reads from the current working directory. |
| `DefaultPort` | `= "8080"` | DefaultPort is the value of Env.Port when PORT is unset or empty. |

### Types

#### Env

Env is an immutable snapshot of the configuration visible to the application.

| Field | Type | Description |
| --- | --- | --- |
| `AppURL` | `string` | AppURL is the public base URL of the application, used to build links such as the ones in verification emails. |
| `Port` | `string` | Port is the HTTP port the application listens on (PORT, default "8080"). |
| `MongoURL` | `string` | MongoURL is the MongoDB connection string from MONGODB_CONNECTION_URL; empty when MongoDB is not configured. |
| `MongoDatabase` | `string` | MongoDatabase is the MongoDB database name from MONGODB_DATABASE; empty when not set. |
| `PostgresURL` | `string` | PostgresURL is the PostgreSQL connection string from POSTGRES_CONNECTION_URL; empty when PostgreSQL is not configured. |

| Function / method | Description |
| --- | --- |
| `func Load() (*Env, error)` | Load reads ./.env when it exists, merges it with the process environment and returns the resulting snapshot. |
| `func LoadFile(path string) (*Env, error)` | LoadFile is like Load but reads the given file instead of ./.env. |
| `func (e *Env) Bool(key string, def bool) bool` | Bool returns the variable parsed with strconv.ParseBool (1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False), or def when it is unset, empty or not a valid boolean. |
| `func (e *Env) Duration(key string, def time.Duration) time.Duration` | Duration returns the variable parsed with time.ParseDuration (for example "30s", "5m", "1h30m"), or def when it is unset, empty or not a valid duration. |
| `func (e *Env) Get(key string) (string, bool)` | Get returns the raw value of a variable and whether it is set. |
| `func (e *Env) GetOrDefault(key, def string) string` | GetOrDefault returns the value of a variable with surrounding whitespace removed, or def when the variable is unset, empty or only whitespace. |
| `func (e *Env) Int(key string, def int) int` | Int returns the variable parsed as a base-10 integer, or def when it is unset, empty or not a valid integer. |
| `func (e *Env) Strings(key string) []string` | Strings splits a comma separated variable into its elements, trimming whitespace around each one and dropping empty elements. |

