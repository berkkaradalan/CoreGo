# Authentication

The `auth` package is CoreGo's authentication service: email and password
sign-up and sign-in, server-side sessions, email verification, password
reset, email and password changes, account deletion and admin operations
(bans, user listing, cleanup). It is framework agnostic and persists
everything through a `Store` (memory, MongoDB or PostgreSQL). The HTTP
routes live in `auth/authhttp` and are mounted for you by the facade.

## The 5 things you will do

**1. Sign up and sign in over HTTP.** With the facade, the auth routes are
mounted under `/auth` automatically:

```bash
curl -X POST localhost:8080/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada","email":"ada@example.com","password":"analytical-engine"}'

curl -X POST localhost:8080/auth/signin \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"analytical-engine"}'
# -> {"user": {...}, "token": "<session token>", "expires_at": "..."}
```

Send the token as `Authorization: Bearer <token>` (or enable cookies). Every
route, body and status code is listed in [http-api.md](http-api.md).

**2. Protect a route.** Put `RequireAuth()` on a group or a single route:

```go
core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
if err != nil {
	log.Fatal(err)
}
app := core.Server()
api := app.Group("/api", core.RequireAuth())
```

Requests without a valid session are answered with `401` before your
handler runs (`403` for banned users and, when verification is required,
unverified ones).

**3. Read the current user** with `auth.MustUser(c)`. `*web.Context` is a
`context.Context`, so it goes straight in:

```go
api.Get("/hello", func(c *web.Context) error {
	user := auth.MustUser(c)
	return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
})
```

On routes where signing in is optional, put `core.OptionalAuth()` on the
route and use `auth.UserFromContext(c)` instead (see
[Reading the user in handlers](#reading-the-user-in-handlers)).

**4. Configure email.** Set `SMTP_HOST` (plus `SMTP_PORT`, `SMTP_USERNAME`,
`SMTP_PASSWORD`, `SMTP_FROM`) and the facade wires the built-in
`DefaultMailer` for verification and password reset emails. In
development, pass a sender in code instead, for example
`AuthOptions{MailSender: mail.NewLogSender(nil)}` to read the emails in
your logs. Without any mailer nothing is sent and a warning is logged at
startup. Details: [Mailers](#mailers) below and [mail.md](mail.md).

**5. Choose a store.** The facade picks MongoDB or PostgreSQL when a
connection URL is configured and falls back to memory otherwise; set
`COREGO_AUTH_STORE=memory|mongo|postgres` to choose explicitly. See
[auth-storage.md](auth-storage.md).

The rest of this page documents the service itself: what each operation
does, its errors and its security properties. Use it directly when you are
not using HTTP or need custom flows. Building handlers and middleware is
covered in [web-framework.md](web-framework.md).

## Using the service directly

```go
store := auth.NewMemoryStore() // or mongostore / pgstore, see auth-storage.md
svc, err := auth.New(auth.Config{
	RequireVerifiedEmail: true,
	Mailer: auth.NewDefaultMailer(mail.NewLogSender(nil), auth.DefaultMailerConfig{
		AppName:          "Acme",
		VerifyEmailURL:   "http://localhost:8080/auth/verify-email",
		ResetPasswordURL: "http://localhost:3000/reset-password",
	}),
}, store)
if err != nil {
	log.Fatal(err)
}
defer func() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = svc.Shutdown(ctx) // let background emails finish
}()
```

The facade exposes the same service as `core.Auth`, so everything below also
works on `core.Auth` inside handlers.

## Configuration

`auth.New(cfg, store)` validates the configuration and replaces zero values
with the package defaults. A nil store, a negative duration, a negative
`MinPasswordLength` or a negative `MaxPendingEmails` yields
`ErrInvalidConfig`. Enabling
`RequireVerifiedEmail` without a `Mailer` is allowed but logged as a
warning, because users could never verify their address.

| `auth.Config` field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `SessionTTL` | `time.Duration` | 7 days (`DefaultSessionTTL`) | Lifetime of a regular session. |
| `RememberMeTTL` | `time.Duration` | 30 days (`DefaultRememberMeTTL`) | Lifetime of a session created with `RememberMe: true`. |
| `EmailVerificationTokenTTL` | `time.Duration` | 24 hours (`DefaultEmailVerificationTokenTTL`) | How long a verification link stays valid. |
| `PasswordResetTokenTTL` | `time.Duration` | 1 hour (`DefaultPasswordResetTokenTTL`) | How long a password reset link stays valid. |
| `RequireVerifiedEmail` | `bool` | `false` | Make a verified address mandatory for being signed in: `SignUp` creates no session, and `SignIn` and `GetSession` return `ErrEmailNotVerified` for unverified users. |
| `MinPasswordLength` | `int` | 8 (`DefaultMinPasswordLength`) | Minimum password length in bytes. The maximum is 72 bytes (the bcrypt limit); longer passwords are rejected with `ErrWeakPassword`. |
| `Mailer` | `auth.Mailer` | `nil` | Sends verification and reset emails. When nil no email is sent (see [Email without a mailer](#email-without-a-mailer)). |
| `MaxPendingEmails` | `int` | 100 (`DefaultMaxPendingEmails`) | Most background email jobs in flight at once; further ones are dropped and logged (see [Background delivery and Shutdown](#background-delivery-and-shutdown)). |
| `Logger` | `*slog.Logger` | `nil` | Receives background delivery failures and configuration warnings. nil uses `slog.Default()` at the time of logging. |

## Sign-up

```go
out, err := svc.SignUp(ctx, auth.SignUpInput{
	Name:     "Ada Lovelace",
	Email:    "Ada@Example.com", // stored as ada@example.com
	Password: "analytical-engine",
})
if errors.Is(err, auth.ErrUserAlreadyExists) {
	// 409 in the HTTP layer
}
```

| `SignUpInput` field | Meaning |
| --- | --- |
| `Name` | Display name, required; surrounding whitespace is trimmed and at most 256 characters (Unicode code points) may remain. |
| `Email` | Login address, required; trimmed, lowercased and validated (`net/mail`, no display names). |
| `Password` | Required; must satisfy the password policy. |
| `Image` | Optional avatar URL stored on the user. Trimmed; when not empty it must be an absolute `http` or `https` URL with a host, at most 2048 bytes. |
| `RememberMe` | Session lifetime `RememberMeTTL` instead of `SessionTTL`. |
| `IPAddress`, `UserAgent` | Recorded on the session for auditing. |

`SignUpOutput` holds `User`, `SessionToken` (the raw opaque token, returned
exactly once; only its SHA-256 hash is stored) and `Session` (the stored
record, without the raw token).

- Validation errors (`ErrNameRequired`, `ErrNameTooLong`,
  `ErrInvalidImageURL`, `ErrEmailRequired`, `ErrInvalidEmail`,
  `ErrPasswordRequired`, `ErrWeakPassword`) are returned before anything
  is stored.
- An address that is already registered yields `ErrUserAlreadyExists`, also
  when a concurrent registration wins the race. If the credential account
  cannot be created, the half-created user is removed again so the address
  is never blocked.
- With `RequireVerifiedEmail` enabled, **no session is created**:
  `SessionToken` is `""` and `Session` is `nil`. The user signs in after
  verifying the address.
- With a mailer configured, a verification token is stored and the
  verification email is sent **in the background**: `SignUp` neither waits
  for nor reports the delivery. Failures are logged; the user can ask for a
  new email with `RequestEmailVerification` (public) or
  `SendVerificationEmail` (signed in). When `MaxPendingEmails` emails are
  already in flight, the email (and its token) is skipped and logged; the
  registration still succeeds.

## Sign-in

```go
out, err := svc.SignIn(ctx, auth.SignInInput{
	Email:      "ada@example.com",
	Password:   "analytical-engine",
	RememberMe: true,
	IPAddress:  remoteIP,
	UserAgent:  userAgent,
})
switch {
case errors.Is(err, auth.ErrInvalidCredentials): // wrong email or password, indistinguishable
case errors.Is(err, auth.ErrEmailNotVerified):   // RequireVerifiedEmail is on
case errors.Is(err, auth.ErrUserBanned):
}
```

`SignInOutput` mirrors `SignUpOutput` (`User`, `SessionToken`, `Session`).

- Unknown email, missing credential account and wrong password all return
  `ErrInvalidCredentials`. For unknown emails a bcrypt comparison against a
  dummy hash still runs, so timing does not reveal whether an address is
  registered.
- Only **after** the password matched: unverified users are rejected with
  `ErrEmailNotVerified` when `RequireVerifiedEmail` is on, and banned users
  with `ErrUserBanned`. Someone without the password therefore learns
  nothing about the account.

## Sessions

Sessions are server-side records identified by an opaque token: 32 random
bytes from `crypto/rand`, base64url encoded (43 characters). Only
`auth.HashToken(token)` (SHA-256, hex) is stored. A session is valid
**through** its `ExpiresAt`.

| Method | Behavior |
| --- | --- |
| `GetSession(ctx, token)` | Returns `(*Session, *User, error)`. `ErrSessionNotFound` for an empty or unknown token and for a session whose user no longer exists (the orphan is deleted); `ErrSessionExpired` for an expired session (deleted on the spot); `ErrUserBanned` while the user is banned; `ErrEmailNotVerified` when `RequireVerifiedEmail` is on and the address is unverified (for example after an email change). |
| `SignOut(ctx, token)` | Deletes that session. Idempotent: unknown tokens return nil. |
| `ListSessions(ctx, userID)` | All sessions of the user, newest first, including expired ones not yet cleaned up. |
| `RevokeSession(ctx, userID, sessionID)` | Deletes one session, but only if it belongs to `userID`; otherwise `ErrSessionNotFound`, so other users' sessions cannot be probed. |
| `RevokeAllSessions(ctx, userID)` | Deletes every session of the user. |
| `RevokeOtherSessions(ctx, userID, keepSessionID)` | "Sign out everywhere else". |

`ResetPassword`, `ChangePassword`, `BanUser` and `DeleteUser` also revoke all
sessions of the user.

## Reading the user in handlers

The HTTP layer's `RequireAuth()` middleware resolves the token with
`GetSession` and stores the result in the request context with
`auth.ContextWithSession`. Four framework-agnostic helpers work with any
`context.Context`, including `*web.Context`:

```go
func ContextWithSession(ctx context.Context, session *auth.Session, user *auth.User) context.Context
func UserFromContext(ctx context.Context) (*auth.User, bool)
func SessionFromContext(ctx context.Context) (*auth.Session, bool)
func MustUser(ctx context.Context) *auth.User
```

```go
// Behind RequireAuth: a user is always present.
api.Get("/devices", func(c *web.Context) error {
	user := auth.MustUser(c)
	current, _ := auth.SessionFromContext(c)
	sessions, err := core.Auth.ListSessions(c, user.ID)
	if err != nil {
		return err
	}
	type device struct {
		ID        string    `json:"id"`
		UserAgent string    `json:"user_agent"`
		CreatedAt time.Time `json:"created_at"`
		Current   bool      `json:"current"`
	}
	devices := make([]device, 0, len(sessions))
	for _, s := range sessions {
		devices = append(devices, device{ID: s.ID, UserAgent: s.UserAgent, CreatedAt: s.CreatedAt, Current: s.ID == current.ID})
	}
	return c.JSON(http.StatusOK, devices)
})
```

The models can be encoded directly (see [JSON](#json)), but `auth.User`
includes the ban fields, so map records to the fields you want to expose
in public responses, as above. (`GET /auth/sessions` already returns this
list in a sanitized form.)

`MustUser` panics with `auth: no authenticated user in context; protect the
route with RequireAuth` when no user is present. That is a programming
error (an unprotected route), and the web framework's panic recovery turns
it into a `500`. The readers return `(nil, false)` for a nil context, a
context without a session, and a stored nil value. The context key is an
unexported type, so it can never collide with other packages' keys.

`UserFromContext` only finds a user that `RequireAuth`, `OptionalAuth` (or
your own middleware calling `ContextWithSession`) stored. A route that
serves both guests and signed-in users uses `OptionalAuth`: with a valid
session it stores the user like `RequireAuth`, otherwise the request
continues anonymously instead of being answered `401`:

```go
app.Get("/greeting", func(c *web.Context) error {
	if user, ok := auth.UserFromContext(c); ok {
		return c.String(http.StatusOK, "Welcome back, "+user.Name)
	}
	return c.String(http.StatusOK, "Hello, guest")
}, core.OptionalAuth())
```

Banned users, unverified users (with `RequireVerifiedEmail`) and cookie
requests that fail the [CSRF check](http-api.md#csrf-protection) are
treated as guests too; a store failure still answers `500`.

### JSON

`auth.User`, `auth.Session`, `auth.Account` and `auth.VerificationToken`
carry snake_case JSON tags (`email_verified`, `expires_at`, `user_id`,
...). Secrets are never marshalled: `Account.Password`,
`Session.TokenHash`, `VerificationToken.ValueHash` and the `Version`
counters are tagged `json:"-"`. `auth.User` does include `banned`,
`ban_reason` and `ban_expires` (omitted when nil), which is right for an
admin API but usually not for what a user or the public sees; render your
own shape there, like the auth routes do
([response shapes](http-api.md#response-shapes)).

## Email verification

There are three ways to send a verification email and one way to consume it:

```go
err := svc.RequestEmailVerification(ctx, email) // public, by address, uniform nil
err = svc.SendVerificationEmail(ctx, userID)    // signed in, synchronous, reports errors
err = svc.VerifyEmail(ctx, email, token)        // consume the token from the link
```

`SignUp` and `ChangeEmail` send one automatically when a mailer is
configured.

- **`RequestEmailVerification`** is the public "resend the link" flow. It
  returns nil for malformed and unknown addresses, already verified
  addresses and when no mailer is configured. The only synchronous work is
  the user lookup, made for every valid address alike; for a registered,
  unverified address a background job stores a fresh token and sends the
  email. Only a failing user lookup is returned.
- **`SendVerificationEmail`** is for a signed-in user pressing "resend". It
  is synchronous and reports the outcome: `ErrUserNotFound`,
  `ErrEmailAlreadyVerified`, `ErrMailerNotConfigured`, or an error wrapping
  `ErrEmailDeliveryFailed` (cause preserved). The token is stored before
  sending, so it stays valid when delivery fails.
- **`VerifyEmail`** accepts only verification tokens: a password reset token
  yields `ErrInvalidToken`. The email is normalized before lookup. Unknown
  tokens yield `ErrInvalidToken`; expired tokens are deleted and yield
  `ErrTokenExpired` (a second attempt then reports `ErrInvalidToken`). A
  token whose address now belongs to nobody, or to an account created after
  the token was issued, yields `ErrInvalidToken`.
- The token is **consumed first**: it is deleted right after the lookup and
  before the user is updated, so two concurrent attempts with the same
  token can never both succeed (the second gets `ErrInvalidToken`). A
  failure after that point (a store error, `ErrConflict`) leaves the token
  consumed; the user requests a new one.
- A token only verifies the address it was issued for: when the user's
  address changed in the meantime, `VerifyEmail` returns `ErrInvalidToken`
  and the new address stays unverified. On success `EmailVerified` is set.
- Several outstanding verification tokens may exist (resend buttons are
  safe); any one of them verifies the address, and an older link still
  succeeds after the address was verified.
- A token is valid through its expiry instant (`EmailVerificationTokenTTL`,
  24 hours by default).

The link in the default email points at the API's `GET /auth/verify-email`
route, which calls `VerifyEmail` and redirects or renders a small result
page ([http-api.md](http-api.md)).

## Password reset

```go
err := svc.RequestPasswordReset(ctx, email)              // public, uniform nil
err = svc.ResetPassword(ctx, email, token, newPassword)  // from your reset page
```

- **`RequestPasswordReset`** returns nil for malformed and unknown addresses
  and when no mailer is configured, so it never reveals which addresses are
  registered. The only synchronous work is the user lookup, made for every
  valid address alike; for a registered address a background job stores a
  single use token (`PasswordResetTokenTTL`, 1 hour by default) and sends
  the email, so neither the token write nor SMTP latency and failures reach
  the caller. Only a failing user lookup is returned.
- **`ResetPassword`** validates the new password first (`ErrPasswordRequired`
  / `ErrWeakPassword`); a rejected password does not consume the token. It
  accepts only password reset tokens: a verification token yields
  `ErrInvalidToken`, as do unknown tokens and tokens whose address now
  belongs to nobody or to a newer account. Expired tokens are deleted and
  yield `ErrTokenExpired`.
- The token is then **consumed before the password changes**, so two
  concurrent attempts with the same token can never both succeed (the
  second gets `ErrInvalidToken`). A failure after that point (a user
  without a credential account yields `ErrAccountNotFound`, a store error,
  `ErrConflict`) leaves the token consumed; the user requests a new one.
- On success the password hash is replaced, **every session of the user is
  revoked**, and **every outstanding reset token** of the address is
  deleted, not just the one used.

## Changing the password and the email address

Both operations require the current password.

```go
err := svc.ChangePassword(ctx, userID, currentPassword, newPassword)
user, err := svc.ChangeEmail(ctx, userID, newEmail, currentPassword)
err = svc.CheckPassword(ctx, userID, password) // just verify, for your own sensitive actions
```

**`CheckPassword`** returns nil when the password matches, otherwise
`ErrUserNotFound`, `ErrAccountNotFound` (no credential account, for example
an OAuth-only user) or `ErrInvalidCurrentPassword`. It has no side effects.

**`ChangePassword`** checks the current password (errors as above), then the
policy for the new one, replaces the hash, revokes **all** of the user's
sessions and invalidates outstanding password reset tokens. Re-using the
current password is allowed (bcrypt salts every hash).

**`ChangeEmail`** works in this order:

1. The new address is normalized and validated (`ErrEmailRequired`,
   `ErrInvalidEmail`).
2. The current password is checked (`ErrUserNotFound`,
   `ErrAccountNotFound`, `ErrInvalidCurrentPassword`). This happens before
   the address is looked up, so the method cannot be used to probe which
   addresses are registered.
3. The same address (after normalization) returns the user unchanged,
   including its verification state.
4. An address owned by another user yields `ErrUserAlreadyExists`, also when
   a concurrent sign-up or change wins the race.

On success `EmailVerified` becomes false, the credential account follows
the new address, and **every outstanding verification and reset token of
the old address is deleted**, so links sent there can never act on whoever
registers that address next. When a mailer is configured, a verification
email for the new address is sent in the background. Existing sessions are
kept; with `RequireVerifiedEmail` on they return `ErrEmailNotVerified` until
the new address is verified.

## Profile and account deletion

```go
user, err := svc.GetUser(ctx, userID) // ErrUserNotFound

name, image := "Ada King", "https://cdn.example.com/ada.png"
user, err = svc.UpdateUser(ctx, userID, auth.UpdateUserInput{
	Name:  &name, // nil fields are left unchanged
	Image: &image,
})

accounts, err := svc.ListAccounts(ctx, userID) // linked provider accounts, oldest first
err = svc.DeleteUser(ctx, userID)
```

- `UpdateUser` changes only `Name` (trimmed; empty yields
  `ErrNameRequired`, more than 256 characters `ErrNameTooLong`) and `Image`
  (trimmed; empty removes it, anything else must be an absolute
  `http`/`https` URL of at most 2048 bytes, otherwise `ErrInvalidImageURL`).
  The change is applied to a freshly read record, so it never reverts a
  concurrent ban or email change (see [Concurrent updates](#concurrent-updates)).
  The email address is a credential and changes through `ChangeEmail`.
- `DeleteUser` first deletes every verification and reset token of the
  user's address, then the store deletes the user together with its
  accounts and sessions. Unknown IDs yield `ErrUserNotFound`. It does not
  ask for a password, because it serves admin deletions too. For
  self-service deletion, call `CheckPassword` first, as the HTTP layer's
  `DELETE /auth/me` does:

```go
if err := svc.CheckPassword(ctx, user.ID, password); err != nil {
	return err // for example auth.ErrInvalidCurrentPassword
}
return svc.DeleteUser(ctx, user.ID)
```

## Admin: users and bans

```go
users, err := svc.ListUsers(ctx, auth.ListOptions{Limit: 50, Offset: 100}) // newest first
count, err := svc.CountUsers(ctx)

err = svc.BanUser(ctx, userID, "spam", nil) // permanent
until := time.Now().Add(72 * time.Hour)
err = svc.BanUser(ctx, userID, "cool-off", &until) // temporary
err = svc.UnbanUser(ctx, userID)
```

- `ListOptions`: zero `Limit` selects the store default (100); a negative
  `Offset` counts as zero.
- `BanUser` records the reason and optional expiry and **revokes every
  session at once**. While the ban is active, `SignIn` and `GetSession`
  return `ErrUserBanned`. The service judges bans by its own clock:
  `user.IsBannedAt(t)` is true while `t` is before `BanExpires` (or always,
  for a permanent ban). `user.IsBanned()` is shorthand for
  `IsBannedAt(time.Now())`.
- `UnbanUser` clears every ban field. Sessions are not restored; the user
  signs in again.
- Both are written with optimistic concurrency, so a profile update or
  email change running at the same moment can never silently undo a ban.

## Concurrent updates

Users and accounts carry a `Version` counter that the store manages
(optimistic concurrency). Every operation that modifies a user or an
account (`UpdateUser`, `BanUser`, `UnbanUser`, `VerifyEmail`,
`ChangeEmail`, `ChangePassword`, `ResetPassword`) reads the record,
applies its change and writes it back only if nobody else wrote it in
between. When a concurrent write wins, the service reads the record again
and re-applies the change, up to 5 attempts. Only when every attempt loses
the race does it return `auth.ErrConflict` (`409 The account was modified
concurrently. Please retry.` over HTTP); repeating the call is safe.

Two writers can therefore never overwrite each other's changes: an admin
banning a user while the user renames themselves ends with both the ban
and the new name. Never set `Version` yourself; pass back the value you
read. The store side of the contract is described in
[auth-storage.md](auth-storage.md#writing-your-own-store).

## Cleanup

Expired tokens and sessions are rejected and deleted when they are
presented, but records nobody presents again stay in the store.
`CleanupExpired` deletes every verification token, reset token and session
whose expiry lies before now. It is idempotent; run it periodically:

```go
go func() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if err := svc.CleanupExpired(context.Background()); err != nil {
			log.Printf("auth cleanup: %v", err)
		}
	}
}()
```

Both cleanups always run, and their errors are joined (`errors.Join`). The
MongoDB store additionally has TTL indexes that expire these records on the
server ([auth-storage.md](auth-storage.md)).

## Background delivery and Shutdown

`SignUp` and `ChangeEmail` store the verification token synchronously,
then hand the email to the mailer on a background goroutine.
`RequestEmailVerification` and `RequestPasswordReset` do only the user
lookup synchronously; the token write and the delivery both run in the
background job, so their response time does not depend on either.

- At most `Config.MaxPendingEmails` jobs (100 by default) run at once.
  When all slots are taken, a new job is **dropped, never queued**: the
  request that triggered it is neither blocked nor failed, the drop is
  logged at Warn level as `auth: background email dropped: too many emails
  in flight` (with `email`, `user_id` and `max_pending_emails`), and the
  user can simply ask again. A hanging mail server therefore cannot pile
  up one goroutine per request.
- The goroutine gets `context.WithoutCancel(ctx)`: request values such as
  trace IDs are still available, but the request finishing does not cancel
  the delivery. The `Mailer` should apply its own timeouts
  (`mail.SMTPSender` does).
- A failure (of the token write or the delivery) is logged on
  `Config.Logger` at Error level as `auth: background email delivery
  failed` with the attributes `email` (`email_verification` or
  `password_reset`), `user_id` and `error`. The token, the recipient
  address and the message body are never logged. A panicking mailer is
  recovered and logged at Error level as `auth: background email delivery
  panicked`.
- `Shutdown(ctx)` waits until no delivery is running. It returns nil, or
  `ctx.Err()` when the context ends first (the remaining deliveries keep
  going). The service stays usable afterwards. The facade's `Close` calls it
  with a bounded timeout; call it yourself when you use the service
  directly:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := svc.Shutdown(ctx); err != nil {
	log.Printf("auth: pending emails were not delivered: %v", err)
}
```

`SendVerificationEmail` is the only synchronous delivery, because its caller
is signed in and entitled to the result.

### Email without a mailer

When `Config.Mailer` is nil, no email is ever sent and no token is stored:
`SignUp` and `ChangeEmail` skip the verification email,
`RequestEmailVerification` and `RequestPasswordReset` return nil, and only
`SendVerificationEmail` reports `ErrMailerNotConfigured`. The facade logs a
warning at startup in that case.

## Mailers

The service talks to email through a two-method interface and hands over the
recipient and the raw single use token:

```go
type Mailer interface {
	SendVerificationEmail(ctx context.Context, to, token string) error
	SendPasswordResetEmail(ctx context.Context, to, token string) error
}
```

### DefaultMailer

`auth.NewDefaultMailer(sender, cfg)` renders the two emails and delivers
them through any `mail.Sender` (SMTP, log, capture; see [mail.md](mail.md)):

```go
mailer := auth.NewDefaultMailer(mail.NewSMTPSender(mail.SMTPConfig{
	Host:     "smtp.example.com",
	Port:     587,
	Username: "no-reply@example.com",
	Password: os.Getenv("SMTP_PASSWORD"),
	From:     "Acme <no-reply@example.com>",
}), auth.DefaultMailerConfig{
	AppName:          "Acme",
	VerifyEmailURL:   "https://api.acme.example/auth/verify-email",
	ResetPasswordURL: "https://acme.example/reset-password",
})
```

| `DefaultMailerConfig` field | Meaning |
| --- | --- |
| `AppName` | Optional. Used in subjects and bodies: "Verify your email for Acme", "Reset your password for Acme". Whitespace, line breaks included, collapses to single spaces. |
| `VerifyEmailURL` | Required. Absolute `http`/`https` URL the verification link opens. The facade sets `{APP_URL}{auth base path}/verify-email` (`/auth/verify-email` by default), the API route that verifies and then redirects or renders a page. |
| `ResetPasswordURL` | Required. Absolute `http`/`https` URL of your "choose a new password" page. The facade uses `COREGO_RESET_PASSWORD_URL`, or `{APP_URL}/reset-password`. |

Links are built by adding `email` and `token` query parameters to the
configured URL. Existing query parameters are kept (an existing `email` or
`token` parameter is replaced), values are URL-escaped, and a `#fragment`
stays at the end:

```text
https://acme.example/reset-password?email=ada%40example.com&token=Zk3...
https://acme.example/reset?lang=en          -> ...?email=...&lang=en&token=...
https://acme.example/#/reset                -> https://acme.example/?email=...&token=...#/reset
```

Your reset page reads both parameters and submits them together with the new
password to `ResetPassword` (`POST /auth/reset-password` over HTTP).

Every email has a plain text part and a small, inline-styled HTML part with a
button and the raw link. All dynamic values in the HTML are escaped by
`html/template`. The `From` header is left empty, so the sender applies its
default (`SMTPConfig.From`). Configuration problems never panic. A missing
sender, an empty URL or a URL that is not absolute `http`/`https` makes the
send return an error naming the field, for example
`auth: sending verification email: DefaultMailerConfig.VerifyEmailURL is
empty; ...`. The service logs that error (background flows) or returns it
wrapped in `ErrEmailDeliveryFailed` (`SendVerificationEmail`). Errors never
contain the token.

For development, pair `DefaultMailer` with `mail.NewLogSender(nil)` to read
the links from your logs (one Info line per email with a clickable `links`
attribute; the full body is logged at Debug level), or with
`mail.NewCaptureSender()` to assert on them in tests.

### Which mailer the facade uses

With `corego.New`, the auth service's mailer is chosen in this order:

1. `AuthOptions.Service.Mailer`: your own `auth.Mailer`, used as is.
2. `AuthOptions.MailSender`: any `mail.Sender`, wrapped in a
   `DefaultMailer` whose links the facade derives (verification:
   `APP_URL` + auth base path + `/verify-email`; reset:
   `COREGO_RESET_PASSWORD_URL`, default `APP_URL` + `/reset-password`).
3. `SMTP_HOST` in the environment: an SMTP sender built from the `SMTP_*`
   variables, wrapped the same way.
4. Nothing: no email is sent and `corego.New` logs a warning.

```go
core, err := corego.New(&corego.Config{
	Auth: &corego.AuthOptions{
		MailSender: mail.NewLogSender(nil), // development: emails appear in the log
	},
})
```

### A custom mailer

Implement the interface for branded templates or a provider API. The method
receives the raw token; build your own link from it:

```go
// TemplateClient stands for your email provider's SDK.
type TemplateClient interface {
	SendTemplate(ctx context.Context, template, to string, data map[string]string) error
}

type BrandedMailer struct{ client TemplateClient }

func (m BrandedMailer) SendVerificationEmail(ctx context.Context, to, token string) error {
	link := "https://acme.example/verify?" + url.Values{"email": {to}, "token": {token}}.Encode()
	return m.client.SendTemplate(ctx, "verify-email", to, map[string]string{"link": link})
}

func (m BrandedMailer) SendPasswordResetEmail(ctx context.Context, to, token string) error {
	link := "https://acme.example/reset-password?" + url.Values{"email": {to}, "token": {token}}.Encode()
	return m.client.SendTemplate(ctx, "reset-password", to, map[string]string{"link": link})
}
```

Mailers must be safe for concurrent use. Most calls run in the background
with a context that is never canceled, so set your own timeouts.

## Errors

Every failure is a sentinel error of the `auth` package. Compare with
`errors.Is`, never with strings:

| Error | Returned by |
| --- | --- |
| `ErrInvalidConfig` | `New` |
| `ErrNameRequired`, `ErrNameTooLong`, `ErrInvalidImageURL` | `SignUp`, `UpdateUser` |
| `ErrEmailRequired`, `ErrInvalidEmail` | `SignUp`, `ChangeEmail` |
| `ErrPasswordRequired`, `ErrWeakPassword` | `SignUp`, `ResetPassword`, `ChangePassword` (new password) |
| `ErrUserAlreadyExists` | `SignUp`, `ChangeEmail` |
| `ErrUserNotFound` | `GetUser`, `UpdateUser`, `DeleteUser`, `CheckPassword`, `ChangePassword`, `ChangeEmail`, `SendVerificationEmail`, `BanUser`, `UnbanUser` |
| `ErrAccountNotFound` | `CheckPassword`, `ChangePassword`, `ChangeEmail`, `ResetPassword` (no credential account) |
| `ErrInvalidCredentials` | `SignIn` (wrong email or password, indistinguishable) |
| `ErrInvalidCurrentPassword` | `CheckPassword`, `ChangePassword`, `ChangeEmail` |
| `ErrEmailNotVerified` | `SignIn`, `GetSession` (with `RequireVerifiedEmail`) |
| `ErrEmailAlreadyVerified` | `SendVerificationEmail` |
| `ErrUserBanned` | `SignIn`, `GetSession` |
| `ErrSessionNotFound` | `GetSession`, `RevokeSession` |
| `ErrSessionExpired` | `GetSession` |
| `ErrInvalidToken`, `ErrTokenExpired` | `VerifyEmail`, `ResetPassword` |
| `ErrMailerNotConfigured` | `SendVerificationEmail` |
| `ErrEmailDeliveryFailed` | `SendVerificationEmail` (wraps the mailer error) |
| `ErrConflict` | `UpdateUser`, `BanUser`, `UnbanUser`, `VerifyEmail`, `ChangeEmail`, `ChangePassword`, `ResetPassword` (every retry lost to a concurrent change) |

Anything else is an infrastructure error from the store; log it and answer
`500`. Store implementations use the sentinels `auth.ErrNotFound`,
`auth.ErrDuplicate` and `auth.ErrConflict`; the service maps the first two
to the errors above and retries on the third.

## Stores

The service needs an `auth.Store`: `auth.NewMemoryStore()` for development
and tests, `mongostore` or `pgstore` for production. Setup, table and
collection names, prefixes, migrations and indexes are described in
[auth-storage.md](auth-storage.md). For your own adapter, the contract is
documented on the `Store` interface. The essentials:

- Create methods fill in a missing `ID`/`CreatedAt`/`UpdatedAt` and keep
  values supplied by the caller.
- Users and accounts are versioned: `CreateUser`/`CreateAccount` set
  `Version` to 1, and `UpdateUser`/`UpdateAccount` write only when the
  stored `Version` equals the passed one, then increment it; a mismatch
  returns `ErrConflict`. Details in
  [auth-storage.md](auth-storage.md#writing-your-own-store).
- Get methods return `ErrNotFound`. Unique violations return
  `ErrDuplicate`. Single-record deletes return `ErrNotFound` when nothing
  matched. Bulk deletes (`DeleteSessionsByUser`,
  `DeleteVerificationTokensByIdentifier`, `DeleteExpiredSessions`,
  `DeleteExpiredVerificationTokens`) succeed on zero matches.
- `DeleteUser` cascades to accounts and sessions only. Verification tokens
  are deleted by the service, because their identifiers carry a purpose
  prefix.
- "Expired" means `ExpiresAt` strictly before the given instant.

## Security notes

- **Passwords** are hashed with bcrypt at `bcrypt.DefaultCost`. The policy
  accepts `MinPasswordLength` to 72 bytes; 72 is the bcrypt input limit, and
  longer passwords are rejected instead of being silently truncated.
- **Tokens at rest**: session, verification and reset tokens have 256 bits
  of entropy and are stored only as SHA-256 hashes. A leaked database does
  not contain usable tokens, and the raw session token is returned exactly
  once.
- **Purpose separation**: verification tokens are stored under
  `email-verification:<email>` and reset tokens under
  `password-reset:<email>`. `VerifyEmail` only finds the former and
  `ResetPassword` only the latter, so a verification link can never reset a
  password, and the reverse.
- **No stale tokens**: `ChangeEmail` deletes every token of the old address,
  `DeleteUser` deletes every token of the user, a successful
  `ResetPassword` or `ChangePassword` deletes every outstanding reset token,
  and a token issued before the current owner of an address registered is
  rejected. Whoever registers a released address cannot be taken over with
  links sent to its previous owner, and a verification link only verifies
  the address it was sent to, never one the user changed to afterwards.
- **Single use under concurrency**: `VerifyEmail` and `ResetPassword`
  consume the token before applying the change, so one token can never be
  redeemed twice, not even by two simultaneous requests.
- **No lost updates**: user and account writes use optimistic concurrency
  ([Concurrent updates](#concurrent-updates)), so concurrent requests can
  never revert a ban, an email change or a password change.
- **Verified email is enforced everywhere**: with `RequireVerifiedEmail`,
  sign-up creates no session, and both `SignIn` and `GetSession` refuse
  unverified users. That includes sessions created before an email change.
- **Enumeration safety**: sign-in failures are uniform
  (`ErrInvalidCredentials`), and unknown emails still pay for one bcrypt
  comparison. `RequestPasswordReset` and `RequestEmailVerification` return
  nil for unknown, malformed and (for verification) already verified
  addresses, and when no mailer is configured. `ChangeEmail` checks the
  password before it looks at the new address. The verified-or-banned state
  is only revealed after a correct password.
- **Timing safety of the email flows**: the public request flows do one
  user lookup for every valid address and nothing else synchronously; the
  token write and the delivery run in the background, so neither the
  store write nor SMTP latency and failures affect the response time or
  the result.
- **Bounded background work**: at most `MaxPendingEmails` email jobs run
  at once; extra ones are dropped and logged rather than piling up.
- **Sensitive changes need the password**: `ChangePassword`, `ChangeEmail`
  and self-service deletion over HTTP (`CheckPassword` + `DeleteUser`) all
  require the current password, so a stolen session token alone cannot take
  over or delete an account.
- **Session revocation**: password reset, password change, bans and account
  deletion revoke every session of the user.
- **Logging**: background delivery failures are logged with the user ID and
  error only, never with the token, the address or the message body.
- **Rate limiting** is provided by the HTTP layer: `auth/authhttp` limits
  sign-up, sign-in, the email flows, the password and email changes and
  account deletion per client IP (10 requests per minute per route by
  default, IPv6 grouped by /64), and sign-in, forgot-password and the
  verification request also per email address (see
  [http-api.md](http-api.md#rate-limits)). When you call the service from
  your own endpoints, add `web.RateLimit` or a `web.RateLimiter` to them
  ([web-framework.md](web-framework.md#ratelimit)).
- **Profile fields are validated**: names are capped at 256 characters and
  images must be `http(s)` URLs of at most 2048 bytes, so a profile cannot
  carry `javascript:` URLs or megabytes of text.
- **Normalization**: emails are trimmed and lowercased everywhere (sign-up,
  sign-in, token flows, email change), so one mailbox always maps to one
  user.
