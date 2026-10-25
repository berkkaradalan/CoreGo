# AGENTS.md

Guidance for AI coding agents (and human contributors) working on this
repository. Read it fully before making changes.

## Project identity

CoreGo is a backend SDK for Go: authentication with server-side
sessions, MongoDB and PostgreSQL access, transactional email, a web
framework and environment configuration behind one facade
(`corego.New`). It is a public library other developers depend on, so
developer experience and correctness come first.

## Rules

1. **English only, everywhere**: code, identifiers, comments, commit
   messages, test names, docs, file names, error messages.
2. **Clean, readable code and file structure**: small files grouped by
   concern, a doc comment on every exported identifier, a package doc
   comment on every package.
3. **Product mindset**: someone else will use this. No shortcuts.
4. **Every change ships with tests.** `go test ./...` and
   `go test -race ./...` stay green.
5. **Examine every edge case.** Table-driven tests where sensible.
6. **Documentation ships with the change.** Update the affected page in
   `docs/` and `docs/api-reference.md` in the same change. A code change
   with stale docs is incomplete.
7. **Write the code yourself.** Never port or copy another library's
   implementation. A familiar API shape is fine; copied code is not.
8. **Scope policy**: widely used core features only. Social sign-in means
   Google, Apple, Facebook, GitHub and Microsoft; niche providers and
   integrations are out of scope (see `ROADMAP.md`).
9. **Do not commit or push** unless the maintainer explicitly asks; the
   maintainer handles git.
10. **Never put machine-specific details in the repository**: no local
    paths, host names, occupied ports, personal notes or session logs.

## Architecture map

| Package | Role |
| --- | --- |
| `.` (`corego`) | Facade. `New(*Config)` loads the environment, connects databases, selects the auth store, wires the mailer from `SMTP_*`, and exposes `Server()`, `RequireAuth()`, `Run()` and `Close()`. |
| `env` | `.env` + process environment snapshot (process env always wins; `.env` never mutates the process). Typed getters. |
| `database` | MongoDB (driver v2) and PostgreSQL (pgx v5) wrappers: connection lifecycle, CRUD helpers, typed `QueryAll`/`QueryOne`, `WithTx`, `ErrNotFound`. |
| `mail` | `Sender` interface; `SMTPSender`, `LogSender` (development), `CaptureSender` (tests). |
| `web` | Standard-library-only framework: trie router, groups, middleware (CORS, RateLimit, RequestLogger, Recovery), `Context` (a `context.Context`), `HTTPError` helpers, net/http interop, graceful `Run`. Registration panics on programmer errors; response writers return `error`. |
| `auth` | Service core: sign-up/in, sessions (opaque tokens, SHA-256 at rest), email verification, password reset, change email/password, bans, cleanup, background email delivery, request-context helpers (`MustUser`). `Store` contract in `store.go`, sentinel errors in `errors.go`, `MemoryStore` reference implementation. |
| `auth/authhttp` | REST routes for the auth service under `/auth`, bearer and cookie transport, per-route rate limits, `RequireAuth`, net/http `Middleware`. |
| `auth/mongostore` | `auth.Store` on MongoDB with prefixed collections and `EnsureIndexes` (unique + TTL). |
| `auth/pgstore` | `auth.Store` on PostgreSQL with prefixed tables and a versioned `Migrate`. |
| `examples/` | Runnable example applications (part of the main module). |
| `docs/` | User documentation; the single source of the documentation website. |
| `website/` | Next.js landing page and documentation site. It renders `docs/*.md`, `CHANGELOG.md` and `ROADMAP.md` at build time; never copy documentation into it. See `website/README.md`. |

## Development workflow

```bash
go build ./... && go vet ./...   # must always pass
gofmt -l .                       # must print nothing
go test ./...                    # full suite
go test -race ./...              # race detector (auth packages are slow: bcrypt)
```

Integration tests are opt-in and skipped unless their environment
variables are set:

```bash
docker run -d --rm --name corego-test-mongo -p 27117:27017 mongo:7
docker run -d --rm --name corego-test-postgres -p 55432:5432 \
  -e POSTGRES_USER=corego -e POSTGRES_PASSWORD=corego \
  -e POSTGRES_DB=corego_test postgres:17

COREGO_TEST_MONGO_URL=mongodb://127.0.0.1:27117 \
COREGO_TEST_POSTGRES_URL=postgres://corego:corego@127.0.0.1:55432/corego_test \
  go test -race ./...

docker rm -f corego-test-mongo corego-test-postgres
```

See `docs/testing.md` for details.

## Working conventions

- The module `go` directive is the compatibility floor; do not raise it
  or add dependencies that require a newer Go.
- Sentinel errors are compared with `errors.Is`; never match strings.
- Public API changes are breaking changes: record them in `CHANGELOG.md`.
- When several agents work in parallel, partition the work by package
  ownership, freeze the shared contracts (`auth/store.go`,
  `auth/models.go`, `auth/errors.go`, `mail/mail.go`) while they run, and
  scope every `go` command to the agent's own packages.
- Finish every change with: build, vet, gofmt, the full test suite (race
  for touched packages), and a docs sync check.

## Lessons learned

- **Test documented default compositions end to end, not only interfaces
  with mocks.** An early version of `DefaultMailer` composed messages
  without `From` while `Message.Validate` required it, so every
  verification and reset email of the documented default setup failed,
  and unit tests using a custom mailer did not notice. Regression test:
  `auth/mailer_regression_test.go`.
- **Separate token purposes.** Email verification and password reset
  tokens share one table; their identifiers carry a purpose prefix so a
  token of one kind can never be used for the other.
