# Web Framework

The `web` package is CoreGo's HTTP framework: a router,
middleware chain and request context built only on the Go standard library.
The [auth HTTP routes](http-api.md) are built on it, and it is meant for
your own application routes too.

Three ideas carry the whole design:

- A `*web.Server` is an `http.Handler`.
- Handlers have the signature `func(c *web.Context) error` and **return**
  errors; one error handler turns them into JSON responses.
- `*web.Context` is also a `context.Context`, so you pass `c` straight to
  your services and database calls.

## Adding an endpoint

```go
s := web.New()

api := s.Group("/api")
api.Get("/users/:id", func(c *web.Context) error {
	user, err := users.Find(c, c.Param("id")) // c is a context.Context
	if errors.Is(err, ErrUserNotFound) {
		return web.NotFound("user not found") // 404 {"error":"user not found"}
	}
	if err != nil {
		return err // anything else: logged, 500 {"error":"Internal Server Error"}
	}
	return c.JSON(http.StatusOK, user)
})

if err := s.Run(":8080"); err != nil {
	log.Fatal(err)
}
```

That is the whole pattern: register a route, read the request from `c`,
end with `return c.JSON(...)` (every response writer returns an error), or
return an error and let the framework render it. Map only the errors you
expect (here the repository's `ErrUserNotFound`, or
`database.ErrNotFound`) to a status and return everything else unchanged,
so a broken database shows up as a logged 500 instead of a misleading 404.
Registration never returns an error; a mistake such as a duplicate route
panics at startup with a message starting with `web: `.

With the facade, `core.Server()` returns a `*web.Server` that already has
request logging and the auth routes; register on it the same way and
start it with `core.Run()` ([getting-started.md](getting-started.md)).

## Routing

```go
s.Get("/users", listUsers)
s.Post("/users", createUser)
s.Get("/users/:id", getUser)          // c.Param("id")
s.Get("/files/*path", serveFile)      // c.Param("path") = rest of the path
s.Handle("PROPFIND", "/dav", davHandler) // any method
```

`Get`, `Post`, `Put`, `Patch`, `Delete`, `Head` and `Options` are
shortcuts for `Handle(method, path, handler, middleware...)`. Every
registration method accepts optional per-route middleware after the
handler (see [Middleware](#middleware)).

### Matching rules

Routes are stored in one tree per HTTP method. At every level, segments
match with this precedence:

1. **Static segments** (`/users/new`),
2. then **`:name` parameters**, which match exactly one non-empty segment
   (`/users/:id`),
3. then **`*name` wildcards**, which must be the last segment and capture
   the rest of the path, possibly the empty string (`/files/*path` matches
   `/files`, `/files/a` and `/files/a/b.txt`).

Lookup backtracks out of dead ends, so `/users/new` and `/users/:id` can
coexist and a parameter never shadows a matching static route.

Normalization:

- **Trailing slashes are ignored**: `/users/` matches `/users`, and
  registering both is a duplicate.
- **Interior double slashes never match**: `/users//42` is a 404.
- **Percent-decoding happens exactly once**, for matching and for parameter
  values: `/users/a%20b` gives `c.Param("id") == "a b"`, and `%2F` stays
  inside its segment.
- Matching is case-sensitive for paths; methods are case-insensitive
  (`Handle("get", ...)` registers `GET`).

### 404, 405, OPTIONS and HEAD

| Request | Response |
| --- | --- |
| Path matches no route | `404 {"error":"Not Found"}` |
| Path exists, method not registered | `405 {"error":"Method Not Allowed"}` with an `Allow` header listing the registered methods |
| `OPTIONS` on a known path without an explicit OPTIONS route | `204` with `Allow` (registered methods plus `OPTIONS`) |
| `HEAD` without an explicit HEAD route | Served by the GET route; the body is discarded and `Content-Length` reports its size |

These fallbacks are ordinary handlers dispatched through your **global
middleware**, so CORS, request logging and a custom error handler apply to
them too.

### Registration errors panic

Invalid registrations are programmer errors, so (like
`net/http.ServeMux`) they panic immediately with a descriptive `web: `
message instead of returning an error you would have to check on every
line:

| Mistake | Example panic message |
| --- | --- |
| Empty path on the server (`s.Get("", h)`; on a group `""` is allowed, see [Groups](#groups)) | `web: invalid route path for GET: path must not be empty (use "/" for the root)` |
| No leading `/` | `web: invalid route path "users" for GET: must start with '/'` |
| Nil handler | `web: handler for GET "/x" must not be nil` |
| Nil middleware | `web: route GET /x: middleware at index 0 is nil` |
| Empty or malformed method | `web: invalid HTTP method "GE T" (route "/x")` |
| Duplicate method + path | `web: route GET /users is already registered` |
| Different parameter names at one position | `web: route GET /users/:uid: parameter name "uid" conflicts with existing parameter "id" at the same position` |
| Wildcard not last | `web: invalid route "/a/*w/b": wildcard "w" must be the last segment` |
| Empty segment, unnamed parameter, bad escape | `web: invalid route "/a//b": empty path segment at position 1` |
| Invalid group prefix | `web: invalid group prefix "api": must start with '/'` |

Because these happen while your program wires its routes, they surface the
first time the program (or a test that builds the server) starts.

### Listing routes

`s.Routes()` returns every registered route as `[]web.Route{Method, Path}`,
sorted by path and then method, with group prefixes applied. Automatic HEAD
and OPTIONS answers are not listed. Handy for a startup log or a test that
asserts your API surface.

## Groups

```go
api := s.Group("/api", requestID)
v1 := api.Group("/v1", requireAuth) // prefix /api/v1, middleware requestID then requireAuth

v1.Get("/me", me)                 // GET /api/v1/me
v1.Delete("/sessions/:id", revoke)

orgs := s.Group("/orgs/:org")     // parameters are allowed in prefixes
orgs.Get("/repos", listRepos)     // c.Param("org")
```

- A prefix must start with `/`; `""` and `"/"` mean no prefix, and a
  trailing slash is dropped. Prefixes may contain `:name` parameters but
  not wildcards.
- `api.Get("", h)` and `api.Get("/", h)` both register the group's own
  path (`/api`; also reachable as `/api/`). They are the same route, so
  registering both panics as a duplicate. Only groups accept `""`: on the
  server itself an empty path panics (use `"/"` for the root).

```go
users := s.Group("/users")
users.Get("", listUsers)     // GET /users
users.Post("", createUser)   // POST /users
users.Get("/:id", showUser)  // GET /users/:id
```
- Child groups extend the parent's prefix; the parent's middleware always
  runs before the child's.
- `g.Use(mw...)` adds middleware to an existing group. It applies to routes
  registered **after** the call, on the group and on its child groups
  (including children created before the call). Routes that already exist
  are not changed, so call `Use` before registering routes.
- Two groups with the same prefix are independent: each keeps its own
  middleware.

## Middleware

```go
type HandlerFunc func(*web.Context) error
type MiddlewareFunc func(web.HandlerFunc) web.HandlerFunc
```

### Order

For a matched route, middleware runs outermost first in this order:

1. **Global** middleware from `s.Use(...)`, first registered first. It also
   wraps the 404, 405 and automatic OPTIONS answers.
2. **Group** middleware, parent groups before child groups.
3. **Per-route** middleware, `s.Get(path, h, first, second)`.
4. The built-in **panic recovery**.
5. The handler.

```go
s.Use(web.RequestLogger(nil))   // 1
api := s.Group("/api", authn)   // 2
api.Post("/upload", upload, web.RateLimit(web.RateLimitConfig{ // 3
	Limit: 5, Window: time.Minute,
}))
```

The outer function of a middleware runs once when the chain is composed
(per route for group and per-route middleware, once for the global chain),
not per request. Put setup there and per-request work in the returned
handler.

### Writing your own

```go
func requestID(next web.HandlerFunc) web.HandlerFunc {
	return func(c *web.Context) error {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = newID()
		}
		c.SetHeader("X-Request-ID", id)
		// Make the ID visible to anything reading the context.Context.
		ctx := context.WithValue(c.Request().Context(), requestIDKey{}, id)
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func requireAdmin(next web.HandlerFunc) web.HandlerFunc {
	return func(c *web.Context) error {
		if !isAdmin(c) {
			return web.Forbidden("admins only") // abort: the error handler answers 403
		}
		return next(c)
	}
}
```

A middleware can:

- **continue** by calling `next(c)` (and may inspect its error),
- **short-circuit** by writing a response without calling `next`
  (`return c.NoContent()`),
- **abort** by returning an error, which the error handler renders.

### Recovery

Every route is wrapped in a panic recovery, innermost of all, and the
server catches panics in global middleware as a last resort, so you do not
need to install anything. A panic is logged (with the stack trace) to
`Server.Logger` and answered with a generic `500 {"error":"Internal Server
Error"}`; panic details never reach the client and the server keeps
serving. `web.Recovery()` is exported for custom stacks that want recovery
at a specific position. A panic with `http.ErrAbortHandler` is re-raised so
`net/http` aborts the response silently, as that sentinel intends.

### RequestLogger

```go
s.Use(web.RequestLogger(nil)) // nil = slog.Default(), resolved per request
```

Logs one `http request` record per request with `method`, `path`,
`status`, `duration` and `remote_ip`; 5xx responses are logged at Error
level, everything else at Info. Register it first so it is outermost and
also sees 404/405/OPTIONS answers and panics.

To log the final status, `RequestLogger` renders errors from the rest of
the chain through the error handler itself and then returns nil, so
middleware registered before it sees a nil error. The query string is
deliberately not logged, because it may carry secrets such as tokens.

### CORS

```go
s.Use(web.CORS(web.CORSConfig{
	AllowedOrigins:   []string{"https://app.example.com"},
	AllowCredentials: true,
}))
```

Install CORS with `s.Use` so it also answers preflight requests for routes
that have no explicit OPTIONS handler.

| Field | Default | Meaning |
| --- | --- | --- |
| `AllowedOrigins` | none | Exact origins (`https://app.example.com`; scheme and host compared case-insensitively, a trailing slash ignored), or `"*"` for any origin. `"*"` never matches the opaque `null` origin; list `"null"` explicitly if you need it. |
| `AllowOriginFunc` | nil | `func(origin string) bool`, consulted when `AllowedOrigins` does not match (for example preview deployments on a common domain). |
| `AllowedMethods` | `GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS` | Sent in `Access-Control-Allow-Methods`. |
| `AllowedHeaders` | `Content-Type, Authorization` | Sent in `Access-Control-Allow-Headers`. `"*"` echoes whatever the preflight requests. |
| `ExposedHeaders` | none | Response headers scripts may read (`Access-Control-Expose-Headers`). |
| `AllowCredentials` | `false` | Allow cookies and HTTP auth (`Access-Control-Allow-Credentials: true`). **Cannot be combined with `"*"`**: `CORS` panics, because every website could then make authenticated requests with your visitors' cookies. List the origins, or decide per origin with `AllowOriginFunc`. |
| `MaxAge` | 10 minutes | Preflight cache (`Access-Control-Max-Age`, whole seconds). Negative omits the header. |

An empty `CORSConfig` allows no origin. `"*"` (without credentials) is
answered with the literal `Access-Control-Allow-Origin: *`; listed and
`AllowOriginFunc` origins are echoed back. `AllowCredentials` together with
`"*"` panics at construction with:

```
web: CORS: AllowCredentials cannot be combined with the "*" origin; list the allowed origins or use AllowOriginFunc
```

Behavior:

- Requests **without an `Origin` header** pass through untouched.
- Every request with an Origin gets `Vary: Origin`, so caches keep
  per-origin variants apart.
- **Preflight** (OPTIONS + `Origin` + `Access-Control-Request-Method`) from
  an allowed origin: `204` with Allow-Origin, Allow-Methods, Allow-Headers,
  Max-Age and (when enabled) Allow-Credentials. From a disallowed origin:
  `403 {"error":"Forbidden"}`. Either way the chain stops there.
- **Other requests** from an allowed origin get `Access-Control-Allow-Origin`
  (plus Allow-Credentials and Expose-Headers when configured) and continue;
  the headers are set before your handler runs, so error responses carry
  them too. Requests from a disallowed origin continue **without** CORS
  headers and the browser withholds the response from the calling script.

### RateLimit

```go
auth := s.Group("/auth", web.RateLimit(web.RateLimitConfig{
	Limit:  10,
	Window: time.Minute,
}))
```

| Field | Default | Meaning |
| --- | --- | --- |
| `Limit` | required, > 0 | Requests allowed per `Window` for one key. |
| `Window` | required, > 0 | Length of the window. |
| `Key` | `web.ClientKey` (client IP, IPv6 grouped by /64) | `func(*web.Context) string` choosing the bucket, for example a user ID or API key. |
| `Message` | `web.DefaultRateLimitMessage` ("Too many requests. Please try again later.") | Error message of the 429 response. |
| `MaxKeys` | `web.DefaultRateLimitMaxKeys` (100000) | Most keys tracked individually; see below. Must not be negative. |

`RateLimit` panics when `Limit` or `Window` is not positive or `MaxKeys` is
negative.

- **Algorithm: sliding-window counter.** Each key counts requests accepted
  in the current fixed window and the previous one; a request is accepted
  while `previous × (unelapsed fraction of the current window) + current <
  Limit`. This approximates "at most `Limit` requests in any `Window`-long
  span" without storing timestamps and without the double burst that plain
  fixed windows allow at a boundary. Rejected requests do not count.
- Every response carries `RateLimit-Limit` and `RateLimit-Remaining`.
  A rejected request gets `429 {"error":"<Message>"}` (via
  `web.TooManyRequests`) and a `Retry-After` header in whole seconds (at
  least 1) telling the client when its next request will be accepted.
- **Each `RateLimit` call is an independent limiter.** Used as group
  middleware, one limiter is shared by all routes of the group; used per
  route, each route has its own budget.
- Safe for concurrent use. Keys idle for two windows are evicted (a lazy
  sweep, at most once per window).
- **Memory is bounded by `MaxKeys`.** When `MaxKeys` keys are tracked and
  the sweep frees nothing, every further new key shares **one overflow
  bucket** (same `Limit` and `Window`) until room frees up. A client
  rotating keys (addresses, user names) is therefore limited collectively
  instead of getting a fresh budget per key. Keys already tracked keep
  their own budget; the trade-off is that legitimate new keys arriving
  while the limiter is full share the overflow bucket too.
- State is in memory, per process: with several instances behind a load
  balancer each instance enforces its own limit.
- The default key is `web.ClientKey(c)`: the client IP, with **IPv6
  addresses reduced to their /64 prefix** (`2001:db8:1:2::/64`). One IPv6
  subscriber usually controls a whole /64 and could otherwise rotate
  through addresses to bypass the limit. IPv4 addresses (and IPv4-mapped
  IPv6, which is unmapped) are used as-is. Behind a reverse proxy, enable
  [`TrustProxyHeaders`](#behind-a-reverse-proxy) or every client shares the
  proxy's address and budget.

#### RateLimiter

`RateLimit` is built on an exported limiter you can use directly, for
example to limit by a value from the request body or to apply several
limits in one handler:

```go
perEmail := web.NewRateLimiter(web.RateLimitConfig{Limit: 5, Window: time.Minute})

s.Post("/login", func(c *web.Context) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BindJSON(&in); err != nil {
		return err
	}
	allowed, retryAfter, _ := perEmail.Allow(strings.ToLower(strings.TrimSpace(in.Email)))
	if !allowed {
		c.SetHeader("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
		return web.TooManyRequests("")
	}
	// ... check the password and start the session ...
	return c.NoContent()
})
```

| API | Meaning |
| --- | --- |
| `web.NewRateLimiter(cfg)` | Same algorithm, eviction, `MaxKeys` bound and panics as `RateLimit`; `Key` and `Message` are ignored. |
| `(*RateLimiter).Allow(key) (allowed bool, retryAfter time.Duration, remaining int)` | Counts one request for `key` if it fits. `retryAfter` is how long until the next request for `key` would be accepted (0 when allowed); `remaining` is how many more fit right now (0 when rejected). Any string, including `""`, is a valid key. |
| `web.ClientKey(c)` | The default `RateLimit` key described above; use it for your own per-client limiters. |

## The Context

The server creates one `*web.Context` per request. It is not safe for
concurrent use by several goroutines and must not be kept after the
handler returns.

### Request data

| Method | Returns |
| --- | --- |
| `Param(name)` | Value of a `:name` / `*name` path segment, `""` when absent. Percent-decoded once. |
| `Query(name)` | First query value, `""` when absent. |
| `DefaultQuery(name, def)` | First query value, or `def` when the parameter is absent (`?q=` returns `""`). |
| `Queries()` | All query parameters (`url.Values`). |
| `Path()` / `Method()` | Decoded path without the query string; HTTP method. |
| `GetHeader(name)` | First request header value (case-insensitive name). |
| `Cookie(name)` | Cookie value, or `http.ErrNoCookie`. |
| `RemoteIP()` | Client IP address; see [Behind a reverse proxy](#behind-a-reverse-proxy). |
| `Request()` | The current `*http.Request` (streaming uploads, TLS info, ...). |

### Context is a context.Context

`Deadline`, `Done`, `Err` and `Value` delegate to the current request's
context, so `c` is canceled when the client disconnects and can be passed
anywhere a `context.Context` is expected:

```go
rows, err := db.Query(c, "SELECT ...")   // canceled if the client goes away
user := auth.MustUser(c)                 // reads a value attached by middleware
```

To attach a value for downstream code, derive a new request context and
install it with `SetRequest`:

```go
ctx := context.WithValue(c.Request().Context(), tenantKey{}, tenant)
c.SetRequest(c.Request().WithContext(ctx))
```

`SetRequest` keeps path parameters and `Set` values; it panics on nil.
`Value` sees only request-context values, not values stored with `Set`.

### Reading the body

`Body()` reads the body once and caches it. `BindJSON(&v)` requires
`Content-Type: application/json` (parameters such as `charset` are ignored)
and decodes the cached body:

```go
var in struct {
	Name string `json:"name"`
}
if err := c.BindJSON(&in); err != nil {
	return err // 415, 400 or 413, rendered for you
}
```

| Situation | Error | Default response |
| --- | --- | --- |
| Wrong or missing Content-Type | `web.ErrUnsupportedMediaType` | 415 `{"error":"Content-Type must be application/json."}` |
| Empty or malformed JSON, data after the JSON value | `web.ErrInvalidJSON` | 400 `{"error":"Request body is not valid JSON."}` |
| Body larger than `Server.MaxBodyBytes` | `web.ErrBodyTooLarge` | 413 `{"error":"Request body exceeds the configured size limit."}` |

The errors are wrapped with detail (the offending Content-Type, the JSON
syntax error) but always match `errors.Is`; the detail is for your logs and
never reaches the client. The CoreGo auth routes answer these cases with
exactly the same messages.

#### Strict binding

`BindJSON` follows `encoding/json`: object keys that match no field are
**ignored**, so `{"nmae": "Ada"}` silently leaves `Name` empty.
`BindJSONStrict(&v)` behaves the same in every other way but also rejects
unknown keys (at any nesting level) with `web.ErrInvalidJSON`:

```go
var in createUser
if err := c.BindJSONStrict(&in); err != nil {
	return err // {"nmae":"Ada"} -> 400 "Request body is not valid JSON."
}
```

| | `BindJSON` | `BindJSONStrict` |
| --- | --- | --- |
| Unknown object keys | ignored | `ErrInvalidJSON` wrapped as `web: invalid JSON body: json: unknown field "nmae"` |
| Data after the JSON value (`{} x`, `{}{}`) | `ErrInvalidJSON` | `ErrInvalidJSON` (`unexpected data after the JSON value`) |
| Content-Type, empty body, size limit, malformed JSON | identical | identical |

The field name is only in the returned error; the default error handler
still answers the generic 400 message. To show it to the client, return
your own error, for example `web.BadRequest("unknown field in request")`.
Field names match like `encoding/json` (exact first, then
case-insensitive), and map destinations accept any key. On an error the
destination may be partially filled, as with `BindJSON`.

### Writing the response

Every writer returns an error (the `ResponseWriter`'s write error, normally
nil), so a handler ends with `return c.<Writer>(...)`:

| Method | Writes |
| --- | --- |
| `JSON(code, v)` | `v` as JSON, `Content-Type: application/json; charset=utf-8`. If `v` cannot be encoded, **nothing** is written and an error is returned, so the client gets a clean 500. |
| `String(code, s)` | `text/plain; charset=utf-8` |
| `HTML(code, s)` | `text/html; charset=utf-8`; `s` is written verbatim, so escape untrusted data first (for example with `html/template`). |
| `Data(code, contentType, b)` | Raw bytes; an empty content type leaves the header unset. |
| `NoContent()` | `204` without a body. |
| `Status(code)` | Status without a body. |
| `Redirect(code, location)` | 3xx with `Location`; a code outside 300-399 writes nothing and returns an error. |

Response helpers:

- `SetHeader(name, value)` sets a response header (before writing).
- `SetCookie(cookie *http.Cookie)` adds a `Set-Cookie` header; nil is
  ignored.
- The first status written wins; later status writes are ignored.
- `Written()` reports whether the response has started, including writes
  made directly through `Response()`.
- `Response()` returns the `http.ResponseWriter`. It supports
  `http.Flusher`, `http.Hijacker` and `http.ResponseController` when the
  connection does, so streaming and WebSocket upgrades work.

### Per-request values

`c.Set(key, value)` and `c.Get(key)` share values between middleware and
handlers within one request. Prefer `SetRequest` with a context value when
services that only receive a `context.Context` need the value.

## Errors

Handlers and middleware return errors; one error handler renders them.

```go
return web.NotFound("user not found")      // 404 {"error":"user not found"}
return web.BadRequest("")                  // 400 {"error":"Bad Request"}
return &web.HTTPError{Code: 502, Message: "upstream unavailable", Err: err}
return fmt.Errorf("load user: %w", err)    // logged, 500 {"error":"Internal Server Error"}
```

| Helper | Status |
| --- | --- |
| `web.BadRequest(msg)` | 400 |
| `web.Unauthorized(msg)` | 401 |
| `web.Forbidden(msg)` | 403 |
| `web.NotFound(msg)` | 404 |
| `web.Conflict(msg)` | 409 |
| `web.TooManyRequests(msg)` | 429 |
| `web.NewHTTPError(code, msg)` | any |

The default error handler:

- renders `*web.HTTPError` (also when wrapped) as its status with
  `{"error": Message}`; an empty message becomes the standard status text.
  The optional `Err` cause is kept for `errors.Is`/`errors.As` and logging
  but never sent to the client;
- maps `ErrBodyTooLarge`, `ErrUnsupportedMediaType` and `ErrInvalidJSON`
  (bare or wrapped) to 413, 415 and 400 with the descriptive messages
  listed under [Reading the body](#reading-the-body);
- logs any other error (`method`, `path`, `error`) to `Server.Logger` and
  answers a generic 500, so internal details never leak;
- only logs the error when the response was already written, instead of
  corrupting it.

Replace it with `s.OnError`; `nil` restores the default. The custom
handler also renders the framework's 404 and 405 answers, which arrive as
`*web.HTTPError` values. `web.AsHTTPError(err)` returns the `*HTTPError`
the default handler would render (an `*HTTPError` in the chain, or the
mapped body sentinel with its status and message) and `false` for anything
that would be a 500, so a custom handler keeps the default mapping without
copying it:

```go
s.OnError(func(c *web.Context, err error) {
	if httpErr, ok := web.AsHTTPError(err); ok {
		// httpErr.Message may be empty: fall back to http.StatusText(httpErr.Code).
		_ = c.JSON(httpErr.Code, problem{Title: httpErr.Message})
		return
	}
	slog.ErrorContext(c, "request failed", "error", err)
	_ = c.JSON(http.StatusInternalServerError, problem{Title: "internal error"})
})
```

## net/http interop

### Mounting standard handlers

`web.WrapHandler(h http.Handler)` turns any `http.Handler` into a route
handler:

```go
static := http.StripPrefix("/static/", http.FileServer(http.Dir("./public")))
s.Get("/static/*path", web.WrapHandler(static))
s.Get("/debug/pprof/*path", web.WrapHandler(http.DefaultServeMux)) // with net/http/pprof imported
```

The handler receives the current request (including context values set by
earlier middleware) and the tracked response writer.

### Using standard middleware

`web.WrapMiddleware(mw func(http.Handler) http.Handler)` adapts the common
`net/http` middleware shape:

```go
s.Use(web.WrapMiddleware(securityHeaders)) // func securityHeaders(next http.Handler) http.Handler
```

- The standard middleware is constructed once per composed chain, not per
  request.
- The request it passes to `next` becomes `c.Request()`, so values it
  attached with `r.WithContext` are visible through `c.Value` and to every
  downstream handler.
- The response writer it passes to `next` receives everything written
  downstream (compression, buffering and status-capturing wrappers work).
- If it does not call `next`, the chain stops and whatever it wrote is the
  response.
- Errors returned downstream are rendered by the error handler **before**
  control returns to the standard middleware, so it observes the final
  status and body (a standard logging middleware logs the real 404 or
  500). The wrapped middleware therefore returns nil to middleware outside
  it.

### Mounting the server elsewhere

`*web.Server` implements `http.Handler`, so it also plugs into any
`net/http` setup or test:

```go
rec := httptest.NewRecorder()
s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/users/42", nil))
```

## Running

| Method | Behavior |
| --- | --- |
| `Run(addr)` | Listens on `addr` (for example `":8080"`) and serves until the process receives SIGINT or SIGTERM, then shuts down gracefully: stops accepting connections, waits up to `ShutdownTimeout` for in-flight requests, and closes whatever is still running when that budget is spent (returning `context.DeadlineExceeded`). Returns nil after a clean shutdown and the listen error if the server cannot start. |
| `ListenAndServe(addr)` | Serves until `Shutdown`, then returns nil. No signal handling. |
| `Serve(ln)` | Serves on an existing `net.Listener` until `Shutdown` (which closes the listener), then returns nil. |
| `Shutdown(ctx)` | Graceful stop: stops accepting, closes idle connections and waits for in-flight requests or `ctx`. Also makes a running `Run` return. Errors when the server is not running. |
| `ServeHTTP(w, r)` | The `http.Handler` implementation. |

Starting a server that is already running returns an error ("server is
already running"); a stopped server can be started again.

### Configuration

`web.New()` sets every field to its default. Change fields after `New` and
before serving starts:

```go
s := web.New()
s.MaxBodyBytes = 10 << 20 // 10 MiB uploads
s.WriteTimeout = 2 * time.Minute
s.Logger = logger
```

| Field | Default | Meaning |
| --- | --- | --- |
| `MaxBodyBytes` | `web.DefaultMaxBodyBytes` (1 MiB) | Largest body `Body`, `BindJSON` and `BindJSONStrict` will read. `0` = unlimited. |
| `TrustProxyHeaders` | `false` | Read the client IP from `ClientIPHeader`. |
| `ClientIPHeader` | `"X-Forwarded-For"` | Header used when `TrustProxyHeaders` is on (`""` means `X-Forwarded-For`). |
| `ReadHeaderTimeout` | 10s | Time to read request headers. |
| `ReadTimeout` | 30s | Time to read the whole request, body included. |
| `WriteTimeout` | 30s | Time from the end of the request headers to the end of the response. |
| `IdleTimeout` | 120s | How long a keep-alive connection may wait for its next request. |
| `ShutdownTimeout` | 10s | `Run`'s graceful shutdown budget. `0` waits indefinitely. |
| `Logger` | nil = `slog.Default()` | Receives framework logs: recovered panics, unhandled errors and `net/http` server errors (bridged at Error level). A nil value is resolved to `slog.Default()` at the moment of logging (for the `net/http` bridge: when serving starts). |

The four timeouts are passed to the internal `http.Server` exactly as they
are when serving starts; `0` means no timeout, as in `net/http`. The
defaults protect against slow clients holding connections open
(Slowloris). Raise `WriteTimeout` for long downloads or streaming
responses, or set it to `0` and enforce limits per route.

Registration (routes, `Use`, `OnError`, groups) is safe for concurrent use,
even while serving, but changing the exported fields while serving is a
data race.

### Behind a reverse proxy

By default `c.RemoteIP()` is the TCP peer address. Behind a load balancer
or reverse proxy that is the proxy's address, so enable proxy headers:

```go
s.TrustProxyHeaders = true                 // X-Forwarded-For (default header)
s.ClientIPHeader = "CF-Connecting-IP"      // or a single-value header your proxy sets
```

- With `X-Forwarded-For`, the **rightmost** non-empty entry is used: the
  address your nearest proxy appended. Entries further left come from the
  client and can be forged, so they are never trusted. Multiple
  `X-Forwarded-For` header lines are treated as one list.
- With any other header name (`X-Real-IP`, `CF-Connecting-IP`,
  `True-Client-IP`, ...), the whole trimmed value is used. `X-Real-IP` is
  never consulted unless you name it.
- If the header is absent, empty or not an IP address, the peer address is
  used instead.
- Addresses are returned in canonical form: a port is dropped
  (`1.2.3.4:5678` becomes `1.2.3.4`), IPv6 is lower-cased and IPv4-mapped
  IPv6 addresses are unmapped.

Only enable `TrustProxyHeaders` when every request really passes through a
proxy you control that sets the header; otherwise any client can choose
its own IP, which defeats IP-based rate limiting.

While `TrustProxyHeaders` is `false`, the first request carrying
`X-Forwarded-For` (or the configured `ClientIPHeader`) makes the server log
**one** Warn through `Server.Logger` (once per `Server`, the header value is
not logged): client IPs and rate limits then see the proxy's address. If
you run behind a trusted proxy, enable `TrustProxyHeaders`; if the server
is exposed directly, the client sent the header itself and the warning can
be ignored.
