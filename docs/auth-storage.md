# Auth Storage

The auth service keeps four kinds of records: **users**, **accounts** (a
user's sign-in methods; the email and password account holds the bcrypt
hash), **sessions** and **verification tokens** (email verification and
password reset). Where they live is decided by an `auth.Store`. CoreGo
ships three:

| Store | Package | Use it for |
| --- | --- | --- |
| Memory | `auth.NewMemoryStore()` | Development, examples and tests. Everything is lost when the process exits, and each process has its own data. |
| MongoDB | `auth/mongostore` | Production on MongoDB (standalone, replica set or Atlas). Unique indexes, TTL indexes that expire old sessions and tokens on their own. |
| PostgreSQL | `auth/pgstore` | Production on PostgreSQL. Foreign keys with cascading deletes, versioned migrations. |

- [Choosing a store with the facade](#choosing-a-store-with-the-facade)
- [Memory store](#memory-store)
- [MongoDB store](#mongodb-store)
- [PostgreSQL store](#postgresql-store)
- [Writing your own store](#writing-your-own-store)

## Choosing a store with the facade

With `corego.New` you normally do not pick a store at all: configure a
database and the auth service uses it. The facade resolves the store in
this order:

1. `AuthOptions.Store`: `corego.StoreMemory` (`"memory"`),
   `corego.StoreMongo` (`"mongo"`) or `corego.StorePostgres`
   (`"postgres"`).
2. The `COREGO_AUTH_STORE` environment variable, with the same values.
3. The first connected database: MongoDB, then PostgreSQL.
4. The memory store, with a startup warning.

```go
// Automatic: whatever MONGODB_CONNECTION_URL / POSTGRES_CONNECTION_URL configure.
core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})

// Explicit, for example when both databases are connected:
core, err = corego.New(&corego.Config{Auth: &corego.AuthOptions{Store: corego.StorePostgres}})
```

Selecting `mongo` or `postgres` without that database connected, or an
unknown name, makes `corego.New` return an error. For a database store the
facade creates the MongoDB indexes or runs the PostgreSQL migrations
before `New` returns (within 30 seconds), with the `corego_` prefix unless
`AuthOptions.TablePrefix` or `COREGO_AUTH_TABLE_PREFIX` says otherwise.
The startup log line `corego: auth ready` names the store that was
selected (`memory`, `mongo`, `postgres` or `custom`) and, for a database
store, the effective prefix.

The auth tables or collections share the database with your own data, so
one connection serves both. The prefix keeps them apart: an existing
`users` or `sessions` table of your application is never touched.

### Another prefix

`AuthOptions.TablePrefix` replaces the default `corego_` prefix of the
store the facade creates: it becomes `pgstore.Options.TablePrefix` on
PostgreSQL and `mongostore.Options.CollectionPrefix` on MongoDB. When it
is empty, the `COREGO_AUTH_TABLE_PREFIX` environment variable is used, so
the prefix can also live in `.env`:

```dotenv
COREGO_AUTH_TABLE_PREFIX=myapp_auth_
```

```go
core, err := corego.New(&corego.Config{
	Auth: &corego.AuthOptions{TablePrefix: "myapp_auth_"}, // myapp_auth_users, myapp_auth_sessions, ...
})
if err != nil {
	log.Fatal(err)
}
defer core.Close()
```

Only lowercase ASCII letters, digits and underscores are allowed (at most
24 bytes on PostgreSQL, 64 characters on MongoDB); `corego.New` returns
the store's error for anything else, naming where the prefix came from
(`AuthOptions.TablePrefix` or `COREGO_AUTH_TABLE_PREFIX`). The memory
store ignores the prefix.

### A dedicated schema

On PostgreSQL the prefix is not the only way to keep the auth tables
apart: you can put them, prefixed or not, into a schema of their own.
The facade creates its tables in the first schema of the connection's
`search_path`, and pgx sets `search_path` (like any other server setting)
from a parameter of the connection URL:

```sql
-- once, as a user allowed to create schemas:
CREATE SCHEMA myapp;
```

```dotenv
POSTGRES_CONNECTION_URL=postgres://app:secret@localhost:5432/app?search_path=myapp
```

`corego.New` then migrates `myapp.corego_users`, `myapp.corego_sessions`
and the other auth tables, and every pooled connection uses `myapp` by
default, including `core.Postgres` for your own queries (qualify names,
for example `public.orders`, to reach other schemas, or list several
schemas: `search_path=myapp,public`; the first one receives the auth
tables).

**The schema must exist beforehand.** PostgreSQL accepts a
`search_path` that names a missing schema without complaint, so the
connection succeeds, but the migration then has nowhere to create its
tables and `corego.New` fails with

```text
corego: migrate PostgreSQL auth schema: pgstore: migrate: create schema_migrations table: ERROR: no schema has been selected to create in (SQLSTATE 3F000)
```

The facade never creates schemas itself. (Verified against PostgreSQL 17
with pgx v5.7; the facade's own integration tests run every PostgreSQL
test in a dedicated schema this way.)

### Your own store

`AuthOptions.CustomStore` hands the facade a ready `auth.Store`: your own
implementation, a wrapper around another store, or a CoreGo store built
with options or on a connection the facade does not manage. It takes
precedence over `Store`, `COREGO_AUTH_STORE`, the automatic selection and
`TablePrefix`. Everything else stays automatic: the mailer, the auth
routes, `core.RequireAuth()` and waiting for background emails in
`core.Close()`.

The facade uses the store as it is, so prepare it first: it neither
creates MongoDB indexes nor runs PostgreSQL migrations for a custom
store. For example, keeping the auth tables in a separate database:

```go
authDB, err := database.NewPostgresDB(database.PostgresConfig{URL: os.Getenv("AUTH_DATABASE_URL")})
if err != nil {
	log.Fatal(err)
}
defer authDB.Close() // the facade does not own this connection

store, err := pgstore.NewStore(authDB.Pool(), pgstore.Options{})
if err != nil {
	log.Fatal(err)
}
if err := store.Migrate(context.Background()); err != nil {
	log.Fatal(err)
}

core, err := corego.New(&corego.Config{
	Auth: &corego.AuthOptions{CustomStore: store},
})
if err != nil {
	log.Fatal(err)
}
defer core.Close()
```

To go further and build the `auth.Auth` service yourself, leave
`Config.Auth` nil and mount `authhttp.New(svc, cfg)` on `core.Server()`
([http-api.md](http-api.md#mount-in-3-lines)). The facade then knows
nothing about that service: call `svc.Shutdown` yourself after `core.Run`
returns, and configure `auth.Config.Mailer` yourself
([mail.md](mail.md#using-mail-with-auth)).

## Memory store

```go
store := auth.NewMemoryStore()
svc, err := auth.New(auth.Config{}, store)
```

`MemoryStore` implements the complete `Store` contract, including the
unique constraints, and is safe for concurrent use. Records are deep
copies, so mutating a returned `*auth.User` never changes the store. It is
what the facade falls back to without a database, and the natural choice
for tests ([testing.md](testing.md#testing-your-application)).

Do not run it in production: data does not survive a restart, and with
several instances each one has its own users and sessions.

## MongoDB store

Package `github.com/berkkaradalan/CoreGo/auth/mongostore` stores users,
accounts, sessions and verification tokens in MongoDB (driver
`go.mongodb.org/mongo-driver/v2`). It works on standalone servers,
replica sets and Atlas: it never needs transactions.

### Via the corego facade

When MongoDB is configured, the facade picks this store automatically,
creates its indexes at startup and wires it into the auth service:

```go
core, err := corego.New(&corego.Config{
	Mongo: &database.MongoConfig{URL: "mongodb://localhost:27017/myapp"},
	Auth:  &corego.AuthOptions{},
})
if err != nil {
	log.Fatal(err)
}
defer core.Close()
```

Leaving `Mongo` nil works too: the facade then reads
`MONGODB_CONNECTION_URL` (and `MONGODB_DATABASE`) from the environment.
If MongoDB is connected, it is chosen automatically, with a higher
priority than PostgreSQL. To choose it explicitly (for example when both
databases are connected), set `AuthOptions.Store: corego.StoreMongo` or
the environment variable:

```bash
COREGO_AUTH_STORE=mongo
```

### Standalone

```go
db, err := database.NewMongoDB(database.MongoConfig{URL: "mongodb://localhost:27017/myapp"})
if err != nil {
	log.Fatal(err)
}
defer db.Close()

store, err := mongostore.NewStore(db, mongostore.Options{})
if err != nil {
	log.Fatal(err)
}
if err := store.EnsureIndexes(context.Background()); err != nil {
	log.Fatal(err)
}

svc, err := auth.New(auth.Config{}, store)
if err != nil {
	log.Fatal(err)
}
```

`NewStore` returns an error for a nil `*database.MongoDB` or an invalid
prefix. It does not contact the server. The store uses the database that
`db` is bound to: `MongoConfig.Database`, else the database in the URL,
else `corego`.

### Collection prefix

All four collections share a prefix, which defaults to `corego_`, so they
never collide with your application's own collections:

```go
store, err := mongostore.NewStore(db, mongostore.Options{CollectionPrefix: "billing_auth_"})
// collections: billing_auth_users, billing_auth_accounts,
//              billing_auth_sessions, billing_auth_verification_tokens
```

| Rule | Value |
| --- | --- |
| Default (empty `CollectionPrefix`) | `corego_` (`mongostore.DefaultCollectionPrefix`) |
| Allowed characters | lowercase `a-z`, digits `0-9`, `_` |
| Maximum length | 64 characters |

Stores with different prefixes are fully isolated, even in the same
database. Each one has its own users, sessions and unique constraints.
That makes prefixes useful for multi-tenant setups and parallel tests.
Changing the prefix of an existing deployment points the store at new,
empty collections. It does not rename the old ones.

### Indexes

Call `EnsureIndexes(ctx)` once at startup. The facade does this for you.
It is idempotent, so running it again, or from several app instances at
the same time, is safe. It uses only the caller's context with no extra
timeout, because building an index over a large collection can take a
while.

| Collection | Index name | Keys | Kind |
| --- | --- | --- | --- |
| `{prefix}users` | `ux_users_email` | `email` | unique |
| `{prefix}accounts` | `ux_accounts_provider_provider_account_id` | `provider`, `provider_account_id` | unique |
| `{prefix}accounts` | `ux_accounts_user_id_provider` | `user_id`, `provider` | unique |
| `{prefix}sessions` | `ux_sessions_token_hash` | `token_hash` | unique |
| `{prefix}sessions` | `ix_sessions_user_id_created_at` | `user_id`, `created_at` (descending) | regular |
| `{prefix}sessions` | `ttl_sessions_expires_at` | `expires_at` | TTL, `expireAfterSeconds: 0` |
| `{prefix}verification_tokens` | `ux_verification_tokens_identifier_value_hash` | `identifier`, `value_hash` | unique |
| `{prefix}verification_tokens` | `ttl_verification_tokens_expires_at` | `expires_at` | TTL, `expireAfterSeconds: 0` |

- **Unique indexes** drive duplicate detection: a violation surfaces as
  `auth.ErrDuplicate`, which the service turns into errors such as
  `auth.ErrUserAlreadyExists`. Without them, duplicates are silently
  accepted, so do not skip `EnsureIndexes`.
- **TTL indexes** let MongoDB delete expired sessions and tokens in the
  background. The TTL monitor runs about once a minute, so the collections
  stay small even if you never call `svc.CleanupExpired(ctx)`. Calling it
  is still fine: it deletes the same records right away.
- **Existing indexes**: if an index with one of these names or key
  patterns already exists with a different definition, `EnsureIndexes`
  keeps it as long as it still gives the required guarantee. For example,
  a unique index on the same keys under another name is kept, and so is a
  TTL index whose `expireAfterSeconds` you tuned. Otherwise
  `EnsureIndexes` returns an error that names the collection and the
  index, and you have to drop the conflicting index. If duplicate
  documents already exist, building a unique index fails with the
  server's duplicate key error: clean up the data first.

### Collections and documents

Every record's ID is stored as the document `_id`: a 32 character hex
string generated with `crypto/rand`, not an ObjectID, so IDs look the same
on every store backend. Field names are snake_case. Timestamps are BSON
dates.

| Collection | Fields |
| --- | --- |
| `{prefix}users` | `_id`, `name`, `email` (lowercase), `email_verified`, `image`, `banned`, `ban_reason`, `ban_expires` (date or null), `created_at`, `updated_at`, `version` (64-bit integer) |
| `{prefix}accounts` | `_id`, `user_id`, `provider` (`credential`, `google`, ...), `provider_account_id` (the email for credential accounts), `password` (bcrypt hash; empty for OAuth accounts), `created_at`, `updated_at`, `version` (64-bit integer) |
| `{prefix}sessions` | `_id`, `user_id`, `token_hash` (SHA-256 hex of the session token), `expires_at`, `ip_address`, `user_agent`, `created_at`, `updated_at` |
| `{prefix}verification_tokens` | `_id`, `identifier` (`email-verification:<email>` or `password-reset:<email>`), `value_hash` (SHA-256 hex of the token), `expires_at`, `created_at` |

Behavior worth knowing:

- **No plaintext secrets.** Passwords are stored only as bcrypt hashes.
  Session and verification tokens are stored only as SHA-256 hashes.
- **Millisecond precision.** BSON dates store milliseconds, and values are
  read back in UTC. Timestamps that the store generates are
  millisecond-precise already, so a struct returned by a create or update
  equals what a later read returns. Truncation keeps the order of
  timestamps, so a token issued in the same millisecond as a sign-up is
  never rejected as older than its user.
- **Optimistic concurrency.** Users and accounts carry a `version` field
  (`auth.User.Version`, `auth.Account.Version`). Creates set it to 1;
  updates replace the document only while its stored `version` equals the
  passed one, in a single atomic write, and increment it. A mismatch
  returns `auth.ErrConflict`, a missing document `auth.ErrNotFound`.
  Documents written by a pre-release build of 0.2.0 have no `version`
  field and every update of them fails with `auth.ErrConflict`; give them
  one before upgrading (`db.corego_users.updateMany({version: {$exists:
  false}}, {$set: {version: NumberLong(1)}})`, and the same for
  `corego_accounts`) or drop the collections.
- **Cascading deletes.** Deleting a user also deletes its accounts and
  sessions. This runs without a transaction, and the user document is
  deleted last, so a failed delete can simply be retried. The auth service
  removes the user's verification tokens itself.
- **Timeouts.** Each store operation has a 5 second budget derived from
  the caller's context. A shorter caller deadline or a cancellation still
  applies.

## PostgreSQL store

Package `github.com/berkkaradalan/CoreGo/auth/pgstore` stores users,
accounts, sessions and verification tokens in PostgreSQL through a pgx
connection pool. It implements `auth.Store` completely and keeps its
tables apart from yours with a table name prefix.

### Via the facade

`corego.New` picks the PostgreSQL store, creates it on the facade's own
connection pool and runs the migrations for you when:

- `AuthOptions.Store` is `corego.StorePostgres` (`"postgres"`), or
- `AuthOptions.Store` is empty and `COREGO_AUTH_STORE=postgres`, or
- neither is set, PostgreSQL is connected and MongoDB is not (automatic
  selection prefers MongoDB when both are connected).

```go
// POSTGRES_CONNECTION_URL=postgres://app:secret@localhost:5432/app
core, err := corego.New(&corego.Config{
	Auth: &corego.AuthOptions{Store: corego.StorePostgres}, // or leave empty to auto-select
})
if err != nil {
	log.Fatal(err)
}
defer core.Close()
```

The facade uses the prefix `corego_`, or `AuthOptions.TablePrefix` /
`COREGO_AUTH_TABLE_PREFIX` when set ([Another prefix](#another-prefix)).
To use a schema other than `public`, see
[A dedicated schema](#a-dedicated-schema).

### Standalone

```go
func newAuth(ctx context.Context, url string) (*auth.Auth, *database.PostgresDB, error) {
	db, err := database.NewPostgresDB(database.PostgresConfig{URL: url})
	if err != nil {
		return nil, nil, err
	}

	store, err := pgstore.NewStore(db.Pool(), pgstore.Options{})
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	// Creates or upgrades the tables. Idempotent: run it on every start.
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		return nil, nil, err
	}

	service, err := auth.New(auth.Config{}, store)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return service, db, nil
}
```

`NewStore` accepts any `*pgxpool.Pool`, not only the one from the
`database` package. The store does not own the pool: closing it is up to
you (here with `db.Close()`). `NewStore` does not contact the server. Call
`Migrate` before you use the store.

`NewStore` returns `pgstore.ErrNilPool` for a nil pool and
`pgstore.ErrInvalidTablePrefix` for a prefix it rejects. Compare them with
`errors.Is`.

### Table prefix

```go
store, err := pgstore.NewStore(pool, pgstore.Options{TablePrefix: "myapp_auth_"})
```

| Rule | Detail |
| --- | --- |
| Default | An empty `TablePrefix` means `corego_` (`pgstore.DefaultTablePrefix`). |
| Usage | Prepended verbatim to every table, constraint and index name: `myapp_auth_` gives `myapp_auth_users`, `myapp_auth_sessions`, ... Without a trailing underscore, `auth` gives `authusers`. |
| Allowed characters | Lowercase ASCII letters `a-z`, digits `0-9` and `_` only. |
| Maximum length | 24 bytes (`pgstore.MaxTablePrefixLength`). With this limit every derived name stays within PostgreSQL's 63-byte identifier limit. |

The prefix becomes part of SQL identifiers, so the validation is a
security boundary. `NewStore` rejects anything else (uppercase letters,
dots, quotes, spaces, non-ASCII). All SQL text is built once in `NewStore`
from the validated prefix, and every value travels as a query parameter.

Because of the prefix, the store never touches an existing `users` or
`sessions` table of your application. Several independent stores can also
share one database, each with its own prefix. Tables are created in the
first schema of the connection's `search_path` (normally `public`). To
place them in a dedicated schema, set `search_path` on the pool, for
example with `?search_path=myschema` in the connection URL
([A dedicated schema](#a-dedicated-schema)).

### Migrations

`Migrate(ctx)` is a small versioned migration runner:

1. It opens one transaction and takes a transaction-scoped advisory lock
   (`pg_advisory_xact_lock`). The key is derived deterministically from
   the prefix, so every instance using the same prefix waits for the same
   lock, and stores with different prefixes do not block each other.
2. It creates `{prefix}schema_migrations` if needed and reads the versions
   that are already applied.
3. It applies every pending migration in version order and records each
   one with its `applied_at` time.
4. It commits. If any statement fails, the whole transaction rolls back
   and the database is left as it was.

As a result, calling `Migrate` on every start is safe, including from many
application instances starting at the same time. One instance applies the
pending migrations; the others wait and find nothing left to do.

**Upgrading CoreGo.** New releases add schema changes as new migration
versions; released migrations are never edited. After an upgrade, the
next `Migrate` call (or the next `corego.New` through the facade) applies
only the new versions. Versions in `schema_migrations` that the running
release does not know, written by a newer release, are left alone.

**Conflicting tables.** Migrations create tables with plain
`CREATE TABLE`, not `IF NOT EXISTS`. If a table with one of the store's
names already exists but is not recorded in `schema_migrations`, `Migrate`
fails with PostgreSQL's "relation already exists" error and rolls back. It
never adopts a table it did not create. Choose a different prefix, or
remove the stray table.

| Version | Change |
| --- | --- |
| 1 | Creates the users, accounts, sessions and verification_tokens tables with their constraints and indexes, including the `version` columns of users and accounts. |

Migration 1 gained the `version` columns shortly before the 0.2.0
release. A development database migrated by a pre-release build already
records version 1 without those columns, so `Migrate` will not add them:
drop its auth tables (including `{prefix}schema_migrations`) and let
`Migrate` create them again. Databases created by the 0.2.0 release are
not affected.

### Tables

All timestamps are `TIMESTAMPTZ` and are stored and returned in UTC with
microsecond precision (sub-microsecond digits are truncated, which never
changes the order of two instants). IDs are random UUIDv4 strings unless
the caller supplies one. Emails and token identifiers are stored exactly
as the auth service passes them (the service lowercases email addresses).

The tables below use the default prefix.

#### `corego_users`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `TEXT` | Primary key (`corego_users_pkey`). |
| `name` | `TEXT NOT NULL` | Display name. |
| `email` | `TEXT NOT NULL` | Unique (`corego_users_email_key`). |
| `email_verified` | `BOOLEAN NOT NULL DEFAULT FALSE` | |
| `image` | `TEXT NOT NULL DEFAULT ''` | Avatar URL. |
| `banned` | `BOOLEAN NOT NULL DEFAULT FALSE` | |
| `ban_reason` | `TEXT NOT NULL DEFAULT ''` | |
| `ban_expires` | `TIMESTAMPTZ` | `NULL` = permanent ban (when `banned`). |
| `created_at` | `TIMESTAMPTZ NOT NULL` | Indexed (`corego_users_created_at_idx`) for `ListUsers`. |
| `updated_at` | `TIMESTAMPTZ NOT NULL` | |
| `version` | `BIGINT NOT NULL DEFAULT 1` | Optimistic concurrency counter (`auth.User.Version`). |

#### `corego_accounts`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `TEXT` | Primary key. |
| `user_id` | `TEXT NOT NULL` | References `corego_users(id)` `ON DELETE CASCADE`. |
| `provider` | `TEXT NOT NULL` | `credential`, `google`, ... |
| `provider_account_id` | `TEXT NOT NULL` | The email address for credential accounts. |
| `password` | `TEXT NOT NULL DEFAULT ''` | bcrypt hash; empty for OAuth accounts. |
| `created_at` | `TIMESTAMPTZ NOT NULL` | |
| `updated_at` | `TIMESTAMPTZ NOT NULL` | |
| `version` | `BIGINT NOT NULL DEFAULT 1` | Optimistic concurrency counter (`auth.Account.Version`). |

Unique constraints: `(provider, provider_account_id)`
(`corego_accounts_provider_account_key`) and `(user_id, provider)`
(`corego_accounts_user_provider_key`). The second one leads with
`user_id`, so it also serves per-user lookups and the cascade.

#### `corego_sessions`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `TEXT` | Primary key. |
| `user_id` | `TEXT NOT NULL` | References `corego_users(id)` `ON DELETE CASCADE`; indexed (`corego_sessions_user_id_idx`). |
| `token_hash` | `TEXT NOT NULL` | SHA-256 hex digest of the session token, never the token itself. Unique (`corego_sessions_token_hash_key`). |
| `expires_at` | `TIMESTAMPTZ NOT NULL` | Indexed (`corego_sessions_expires_at_idx`) for cleanup. |
| `ip_address` | `TEXT NOT NULL DEFAULT ''` | |
| `user_agent` | `TEXT NOT NULL DEFAULT ''` | |
| `created_at` | `TIMESTAMPTZ NOT NULL` | |
| `updated_at` | `TIMESTAMPTZ NOT NULL` | |

#### `corego_verification_tokens`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `TEXT` | Primary key. |
| `identifier` | `TEXT NOT NULL` | Purpose-prefixed address: `email-verification:<email>` or `password-reset:<email>`. |
| `value_hash` | `TEXT NOT NULL` | SHA-256 hex digest of the token. |
| `expires_at` | `TIMESTAMPTZ NOT NULL` | Indexed (`corego_verification_tokens_expires_at_idx`) for cleanup. |
| `created_at` | `TIMESTAMPTZ NOT NULL` | Set by the service; tokens created before the user's `created_at` are rejected. |

Unique constraint: `(identifier, value_hash)`
(`corego_verification_tokens_lookup_key`). This table has no foreign key,
because the identifier is an address and not a user id. The auth service
deletes a user's tokens itself before deleting the user.

#### `corego_schema_migrations`

| Column | Type | Notes |
| --- | --- | --- |
| `version` | `INTEGER` | Primary key; one row per applied migration. |
| `applied_at` | `TIMESTAMPTZ NOT NULL` | When it was applied. |

### Behavior notes

- Deleting a user removes its accounts and sessions in the same statement
  through the cascading foreign keys.
- `UpdateUser` and `UpdateAccount` are one conditional statement
  (`UPDATE ... SET ..., version = version + 1 WHERE id = $n AND version =
  $m`). When no row matches, the store tells a missing row
  (`auth.ErrNotFound`) from a changed one (`auth.ErrConflict`).
- Expired rows stay in the tables until a cleanup removes them. Run
  `Auth.CleanupExpired` periodically; it deletes sessions and tokens whose
  `expires_at` is strictly before the current time
  ([authentication.md](authentication.md#cleanup)).
- A unique violation is reported as `auth.ErrDuplicate` and a missing row
  as `auth.ErrNotFound`. Other errors are returned unchanged, so
  `errors.Is(err, context.Canceled)` and `errors.As(err, &pgErr)` keep
  working. For example, creating an account or session for a user id that
  does not exist returns the foreign key violation (SQLSTATE `23503`).
- `store.TablePrefix()` returns the effective prefix, which is handy for
  your own queries that join against the auth tables.

## Writing your own store

Any type that implements `auth.Store` works with `auth.New`: a MySQL
adapter, a cache in front of another store, an instrumented wrapper. The
interface has methods for the four record types (see the
[API reference](api-reference.md#auth) for the full list), and the
service relies on this contract:

- **Create** methods fill in `ID`, `CreatedAt` and `UpdatedAt` when the
  caller left them zero and keep caller-supplied values otherwise, so the
  service can read them after the call. A verification token's
  `CreatedAt` is always set by the service and must be persisted as given
  (with enough precision to keep its order relative to the user's
  `CreatedAt`).
- **Update** methods replace the full record and set `UpdatedAt` to the
  current time.
- **Users and accounts are versioned** (optimistic concurrency):
  `CreateUser` and `CreateAccount` set `Version` to 1 on the passed struct,
  whatever it held. `UpdateUser` and `UpdateAccount` replace the record
  only when the stored `Version` equals the passed `Version`, and on
  success increment it in the store and on the passed struct. A missing
  record returns `auth.ErrNotFound` and a version mismatch
  `auth.ErrConflict`; neither changes anything. The check and the write
  must be atomic (one conditional statement, or one critical section), and
  Get and List methods return the stored `Version`. The service relies on
  `ErrConflict`: it re-reads the record and retries the change (up to 5
  attempts), so returning it is how your store prevents lost updates such
  as a profile update silently reverting a ban.
- **Get** methods return `auth.ErrNotFound` when nothing matches; **list**
  methods return an empty, non-nil slice.
- **Unique constraints** return `auth.ErrDuplicate`: user email, account
  `(provider, provider_account_id)`, account `(user_id, provider)`,
  session token hash, verification token `(identifier, value_hash)`.
- **Single-record deletes** return `auth.ErrNotFound` when nothing
  matched; **bulk deletes** (`DeleteSessionsByUser`,
  `DeleteVerificationTokensByIdentifier`, `DeleteExpiredSessions`,
  `DeleteExpiredVerificationTokens`) succeed on zero matches.
- **`DeleteUser`** cascades to the user's accounts and sessions only;
  verification tokens are removed by the service.
- **"Expired"** means `ExpiresAt` strictly before the given instant.
- Lists are ordered: users and sessions newest first, accounts oldest
  first. `ListUsers` treats `Limit <= 0` as the default (100) and a
  negative `Offset` as 0.
- Values are stored verbatim; the service normalizes emails before they
  reach the store. Secrets arrive already hashed.
- Every method must be safe for concurrent use.

Wrap backend errors so that the three sentinels (`ErrNotFound`,
`ErrDuplicate`, `ErrConflict`) match with `errors.Is`, and return
everything else unchanged. `auth.MemoryStore` is a compact
reference implementation of the whole contract; testing your store with
the same scenarios its tests cover (duplicates, cascades, ordering,
expiry boundaries) is the quickest way to gain confidence.
