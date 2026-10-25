# Testing

This page has two parts: how to test **your application** built on
CoreGo, and how to run **CoreGo's own** test suite when you contribute.

- [Testing your application](#testing-your-application)
  - [End-to-end through the real server](#end-to-end-through-the-real-server)
  - [Emails in tests](#emails-in-tests)
  - [A single handler, without auth](#a-single-handler-without-auth)
  - [Tips](#tips)
- [Testing CoreGo itself](#testing-corego-itself)

## Testing your application

Everything CoreGo serves is a plain `http.Handler`, the auth service runs
on an in-memory store and email can be captured in memory, so tests need
no database, no SMTP server and no open port.

### End-to-end through the real server

Keep route registration in a function that both `main` and the tests
call:

```go
package main

import (
	"log"
	"net/http"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// registerRoutes adds the application's routes; main and the tests share it.
func registerRoutes(core *corego.Core) {
	api := core.Server().Group("/api", core.RequireAuth())
	api.Get("/hello", func(c *web.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"hello": auth.MustUser(c).Name})
	})
}

func main() {
	core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
	if err != nil {
		log.Fatal(err)
	}
	registerRoutes(core)
	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}
```

The test builds the same application on the memory store and sends
requests to `core.Server()` with `net/http/httptest`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/mail"
)

// newTestApp starts the application on the in-memory auth store with a
// capturing mail sender. No database, SMTP server or network is needed.
func newTestApp(t *testing.T) (*corego.Core, *mail.CaptureSender) {
	t.Helper()
	// Keep databases and auth switches configured in the shell or in .env
	// out of the tests.
	t.Setenv("MONGODB_CONNECTION_URL", "")
	t.Setenv("POSTGRES_CONNECTION_URL", "")
	t.Setenv("COREGO_REQUIRE_VERIFIED_EMAIL", "")
	t.Setenv("COREGO_AUTH_COOKIE", "")

	capture := mail.NewCaptureSender()
	core, err := corego.New(&corego.Config{
		Auth: &corego.AuthOptions{
			Store:      corego.StoreMemory,
			MailSender: capture,
			// Every test request comes from the same address; do not let
			// the per-IP (and per-email) limits of the auth routes get in the way.
			HTTP: authhttp.Config{RateLimit: authhttp.RateLimitConfig{Disabled: true}},
		},
		Server: corego.ServerOptions{DisableRequestLog: true},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Close() })
	registerRoutes(core)
	return core, capture
}

// call sends one request and decodes a JSON response into out (unless out is nil).
func call(t *testing.T, core *corego.Core, method, path, token, body string, out any) int {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	core.Server().ServeHTTP(rec, req)
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec.Code
}

const ada = `{"name":"Ada","email":"ada@example.com","password":"analytical-engine"}`

func TestHelloNeedsASession(t *testing.T) {
	core, _ := newTestApp(t)

	if code := call(t, core, "GET", "/api/hello", "", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous request: status %d, want 401", code)
	}

	var signup struct {
		Token string `json:"token"`
	}
	if code := call(t, core, "POST", "/auth/signup", "", ada, &signup); code != http.StatusCreated {
		t.Fatalf("signup: status %d", code)
	}

	var hello map[string]string
	code := call(t, core, "GET", "/api/hello", signup.Token, "", &hello)
	if code != http.StatusOK || hello["hello"] != "Ada" {
		t.Fatalf("hello: status %d, body %v", code, hello)
	}
}

func TestSignUpSendsAWorkingVerificationLink(t *testing.T) {
	core, capture := newTestApp(t)
	call(t, core, "POST", "/auth/signup", "", ada, nil)

	// Auth emails are delivered in the background; Shutdown waits for them.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := core.Auth.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	msg, ok := capture.Last()
	if !ok || msg.To[0] != "ada@example.com" {
		t.Fatalf("no verification email for ada: %+v", msg)
	}
	link, err := url.Parse(regexp.MustCompile(`https?://\S+`).FindString(msg.Text))
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"email":%q,"token":%q}`, link.Query().Get("email"), link.Query().Get("token"))
	if code := call(t, core, "POST", "/auth/verify-email", "", body, nil); code != http.StatusNoContent {
		t.Fatalf("verify: status %d, want 204", code)
	}
}
```

```bash
go test ./...
```

What makes this work:

- `Store: corego.StoreMemory` keeps every test's users in its own fresh
  store. The `t.Setenv` calls make sure a database configured in your
  shell or `.env` is not connected, and that on-only switches such as
  `COREGO_REQUIRE_VERIFIED_EMAIL=true` in a developer's `.env` do not
  change what the tests see (a set-but-empty variable also blocks
  `.env`). `COREGO_MAIL` needs no blanking here: `MailSender` takes
  precedence over it.
- `MailSender: capture` routes the auth emails into a
  `mail.CaptureSender`. They are sent in the background, so wait with
  `core.Auth.Shutdown(ctx)` before reading `capture.Last()` or
  `capture.Messages()`.
- `core.Server()` is an `http.Handler`; `ServeHTTP` with a recorder runs
  the whole stack (middleware, auth, your handler) without a listener.
- Rate limiting of the auth routes is disabled because every
  `httptest.NewRequest` comes from the same address (`192.0.2.1`). The
  request log, the startup warnings and the `corego: auth ready` line are
  discarded.

`t.Setenv` cannot be combined with `t.Parallel()`. For parallel tests,
make sure the process environment of `go test` has no database variables
instead (for example in CI).

### Emails in tests

`mail.CaptureSender` records every message:

```go
capture := mail.NewCaptureSender()
// ... run the code that sends ...
msgs := capture.Messages() // copies, in send order
last, ok := capture.Last()
capture.Reset()
```

Use it wherever your code takes a `mail.Sender` (your own emails) and as
`AuthOptions.MailSender` (auth emails). For a service built without the
facade, wrap it in `auth.NewDefaultMailer(capture, auth.DefaultMailerConfig{...})`
and wait for background deliveries with `svc.Shutdown(ctx)`.

For **manual testing** during development, use `mail.NewLogSender` with a
Debug-level logger: the email bodies, links included, appear in the
terminal ([mail.md](mail.md#logsender-development)).

### A single handler, without auth

A handler that reads the signed-in user can be tested on a bare
`web.Server`, with a small middleware that puts a user into the request
context the same way `RequireAuth` does:

```go
// signedInAs stands in for RequireAuth in handler tests.
func signedInAs(user *auth.User) web.MiddlewareFunc {
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(c *web.Context) error {
			session := &auth.Session{ID: "test-session", UserID: user.ID}
			ctx := auth.ContextWithSession(c.Request().Context(), session, user)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

func TestProfile(t *testing.T) {
	s := web.New()
	s.Get("/profile", profileHandler, signedInAs(&auth.User{ID: "u1", Name: "Ada"}))

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/profile", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Ada") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}
```

To test against the real auth routes without the facade, combine
`auth.New(auth.Config{}, auth.NewMemoryStore())`,
`authhttp.New(service, authhttp.Config{})` and `h.Mount(s)`
([http-api.md](http-api.md#mount-in-3-lines)).

### Tips

- **Assert on status codes and the `error` message.** Error bodies are
  always `{"error": "..."}`, and the auth routes use stable messages
  ([http-api.md](http-api.md#errors)).
- **Your own database code** is best tested against a real, disposable
  database (for example a container), the same way CoreGo tests its
  stores; skip those tests when the connection URL variable is not set.
- **Cookie authentication** works in tests too: enable
  `authhttp.CookieConfig{Enabled: true}` and copy the `Set-Cookie` header of
  the sign-in response into later requests. Requests built with
  `httptest.NewRequest` carry neither `Origin` nor `Sec-Fetch-Site`, so they
  pass the [CSRF check](http-api.md#csrf-protection); set
  `Sec-Fetch-Site: cross-site` and an `Origin` to test that a foreign site
  is answered `403`.
- **Time-dependent behavior** (session and token expiry) is easiest to
  test with short TTLs in `auth.Config` (for example
  `SessionTTL: time.Second`).
- **Environment-dependent code**: use `t.Setenv` and `env.LoadFile` with a
  file in `t.TempDir()` ([environment.md](environment.md#testing-code-that-reads-the-environment)).

## Testing CoreGo itself

From the repository root:

```bash
go build ./... && go vet ./...
go test ./...
go test -race ./...
gofmt -l .        # must print nothing
```

The default run needs no infrastructure: the integration tests skip
themselves when their environment variable is not set.

### Integration tests

Two opt-in variables enable the tests that need a real server:

| Variable | Enables |
| --- | --- |
| `COREGO_TEST_MONGO_URL` | `database` (MongoDB helpers), `auth/mongostore` (the full `auth.Store` contract, indexes, prefixes) and the facade's MongoDB tests in the root package. |
| `COREGO_TEST_POSTGRES_URL` | `database` (PostgreSQL helpers, transactions), `auth/pgstore` (the store contract, migrations, prefixes, concurrency) and the facade's PostgreSQL tests in the root package. |

The suites create randomly named databases, collections, schemas or
tables and drop them when they finish, so they do not interfere with each
other. Still, point them only at disposable servers, never at a database
whose data you care about. When a variable is set but the server is not
reachable, the tests fail rather than skip.

Start throwaway servers with Docker (change the host ports if they are
taken on your machine):

```bash
docker run -d --rm --name corego-test-mongo -p 27017:27017 mongo:7
docker run -d --rm --name corego-test-postgres -p 5432:5432 \
  -e POSTGRES_USER=corego -e POSTGRES_PASSWORD=corego -e POSTGRES_DB=corego_test \
  postgres:17

COREGO_TEST_MONGO_URL=mongodb://127.0.0.1:27017 \
COREGO_TEST_POSTGRES_URL=postgres://corego:corego@127.0.0.1:5432/corego_test \
  go test -race ./...

docker rm -f corego-test-mongo corego-test-postgres
```

### Documentation accuracy

Docs are part of the product. If you change code, change the affected
page and [api-reference.md](api-reference.md) in the same pull request:
a new or changed exported identifier, a different default, status code,
error message or environment variable all count. Code samples in the
docs must compile against the current code.
