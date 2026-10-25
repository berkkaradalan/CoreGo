# CoreGo

CoreGo is a backend SDK for Go that gives you the parts every API ends up
needing (email and password authentication with server-side sessions,
email verification and password reset, MongoDB and PostgreSQL access,
transactional email, a small web framework and `.env` configuration)
behind one facade, `corego.New`. It is built on the standard library plus
the official MongoDB and PostgreSQL drivers.

## Features

- **Authentication**: sign-up, sign-in, opaque server-side sessions
  (hashed at rest, remember-me), email verification, password reset,
  password and email change, account deletion, bans, user listing.
  Enumeration-safe and timing-safe by design.
- **17 ready-made auth routes** under `/auth`, with bearer-token or
  HttpOnly-cookie transport (CSRF-checked), per-IP and per-email rate
  limiting and stable JSON errors.
- **One-line route protection**: `core.RequireAuth()` and
  `auth.MustUser(c)`, or `core.OptionalAuth()` for routes that serve
  guests too.
- **Safe under concurrency**: optimistic locking on users and accounts, so
  concurrent requests never lose each other's changes, and single-use
  tokens that cannot be redeemed twice.
- **Web framework**: a standard-library-only router with path parameters,
  groups, middleware, JSON binding, CORS, rate limiting, request logging,
  panic recovery, server timeouts and graceful shutdown.
- **Auth stores** for MongoDB, PostgreSQL (versioned migrations) and
  memory, chosen automatically from the environment.
- **Databases**: context-aware MongoDB CRUD helpers, typed PostgreSQL
  queries (`QueryAll[T]`, `QueryOne[T]`), transactions, raw driver access.
- **Mail**: SMTP (STARTTLS or implicit TLS), a log sender for development
  and a capture sender for tests.
- **Configuration**: `.env` plus process environment, typed getters,
  everything overridable in code.

## Install

```bash
go get github.com/berkkaradalan/CoreGo@v0.2.0
```

Go 1.24 or newer. (Until the `v0.2.0` tag is published, use `@main`.)

## Hello world

A complete API with sign-up, sign-in and a protected route:

```go
package main

import (
	"log"
	"net/http"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

func main() {
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
}
```

It runs without any infrastructure: with no database configured, users
and sessions are kept in memory (a startup warning says so). Set
`POSTGRES_CONNECTION_URL` or `MONGODB_CONNECTION_URL` and the same code
stores them in that database.

## Try it

```bash
go run .

# 1. Sign up; copy the token from the response.
curl -s -X POST localhost:8080/auth/signup -H 'Content-Type: application/json' \
  -d '{"name":"Ada Lovelace","email":"ada@example.com","password":"analytical-engine"}'
# {"user":{...,"name":"Ada Lovelace",...},"token":"<token>","expires_at":"...","verification_required":false}

# 2. The protected route rejects anonymous requests.
curl -s localhost:8080/api/hello
# {"error":"Missing session token."}

# 3. With the token it greets you.
curl -s localhost:8080/api/hello -H "Authorization: Bearer <token>"
# {"hello":"Ada Lovelace"}
```

## Configure with .env

Everything common can be switched on without touching the code. A local
development `.env` that stores users in PostgreSQL, requires verified
email addresses and prints the verification links to the console:

```dotenv
POSTGRES_CONNECTION_URL=postgres://app:secret@localhost:5432/myapp
COREGO_REQUIRE_VERIFIED_EMAIL=true
COREGO_MAIL=log            # development only; set SMTP_* (and COREGO_MAIL=smtp) in production
# COREGO_AUTH_COOKIE=true          also set the HttpOnly session cookie
# COREGO_AUTH_TABLE_PREFIX=myapp_  auth tables become myapp_users, myapp_sessions, ...
```

`corego.New` logs what it resolved, for example
`corego: auth ready store=postgres table_prefix=corego_ base_path=/auth mail=log require_verified_email=true cookie=false`.
Every variable is listed in [docs/environment.md](docs/environment.md)
and in [.env.example](.env.example).

## Packages

| Import path | What it is |
| --- | --- |
| `github.com/berkkaradalan/CoreGo` | The facade: `corego.New`, `Core.Server`, `Core.RequireAuth`, `Core.OptionalAuth`, `Core.Run`. |
| `github.com/berkkaradalan/CoreGo/web` | The web framework: router, groups, middleware, `Context`, errors, CORS, rate limiting. |
| `github.com/berkkaradalan/CoreGo/auth` | The auth service, its models and errors, `MustUser`, the memory store and the default mailer. |
| `github.com/berkkaradalan/CoreGo/auth/authhttp` | The auth HTTP routes and the `RequireAuth` and `OptionalAuth` middleware. |
| `github.com/berkkaradalan/CoreGo/auth/mongostore` | MongoDB `auth.Store`. |
| `github.com/berkkaradalan/CoreGo/auth/pgstore` | PostgreSQL `auth.Store` with migrations. |
| `github.com/berkkaradalan/CoreGo/database` | MongoDB and PostgreSQL connections and helpers. |
| `github.com/berkkaradalan/CoreGo/mail` | Email senders: SMTP, log, capture. |
| `github.com/berkkaradalan/CoreGo/env` | `.env` and environment loading with typed getters. |

## Documentation

- [Getting started](docs/getting-started.md): hello world, then recipes
  for adding endpoints, protecting routes, storing data, sending email,
  browser frontends and deploying.
- [Documentation index](docs/index.md): every page, and where to find
  what.
- [API reference](docs/api-reference.md): every exported identifier.
- [Migrating from 0.1](docs/migrating-from-0.1.md) and the
  [changelog](CHANGELOG.md).
- [Quickstart example](examples/quickstart/README.md): a runnable app
  with a recorded curl session.

## Status

CoreGo **0.2.0** is a complete rewrite of 0.1 with a new, stable-minded
API (see the [migration guide](docs/migrating-from-0.1.md)). It is
pre-1.0: minor releases may still change the API, and every change is
recorded in the [changelog](CHANGELOG.md).

Not available yet (planned, see the [roadmap](ROADMAP.md)): social sign-in
(Google, Apple, Facebook, GitHub, Microsoft), two-factor authentication,
organizations and teams, role-based access control, magic links and email
one-time codes, Redis-backed rate limiting and MySQL.

## Contributing

Contributions are welcome. The project rules, in short (the full list is
in [AGENTS.md](AGENTS.md)):

- English only in code, comments, tests and docs.
- Do not port or copy code from other libraries.
- Every change ships with tests, edge cases included; `go test ./...` and
  `go test -race ./...` stay green, `gofmt -l .` stays empty.
- Documentation ships with the change: update the affected page and
  [docs/api-reference.md](docs/api-reference.md) in the same pull request.

How to run the test suite, including the opt-in database integration
tests, is described in [docs/testing.md](docs/testing.md).

## License

MIT, see [LICENSE](LICENSE).
