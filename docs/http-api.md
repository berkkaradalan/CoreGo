# HTTP API

The `auth/authhttp` package turns the [auth service](authentication.md) into
JSON routes on the CoreGo [web framework](web-framework.md), and gives you
the middleware that protects your own routes.

## Routes

Paths are relative to the base path, `/auth` by default (`/signup` means
`POST /auth/signup`). **auth** routes need a session token; **RL** routes
are rate limited per client IP (10 requests per minute per route by
default, IPv6 clients grouped by /64), and `POST /signin`,
`/forgot-password` and `/verify-email/request` additionally per email
address (see [Rate limits](#rate-limits)).

| Method | Path | | Request body | Success |
| --- | --- | --- | --- | --- |
| POST | `/signup` | RL | `{name, email, password, image?, remember_me?}` | 201 `{user, token?, expires_at?, verification_required}` |
| POST | `/signin` | RL | `{email, password, remember_me?}` | 200 `{user, token?, expires_at}` |
| POST | `/signout` | auth | none | 204, clears the cookie |
| GET | `/session` | auth | none | 200 `{user, session}` |
| GET | `/sessions` | auth | none | 200 `{sessions: [...]}` |
| DELETE | `/sessions` | auth | none | 204, revokes every session, clears the cookie |
| DELETE | `/sessions/:id` | auth | none | 204 (404 for unknown or foreign IDs) |
| POST | `/verify-email/request` | RL | `{email}` | 202 `{message}`, always |
| POST | `/verify-email` | RL | `{email, token}` | 204 |
| GET | `/verify-email` | RL | query `?email=&token=` | 303 redirect or an HTML page (the link in verification emails) |
| POST | `/forgot-password` | RL | `{email}` | 202 `{message}`, always |
| POST | `/reset-password` | RL | `{email, token, new_password}` | 204 |
| POST | `/change-password` | auth, RL | `{current_password, new_password}` | 204, revokes every session, clears the cookie |
| POST | `/change-email` | auth, RL | `{new_email, current_password}` | 200 `{user}` |
| GET | `/me` | auth | none | 200 `{user}` |
| PATCH | `/me` | auth | `{name?, image?}` | 200 `{user}` |
| DELETE | `/me` | auth, RL | `{password}` | 204, clears the cookie |

Request bodies are JSON (`Content-Type: application/json`). Every response
of these routes carries `Cache-Control: no-store`, because they contain
session tokens, personal data or one-time links.

## Mount in 3 lines

**With the facade** there is nothing to do: `core.Server()` mounts every
route above for you.

```go
core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
if err != nil {
	log.Fatal(err)
}
app := core.Server() // the /auth routes are already there
```

Configure the HTTP layer through `AuthOptions.HTTP`, which is an
`authhttp.Config` (see [Configuration](#configuration)):

```go
core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{
	HTTP: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}},
}})
```

**Standalone**, with your own `*auth.Auth` service and `*web.Server`:

```go
h := authhttp.New(service, authhttp.Config{})
server := web.New()
h.Mount(server) // POST /auth/signup, POST /auth/signin, ...
```

`Mount` also accepts a `*web.Group`, so the routes can live below a prefix:

```go
h.Mount(server.Group("/api/v1")) // POST /api/v1/auth/signup, ...
```

Group middleware runs outside the auth middleware. Mounting twice on the
same router, or mounting where a route already exists, panics with the web
framework's `web: route ... is already registered` message, like any other
duplicate registration.

## A first session with curl

Sign up. The response contains the session token, exactly once:

```bash
curl -i -X POST localhost:8080/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada","email":"ada@example.com","password":"analytical-engine"}'
```

```json
{
  "user": {
    "id": "6f1c...",
    "name": "Ada",
    "email": "ada@example.com",
    "email_verified": false,
    "image": "",
    "created_at": "2026-10-24T09:00:00Z",
    "updated_at": "2026-10-24T09:00:00Z"
  },
  "token": "q3J9...",
  "expires_at": "2026-10-31T09:00:00Z",
  "verification_required": false
}
```

Sign in later with the same address and password (`remember_me: true`
extends the session from 7 to 30 days by default):

```bash
curl -X POST localhost:8080/auth/signin \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"analytical-engine","remember_me":true}'
```

Call a protected route with the token:

```bash
TOKEN=q3J9...
curl localhost:8080/auth/me -H "Authorization: Bearer $TOKEN"
# {"user":{"id":"6f1c...","name":"Ada","email":"ada@example.com",...}}
```

Sign out. The token stops working immediately:

```bash
curl -i -X POST localhost:8080/auth/signout -H "Authorization: Bearer $TOKEN"
# HTTP/1.1 204 No Content
```

When the service requires verified email addresses
(`auth.Config{RequireVerifiedEmail: true}`), sign-up creates the account but
no session: the response has no `token` and no `expires_at`, no cookie is
set, and `verification_required` is `true`. Tell the user to check their
inbox; sign-in works once the address is verified (until then it answers
`403 Email address is not verified.`).

With `CookieConfig.OmitTokenFromBody` (see
[Bearer tokens or cookies](#bearer-tokens-or-cookies)) the sign-up and
sign-in responses never contain `token`; the session lives in the HttpOnly
cookie only.

### Response shapes

- **user**: `{id, name, email, email_verified, image, created_at,
  updated_at}`. Never a password hash, never ban internals. (This is the
  routes' own shape; `auth.User` marshals to a wider one that includes the
  ban fields, see [authentication.md](authentication.md#json).)
- **session**: `{id, user_id, expires_at, ip_address, user_agent,
  created_at, updated_at, current}`. Never token material. `current` is
  `true` for the session the request is authenticated with, so a "your
  devices" page can mark "this device".
- **message** (202 responses): `{"message": "If an account exists for this
  email, we have sent it an email with further instructions."}`.
- **error**: `{"error": "<message>"}`, see [Errors](#errors).

Sessions record the client IP and the `User-Agent` (cut to 512 bytes).

`PATCH /me` only changes the fields present in the body: `{"image": ""}`
clears the image and leaves the name alone. Names are trimmed and limited
to 256 characters; an image must be empty or an absolute `http`/`https`
URL of at most 2048 bytes (the same rules apply at sign-up). The email
address is a credential, so a body containing `email` is rejected with
`400` and a pointer to `POST /change-email`.

## Bearer tokens or cookies

The bearer header always works:

```http
Authorization: Bearer <token>
```

Use it for mobile apps, CLIs, server-to-server calls and SPAs that keep the
token in memory.

For browser apps, enable the cookie transport so the browser stores and
sends the token for you:

```go
authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true, Secure: true}}
```

- Sign-up and sign-in set the cookie (`corego_session` by default),
  expiring together with the session. It is **always `HttpOnly`**: scripts
  cannot read the cookie.
- The sign-up and sign-in responses **still return the token in the JSON
  body** as well, so the same API keeps serving bearer clients (mobile
  apps, CLIs). Your frontend code (and an injected script) can therefore
  read the token from that response. For a browser-only app, set
  `OmitTokenFromBody: true` so the token never reaches JavaScript at all:

  ```go
  authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true, Secure: true, OmitTokenFromBody: true}}
  ```

  The responses then carry `user`, `expires_at` and (sign-up)
  `verification_required`, but no `token`. `OmitTokenFromBody` without
  `Enabled` would leave the client with no token at all, so `New` panics
  and `Config.Validate` returns an error.
- Requests without an `Authorization` header authenticate with the cookie.
  When a request carries both a `Bearer` header and the cookie, the
  **bearer header wins**, even if its token is invalid (there is no silent
  fallback to the cookie). An `Authorization` header with another scheme
  (for example `Basic`) is ignored.
- Sign-out, `DELETE /sessions`, revoking the current session through
  `DELETE /sessions/:id`, `POST /change-password` and `DELETE /me` clear the
  cookie (an empty value with `Max-Age=0`, same name, path and domain).
- Cookie-authenticated requests that change state are checked for
  cross-site request forgery; see [CSRF protection](#csrf-protection).

| `CookieConfig` field | Default | Meaning |
| --- | --- | --- |
| `Enabled` | `false` | Turn the cookie transport on. |
| `Name` | `corego_session` | Cookie name. |
| `Path` | `/` | Cookie path. |
| `Domain` | empty (host-only) | Set it (for example `example.com`) to share the cookie with subdomains. |
| `Secure` | `false` | Send the cookie over HTTPS only. **Set it to `true` in production.** The facade turns it on automatically when `APP_URL` starts with `https://`. |
| `SameSite` | `http.SameSiteLaxMode` | `Lax` blocks the cookie on cross-site sub-requests (forms, `fetch`) while keeping top-level navigation signed in. |
| `OmitTokenFromBody` | `false` | Leave `token` out of the sign-up and sign-in responses, so the token only lives in the HttpOnly cookie. Requires `Enabled`. |

### SPAs on another origin

When the frontend runs on a different origin than the API (for example
`https://app.example.com` calling `https://api.example.com`, or Vite on
`http://localhost:5173` calling `http://localhost:8080`):

1. **Allow the origin with credentials.** With the facade, set
   `CORS_ALLOWED_ORIGINS=https://app.example.com` (credentials are allowed
   for these origins, and they also become the trusted origins of the
   [CSRF check](#csrf-protection)). Standalone, install
   `web.CORS(web.CORSConfig{AllowedOrigins: []string{"https://app.example.com"}, AllowCredentials: true})`
   with `server.Use` (see [CORS](web-framework.md#cors)) and list the same
   origin in `authhttp.Config.TrustedOrigins` (see below). The wildcard `"*"` cannot
   carry cookies: `web.CORS` panics on `"*"` with `AllowCredentials`, and
   the facade treats `CORS_ALLOWED_ORIGINS=*` as "any origin, without
   credentials", so only the bearer header works cross-origin then.
2. **Send credentials from the browser**:
   `fetch(url, { credentials: "include" })` or
   `axios.defaults.withCredentials = true`.
3. **Pick SameSite by site, not origin.** `app.example.com` and
   `api.example.com` are the *same site*, so the default `Lax` works. Two
   different registrable domains (`app.example.net` calling
   `api.example.com`) are cross-site: use `SameSite: http.SameSiteNoneMode`
   together with `Secure: true` (browsers reject `None` without `Secure`, so
   `New` panics on that combination). `localhost` on two ports is the same
   site.

### CSRF protection

Browsers attach cookies to requests that other sites trigger (a hidden
form, a `fetch` with `credentials: "include"`), so a cookie alone does not
prove that your frontend sent a request. `RequireAuth`, `OptionalAuth`,
`Middleware` and therefore every protected auth route apply a cross-site
request forgery check to requests that are **authenticated with the
session cookie** and use a method other than `GET`, `HEAD` and `OPTIONS`:

| Request | Result |
| --- | --- |
| `Sec-Fetch-Site: same-origin`, `same-site` or `none` | allowed |
| `Sec-Fetch-Site: cross-site` | allowed only when `Origin` is listed in `Config.TrustedOrigins` |
| no `Sec-Fetch-Site`, no `Origin` (non-browser clients, old browsers) | allowed |
| no `Sec-Fetch-Site`, with `Origin` | allowed when the `Origin` host equals the request's `Host`, or `Origin` is trusted |
| anything else | `403 {"error":"Cross-site request blocked."}` |

Requests that carry an `Authorization: Bearer` header are never checked:
other sites cannot make a browser attach that header. `GET`, `HEAD` and
`OPTIONS` are not checked either, so keep your own `GET` handlers free of
side effects.

`TrustedOrigins` lists exact origins (`"https://app.example.com"`,
`"http://localhost:5173"`: scheme, host and optional port, compared
case-insensitively, a trailing slash ignored). Anything else, including
`"*"`, makes `New` panic (`Config.Validate` reports it as an error). A
frontend on **another site** that sends cookies with `SameSite=None` needs
it. A frontend on the same site (a subdomain of the API's domain, or
`localhost` on another port) is marked `same-site` by every current
browser and passes anyway; listing it also covers older browsers that send
`Origin` but not `Sec-Fetch-Site`. The facade fills an empty
`TrustedOrigins` from the explicit CORS origins (`ServerOptions.CORS` or
`CORS_ALLOWED_ORIGINS`).

```go
authhttp.Config{
	Cookie:         authhttp.CookieConfig{Enabled: true, Secure: true, SameSite: http.SameSiteNoneMode},
	TrustedOrigins: []string{"https://app.example.net"},
}
```

Your own handlers that read the cookie some other way (for example
through `h.Token`) do not get this check and must protect themselves.

**Debugging a 403.** Every blocked request is logged at **Debug** level
through `slog.Default()` as `authhttp: cross-site request blocked`, with
`method`, `path`, `origin`, `sec_fetch_site` and a `reason` (an untrusted
cross-site `Origin`, an `Origin` that matches neither the `Host` nor a
trusted origin, or an unknown `Sec-Fetch-Site` value). The session token
and cookies are never logged. Enable Debug for the default logger while
you investigate, for example
`slog.SetLogLoggerLevel(slog.LevelDebug)`.

## The email link flow

### Verifying an address

1. On sign-up (and on `POST /change-email`, and on request), the user gets
   an email with a link such as
   `https://api.example.com/auth/verify-email?token=...&email=ada%40example.com`.
   The facade points the link at this route (`APP_URL` + base path +
   `/verify-email`).
2. The user clicks it. `GET /verify-email` verifies the address, then:
   - with `Config.VerifyEmailRedirectURL` set (facade:
     `COREGO_VERIFY_EMAIL_REDIRECT_URL`), answers `303 See Other` to that URL
     with `status=verified`, `status=invalid` or `status=expired` added.
     Existing query parameters and the fragment are kept:
     `https://app.example.com/verified?lang=en` becomes
     `https://app.example.com/verified?lang=en&status=verified`. Show the
     right message on that page.
   - without it, renders a small built-in page: `200` "Email address
     verified", or `400` "Invalid verification link" / "Verification link
     expired". The page never echoes anything from the URL.
3. A missing `email` or `token` counts as `invalid`. A link works once: a
   second click reports `invalid`. Many mail providers and corporate
   security scanners open every link in an incoming email before the user
   sees it, so the user's own first click can be the second request. For
   that reason the built-in page does not just say "invalid": it says the
   link is invalid **or has already been used**, and that a user whose
   address is already verified can simply sign in. With a redirect URL,
   treat `status=invalid` the same way on your page (for example, check
   `email_verified` after sign-in instead of showing an error).

The token is consumed before the address is marked verified, so two
concurrent clicks can never both succeed. A link only verifies the
address it was sent to: when the user changed their address in the
meantime, it reports `invalid`.

The responses carry `Content-Security-Policy: default-src 'none';
style-src 'unsafe-inline'`, `X-Content-Type-Options: nosniff`,
`Referrer-Policy: no-referrer` (the URL contains a token) and
`Cache-Control: no-store`. `HEAD` requests (sent by some link previewers),
whatever the letter case of the method, get the headers and an empty
`200` without using the token.

SPAs that prefer to handle the link themselves can point the email at a
frontend page and call `POST /verify-email` with `{email, token}` (204 on
success, `400 Invalid token.` / `400 Token has expired.` otherwise).

To send a new link, call `POST /verify-email/request` with `{email}`.

### Resetting a password

1. Your "forgot password" form calls `POST /forgot-password` with
   `{email}`. The answer is always `202` with the same message.
2. The user gets an email linking to **your application's** reset page
   (facade: `COREGO_RESET_PASSWORD_URL`, default `APP_URL` +
   `/reset-password`) with `token` and `email` query parameters.
3. That page asks for a new password and calls `POST /reset-password`:

```js
const params = new URLSearchParams(location.search);
const res = await fetch("https://api.example.com/auth/reset-password", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    email: params.get("email"),
    token: params.get("token"),
    new_password: newPassword,
  }),
});
// 204: done, send the user to sign-in.
// 400: {"error": "Invalid token."}, "Token has expired." or a password policy message.
```

A successful reset signs the user out everywhere (every session is
revoked). A rejected password (too short or too long) does not use up the
token, so the user can try again with the same link. Any other attempt
consumes the token before the password is changed, so two concurrent
submissions can never both succeed (the second gets `400 Invalid token.`),
and a failure after that point (`409`, `500`) means the user has to request
a new link.

### Enumeration safety

`POST /verify-email/request` and `POST /forgot-password` answer **`202`
with the same body** for registered, unknown, already verified and
malformed addresses, and whether or not email delivery is configured.
The request itself only looks the address up; creating the token and
sending the email happen in the background, so the response time does not
depend on either. Only a malformed request body (`400`/`413`/`415`), a
rate limit (`429`, applied per address to registered and unknown addresses
alike) or a database failure (`500`) produces another status.

Background emails are bounded by `auth.Config.MaxPendingEmails` (100 by
default): when that many are in flight, further ones are dropped and
logged at Warn level instead of queuing up behind a slow mail server. The
response is the same either way; the user simply asks again.

## Rate limits

Every RL route in the [table](#routes) has its **own** limiter, keyed by the
client IP: by default each IP may send 10 requests per minute to each of
them. `POST /signin`, `/forgot-password` and `/verify-email/request` also
have a limiter **per email address** (trimmed and lowercased), with the
same limit and window, so an attacker spreading requests over many IPs
can neither guess one account's password nor flood one inbox. The per
address limit treats registered and unknown addresses identically and
reveals nothing about which addresses exist. Exceeding either limit
answers:

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 66
RateLimit-Limit: 10
RateLimit-Remaining: 0

{"error":"Too many requests. Please try again later."}
```

`Retry-After` is the number of seconds until the next request is accepted
(the limiter uses a sliding window, so after a full burst the wait is a bit
over one window). Every response of an RL route carries `RateLimit-Limit`
and `RateLimit-Remaining` (on the routes limited per address too, the
stricter of the two remaining budgets).

- Limiters are independent: hammering `/signin` does not block `/signup`,
  and `GET` and `POST /verify-email` are limited separately.
- On routes that are both auth and RL, the limit is checked **before** the
  session, so unauthenticated attempts count too.
- The per client key is `web.ClientKey`: `web.Context.RemoteIP()`, with
  IPv6 addresses reduced to their /64 prefix (one IPv6 subscriber usually
  controls a whole /64 and could otherwise rotate addresses). Behind a
  reverse proxy, enable `web.Server.TrustProxyHeaders` (facade:
  `TRUST_PROXY_HEADERS=true`) so the real client address is used;
  otherwise every request appears to come from the proxy and all clients
  share one bucket. The server logs a one-time warning when requests carry
  `X-Forwarded-For` while the setting is off. Only enable it behind a
  proxy you control, or clients can pick their own bucket.
- Each limiter tracks at most `web.DefaultRateLimitMaxKeys` (100000) keys;
  beyond that, new keys share one overflow bucket, so memory stays bounded
  even when an attacker rotates addresses (see
  [RateLimit](web-framework.md#ratelimit)).
- Limits are per process. Each `Mount` call creates fresh limiters.

Tune or disable them:

```go
authhttp.Config{RateLimit: authhttp.RateLimitConfig{Limit: 5, Window: 10 * time.Minute}}
authhttp.Config{RateLimit: authhttp.RateLimitConfig{Disabled: true}} // e.g. an API gateway limits already
```

## Errors

Every error is a JSON body with one field:

```json
{"error": "Invalid email or password."}
```

| Status | Message | When |
| --- | --- | --- |
| 400 | `Request body is not valid JSON.` | Empty or malformed body, data after the JSON value, or a field of the wrong type. |
| 400 | `Name is required.` | Sign-up without a name, or `PATCH /me` with a blank one. |
| 400 | `Name must be at most 256 characters.` | Sign-up or `PATCH /me` with a longer name (counted in characters after trimming). |
| 400 | `Image must be empty or an absolute http or https URL of at most 2048 bytes.` | Sign-up or `PATCH /me` with another image value. |
| 400 | `Email is required.` / `Invalid email address.` | Sign-up or `POST /change-email` with a missing or malformed address. |
| 400 | `Password is required.` / `Password does not meet the requirements.` | Missing password, or one shorter than the minimum (8 bytes by default) or longer than 72 bytes. |
| 400 | `Current password is incorrect.` | Wrong password on `POST /change-password`, `POST /change-email` or `DELETE /me`. |
| 400 | `Invalid token.` / `Token has expired.` | Verification or reset token that is unknown, used, for another flow or expired. |
| 400 | `The email address cannot be changed here; use POST /change-email.` | `PATCH /me` with an `email` field. |
| 401 | `Missing session token.` | Protected route without a bearer header or session cookie. |
| 401 | `Invalid session token.` | Unknown, revoked or signed-out session. |
| 401 | `Session has expired. Please sign in again.` | Expired session. |
| 401 | `Invalid email or password.` | Sign-in failure. Never says which of the two was wrong. |
| 403 | `This account has been banned.` | Banned user signing in or using a session. |
| 403 | `Email address is not verified.` | With `RequireVerifiedEmail`: sign-in or a session of an unverified user (also after `POST /change-email`, until the new address is verified). |
| 403 | `Cross-site request blocked.` | A cookie-authenticated `POST`, `PATCH`, `DELETE`, ... that fails the [CSRF check](#csrf-protection). |
| 404 | `Session not found.` | `DELETE /sessions/:id` with an unknown ID or another user's session. |
| 404 | `User not found.` | The user disappeared while the request ran. |
| 409 | `A user with this email already exists.` | Sign-up or email change to a registered address. |
| 409 | `This account has no password.` | Password-confirmed routes for a user without a password (for example one who signed up with a social provider). |
| 409 | `Email address is already verified.` | Reserved for the authenticated resend flow of the service. |
| 409 | `The account was modified concurrently. Please retry.` | Concurrent changes to the same user or account kept winning after the service's retries. Repeating the request is safe. |
| 413 | `Request body exceeds the configured size limit.` | Body larger than `web.Server.MaxBodyBytes` (1 MiB by default). |
| 415 | `Content-Type must be application/json.` | Body sent without `Content-Type: application/json`. |
| 429 | `Too many requests. Please try again later.` | Rate limit exceeded; see `Retry-After`. |
| 500 | `Internal Server Error` | Anything unexpected, such as a database failure. The cause is logged by the web server's logger and never sent to the client. |
| 502 / 503 | `Email delivery failed.` / `Email delivery is not configured.` | Reserved for synchronous delivery errors of the service; the HTTP routes deliver email in the background and never return them. |

Two deliberate choices: a wrong *current* password is `400`, not `401`, so
SPA interceptors that sign the user out on `401` keep the session; and
`401` responses of protected routes carry `WWW-Authenticate: Bearer`.

The mapped errors are `*web.HTTPError` values that wrap the original auth
sentinel, so a custom `server.OnError` handler can still test
`errors.Is(err, auth.ErrSessionExpired)`. The 400 (body), 413 and 415
messages are the web framework's own (`web.AsHTTPError`), so your own
routes that use `c.BindJSON` answer body errors with exactly the same
messages.

## Protecting your own routes

`RequireAuth()` checks the session exactly like the auth routes do and puts
the user and session in the request context. Read them with
`auth.MustUser(c)`; `*web.Context` is a `context.Context`:

```go
api := server.Group("/api", h.RequireAuth()) // facade: core.RequireAuth()
api.Get("/dashboard", func(c *web.Context) error {
	user := auth.MustUser(c)
	return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
})
```

Or protect a single route:

```go
server.Get("/reports", reports, h.RequireAuth())
```

| Helper | Use |
| --- | --- |
| `auth.MustUser(c)` | Behind `RequireAuth`. Panics (a 500 via the web recovery) when the route is not protected, so the mistake shows up immediately. |
| `auth.UserFromContext(c)` | Returns `(user, ok)`. Behind `OptionalAuth`, and for code that also runs without authentication. |
| `auth.SessionFromContext(c)` | Returns `(session, ok)`, for example to know the current session ID. |

Requests that fail the check never reach your handler; they get the `401`
or `403` responses listed in [Errors](#errors). Everything the request
context carries is also visible to services you call with `c`, and to
`c.Request().Context()`.

### Optional sign-in

`OptionalAuth()` is for routes that serve signed-in and anonymous visitors
alike, such as a home page that greets returning users. With a valid
session it stores the user and session exactly like `RequireAuth`;
otherwise the request continues anonymously, never with a `401`:

```go
server.Get("/", func(c *web.Context) error {
	if user, ok := auth.UserFromContext(c); ok {
		return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
	}
	return c.JSON(http.StatusOK, map[string]string{"hello": "guest"})
}, h.OptionalAuth()) // facade: core.OptionalAuth()
```

A missing, unknown or expired token, a banned (or, with
`RequireVerifiedEmail`, unverified) user and a cookie request that fails
the [CSRF check](#csrf-protection) all count as anonymous; the cookie is
left untouched. Store failures still answer `500`, so an outage never
silently signs visitors out.

`h.Token(r *http.Request) (string, bool)` returns the raw token of a
request (bearer header first, then the cookie when enabled) if you need it
yourself. It does not run the CSRF check.

## Plain net/http

`Middleware` is `RequireAuth` for `http.Handler`s: the standard library's
`http.ServeMux`, other routers, or handlers you do not want to port:

```go
h := authhttp.New(service, authhttp.Config{})

mux := http.NewServeMux()
mux.Handle("/dashboard", h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	fmt.Fprintf(w, "hello %s", user.Name)
})))
```

Failures produce the same status codes, `WWW-Authenticate` header and JSON
bodies as `RequireAuth` with the web framework's default error handler.
Unexpected errors are logged with `slog.Default()` and answered `500`.

## Configuration

```go
h := authhttp.New(service, authhttp.Config{
	BasePath:               "/auth",                               // default; "/" mounts at the root
	Cookie:                 authhttp.CookieConfig{Enabled: true},  // see "Bearer tokens or cookies"
	RateLimit:              authhttp.RateLimitConfig{Limit: 10, Window: time.Minute},
	TrustedOrigins:         []string{"https://app.example.com"},      // see "CSRF protection"
	VerifyEmailRedirectURL: "https://app.example.com/email-verified",
})
```

| `Config` field | Default | Meaning |
| --- | --- | --- |
| `BasePath` | `/auth` (`authhttp.DefaultBasePath`) | Prefix of every route. `"/"` mounts at the root; a trailing slash is ignored. Static segments only. |
| `Cookie` | disabled | Session cookie transport. |
| `RateLimit` | 10 per minute per route and IP (and per address on three routes) | `Disabled`, `Limit`, `Window`. |
| `TrustedOrigins` | none | Origins of other sites whose cookie-authenticated, state-changing requests pass the [CSRF check](#csrf-protection). |
| `VerifyEmailRedirectURL` | empty (built-in page) | Absolute `http(s)` URL or absolute path (`/email-verified`) that `GET /verify-email` redirects to. |

`New` panics on configuration that can never work, because it is a
programming error. When the values come from the environment or user
input and you prefer an error, call `cfg.Validate()` first (the facade
does, and returns the problem from `corego.New`). `New` panics on a nil
service and on every configuration error `Validate` reports: a `BasePath`
that does not start with `/`
or contains empty, `:param` or `*wildcard` segments, a query or whitespace,
a negative rate limit, a `VerifyEmailRedirectURL` that is not an absolute
`http(s)` URL or path (protocol-relative `//host` URLs are rejected too)
and, with cookies enabled, a cookie name, path or domain that net/http
rejects or `SameSite=None` without `Secure`, and a `TrustedOrigins` entry
that is not an absolute `http(s)` origin. To mount below a dynamic
prefix such as `/tenants/:tenant`, mount on a group instead:
`h.Mount(server.Group("/tenants/:tenant"))`.

The resolved prefix is available for building links to the routes:
`h.BasePath()` returns it for existing handlers (`"/auth"` by default,
`""` when mounted at the root; the prefix of a group you mount on is not
included), and `cfg.ResolvedBasePath()` computes the same value, with an
error for an invalid `BasePath`, before any handlers exist:

```go
link := appURL + h.BasePath() + "/verify-email" // "https://api.example.com/auth/verify-email"
```
