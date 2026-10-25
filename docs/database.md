# Database

The `database` package connects your application to **MongoDB** (official
driver v2) and **PostgreSQL** (pgx v5) and adds a small set of typed,
context-aware helpers on top. You can use it through the `corego` facade or
on its own.

- [Connect](#connect)
- [MongoDB](#mongodb)
- [PostgreSQL](#postgresql)
- [Typed queries: QueryAll and QueryOne](#typed-queries-queryall-and-queryone)
- [Transactions](#transactions)
- [Handling "not found"](#handling-not-found)
- [Escape hatches: the raw drivers](#escape-hatches-the-raw-drivers)
- [Reference](#reference)

## Connect

### With the facade

Set a connection URL and the facade connects for you when the app starts:

```bash
# .env (or the process environment)
MONGODB_CONNECTION_URL=mongodb://localhost:27017/myapp
POSTGRES_CONNECTION_URL=postgres://user:password@localhost:5432/myapp
```

```go
core, err := corego.New(nil)
if err != nil {
	log.Fatal(err)
}
defer core.Close() // closes both connections

mongo := core.Mongo // *database.MongoDB, nil when MongoDB is not configured
pg := core.Postgres  // *database.PostgresDB, nil when PostgreSQL is not configured
```

`MONGODB_DATABASE` optionally names the MongoDB database (see
[Choosing the MongoDB database](#choosing-the-mongodb-database)). To configure
in code instead of the environment, set `Config.Mongo` / `Config.Postgres`;
an explicit config always wins over the environment:

```go
core, err := corego.New(&corego.Config{
	Mongo:    &database.MongoConfig{URL: "mongodb://localhost:27017", Database: "myapp"},
	Postgres: &database.PostgresConfig{URL: "postgres://user:password@localhost:5432/myapp"},
})
```

### Standalone

```go
mongo, err := database.NewMongoDB(database.MongoConfig{URL: "mongodb://localhost:27017/myapp"})
if err != nil {
	log.Fatal(err)
}
defer mongo.Close()

pg, err := database.NewPostgresDB(database.PostgresConfig{URL: "postgres://user:password@localhost:5432/myapp"})
if err != nil {
	log.Fatal(err)
}
defer pg.Close()
```

Both constructors:

- reject an empty URL with an error;
- connect and **ping** the server within a 10 second budget, so a returned
  handle is known to work (an unreachable server fails at startup, not on the
  first request);
- release the client / pool again when the ping fails, so nothing leaks;
- never include your password in an error message.

`Close` is safe to call more than once and on a nil handle. `MongoDB.Close`
waits at most 10 seconds for in-flight operations.

### Choosing the MongoDB database

The MongoDB helpers all work on one database, resolved in this order:

1. `MongoConfig.Database` (with the facade: `MONGODB_DATABASE`);
2. the database in the URL path, e.g. `myapp` in
   `mongodb+srv://user:pass@cluster0.example.net/myapp?retryWrites=true`;
3. `"corego"` (`database.DefaultMongoDatabase`).

```go
mongo.Name() // "myapp"
```

Names MongoDB would reject (empty, longer than 63 bytes, or containing
`/ \ . " $`, a space or NUL) are reported by `NewMongoDB` instead of failing
on the first query.

### Contexts

Every helper takes a `context.Context` as its first argument and there is no
hidden per-operation timeout: cancellation and deadlines reach the server.
Inside a CoreGo handler the `*web.Context` **is** a `context.Context`, so pass
`c` straight through:

```go
app.Get("/tasks", func(c *web.Context) error {
	var tasks []Task
	if err := core.Mongo.Find(c, "tasks", bson.M{"done": false}, &tasks); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, tasks)
})
```

## MongoDB

Documents are your own structs with `bson` tags (or `bson.M` / `bson.D` /
maps). The examples below use this type:

```go
import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Task struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Title     string        `bson:"title"         json:"title"`
	Done      bool          `bson:"done"          json:"done"`
	OwnerID   string        `bson:"owner_id"      json:"owner_id"`
	CreatedAt time.Time     `bson:"created_at"    json:"created_at"`
}
```

### Insert

```go
id, err := core.Mongo.InsertOne(ctx, "tasks", Task{
	Title:     "ship the docs",
	OwnerID:   userID,
	CreatedAt: time.Now(),
})
taskID := id.(bson.ObjectID) // the driver generated an ObjectID for the empty _id
```

`InsertOne` returns the inserted `_id` exactly as the driver reports it: a
`bson.ObjectID` when the driver generated one, or your own `_id` value
(for example a string) when the document carried one.

### Find one

```go
var task Task
err := core.Mongo.FindOne(ctx, "tasks", bson.M{"_id": taskID}, &task)
if errors.Is(err, database.ErrNotFound) {
	return web.NotFound("task not found")
}
if err != nil {
	return err
}
```

### Find many

`Find` decodes into a pointer to a slice. Sorting, paging and projections are
ordinary driver options:

```go
var tasks []Task
err := core.Mongo.Find(ctx, "tasks",
	bson.M{"owner_id": userID, "done": false},
	&tasks,
	options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(20).
		SetLimit(20),
)
```

When nothing matches, `tasks` is an **empty, non-nil** slice, so it encodes
as `[]` (not `null`) in JSON responses. Anything already in the slice is
replaced.

### Count

```go
open, err := core.Mongo.Count(ctx, "tasks", bson.M{"owner_id": userID, "done": false})
all, err := core.Mongo.Count(ctx, "tasks", nil) // nil filter = every document
```

### Update

```go
// Exactly one document; ErrNotFound when nothing matched.
err := core.Mongo.UpdateOne(ctx, "tasks",
	bson.M{"_id": taskID, "owner_id": userID},
	bson.M{"$set": bson.M{"done": true}})

// Every match; returns how many documents matched.
n, err := core.Mongo.UpdateMany(ctx, "tasks",
	bson.M{"owner_id": userID},
	bson.M{"$set": bson.M{"done": true}})
```

`UpdateOne` succeeds when a document matched, even if the update left it
unchanged. The update must use operators such as `$set`; to replace a whole
document use the driver's `ReplaceOne` (see [escape hatches](#escape-hatches-the-raw-drivers)).

### Delete

```go
// Exactly one document; ErrNotFound when nothing was deleted.
err := core.Mongo.DeleteOne(ctx, "tasks", bson.M{"_id": taskID, "owner_id": userID})

// Every match; returns how many were deleted.
n, err := core.Mongo.DeleteMany(ctx, "tasks", bson.M{"done": true})
```

### nil filters

For the **read** helpers (`FindOne`, `Find`, `Count`) a `nil` filter matches
every document. The **write** helpers (`UpdateOne`, `UpdateMany`,
`DeleteOne`, `DeleteMany`) reject a `nil` filter with the driver's
`mongo.ErrNilDocument`, so a forgotten filter can never modify or delete a
whole collection. Pass `bson.M{}` when you really mean "all documents".

## PostgreSQL

Always pass values as arguments (`$1`, `$2`, ...); never format them into
the SQL string.

### Exec

`Exec` runs statements that return no rows and reports the affected row
count:

```go
_, err := core.Postgres.Exec(ctx, `CREATE TABLE IF NOT EXISTS tasks (
	id         BIGSERIAL PRIMARY KEY,
	title      TEXT NOT NULL,
	done       BOOLEAN NOT NULL DEFAULT FALSE,
	owner_id   TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`)

n, err := core.Postgres.Exec(ctx,
	`UPDATE tasks SET done = TRUE WHERE id = $1 AND owner_id = $2`, taskID, userID)
if err != nil {
	return err
}
if n == 0 {
	return web.NotFound("task not found")
}
```

### Rows

For reading rows use the typed [QueryAll / QueryOne](#typed-queries-queryall-and-queryone)
functions. Two lower-level methods are also available:

```go
// Maps keyed by column name, for ad-hoc queries. Empty result -> empty non-nil slice.
rows, err := core.Postgres.QueryMaps(ctx, `SELECT id, title FROM tasks WHERE done = $1`, false)
fmt.Println(rows[0]["title"])

// Raw pgx.Rows; you must Close them (or consume them with a pgx collect function).
raw, err := core.Postgres.Query(ctx, `SELECT title FROM tasks`)
if err != nil {
	return err
}
titles, err := pgx.CollectRows(raw, pgx.RowTo[string])
```

## Typed queries: QueryAll and QueryOne

`QueryAll[T]` and `QueryOne[T]` scan rows straight into your own types:

```go
type Task struct {
	ID        int64     `db:"id"         json:"id"`
	Title     string    `db:"title"      json:"title"`
	Done      bool      `db:"done"       json:"done"`
	OwnerID   string    `db:"owner_id"   json:"owner_id"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

tasks, err := database.QueryAll[Task](ctx, core.Postgres,
	`SELECT id, title, done, owner_id, created_at
	   FROM tasks WHERE owner_id = $1 ORDER BY created_at DESC`, userID)

task, err := database.QueryOne[Task](ctx, core.Postgres,
	`SELECT id, title, done, owner_id, created_at FROM tasks WHERE id = $1`, taskID)
if errors.Is(err, database.ErrNotFound) {
	return web.NotFound("task not found")
}

// INSERT ... RETURNING gives you generated values in the same round trip.
id, err := database.QueryOne[int64](ctx, core.Postgres,
	`INSERT INTO tasks (title, owner_id) VALUES ($1, $2) RETURNING id`, "ship the docs", userID)

// Single-column results scan into plain types.
count, err := database.QueryOne[int64](ctx, core.Postgres, `SELECT count(*) FROM tasks`)
titles, err := database.QueryAll[string](ctx, core.Postgres, `SELECT title FROM tasks`)
```

How rows map to `T`:

- **Struct `T`**: each column fills the exported field whose `db` tag equals
  the column name; fields without a tag match by name case-insensitively,
  ignoring underscores (`created_at` fills `CreatedAt`). Every selected column
  must map to a field and every field must be selected; tag a field
  `db:"-"` to leave it out. Embedded structs are flattened. These are pgx's
  `RowToStructByName` rules.
- **Any other `T`** (`int64`, `string`, `bool`, `[]byte`, ...) and types the
  driver scans as one value (`time.Time`, `sql.NullString`, `pgtype.Numeric`
  and other `sql.Scanner` implementations): the query must return exactly
  one column.
- `QueryAll` returns an **empty, non-nil** slice when there are no rows.
- `QueryOne` returns the first row (add `LIMIT 1` or a unique condition to
  make that explicit) and an error matching `database.ErrNotFound` when
  there is none.

The second argument is a `database.Querier`: anything with pgx's `Query`
method. `*database.PostgresDB`, `*pgxpool.Pool`, `pgx.Tx` and `*pgx.Conn` all
qualify, so the same calls work inside a transaction.

### Time zones of TIMESTAMPTZ values

A `TIMESTAMPTZ` column scans into a `time.Time` that holds the right
instant but is expressed in the **time zone of your Go process**
(`time.Local`, set by the `TZ` variable or the system), not in UTC. A
server at UTC and an application running on a laptop in Berlin give
`2026-01-02 04:04:05 +0100 CET` for a value stored as
`2026-01-02 03:04:05+00`. Instants compare correctly with `Equal`,
`Before` and `After`, but `==`, the JSON encoding (`"...+01:00"`) and
formatting differ from machine to machine.

Convert explicitly when you need UTC, for example before returning the
value in an API response:

```go
task.CreatedAt = task.CreatedAt.UTC()
```

or run the process with `TZ=UTC`. The PostgreSQL session setting does
**not** help here: `?timezone=UTC` in the connection URL changes how the
server formats timestamps as text (`now()::text`, `SHOW TimeZone`), but
pgx receives `TIMESTAMPTZ` values in binary form and always decodes them
in `time.Local`. (Verified with pgx v5.7 against PostgreSQL 17.)

The auth stores are not affected: `auth/pgstore` and `auth/mongostore`
normalize every timestamp of users, sessions and tokens to UTC.

### Connection URL parameters

pgx passes parameters it does not handle itself (everything except
options such as `sslmode`, `pool_max_conns` or `connect_timeout`) to the
server as session settings when each connection starts. For example
`?search_path=myapp` makes `myapp` the default schema of every pooled
connection, and `?application_name=api` labels the connections in
`pg_stat_activity`:

```bash
POSTGRES_CONNECTION_URL=postgres://app:secret@localhost:5432/app?search_path=myapp&sslmode=disable
```

[auth-storage.md](auth-storage.md#a-dedicated-schema) shows how to keep
the auth tables in their own schema this way.

## Transactions

`WithTx` runs a function inside a transaction. Return `nil` to commit; return
an error to roll back. A panic also rolls back and is then re-raised.

```go
err := core.Postgres.WithTx(ctx, func(tx pgx.Tx) error {
	balance, err := database.QueryOne[int64](ctx, tx,
		`SELECT balance FROM accounts WHERE id = $1 FOR UPDATE`, fromID)
	if err != nil {
		return err
	}
	if balance < amount {
		return ErrInsufficientFunds // rolled back; WithTx returns this error unchanged
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET balance = balance - $1 WHERE id = $2`, amount, fromID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE accounts SET balance = balance + $1 WHERE id = $2`, amount, toID)
	return err
})
if errors.Is(err, ErrInsufficientFunds) {
	return web.Conflict("insufficient funds")
}
```

Things to know:

- Run every statement of the transaction on `tx`, not on `core.Postgres`;
  statements on the pool run outside the transaction.
- The error your function returns comes back unchanged, so `errors.Is` /
  `errors.As` work. If the rollback itself fails, both errors are joined.
- A failed statement aborts the transaction in PostgreSQL. If you ignore its
  error and return `nil`, the commit fails and `WithTx` returns that error.
- The rollback runs even when `ctx` has already been canceled, so the
  connection always goes back to the pool clean.

## Handling "not found"

These calls report "nothing matched" with an error that matches
`database.ErrNotFound`:

| Call | When |
| --- | --- |
| `MongoDB.FindOne` | no document matches |
| `MongoDB.UpdateOne` | no document matches |
| `MongoDB.DeleteOne` | nothing was deleted |
| `QueryOne` | the query returned no row |

```go
if errors.Is(err, database.ErrNotFound) {
	return web.NotFound("task not found")
}
```

The same errors also match the driver sentinels, so existing code keeps
working: `mongo.ErrNoDocuments` for MongoDB, and `pgx.ErrNoRows` (and
`sql.ErrNoRows`) for `QueryOne`.

Calls that work on many rows never report "not found": `Find` and `QueryAll`
return an empty slice, `Count` returns 0, and `UpdateMany`, `DeleteMany` and
`Exec` return a count of 0.

## Escape hatches: the raw drivers

The helpers cover everyday CRUD. Everything else is one call away on the
official drivers:

```go
// MongoDB
core.Mongo.Collection("tasks") // *mongo.Collection: Aggregate, ReplaceOne, upserts, BulkWrite, Indexes()...
core.Mongo.Database()          // *mongo.Database: RunCommand, ListCollectionNames, ...
core.Mongo.Client()            // *mongo.Client: sessions and multi-document transactions

// PostgreSQL
core.Postgres.Pool()           // *pgxpool.Pool: SendBatch, CopyFrom, Acquire (LISTEN/NOTIFY), ...
```

For example, an upsert and an aggregation:

```go
_, err := core.Mongo.Collection("settings").UpdateOne(ctx,
	bson.M{"user_id": userID},
	bson.M{"$set": bson.M{"theme": "dark"}},
	options.UpdateOne().SetUpsert(true))

cursor, err := core.Mongo.Collection("tasks").Aggregate(ctx, mongo.Pipeline{
	{{Key: "$match", Value: bson.M{"owner_id": userID}}},
	{{Key: "$group", Value: bson.M{"_id": "$done", "count": bson.M{"$sum": 1}}}},
})
```

The CoreGo auth stores keep their data in their own prefixed collections and
tables (`corego_users`, `corego_sessions`, ...) in the same database; read
them if you need to, but let the auth service write them.

## Reference

```go
var ErrNotFound error
const DefaultMongoDatabase = "corego"

type MongoConfig struct {
	URL      string // required
	Database string // optional; default: database in the URL path, else "corego"
}
type PostgresConfig struct {
	URL string // required
}

func NewMongoDB(cfg MongoConfig) (*MongoDB, error)
func (m *MongoDB) Client() *mongo.Client
func (m *MongoDB) Database() *mongo.Database
func (m *MongoDB) Name() string
func (m *MongoDB) Collection(name string) *mongo.Collection
func (m *MongoDB) Ping(ctx context.Context) error
func (m *MongoDB) Close() error
func (m *MongoDB) InsertOne(ctx context.Context, collection string, document any) (any, error)
func (m *MongoDB) FindOne(ctx context.Context, collection string, filter any, out any) error
func (m *MongoDB) Find(ctx context.Context, collection string, filter any, out any, opts ...options.Lister[options.FindOptions]) error
func (m *MongoDB) Count(ctx context.Context, collection string, filter any) (int64, error)
func (m *MongoDB) UpdateOne(ctx context.Context, collection string, filter, update any) error
func (m *MongoDB) UpdateMany(ctx context.Context, collection string, filter, update any) (int64, error)
func (m *MongoDB) DeleteOne(ctx context.Context, collection string, filter any) error
func (m *MongoDB) DeleteMany(ctx context.Context, collection string, filter any) (int64, error)

func NewPostgresDB(cfg PostgresConfig) (*PostgresDB, error)
func (p *PostgresDB) Pool() *pgxpool.Pool
func (p *PostgresDB) Ping(ctx context.Context) error
func (p *PostgresDB) Close() error
func (p *PostgresDB) Exec(ctx context.Context, sql string, args ...any) (int64, error)
func (p *PostgresDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
func (p *PostgresDB) QueryMaps(ctx context.Context, sql string, args ...any) ([]map[string]any, error)
func (p *PostgresDB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
func QueryAll[T any](ctx context.Context, q Querier, sql string, args ...any) ([]T, error)
func QueryOne[T any](ctx context.Context, q Querier, sql string, args ...any) (T, error)
```

The package works with the official MongoDB Go driver **v2**
(`go.mongodb.org/mongo-driver/v2/...`); import `bson`, `mongo` and
`options` from there.
