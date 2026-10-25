# Quickstart

The smallest complete CoreGo application: sign-up, sign-in and server-side
sessions under `/auth`, plus a few routes of its own behind
`core.RequireAuth()`. It needs no infrastructure: without a database the
auth service uses the in-memory store (data is lost on restart).

```go
core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
if err != nil {
	log.Fatal(err)
}
app := core.Server()
api := app.Group("/api", core.RequireAuth())
api.Get("/hello", func(c *web.Context) error {
	user := auth.MustUser(c)
	return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
})
if err := core.Run(); err != nil {
	log.Fatal(err)
}
```

(`main.go` adds a public `/health` route and two more protected routes.)

## What it demonstrates

- `corego.New` loading `.env` and the environment, starting the auth service
  and choosing the store automatically.
- `core.Server()`: the web server with request logging and the 17 auth
  routes already mounted under `/auth`.
- A public route (`GET /health`) and a protected group (`/api`) using
  `core.RequireAuth()`, `auth.MustUser(c)`, `c.BindJSON`, `web.BadRequest`
  and handlers that end with `return c.JSON(...)`.
- Passing `c` (a `context.Context`) straight to the auth service.
- `core.Run()`: serves on `PORT`, shuts down gracefully on Ctrl+C and
  closes everything.

## Run it

From the repository root:

```bash
go run ./examples/quickstart
```

It listens on `:8080`; set `PORT` to change that. At startup it logs two
warnings that are expected for this example: no mailer is configured (so no
verification or reset emails are sent) and no database is connected (so
the in-memory store is used). A `corego: auth ready` line then summarizes
the resolved setup (`store=memory base_path=/auth mail=none ...`).

To try email verification without an SMTP server, configure it from the
environment alone; the verification link appears in the log:

```bash
COREGO_MAIL=log COREGO_REQUIRE_VERIFIED_EMAIL=true go run ./examples/quickstart
# INFO mail: outgoing message to=ada@example.com subject="Verify your email address" links="http://localhost:8080/auth/verify-email?email=...&token=..."
```

Sign-up then answers `"verification_required":true` without a token,
sign-in is refused with `403` until the link has been opened, and works
afterwards. Stop it with Ctrl+C: the server finishes
in-flight requests, `core.Run()` closes everything and returns nil, and
the program exits with status 0.

## Curl walkthrough

The responses below were recorded against the built binary; IDs, tokens and
timestamps will differ on your machine.

Public route:

```bash
curl -s localhost:8080/health
# {"status":"ok"}
```

Sign up. The raw session token is returned exactly once, in `token`:

```bash
curl -s -X POST localhost:8080/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada Lovelace","email":"ada@example.com","password":"analytical-engine"}'
# {"user":{"id":"e2a4e8fc6488cc5af06469c509d01d4e","name":"Ada Lovelace","email":"ada@example.com",
#  "email_verified":false,"image":"","created_at":"...","updated_at":"..."},
#  "token":"LITTqmcqfdM1o3H2yOc10CKOSBgWGefWNcxThOgUB80","expires_at":"...","verification_required":false}

TOKEN=LITTqmcqfdM1o3H2yOc10CKOSBgWGefWNcxThOgUB80   # use your own token
```

Protected routes reject requests without a session:

```bash
curl -s -i localhost:8080/api/hello
# HTTP/1.1 401 Unauthorized
# Content-Type: application/json; charset=utf-8
# Www-Authenticate: Bearer
#
# {"error":"Missing session token."}
```

With the bearer token:

```bash
curl -s localhost:8080/api/hello -H "Authorization: Bearer $TOKEN"
# {"hello":"Ada Lovelace"}
```

Sign in from a second device, then count the sessions:

```bash
curl -s -X POST localhost:8080/auth/signin \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"analytical-engine"}'
# {"user":{...},"token":"-kCLZFVRof0tVdF0urawEVFHtx7aBUINzVLZMd4ytow","expires_at":"..."}

curl -s localhost:8080/api/sessions -H "Authorization: Bearer $TOKEN"
# {"active_sessions":2}
```

Send JSON to a protected route; validation errors come back as JSON too:

```bash
curl -s -X POST localhost:8080/api/notes -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"text":"The engine weaves algebraic patterns."}'
# {"author":"Ada Lovelace","text":"The engine weaves algebraic patterns."}

curl -s -i -X POST localhost:8080/api/notes -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{}'
# HTTP/1.1 400 Bad Request
#
# {"error":"text is required"}
```

The auth routes work with the same token:

```bash
curl -s localhost:8080/auth/me -H "Authorization: Bearer $TOKEN"
# {"user":{"id":"e2a4e8fc6488cc5af06469c509d01d4e","name":"Ada Lovelace",...}}

curl -s -i -X POST localhost:8080/auth/signout -H "Authorization: Bearer $TOKEN"
# HTTP/1.1 204 No Content

curl -s localhost:8080/api/hello -H "Authorization: Bearer $TOKEN"
# {"error":"Invalid session token."}
```

After the startup banner (the services in use and every route), each
request is logged by the server:

```
INFO http request method=GET path=/health status=200 duration=40.882µs remote_ip=::1
INFO http request method=POST path=/auth/signup status=201 duration=47.059554ms remote_ip=::1
INFO http request method=GET path=/api/hello status=401 duration=61.336µs remote_ip=::1
...
INFO web: shutting down signal=interrupt timeout=10s
```

The full route table (sessions, email verification, password reset, email
change, account deletion) is in [docs/http-api.md](../../docs/http-api.md).

## Next steps

- **Persist users and sessions**: set `MONGODB_CONNECTION_URL` (and
  optionally `MONGODB_DATABASE`) or `POSTGRES_CONNECTION_URL`, in the
  environment or in a `.env` file. `corego.New` connects, picks that
  database as the auth store and creates its indexes or tables. No code
  changes are needed. `COREGO_AUTH_STORE=memory|mongo|postgres` chooses
  explicitly.
- **Send emails**: set `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`,
  `SMTP_PASSWORD`, `SMTP_FROM` and `APP_URL`; verification and password
  reset emails then go out automatically. In development, set
  `AuthOptions{MailSender: mail.NewLogSender(logger)}` with a Debug-level
  logger to read the emails, links included, in the terminal.
- **Browser clients**: enable the session cookie and CORS:

  ```go
  core, err := corego.New(&corego.Config{
      Auth: &corego.AuthOptions{
          HTTP: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}},
      },
  })
  ```

  and set `CORS_ALLOWED_ORIGINS=https://app.example.com`. Listed origins
  may send the cookie and are trusted by the CSRF check of
  cookie-authenticated requests; `*` would allow every origin, but without
  cookies ([CSRF protection](../../docs/http-api.md#csrf-protection)).

See [docs/getting-started.md](../../docs/getting-started.md) for the full
tour and [docs/environment.md](../../docs/environment.md) for every
variable.
