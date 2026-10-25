# Environment

CoreGo is configured through environment variables, optionally kept in a
`.env` file during development. The facade loads them for you:
`corego.New` calls `env.Load()` once at startup, reads the variables listed
below and exposes the snapshot as `core.Env`. You only need the `env`
package directly when you use CoreGo's packages standalone or want typed
access to your own variables.

```go
core, err := corego.New(nil) // reads ./.env (when present) + the process environment
if err != nil {
	log.Fatal(err) // for example a malformed .env file
}
fmt.Println(core.Env.AppURL)                 // "http://localhost:8080" by default
workers := core.Env.Int("WORKERS", 4)        // your own variables, typed
```

If your application needs its own settings before it builds the
`corego.Config` (for example to choose options from them), load the
environment yourself and hand the snapshot to the facade through
`Config.Env`. `corego.New` then uses it as-is and does not read `.env`
again; `core.Env` is that same snapshot:

```go
e, err := env.Load()
if err != nil {
	log.Fatal(err)
}
core, err := corego.New(&corego.Config{
	Env: e, // reuse the snapshot: .env is read once
	Auth: &corego.AuthOptions{
		Service: auth.Config{SessionTTL: e.Duration("SESSION_TTL", 7*24*time.Hour)}, // your own variable
	},
})
if err != nil {
	log.Fatal(err)
}
```

## Every variable CoreGo reads

Values configured in code win over the environment: a variable is only
consulted when the matching field in `corego.Config` is left nil or empty.
The two boolean switches `COREGO_REQUIRE_VERIFIED_EMAIL` and
`COREGO_AUTH_COOKIE` are the exception: they can only turn a feature on,
so the effective setting is the code value **or** the environment value.
The `COREGO_*` auth variables are read only when auth is enabled
(`Config.Auth` is set).

| Variable | Default | What it does |
| --- | --- | --- |
| `APP_URL` | `http://localhost:` + `PORT` | Public base URL of your application, without a trailing slash (trailing slashes are removed). Used to build the links in verification and password reset emails. When it starts with `https://`, the facade marks the auth session cookie `Secure`; with cookies enabled, not `Secure` and a host other than `localhost`, `127.0.0.1` or `::1`, `corego.New` logs a warning. Available as `Env.AppURL`. |
| `URL` | none | Legacy name of `APP_URL`, read only when `APP_URL` is unset or empty. Prefer `APP_URL`. |
| `PORT` | `8080` | Port `core.Run()` listens on. Available as `Env.Port`. |
| `MONGODB_CONNECTION_URL` | none | MongoDB connection string. When set (and `Config.Mongo` is nil) the facade connects at startup and the auth store can use MongoDB. Available as `Env.MongoURL`. |
| `MONGODB_DATABASE` | database in the URL path, else `corego` | MongoDB database name. Available as `Env.MongoDatabase`. |
| `POSTGRES_CONNECTION_URL` | none | PostgreSQL connection string. When set (and `Config.Postgres` is nil) the facade connects at startup and the auth store can use PostgreSQL. Available as `Env.PostgresURL`. |
| `COREGO_AUTH_STORE` | auto | Auth store: `memory`, `mongo` or `postgres`. Read when `AuthOptions.Store` is empty. Empty means auto: the first connected database (MongoDB, then PostgreSQL), else the in-memory store with a logged warning. |
| `COREGO_AUTH_TABLE_PREFIX` | `corego_` | Prefix of the auth tables (PostgreSQL) or collections (MongoDB), read when `AuthOptions.TablePrefix` is empty. Lowercase ASCII letters, digits and `_` only; an invalid prefix makes `corego.New` fail with an error naming this variable. Ignored by the memory store and `AuthOptions.CustomStore`. See [auth-storage.md](auth-storage.md#another-prefix). |
| `COREGO_REQUIRE_VERIFIED_EMAIL` | `false` | `true` makes a verified email address mandatory for signing in (`auth.Config.RequireVerifiedEmail`). Code **or** environment: `AuthOptions.Service.RequireVerifiedEmail: true` turns it on as well, and `false` here cannot turn the code setting off. |
| `COREGO_AUTH_COOKIE` | `false` | `true` enables the HttpOnly session cookie next to bearer tokens (`authhttp.CookieConfig.Enabled`), with every other cookie setting at its default or code value. Code **or** environment, like `COREGO_REQUIRE_VERIFIED_EMAIL`. |
| `COREGO_MAIL` | empty (automatic) | How auth emails are delivered when neither `AuthOptions.Service.Mailer` nor `AuthOptions.MailSender` is set. Empty: SMTP when `SMTP_HOST` is set, otherwise no email and a startup warning. `smtp`: SMTP, and `corego.New` fails when `SMTP_HOST` is missing (use it in production so a forgotten variable stops the deploy). `log`: development mode, emails are logged through the facade logger (`Config.Logger`) instead of being sent, with recipient, subject and links at Info level and the full bodies at Debug level (see [mail.md](mail.md)); `corego.New` logs a warning because the links contain live tokens. `none`: no email and no warning. Case insensitive. |
| `SMTP_HOST` | none | SMTP server. When set, and neither `AuthOptions.Service.Mailer` nor `AuthOptions.MailSender` is configured in code (and `COREGO_MAIL` is empty or `smtp`), the facade sends verification and password reset emails over SMTP. See [mail.md](mail.md#using-mail-with-auth). |
| `SMTP_PORT` | `587` | SMTP port. `465` selects implicit TLS, every other port STARTTLS (see [TLS modes](mail.md#tls-modes)). An invalid value is logged and replaced by `587`. |
| `SMTP_USERNAME` | none | SMTP login. Empty disables authentication. Credentials are only ever sent over TLS. |
| `SMTP_PASSWORD` | none | SMTP password (for Gmail: an app password). |
| `SMTP_FROM` | none | Sender address of outgoing emails, optionally with a display name: `My App <no-reply@example.com>`. |
| `CORS_ALLOWED_ORIGINS` | none (CORS off) | Comma separated list of browser origins allowed to call the API, for example `https://app.example.com,http://localhost:5173`. Read when `ServerOptions.CORS` is nil. Credentials (cookies) are allowed for these origins, and unless `AuthOptions.HTTP.TrustedOrigins` is set they also become the trusted origins of the auth routes' [CSRF check](http-api.md#csrf-protection). A list containing `*` allows every origin **without** credentials (only the `Authorization` header works cross-origin then) and logs a warning. |
| `TRUST_PROXY_HEADERS` | `false` | Set to `true` only when the app runs behind a reverse proxy you control. The client IP (used for logging and rate limiting) is then taken from the rightmost `X-Forwarded-For` entry. While it is `false`, the first request that carries `X-Forwarded-For` makes the server log a one-time warning. Read when `ServerOptions.TrustProxyHeaders` is nil. |
| `COREGO_BANNER` | empty (automatic) | The startup banner `core.Run()` prints (address, services in use, every registered route). Empty: printed when standard error is a terminal. `true`: always. `false`: never; `core.Run()` logs one `corego: starting server` line instead. `ServerOptions.DisableBanner` turns it off from code. |
| `NO_COLOR` | none | Any non-empty value turns off the colors of the banner and of the development console logger ([no-color.org](https://no-color.org)). |
| `FORCE_COLOR` | none | Any non-empty value turns colors on even when the output is not a terminal. `NO_COLOR` wins over it. |
| `COREGO_RESET_PASSWORD_URL` | `APP_URL` + `/reset-password` | Page of your frontend that the password reset email links to. CoreGo appends `token` and `email` query parameters. |
| `COREGO_VERIFY_EMAIL_REDIRECT_URL` | none (built-in page) | Where `GET /auth/verify-email` (the link in verification emails) redirects after verifying, with `?status=verified`, `invalid` or `expired`. Empty shows a minimal built-in HTML page instead. |

Boolean variables accept `1`, `t`, `true`, `TRUE`, `True` and `0`, `f`,
`false`, `FALSE`, `False`. For `COREGO_REQUIRE_VERIFIED_EMAIL`,
`COREGO_AUTH_COOKIE` and `COREGO_BANNER` anything else (`yes`, `on`, a typo) makes
`corego.New` return an error naming the variable, and so does an unknown
`COREGO_MAIL` value: a mistyped security switch stops the application
instead of being ignored. The auth variables are validated whenever auth
is enabled, even when a code setting makes them irrelevant;
`COREGO_BANNER` is validated on every start. `TRUST_PROXY_HEADERS`
follows the same strict rule whenever `ServerOptions.TrustProxyHeaders`
is nil.

With auth enabled, `corego.New` ends with one Info line that shows what
the environment and the code resolved to:

```text
INFO corego: auth ready store=postgres table_prefix=corego_ base_path=/auth mail=smtp require_verified_email=true cookie=false
```

`store` is `memory`, `mongo`, `postgres` or `custom` (`AuthOptions.CustomStore`;
`table_prefix` is only shown for the database stores), and `mail` is
`smtp`, `log`, `custom` (`AuthOptions.Service.Mailer` or
`AuthOptions.MailSender`) or `none`.

CoreGo's own integration tests additionally read `COREGO_TEST_MONGO_URL` and
`COREGO_TEST_POSTGRES_URL`; applications never need them (see
[testing.md](testing.md)).

The repository ships [.env.example](../.env.example) with every variable and
a one-line explanation. Copy it to `.env` and uncomment what you need:

```bash
cp .env.example .env
```

Never commit your real `.env`; it usually contains passwords.

## Precedence

1. **Code wins.** A value set in `corego.Config` is used as-is; the
   environment is only a fallback. The exception are the on-only
   switches `COREGO_REQUIRE_VERIFIED_EMAIL` and `COREGO_AUTH_COOKIE`
   (code OR environment).
2. **The process environment wins over `.env`.** `.env` only fills in
   variables that are not set at all. A variable exported in your shell, by
   Docker or by your hosting platform always takes precedence.
3. **Set-but-empty still counts as set.** `PORT=` in the process environment
   blocks `PORT=4000` in `.env`; the typed field then falls back to its
   default (`8080`). To let `.env` supply a value, `unset` the variable.

For the typed fields and the typed getters an empty or whitespace-only value
means "not configured": `Env.MongoURL` is `""`, `Env.Int` returns your
default, and so on. Surrounding whitespace is trimmed.

## The env package

### Loading

```go
import "github.com/berkkaradalan/CoreGo/env"

e, err := env.Load() // ./.env when present + the process environment
if err != nil {
	log.Fatal(err)
}

e, err = env.LoadFile("config/staging.env") // explicit file, must exist
```

| Function | Missing file | Malformed file |
| --- | --- | --- |
| `env.Load()` | Not an error, nothing is logged (production usually has no `.env`). | Error. |
| `env.LoadFile(path)` | Error; `errors.Is(err, fs.ErrNotExist)` is true. | Error. |

Parse errors name the file and line but never echo its contents, so secrets
cannot leak into logs:

```
env: parse .env: line 3: unterminated quoted value
```

Loading never modifies the process environment: `os.Getenv` does not see
values that exist only in `.env`. Read them through the returned `Env`.

### Typed fields

| Field | Variable | Default |
| --- | --- | --- |
| `AppURL` | `APP_URL`, then `URL` | `"http://localhost:" + Port` |
| `Port` | `PORT` | `"8080"` (`env.DefaultPort`) |
| `MongoURL` | `MONGODB_CONNECTION_URL` | `""` (not configured) |
| `MongoDatabase` | `MONGODB_DATABASE` | `""` (not set) |
| `PostgresURL` | `POSTGRES_CONNECTION_URL` | `""` (not configured) |

### Reading your own variables

```go
key, ok := e.Get("STRIPE_KEY")                       // raw value, and whether it is set
region := e.GetOrDefault("AWS_REGION", "eu-west-1")  // trimmed value or the default
workers := e.Int("WORKERS", 4)                       // base-10 integer
debug := e.Bool("DEBUG", false)                      // strconv.ParseBool
timeout := e.Duration("HTTP_TIMEOUT", 30*time.Second) // "30s", "5m", "1h30m"
hosts := e.Strings("ALLOWED_HOSTS")                  // "a.com, b.com" -> ["a.com" "b.com"]
```

| Method | Returns the default when | Notes |
| --- | --- | --- |
| `Get(key)` | never (returns `"", false` when unset) | Raw, untrimmed value. A set-but-empty variable returns `"", true`. |
| `GetOrDefault(key, def)` | unset, empty or whitespace only | Value is trimmed. |
| `Int(key, def)` | unset, empty or not an integer (`"1.5"`, `"10s"`, overflow) | |
| `Bool(key, def)` | unset, empty or not a `strconv.ParseBool` value (`"yes"`, `"on"`) | |
| `Duration(key, def)` | unset, empty or not a `time.ParseDuration` value | A bare number such as `"30"` has no unit and is invalid (`"0"` is the only exception). |
| `Strings(key)` | returns `nil` when unset or empty | Splits on commas, trims each element and drops empty ones: `" a, ,b ,"` gives `["a" "b"]`. Each call returns a fresh slice. |

Invalid values never cause an error or a panic; validate them yourself when
a typo must stop the application:

```go
if raw, ok := e.Get("WORKERS"); ok {
	if _, err := strconv.Atoi(strings.TrimSpace(raw)); err != nil {
		log.Fatalf("WORKERS must be an integer, got %q", raw)
	}
}
```

### Snapshot semantics

An `Env` is a snapshot taken by `Load`: later `os.Setenv` calls and later
edits to `.env` are invisible to it. Configuration therefore stays constant
for the lifetime of the process. An `Env` is read-only and safe for
concurrent use.

## The .env file format

```dotenv
# Comments start with #
PORT=8080
APP_NAME="My App"                 # double quotes; \n inside becomes a newline
GREETING='Hello $NAME'            # single quotes: taken literally
export SMTP_HOST=smtp.example.com # a leading "export" is allowed
DB_HOST=localhost
DATABASE_URL=postgres://app:secret@${DB_HOST}:5432/app
```

- `${NAME}` expands only variables defined **earlier in the same file**; the
  process environment is not visible to expansion.
- Variable names may contain letters, digits, `_` and `.`. A line without a
  name (`=value`), a name with spaces (`MY VAR=1`) or a bare word without
  `=` is a parse error.
- Windows line endings are accepted.

## Testing code that reads the environment

Use `t.Setenv` for process variables and `LoadFile` (or `t.Chdir` into a
temporary directory with a `.env`) for files. Both are restored
automatically when the test ends:

```go
func TestConfig(t *testing.T) {
	t.Setenv("WORKERS", "8")

	path := filepath.Join(t.TempDir(), "test.env")
	if err := os.WriteFile(path, []byte("APP_URL=https://test.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	e, err := env.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if e.Int("WORKERS", 1) != 8 || e.AppURL != "https://test.example.com" {
		t.Fatalf("unexpected config: %+v", e)
	}
}
```
