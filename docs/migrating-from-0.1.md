# Migrating from 0.1 to 0.2

CoreGo 0.2.0 is a rewrite. The module path is unchanged, but almost every
API is different, authentication moved from JWTs to server-side sessions
and the Gin integration is gone. This page shows, for each 0.1 API, what
replaces it. It also covers the PostgreSQL wrapper that was on the `main`
branch after the 0.1.0 tag.

- [Checklist](#checklist)
- [Dependencies](#dependencies)
- [The facade](#the-facade)
- [Environment](#environment)
- [MongoDB](#mongodb)
- [PostgreSQL](#postgresql)
- [Authentication](#authentication)
- [HTTP routes and the Gin integration](#http-routes-and-the-gin-integration)
- [Migrating existing users](#migrating-existing-users)
- [What API clients notice](#what-api-clients-notice)

## Checklist

1. `go get github.com/berkkaradalan/CoreGo@v0.2.0`, then `go mod tidy`.
2. Replace `mongo-driver` v1 imports with `go.mongodb.org/mongo-driver/v2/...`.
3. Replace `corego.Config{Auth: &auth.Config{...}}` with
   `corego.Config{Auth: &corego.AuthOptions{}}`; drop the JWT secret.
4. Replace the Gin auth handlers with the routes `core.Server()` mounts,
   or keep Gin and use the adapter shown [below](#keeping-gin).
5. Replace `core.Auth.Middleware()` with `core.RequireAuth()` and
   `c.Get("userID")` with `auth.MustUser(c).ID`.
6. Add a `context.Context` as the first argument of every `database`
   call, and rename `GetClient`/`GetPool`/`Disconnect`.
7. Rename `env.LoadEnv()` to `env.Load()` and the `Env` fields.
8. Copy your existing users into the new auth store
   ([script below](#migrating-existing-users)). Everyone signs in once
   more, because 0.1 JWTs are not accepted any more.

## Dependencies

| 0.1 | 0.2 |
| --- | --- |
| `github.com/gin-gonic/gin` | removed (CoreGo has its own `web` package) |
| `github.com/golang-jwt/jwt/v5` | removed (opaque server-side sessions) |
| `go.mongodb.org/mongo-driver` (v1) | `go.mongodb.org/mongo-driver/v2` |
| `go.mongodb.org/mongo-driver/bson/primitive.ObjectID` | `go.mongodb.org/mongo-driver/v2/bson.ObjectID` |
| `go.mongodb.org/mongo-driver/bson.M` | `go.mongodb.org/mongo-driver/v2/bson.M` |
| Go 1.24.1+ (the `go` directive of v0.1.0) | Go 1.24+ |

## The facade

Before (0.1):

```go
core, err := corego.New(&corego.Config{
	Auth: &auth.Config{
		Secret:       os.Getenv("AUTH_SECRET"),
		TokenExpiry:  60,
		DatabaseName: "users",
	},
})
if err != nil {
	panic(err)
}
defer core.Close()

r := gin.Default()
// ... routes ...
r.Run(":8080")
```

After (0.2):

```go
core, err := corego.New(&corego.Config{
	Auth: &corego.AuthOptions{
		Service: auth.Config{SessionTTL: 24 * time.Hour}, // optional; default 7 days
	},
})
if err != nil {
	log.Fatal(err)
}
app := core.Server() // /auth/* routes already mounted
// ... routes ...
if err := core.Run(); err != nil { // serves on PORT, graceful shutdown, closes everything
	log.Fatal(err)
}
```

| 0.1 | 0.2 |
| --- | --- |
| `corego.Config.Auth *auth.Config` | `corego.Config.Auth *corego.AuthOptions` |
| `corego.Core.Auth *auth.Manager` | `corego.Core.Auth *auth.Auth` |
| auth only with MongoDB | auth on MongoDB, PostgreSQL or memory, chosen automatically ([auth-storage.md](auth-storage.md)) |
| `Core.Close()` closes MongoDB | `Core.Close()` waits for pending auth emails, then closes MongoDB and PostgreSQL |
| your own router and `r.Run` | `core.Server()` and `core.Run()` (or mount `core.Server()`, an `http.Handler`, anywhere) |

## Environment

| 0.1 | 0.2 |
| --- | --- |
| `env.LoadEnv() *Env` | `env.Load() (*Env, error)`; `env.LoadFile(path)` for another file |
| `e.URL` (`URL`, default `http://localhost`) | `e.AppURL` (`APP_URL`, falls back to `URL`, default `http://localhost:` + port) |
| `e.PORT` | `e.Port` |
| `e.MONGODB_CONNECTION_URL *string` | `e.MongoURL string` (`""` when unset) |
| `e.POSTGRES_CONNECTION_URL *string` (never filled) | `e.PostgresURL string` |
| `e.MANUAL map[string]string` | `e.Get`, `GetOrDefault`, `Int`, `Bool`, `Duration`, `Strings` |
| `.env` values copied into the process environment | not copied: `os.Getenv` does not see them, read them through `Env` |
| missing `.env` logged as a warning | missing `.env` is silent; a malformed one is an error |

Before:

```go
e := env.LoadEnv()
if e.MONGODB_CONNECTION_URL != nil {
	fmt.Println(*e.MONGODB_CONNECTION_URL)
}
apiKey := os.Getenv("API_KEY") // worked because LoadEnv set it
```

After:

```go
e, err := env.Load()
if err != nil {
	log.Fatal(err)
}
if e.MongoURL != "" {
	fmt.Println(e.MongoURL)
}
apiKey := e.GetOrDefault("API_KEY", "")
```

With the facade the snapshot is `core.Env`. All variables CoreGo reads are
listed in [environment.md](environment.md).

## MongoDB

| 0.1 | 0.2 |
| --- | --- |
| `database.NewMongoDB(&cfg)` | `database.NewMongoDB(cfg)` (by value, never modified) |
| `m.GetClient()` | `m.Client()` |
| `m.Disconnect()` | `m.Close()` |
| `m.InsertOne(coll, doc) (string, error)` (hex ID) | `m.InsertOne(ctx, coll, doc) (any, error)` (the `_id` as the driver reports it, usually `bson.ObjectID`) |
| `m.Find(coll, filter) ([]map[string]any, error)` | `m.Find(ctx, coll, filter, &out, opts...) error` (decodes into your slice) |
| `m.FindOne(coll, filter, &out)` | `m.FindOne(ctx, coll, filter, &out)`; nothing found matches `database.ErrNotFound` |
| `m.UpdateOne(coll, filter, update)` (nil when nothing matched) | `m.UpdateOne(ctx, coll, filter, update)`; `database.ErrNotFound` when nothing matched |
| `m.UpdateMany(coll, filter, update) error` | `m.UpdateMany(ctx, coll, filter, update) (int64, error)` (matched count) |
| `m.DeleteOne(coll, filter)` (nil when nothing matched) | `m.DeleteOne(ctx, coll, filter)`; `database.ErrNotFound` when nothing was deleted |
| `m.DeleteMany(coll, filter) error` | `m.DeleteMany(ctx, coll, filter) (int64, error)` (deleted count) |
| `m.Collection(name)` | unchanged |
| hidden 5 s timeout per call | the caller's context; no hidden timeout |
| database: `MongoConfig.Database`, else `corego` | `MongoConfig.Database` (facade: `MONGODB_DATABASE`), else the database in the URL path, else `corego` |

> **Check your database name.** 0.1 ignored the database in the
> connection URL: with `mongodb://host/myapp` your data went to the
> `corego` database. 0.2 uses `myapp` in that case. Set
> `MONGODB_DATABASE=corego` (or `MongoConfig.Database`) to keep using the
> old database.

Before:

```go
id, err := core.Mongo.InsertOne("tasks", task) // id is a hex string
docs, err := core.Mongo.Find("tasks", bson.M{"done": false})
for _, doc := range docs {
	fmt.Println(doc["title"])
}
```

After:

```go
id, err := core.Mongo.InsertOne(ctx, "tasks", task)
if err != nil {
	return err
}
taskID := id.(bson.ObjectID) // .Hex() for the old string form

var tasks []Task
if err := core.Mongo.Find(ctx, "tasks", bson.M{"done": false}, &tasks); err != nil {
	return err
}
for _, t := range tasks {
	fmt.Println(t.Title, taskID.Hex())
}
```

Inside a handler, pass `c` (a `*web.Context` is a `context.Context`) as
`ctx`. More in [database.md](database.md).

## PostgreSQL

The PostgreSQL wrapper was added on `main` after 0.1.0 and never tagged;
if you used it:

| main after 0.1.0 | 0.2 |
| --- | --- |
| `database.NewPostgresDB(&cfg)` | `database.NewPostgresDB(cfg)` |
| `PostgresConfig.Database` | removed: the database is part of the URL |
| `p.GetPool()` | `p.Pool()` |
| `p.Disconnect()` | `p.Close()` |
| `p.Query(sql, args...) ([]map[string]any, error)` | `p.QueryMaps(ctx, sql, args...)`; or typed: `database.QueryAll[T](ctx, p, sql, args...)` |
| `p.Exec(sql, args...)` | `p.Exec(ctx, sql, args...)` |
| (none) | `p.Query(ctx, ...)` returning `pgx.Rows`, `QueryOne[T]`, `WithTx`, `Ping` |

Before:

```go
rows, err := core.Postgres.Query("SELECT id, title FROM tasks WHERE done = $1", false)
```

After:

```go
rows, err := core.Postgres.QueryMaps(ctx, "SELECT id, title FROM tasks WHERE done = $1", false)

// or straight into a struct with `db` tags:
tasks, err := database.QueryAll[Task](ctx, core.Postgres,
	"SELECT id, title FROM tasks WHERE done = $1", false)
```

## Authentication

0.1 issued a signed JWT and trusted it until it expired. 0.2 issues an
opaque random token, stores only its SHA-256 hash and checks every
request against the store, so sign-out, password changes and bans take
effect immediately. There is no signing secret any more: delete
`AUTH_SECRET`.

| 0.1 | 0.2 |
| --- | --- |
| `auth.New(&auth.Config{Secret, TokenExpiry, DatabaseName}, mongo)` | `auth.New(auth.Config{...}, store)` (the facade does it for you) |
| `auth.Config.TokenExpiry` (minutes) | `auth.Config.SessionTTL` / `RememberMeTTL` (`time.Duration`) |
| `auth.Config.Secret` | not needed |
| `auth.Config.DatabaseName` | store prefix ([auth-storage.md](auth-storage.md)) |
| `m.Signup(auth.SignupRequest{Email, Password, Custom})` | `a.SignUp(ctx, auth.SignUpInput{Name, Email, Password, Image, RememberMe})` |
| `m.Login(auth.LoginRequest{Email, Password})` | `a.SignIn(ctx, auth.SignInInput{Email, Password, RememberMe})` |
| `(user, token, err)` results | `*auth.SignUpOutput` / `*auth.SignInOutput` with `User`, `SessionToken`, `Session` |
| `m.ValidateToken(token) (userID, error)` | `a.GetSession(ctx, token) (*auth.Session, *auth.User, error)` |
| `m.GenerateToken(userID)` | removed: sessions are created by sign-up and sign-in |
| `m.GetUserByID(id)` | `a.GetUser(ctx, id)` |
| `m.GetUserByEmail(email)` | removed from the service (store method `GetUserByEmail`) |
| `m.UpdateProfile(id, auth.UpdateProfileRequest{Custom})` | `a.UpdateUser(ctx, id, auth.UpdateUserInput{Name, Image})` |
| `m.ChangePassword(id, auth.ChangePasswordRequest{OldPassword, NewPassword})` | `a.ChangePassword(ctx, id, currentPassword, newPassword)` (also signs out every session) |
| `m.DeleteAccount(id)` | `a.DeleteUser(ctx, id)`; confirm with `a.CheckPassword` for self-service |
| `auth.HashPassword`, `auth.VerifyPassword` | unchanged |
| `User.Password` | moved to `auth.Account.Password` (the credential account) |
| `User.Custom map[string]any` | removed: keep application data in your own table or collection keyed by `User.ID` |
| errors as plain strings | sentinel errors (`auth.ErrInvalidCredentials`, `auth.ErrUserAlreadyExists`, ...) compared with `errors.Is` |

Before:

```go
user, token, err := core.Auth.Signup(auth.SignupRequest{
	Email:    "ada@example.com",
	Password: "analytical-engine",
	Custom:   map[string]any{"name": "Ada"},
})
userID, err := core.Auth.ValidateToken(token)
```

After:

```go
out, err := core.Auth.SignUp(ctx, auth.SignUpInput{
	Name:     "Ada",
	Email:    "ada@example.com",
	Password: "analytical-engine",
})
if errors.Is(err, auth.ErrUserAlreadyExists) {
	return web.Conflict("email already registered")
}
if err != nil {
	return err
}
session, user, err := core.Auth.GetSession(ctx, out.SessionToken)
```

Most applications never call these directly: the HTTP routes below do.
The service is documented in [authentication.md](authentication.md).

## HTTP routes and the Gin integration

0.1 shipped Gin handlers that you mounted on paths of your choice. 0.2
mounts a fixed set of JSON routes under `/auth` (configurable with
`AuthOptions.HTTP.BasePath`) on its own web server. The closest
equivalents of the 0.1 handlers:

| 0.1 handler (usual path) | 0.2 route |
| --- | --- |
| `SignupHandler()` (`POST /auth/signup`) | `POST /auth/signup` with `{name, email, password}` → `201` |
| `LoginHandler()` (`POST /auth/login`) | `POST /auth/signin` with `{email, password}` → `200` |
| `GetProfileHandler()` (`GET /api/profile`) | `GET /auth/me` → `200 {user}` |
| `UpdateProfileHandler()` (`PATCH /api/profile`) | `PATCH /auth/me` with `{name?, image?}` → `200 {user}` |
| `ChangePasswordHandler()` (`POST /api/change-password`) | `POST /auth/change-password` with `{current_password, new_password}` → `204` |
| `DeleteAccountHandler()` (`DELETE /api/account`) | `DELETE /auth/me` with `{password}` → `204` |
| `Middleware()` | `core.RequireAuth()` |
| `c.Get("userID")` | `auth.MustUser(c).ID` |

New routes have no 0.1 counterpart: sign-out, session listing and
revocation, email verification, password reset and email change. The
full table is in [http-api.md](http-api.md).

Before (Gin):

```go
api := r.Group("/api")
api.Use(core.Auth.Middleware())
api.GET("/dashboard", func(c *gin.Context) {
	userID, _ := c.Get("userID")
	c.JSON(200, gin.H{"user_id": userID})
})
```

After:

```go
api := core.Server().Group("/api", core.RequireAuth())
api.Get("/dashboard", func(c *web.Context) error {
	user := auth.MustUser(c)
	return c.JSON(http.StatusOK, map[string]string{"user_id": user.ID, "name": user.Name})
})
```

Writing handlers for the CoreGo web framework is described in
[web-framework.md](web-framework.md); the differences from Gin that
matter most: handlers return an `error`, response writers return an error
(`return c.JSON(...)`), and `c` is a `context.Context`.

### Keeping Gin

If you want to keep a Gin application, mount CoreGo's server for the auth
routes and protect Gin routes with the auth middleware, which is plain
`net/http`:

```go
// requireSession adapts the auth middleware (net/http) to Gin. Failed
// checks are answered by the middleware itself (401/403 JSON), exactly
// like core.RequireAuth.
func requireSession(h *authhttp.Handlers) gin.HandlerFunc {
	return func(c *gin.Context) {
		passed := false
		h.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			passed = true
			c.Request = r // carries the user for auth.MustUser
		})).ServeHTTP(c.Writer, c.Request)
		if !passed {
			c.Abort()
			return
		}
		c.Next()
	}
}

func main() {
	core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
	if err != nil {
		log.Fatal(err)
	}
	defer core.Close()

	r := gin.Default()
	r.Any("/auth/*path", gin.WrapH(core.Server())) // the 17 auth routes

	api := r.Group("/api", requireSession(core.AuthHandlers()))
	api.GET("/dashboard", func(c *gin.Context) {
		user := auth.MustUser(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"user_id": user.ID})
	})

	if err := r.Run(":" + core.Env.Port); err != nil {
		log.Print(err)
	}
}
```

With Gin you handle graceful shutdown yourself; `core.Close()` must run
after the server has stopped so pending auth emails are delivered.

## Migrating existing users

0.1 kept users in a MongoDB collection (`users` by default, in the
`corego` database unless configured) with an ObjectID `_id`, the email,
a bcrypt password hash and the `custom` map. The bcrypt hashes are
compatible with 0.2, so users keep their passwords. This one-off program
copies them into the MongoDB auth store, keeping the hex form of the old
ObjectID as the user ID so references in your own collections stay valid.
It is safe to run more than once.

```go
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/auth/mongostore"
	"github.com/berkkaradalan/CoreGo/database"
)

// legacyUser is a document of the 0.1 users collection.
type legacyUser struct {
	ID        bson.ObjectID  `bson:"_id"`
	Email     string         `bson:"email"`
	Password  string         `bson:"password"` // bcrypt hash, reusable as is
	Custom    map[string]any `bson:"custom"`
	CreatedAt time.Time      `bson:"created_at"`
}

func main() {
	ctx := context.Background()
	db, err := database.NewMongoDB(database.MongoConfig{
		URL:      os.Getenv("MONGODB_CONNECTION_URL"),
		Database: "corego", // where 0.1 stored its data unless configured otherwise
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store, err := mongostore.NewStore(db, mongostore.Options{})
	if err != nil {
		log.Fatal(err)
	}
	if err := store.EnsureIndexes(ctx); err != nil {
		log.Fatal(err)
	}

	var legacy []legacyUser
	if err := db.Find(ctx, "users", nil, &legacy); err != nil { // 0.1 Config.DatabaseName
		log.Fatal(err)
	}

	for _, old := range legacy {
		email := strings.ToLower(strings.TrimSpace(old.Email))
		name, _ := old.Custom["name"].(string)
		if name == "" {
			name, _, _ = strings.Cut(email, "@")
		}

		user := &auth.User{ID: old.ID.Hex(), Name: name, Email: email, CreatedAt: old.CreatedAt}
		err := store.CreateUser(ctx, user)
		if errors.Is(err, auth.ErrDuplicate) {
			existing, getErr := store.GetUserByEmail(ctx, email)
			if getErr != nil {
				log.Fatal(getErr)
			}
			if existing.ID != user.ID {
				log.Printf("skipping %s: registered again after the upgrade", user.ID)
				continue
			}
		} else if err != nil {
			log.Fatal(err)
		}

		account := &auth.Account{
			UserID:            user.ID,
			Provider:          auth.CredentialProvider,
			ProviderAccountID: email,
			Password:          old.Password,
		}
		if err := store.CreateAccount(ctx, account); err != nil && !errors.Is(err, auth.ErrDuplicate) {
			log.Fatal(err)
		}
	}
	log.Printf("migrated %d users", len(legacy))
}
```

Notes:

- Users arrive with `EmailVerified` false (0.1 had no verification). If
  you enable `RequireVerifiedEmail`, they have to verify their address
  first (`POST /auth/verify-email/request`).
- For a PostgreSQL auth store, use `pgstore.NewStore(pool,
  pgstore.Options{})` and `Migrate` instead; the rest is identical.
- Move whatever you kept in `custom` into your own collection, keyed by
  the same user ID.
- The old `users` collection is left untouched; drop it once you are
  satisfied.

## What API clients notice

- Tokens: same `Authorization: Bearer <token>` header, but 0.1 JWTs are
  rejected; users sign in once more. Tokens are opaque: do not decode
  them.
- Sign-in is `POST /auth/signin` (not `/login`) and returns
  `{user, token, expires_at}`; sign-up also returns `expires_at` and
  `verification_required`, and needs a `name`.
- `user` objects are `{id, name, email, email_verified, image,
  created_at, updated_at}`; there is no `custom` field.
- Errors are `{"error": "<message>"}` with documented status codes and
  messages ([http-api.md](http-api.md#errors)), for example `401 Invalid
  email or password.`
- Changing the password uses `current_password` (not `old_password`) and
  signs out every session; deleting the account requires the password.
- Sign-out exists (`POST /auth/signout`) and takes effect immediately.
- Sign-up, sign-in and the other sensitive routes are rate limited (10
  requests per minute per route and client IP by default, sign-in also per
  email address) and answer `429` with `Retry-After` when exceeded.
- Names are limited to 256 characters, and `image` must be an absolute
  `http(s)` URL (`400` otherwise).
