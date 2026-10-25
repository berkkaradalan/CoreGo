# Getting Started

This page takes you from an empty directory to a running API with sign-up,
sign-in and a protected route, then walks through the six things every
backend needs next: adding endpoints, protecting them, storing data,
sending email, talking to a browser frontend and deploying.

- [Install](#install)
- [Hello world](#hello-world)
- [How a CoreGo app fits together](#how-a-corego-app-fits-together)
- [A suggested project layout](#a-suggested-project-layout)
- Recipes:
  [1. Add an endpoint](#1-add-an-endpoint) ·
  [2. Protect routes and read the current user](#2-protect-routes-and-read-the-current-user) ·
  [3. Connect a database and store your data](#3-connect-a-database-and-store-your-data) ·
  [4. Send email](#4-send-email) ·
  [5. A browser frontend: CORS and cookies](#5-a-browser-frontend-cors-and-cookies) ·
  [6. Deploy checklist](#6-deploy-checklist)

## Install

CoreGo needs Go 1.24 or newer.

```bash
mkdir myapp && cd myapp
go mod init example.com/myapp
go get github.com/berkkaradalan/CoreGo@v0.2.0
```

(Until the `v0.2.0` tag is published, use `@main`.)

## Hello world

`main.go`:

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

Line by line:

| Code | What it does |
| --- | --- |
| `corego.New(&corego.Config{Auth: &corego.AuthOptions{}})` | Loads `.env` and the environment, connects any configured database and starts the auth service. `&corego.AuthOptions{}` means "auth on, all defaults". |
| `core.Server()` | Returns the web server, with request logging and the 17 auth routes already mounted under `/auth` (`/auth/signup`, `/auth/signin`, ...). |
| `app.Group("/api", core.RequireAuth())` | Every route registered on `api` lives under `/api` and requires a valid session. |
| `api.Get("/hello", func(c *web.Context) error {...})` | Adds `GET /api/hello`. Handlers get a `*web.Context` and return an `error`. |
| `auth.MustUser(c)` | The signed-in user, put there by `RequireAuth`. |
| `return c.JSON(http.StatusOK, ...)` | Writes the JSON response. Every response writer returns an error, so a handler ends with `return c.<writer>(...)`. |
| `core.Run()` | Serves on `PORT` (default `8080`) until Ctrl+C or SIGTERM, finishes in-flight requests, then closes the databases. It returns `nil` after a clean shutdown. |

Run it:

```bash
go run .
```

```text
WRN corego: auth emails are disabled: no mailer is configured, ...
WRN corego: no database is connected, so auth uses the in-memory store; ...
INF corego: auth ready store=memory base_path=/auth mail=none require_verified_email=false cookie=false

  CoreGo 0.2.0  http://localhost:8080  (listening on :8080)

  Services
    ✓ Auth        routes under /auth · store memory · email off
    · MongoDB     not configured
    · PostgreSQL  not configured
    · CORS        not configured

  Routes
    GET     /api/hello

  Auth routes (17, under /auth)
    POST    /auth/signup
    POST    /auth/signin
    POST    /auth/signout               session required
    GET     /auth/session               session required
    ...
    DELETE  /auth/me                    session required
```

Both warnings are expected for now: without a database, users live in
memory until the process exits, and without a mailer no verification or
password reset emails are sent. Recipes 3 and 4 fix that. The
`auth ready` line summarizes what `corego.New` resolved from your code
and environment: the store, the route prefix, the mailer, whether email
verification is mandatory and whether the session cookie is on.

The banner below it (printed when the output is a terminal) shows what is
running: the services in use, your routes and the 17 auth routes that
`core.Server()` mounted for you. `session required` marks the routes that
need a session token. Your `/api/hello` needs one as well, because its
group uses `core.RequireAuth()`: opening http://localhost:8080/api/hello
in a browser answers `401 {"error":"Missing session token."}`, and
http://localhost:8080/ answers 404 because nothing is registered there.
Sign up first to get a token, as shown below. In a terminal the output is
colored; set `NO_COLOR=1` to turn the colors off.

Try it from another terminal:

```bash
# 1. Sign up. The response carries the session token, exactly once.
curl -s -X POST localhost:8080/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada Lovelace","email":"ada@example.com","password":"analytical-engine"}'
# {"user":{"id":"...","name":"Ada Lovelace",...},"token":"<token>","expires_at":"...","verification_required":false}

# 2. Without the token, the protected route answers 401.
curl -s localhost:8080/api/hello
# {"error":"Missing session token."}

# 3. With the token as a bearer header:
curl -s localhost:8080/api/hello -H "Authorization: Bearer <token>"
# {"hello":"Ada Lovelace"}
```

Every auth route (sign-in, sign-out, sessions, email verification,
password reset, profile, account deletion) is listed in
[http-api.md](http-api.md).

## How a CoreGo app fits together

```text
corego.New(cfg)          -> *corego.Core
  core.Env               environment snapshot (.env + process env)
  core.Mongo, Postgres   database handles (nil when not configured)
  core.Auth              the auth service (*auth.Auth)
core.Server()            -> *web.Server with logging, CORS (if configured) and /auth/* mounted
  app.Get/Post/...       your routes
  app.Group(prefix, mw)  a prefix plus middleware, e.g. core.RequireAuth()
core.Run()               serve on PORT, graceful shutdown, close everything
```

Three rules cover almost all of the web framework:

1. A handler is `func(c *web.Context) error`. It ends with
   `return c.JSON(...)` (or `c.String`, `c.NoContent`, ...), or it returns an
   error.
2. Returned errors become JSON responses: `web.NotFound("...")` gives
   `404 {"error":"..."}`, `web.BadRequest`, `web.Forbidden`, `web.Conflict`
   and friends work the same way, and any other error is logged and
   answered with a generic `500`.
3. `*web.Context` is a `context.Context`. Pass `c` straight to the auth
   service and the database helpers; the call is canceled when the client
   goes away.

Everything else is in [web-framework.md](web-framework.md).

## A suggested project layout

CoreGo does not impose a layout. This one keeps `main.go` short and each
feature in its own package:

```text
myapp/
  go.mod
  main.go             corego.New, register each feature, core.Run
  .env                local settings; never commit it
  internal/
    tasks/
      routes.go       func Register(api *web.Group, db *database.PostgresDB)
      store.go        SQL or MongoDB queries
      routes_test.go  httptest against a web.Server
```

`main.go` then reads like a table of contents:

```go
api := app.Group("/api", core.RequireAuth())
tasks.Register(api.Group("/tasks"), core.Postgres)
```

## Recipes

Each recipe is a complete `main.go` (or a complete fragment of one) that
compiles against CoreGo 0.2.0.

### 1. Add an endpoint

A small JSON API: list, read, create and delete books. No auth involved.

```go
package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/web"
)

type Book struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Author string `json:"author"`
}

var errBookNotFound = errors.New("book not found")

// shelf is a tiny in-memory repository; recipe 3 uses a real database.
type shelf struct {
	mu    sync.Mutex
	books map[string]Book
	next  int
}

func (s *shelf) list() []Book {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Book, 0, len(s.books))
	for _, b := range s.books {
		out = append(out, b)
	}
	return out
}

func (s *shelf) get(id string) (Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.books[id]
	if !ok {
		return Book{}, errBookNotFound
	}
	return b, nil
}

func (s *shelf) add(title, author string) Book {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	b := Book{ID: strconv.Itoa(s.next), Title: title, Author: author}
	s.books[b.ID] = b
	return b
}

func (s *shelf) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.books[id]; !ok {
		return errBookNotFound
	}
	delete(s.books, id)
	return nil
}

func main() {
	core, err := corego.New(nil) // nil: no auth, databases only if the environment names one
	if err != nil {
		log.Fatal(err)
	}
	app := core.Server()
	books := &shelf{books: map[string]Book{}}

	// GET /books
	app.Get("/books", func(c *web.Context) error {
		return c.JSON(http.StatusOK, books.list())
	})

	// GET /books/:id - path parameters are read with c.Param.
	app.Get("/books/:id", func(c *web.Context) error {
		book, err := books.get(c.Param("id"))
		if errors.Is(err, errBookNotFound) {
			return web.NotFound("book not found") // 404 {"error":"book not found"}
		}
		if err != nil {
			return err // anything unexpected: logged, 500
		}
		return c.JSON(http.StatusOK, book)
	})

	// POST /books - JSON body in, 201 out.
	app.Post("/books", func(c *web.Context) error {
		var in struct {
			Title  string `json:"title"`
			Author string `json:"author"`
		}
		if err := c.BindJSON(&in); err != nil {
			return err // malformed JSON 400, wrong Content-Type 415, too large 413
		}
		if strings.TrimSpace(in.Title) == "" {
			return web.BadRequest("title is required")
		}
		return c.JSON(http.StatusCreated, books.add(in.Title, in.Author))
	})

	// DELETE /books/:id - 204 without a body.
	app.Delete("/books/:id", func(c *web.Context) error {
		err := books.remove(c.Param("id"))
		if errors.Is(err, errBookNotFound) {
			return web.NotFound("book not found")
		}
		if err != nil {
			return err
		}
		return c.NoContent()
	})

	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}
```

```bash
curl -s -X POST localhost:8080/books -H 'Content-Type: application/json' \
  -d '{"title":"Sketch of the Analytical Engine","author":"Ada Lovelace"}'
# {"id":"1","title":"Sketch of the Analytical Engine","author":"Ada Lovelace"}
curl -s localhost:8080/books/42
# {"error":"book not found"}
```

What to remember:

- Map **only the errors you expect** to a status (`errors.Is(err,
  errBookNotFound)` → 404). Return everything else unchanged: the framework
  logs it and answers `500 {"error":"Internal Server Error"}` without
  leaking details. Never turn every error into a 404.
- `c.Query("q")` reads `?q=`, `c.GetHeader("X-Name")` a header,
  `c.Param("id")` a `:id` segment, `c.BindJSON(&v)` a JSON body.
- Routes support `:name` parameters and a final `*rest` wildcard. Mistakes
  such as a duplicate route panic at startup with a `web: ` message, so they
  never reach production.

### 2. Protect routes and read the current user

```go
package main

import (
	"log"
	"net/http"
	"slices"
	"time"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// userView is what this API returns for a user. auth.User marshals to
// JSON too, but that includes the ban fields, so pick what to expose.
type userView struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Verified bool      `json:"verified"`
	Since    time.Time `json:"since"`
}

func viewOf(u *auth.User) userView {
	return userView{ID: u.ID, Name: u.Name, Email: u.Email, Verified: u.EmailVerified, Since: u.CreatedAt}
}

// requireAdmin lets only the listed email addresses through. It must run
// after RequireAuth, which puts the user into the request context.
func requireAdmin(admins []string) web.MiddlewareFunc {
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(c *web.Context) error {
			if !slices.Contains(admins, auth.MustUser(c).Email) {
				return web.Forbidden("admins only")
			}
			return next(c)
		}
	}
}

func main() {
	core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
	if err != nil {
		log.Fatal(err)
	}
	app := core.Server()

	// Public.
	app.Get("/health", func(c *web.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Everything in this group requires a valid session.
	api := app.Group("/api", core.RequireAuth())
	api.Get("/profile", func(c *web.Context) error {
		return c.JSON(http.StatusOK, viewOf(auth.MustUser(c)))
	})

	// A nested group adds more middleware: session first, then the admin check.
	admin := api.Group("/admin", requireAdmin(core.Env.Strings("ADMIN_EMAILS")))
	admin.Get("/users", func(c *web.Context) error {
		users, err := core.Auth.ListUsers(c, auth.ListOptions{Limit: 50})
		if err != nil {
			return err
		}
		views := make([]userView, 0, len(users))
		for _, u := range users {
			views = append(views, viewOf(u))
		}
		return c.JSON(http.StatusOK, views)
	})

	// A single protected route outside any group: per-route middleware
	// goes after the handler.
	app.Get("/reports/today", func(c *web.Context) error {
		return c.String(http.StatusOK, "report for "+auth.MustUser(c).Name)
	}, core.RequireAuth())

	// Optional sign-in: the same route serves guests and signed-in users.
	app.Get("/greeting", func(c *web.Context) error {
		if user, ok := auth.UserFromContext(c); ok {
			return c.String(http.StatusOK, "Welcome back, "+user.Name)
		}
		return c.String(http.StatusOK, "Hello, guest")
	}, core.OptionalAuth())

	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}
```

- `core.RequireAuth()` answers `401` for a missing, unknown or expired
  token and `403` for a banned user (or an unverified one when
  `RequireVerifiedEmail` is on). Your handler only runs for valid sessions.
- `auth.MustUser(c)` returns the `*auth.User` (`ID`, `Name`, `Email`,
  `EmailVerified`, `Image`, `CreatedAt`, ...). It panics on a route that
  is not behind `RequireAuth`, which the framework turns into a `500`, so
  a forgotten middleware shows up the first time you call the route.
- `core.OptionalAuth()` never rejects a request: with a valid session it
  stores the user like `RequireAuth`, otherwise the handler runs for a
  guest. Read the user with `auth.UserFromContext(c)`, which reports
  whether one is present (`MustUser` would panic for guests).
- `auth.SessionFromContext(c)` returns the current session, for example to
  mark "this device" in a session list.
- Group middleware runs parent first: `/api/admin/users` runs
  `RequireAuth`, then `requireAdmin`, then the handler.
- With `ADMIN_EMAILS=ada@example.com` in `.env`, `core.Env.Strings` returns
  `["ada@example.com"]`.

Clients send the token from sign-up or sign-in as
`Authorization: Bearer <token>`. Browser apps can use a cookie instead
(recipe 5).

### 3. Connect a database and store your data

Point CoreGo at a database through the environment. With PostgreSQL:

```dotenv
# .env
POSTGRES_CONNECTION_URL=postgres://app:secret@localhost:5432/myapp
```

`corego.New` connects at startup (and fails fast when the server is
unreachable), uses the same database for users and sessions (tables
prefixed `corego_`, created automatically) and exposes the connection as
`core.Postgres`. A per-user task list:

```go
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/database"
	"github.com/berkkaradalan/CoreGo/web"
)

type Task struct {
	ID        int64     `db:"id"         json:"id"`
	Title     string    `db:"title"      json:"title"`
	Done      bool      `db:"done"       json:"done"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

const schema = `CREATE TABLE IF NOT EXISTS tasks (
	id         BIGSERIAL PRIMARY KEY,
	owner_id   TEXT        NOT NULL,
	title      TEXT        NOT NULL,
	done       BOOLEAN     NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

func main() {
	core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
	if err != nil {
		log.Fatal(err)
	}
	if core.Postgres == nil {
		core.Close()
		log.Fatal("set POSTGRES_CONNECTION_URL")
	}
	db := core.Postgres
	if _, err := db.Exec(context.Background(), schema); err != nil {
		core.Close()
		log.Fatal(err)
	}

	api := core.Server().Group("/api", core.RequireAuth())

	api.Get("/tasks", func(c *web.Context) error {
		tasks, err := database.QueryAll[Task](c, db,
			`SELECT id, title, done, created_at FROM tasks
			  WHERE owner_id = $1 ORDER BY created_at DESC`, auth.MustUser(c).ID)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, tasks) // [] when empty, never null
	})

	api.Post("/tasks", func(c *web.Context) error {
		var in struct {
			Title string `json:"title"`
		}
		if err := c.BindJSON(&in); err != nil {
			return err
		}
		if strings.TrimSpace(in.Title) == "" {
			return web.BadRequest("title is required")
		}
		task, err := database.QueryOne[Task](c, db,
			`INSERT INTO tasks (owner_id, title) VALUES ($1, $2)
			 RETURNING id, title, done, created_at`, auth.MustUser(c).ID, in.Title)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, task)
	})

	api.Get("/tasks/:id", func(c *web.Context) error {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			return web.NotFound("task not found") // not a number: cannot exist
		}
		task, err := database.QueryOne[Task](c, db,
			`SELECT id, title, done, created_at FROM tasks
			  WHERE id = $1 AND owner_id = $2`, id, auth.MustUser(c).ID)
		if errors.Is(err, database.ErrNotFound) {
			return web.NotFound("task not found")
		}
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, task)
	})

	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}
```

- `database.QueryAll[T]` / `QueryOne[T]` scan rows into your struct by its
  `db` tags. `QueryOne` returns an error matching `database.ErrNotFound`
  when there is no row.
- Always pass values as `$1, $2, ...` arguments, never by formatting them
  into the SQL.
- `owner_id` holds `auth.MustUser(c).ID`, so every user only sees their
  own tasks.
- `c` is the context: a client that disconnects cancels its query.

**MongoDB** works the same way. Set `MONGODB_CONNECTION_URL` (and
optionally `MONGODB_DATABASE`) and use `core.Mongo`:

```go
type Note struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	OwnerID   string        `bson:"owner_id"      json:"-"`
	Text      string        `bson:"text"          json:"text"`
	CreatedAt time.Time     `bson:"created_at"    json:"created_at"`
}

api.Post("/notes", func(c *web.Context) error {
	var in struct {
		Text string `json:"text"`
	}
	if err := c.BindJSON(&in); err != nil {
		return err
	}
	note := Note{OwnerID: auth.MustUser(c).ID, Text: in.Text, CreatedAt: time.Now().UTC()}
	id, err := core.Mongo.InsertOne(c, "notes", note)
	if err != nil {
		return err
	}
	note.ID = id.(bson.ObjectID) // generated by the driver for the empty _id
	return c.JSON(http.StatusCreated, note)
})

api.Get("/notes/:id", func(c *web.Context) error {
	oid, err := bson.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		return web.NotFound("note not found") // not an ObjectID: cannot exist
	}
	var note Note
	err = core.Mongo.FindOne(c, "notes", bson.M{"_id": oid, "owner_id": auth.MustUser(c).ID}, &note)
	if errors.Is(err, database.ErrNotFound) {
		return web.NotFound("note not found")
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, note)
})
```

`bson` is `go.mongodb.org/mongo-driver/v2/bson`. Updates, deletes,
counting, sorting, transactions and the raw drivers are covered in
[database.md](database.md); where the auth data lives and how to choose
the auth store is in [auth-storage.md](auth-storage.md).

### 4. Send email

**In development** you usually have no SMTP server. One line in `.env`
makes CoreGo write every auth email, links included, to the log instead
of sending it:

```dotenv
COREGO_MAIL=log
```

```text
WARN corego: COREGO_MAIL=log: auth emails are written to the log instead of being sent, ...
INFO corego: auth ready store=memory base_path=/auth mail=log require_verified_email=false cookie=false
...
INFO mail: outgoing message to=ada@example.com subject="Verify your email address" links="http://localhost:8080/auth/verify-email?email=ada%40example.com&token=..."
```

Copy the link into the browser (or `curl` it) to verify the address. The
links carry live tokens, which is why `corego.New` warns at startup:
never set `COREGO_MAIL=log` in production. Add
`COREGO_REQUIRE_VERIFIED_EMAIL=true` to try the complete "verify before
you can sign in" flow without writing any code.

**In production**, auth emails (email verification, password reset) need
no code either: set the `SMTP_*` variables and CoreGo sends them.

```dotenv
APP_URL=https://api.example.com
COREGO_MAIL=smtp   # optional: fail at startup when SMTP_HOST is missing
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=no-reply@example.com
SMTP_PASSWORD=change-me
SMTP_FROM="My App <no-reply@example.com>"
```

Port 465 selects implicit TLS, every other port STARTTLS. `APP_URL` is
used to build the links in the emails.

**Your own emails** go through the same `mail.Sender` interface. This
program uses SMTP when `SMTP_HOST` is set and a `mail.LogSender`
otherwise, for both the auth emails and its own invitation email. It
loads the environment itself (to build the sender) and passes the
snapshot on as `Config.Env`, so `.env` is read only once:

```go
package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/mail"
	"github.com/berkkaradalan/CoreGo/web"
)

// newSender returns an SMTP sender when SMTP_HOST is configured and a
// development LogSender otherwise.
func newSender(e *env.Env) mail.Sender {
	host := e.GetOrDefault("SMTP_HOST", "")
	if host == "" {
		debug := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
		return mail.NewLogSender(debug)
	}
	port := e.Int("SMTP_PORT", 587)
	tlsMode := mail.TLSStartTLS
	if port == 465 {
		tlsMode = mail.TLSImplicit
	}
	return mail.NewSMTPSender(mail.SMTPConfig{
		Host:     host,
		Port:     port,
		Username: e.GetOrDefault("SMTP_USERNAME", ""),
		Password: e.GetOrDefault("SMTP_PASSWORD", ""),
		From:     e.GetOrDefault("SMTP_FROM", ""),
		TLSMode:  tlsMode,
	})
}

func main() {
	e, err := env.Load()
	if err != nil {
		log.Fatal(err)
	}
	sender := newSender(e)

	core, err := corego.New(&corego.Config{
		Env:  e,                                      // reuse the snapshot instead of loading .env again
		Auth: &corego.AuthOptions{MailSender: sender}, // auth emails use it too
	})
	if err != nil {
		log.Fatal(err)
	}

	api := core.Server().Group("/api", core.RequireAuth())
	api.Post("/invite", func(c *web.Context) error {
		var in struct {
			Email string `json:"email"`
		}
		if err := c.BindJSON(&in); err != nil {
			return err
		}
		msg := mail.Message{
			To:      []string{in.Email},
			Subject: auth.MustUser(c).Name + " invited you",
			Text:    "Join us at " + core.Env.AppURL,
		}
		if err := msg.Validate(); err != nil {
			return web.BadRequest("invalid email address")
		}
		if err := sender.Send(c, msg); err != nil {
			return &web.HTTPError{Code: http.StatusBadGateway, Message: "could not send the invitation", Err: err}
		}
		return c.NoContent()
	})

	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}
```

`mail.LogSender` logs each email's links at Info level and the full
bodies at Debug level, which is why `newSender` gives it a Debug logger.
Never use `LogSender` in production.

Which mailer the auth service uses, in order: `AuthOptions.Service.Mailer`
(your own `auth.Mailer`), then `AuthOptions.MailSender`, then what
`COREGO_MAIL` selects: empty means SMTP when `SMTP_HOST` is set and
otherwise no email with a startup warning, `smtp` requires `SMTP_HOST`,
`log` writes the emails to the log and `none` sends nothing without a
warning. Details: [mail.md](mail.md) and
[authentication.md](authentication.md#mailers).

### 5. A browser frontend: CORS and cookies

When a single-page app on another origin (for example Vite on
`http://localhost:5173`) calls the API on `http://localhost:8080`, enable
CORS and the HttpOnly session cookie, which JavaScript cannot read. Sign-up
and sign-in also return the token in the JSON body, for clients that are
not browsers; set `OmitTokenFromBody` when only browsers use the API, so
the token never reaches JavaScript at all:

```go
core, err := corego.New(&corego.Config{
	Auth: &corego.AuthOptions{
		HTTP: authhttp.Config{
			Cookie: authhttp.CookieConfig{
				Enabled:           true, // HttpOnly, SameSite=Lax
				OmitTokenFromBody: true, // browser-only API: no token in JSON responses
			},
		},
	},
	Server: corego.ServerOptions{
		CORS: &web.CORSConfig{
			AllowedOrigins:   []string{"http://localhost:5173"},
			AllowCredentials: true, // required for cookies
		},
	},
})
```

Instead of `Server.CORS` you can set `CORS_ALLOWED_ORIGINS` (comma
separated); the facade then allows those origins with credentials. The
cookie can be switched on from the environment too, with
`COREGO_AUTH_COOKIE=true` (the token then stays in the JSON body as
well). Two
rules for the origin list:

- **List the origins; do not use `"*"` with cookies.** Browsers never send
  cookies to a wildcard CORS policy, so `web.CORS` panics on `"*"` with
  `AllowCredentials`, and `CORS_ALLOWED_ORIGINS=*` allows every origin
  without credentials (only the `Authorization` header works then; the
  facade logs a warning).
- **The listed origins are also trusted for cookie requests.** Requests
  authenticated with the cookie that change state (`POST`, `PATCH`,
  `DELETE`, ...) pass a cross-site request forgery check only when the
  browser marks them same-site or their origin is trusted; everything else
  is answered `403 Cross-site request blocked.`. The facade trusts the
  explicit CORS origins (from `Server.CORS` or `CORS_ALLOWED_ORIGINS`)
  unless you set `AuthOptions.HTTP.TrustedOrigins` yourself. Requests with
  a bearer token are never checked.

The frontend sends the cookie along with `credentials: "include"`:

```js
await fetch("http://localhost:8080/auth/signin", {
  method: "POST",
  credentials: "include",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password }),
});

const res = await fetch("http://localhost:8080/api/profile", { credentials: "include" });
```

- Sign-up and sign-in set the `corego_session` cookie; sign-out clears it.
  `RequireAuth` and `OptionalAuth` read it when no `Authorization` header
  is sent.
- The cookie is marked `Secure` automatically when `APP_URL` starts with
  `https://`. Without `Secure` and with an `APP_URL` other than
  `localhost`, `corego.New` logs a warning.
- Frontend and API on different registrable domains (`app.example.net`
  calling `api.example.com`) need `SameSite: http.SameSiteNoneMode` with
  `Secure: true`; subdomains of one domain (and `localhost` on two ports)
  are the same site and work with the default.

Details: [http-api.md](http-api.md#bearer-tokens-or-cookies),
[CSRF protection](http-api.md#csrf-protection) and
[CORS](web-framework.md#cors).

### 6. Deploy checklist

Before the first production deploy:

- [ ] **`APP_URL`** is the public `https://` URL of the API. Email links are
      built from it, and with cookies enabled it turns on the `Secure`
      cookie flag.
- [ ] **Cookies are `Secure`.** Automatic with an `https://` `APP_URL`;
      otherwise set `authhttp.CookieConfig{Secure: true}`. Watch the
      startup log: `corego.New` warns when cookies are enabled without
      `Secure` and `APP_URL` is not a localhost address.
- [ ] **A persistent auth store.** Set `MONGODB_CONNECTION_URL` or
      `POSTGRES_CONNECTION_URL` (or `COREGO_AUTH_STORE` when both are
      set). Without one, users and sessions live in memory and vanish on
      restart; the startup log warns about it.
- [ ] **SMTP is configured** (`SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`,
      `SMTP_PASSWORD`, `SMTP_FROM`), with SPF/DKIM set up for the sender
      domain. `COREGO_MAIL=smtp` makes a missing `SMTP_HOST` a startup
      error. No `LogSender` and no `COREGO_MAIL=log` in production.
- [ ] **The reset page exists.** Password reset emails link to
      `COREGO_RESET_PASSWORD_URL` (default `APP_URL/reset-password`). That
      page belongs to your frontend: it reads `token` and `email` from the
      URL and calls `POST /auth/reset-password`
      ([http-api.md](http-api.md#resetting-a-password)).
- [ ] **Behind a reverse proxy or load balancer**, set
      `TRUST_PROXY_HEADERS=true` so logs and the auth rate limits see the
      real client IP; otherwise every client shares the proxy's rate limit
      budget. The server logs a one-time warning (`web: request carries a
      proxy client IP header but Server.TrustProxyHeaders is false; ...`)
      when requests carry `X-Forwarded-For` while the setting is off. Only
      enable it when every request passes through a proxy you control.
- [ ] **CORS** lists your real frontend origins only
      (`CORS_ALLOWED_ORIGINS`), never `*` when the frontend uses cookies.
      These origins are also the ones trusted by the CSRF check of
      cookie-authenticated requests.
- [ ] **Graceful shutdown.** `core.Run()` handles SIGTERM: it stops
      accepting connections, waits up to 10 s for in-flight requests, then
      up to 10 s for auth emails still being sent, then closes the
      databases. Give the container at least 20 s to stop (Docker's
      default is 10 s: use `stop_grace_period` / `--stop-timeout`).
- [ ] **Expired sessions and tokens are cleaned up.** The MongoDB store
      expires them with TTL indexes; on PostgreSQL run
      `core.Auth.CleanupExpired(ctx)` periodically
      ([authentication.md](authentication.md#cleanup)).
- [ ] **Decide on email verification.** With
      `auth.Config{RequireVerifiedEmail: true}` (in
      `AuthOptions.Service`) or `COREGO_REQUIRE_VERIFIED_EMAIL=true`
      users must verify their address before they can sign in.
- [ ] **Check the `corego: auth ready` line** in the startup log: it
      shows the store, mailer, email verification and cookie settings
      the deployment actually resolved to.

A typical production environment:

```dotenv
APP_URL=https://api.example.com
PORT=8080
POSTGRES_CONNECTION_URL=postgres://app:secret@db.internal:5432/app?sslmode=require
COREGO_REQUIRE_VERIFIED_EMAIL=true
COREGO_MAIL=smtp
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=no-reply@example.com
SMTP_PASSWORD=change-me
SMTP_FROM="My App <no-reply@example.com>"
COREGO_RESET_PASSWORD_URL=https://app.example.com/reset-password
CORS_ALLOWED_ORIGINS=https://app.example.com
TRUST_PROXY_HEADERS=true
```

Every variable is described in [environment.md](environment.md).

## Where to go next

- [web-framework.md](web-framework.md): routing rules, middleware, the
  `Context` API, errors, CORS, rate limiting, running the server.
- [authentication.md](authentication.md): everything the auth service
  does, and how to call it from your own code.
- [http-api.md](http-api.md): the 17 auth routes, their bodies, status
  codes and error messages.
- [testing.md](testing.md): testing your application with `httptest`,
  the memory store and a capturing mail sender.
- [api-reference.md](api-reference.md): every exported identifier.
