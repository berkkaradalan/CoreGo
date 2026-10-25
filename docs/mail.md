# Mail

The `mail` package sends email. It has one small interface, `Sender`, and
three implementations: `SMTPSender` for real delivery, `LogSender` for local
development and `CaptureSender` for tests. The auth package sends its
verification and password reset emails through it.

## Send your first email

### With environment variables

Put your SMTP settings in `.env` (or the process environment):

```dotenv
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=no-reply@example.com
SMTP_PASSWORD=change-me
SMTP_FROM="My App <no-reply@example.com>"
```

and build a sender from them:

```go
package main

import (
	"context"
	"log"

	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/mail"
)

func main() {
	e, err := env.Load()
	if err != nil {
		log.Fatal(err)
	}

	port := e.Int("SMTP_PORT", 587)
	tlsMode := mail.TLSStartTLS
	if port == 465 {
		tlsMode = mail.TLSImplicit // 465 speaks TLS from the first byte
	}
	sender := mail.NewSMTPSender(mail.SMTPConfig{
		Host:     e.GetOrDefault("SMTP_HOST", ""),
		Port:     port,
		Username: e.GetOrDefault("SMTP_USERNAME", ""),
		Password: e.GetOrDefault("SMTP_PASSWORD", ""),
		From:     e.GetOrDefault("SMTP_FROM", ""),
		TLSMode:  tlsMode,
	})

	err = sender.Send(context.Background(), mail.Message{
		To:      []string{"ada@example.com"},
		Subject: "Hello from CoreGo",
		Text:    "It works!",
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

When you use the facade with auth enabled you do not need any of this code
for auth emails: if `SMTP_HOST` is set and no mailer or mail sender is
configured in code, `corego.New` builds this SMTP sender from the same
variables (port 465 with implicit TLS, every other port with STARTTLS) and
sends verification and password reset emails with it (see
[Using mail with auth](#using-mail-with-auth)).

### In code

```go
sender := mail.NewSMTPSender(mail.SMTPConfig{
	Host:     "smtp.example.com",
	Port:     587,
	Username: "no-reply@example.com",
	Password: os.Getenv("SMTP_PASSWORD"),
	From:     "My App <no-reply@example.com>",
})

err := sender.Send(ctx, mail.Message{
	To:      []string{"Ada Lovelace <ada@example.com>"},
	Subject: "Welcome aboard",
	Text:    "Hi Ada,\n\nthanks for signing up.",
	HTML:    "<p>Hi Ada,</p><p>thanks for signing up.</p>",
})
if err != nil {
	return err
}
```

`Send` returns an error when the message is invalid, the server cannot be
reached or the server rejects the message. Every error starts with
`mail:`; log it as-is.

**Errors never contain email addresses**, so logging them does not leak
your users' addresses. An address is referred to by field and zero-based
index instead, and addresses that the SMTP server quotes in its reply are
replaced with `[redacted]`:

```
mail: to[1]: invalid address
mail: bcc[0]: contains CR or LF (possible header injection)
mail: smtp: MAIL FROM: 553 5.7.1 <[redacted]>: Sender address rejected
mail: smtp: RCPT TO for cc[0]: 550 5.1.1 <[redacted]>: Recipient address rejected
```

(The exact rendering of the server reply, for example whether it is
quoted, comes from Go's `net/textproto`.) A server reply stays available
as a `*textproto.Error` through `errors.As`, so you can still inspect its
`Code`.

## Senders

```go
type Sender interface {
	Send(ctx context.Context, msg Message) error
}
```

Every sender is safe for concurrent use. Anything with a `Send` method of
this shape is a sender, so you can plug in an HTTP API provider; for a
one-off, `mail.SenderFunc` adapts a plain function:

```go
var sender mail.Sender = mail.SenderFunc(func(ctx context.Context, msg mail.Message) error {
	return myProvider.Deliver(ctx, msg.To, msg.Subject, msg.HTML)
})
```

### SMTPSender (production)

```go
sender := mail.NewSMTPSender(mail.SMTPConfig{
	Host:     "smtp.example.com", // required
	Port:     587,                // 0 means 587
	Username: "no-reply@example.com",
	Password: os.Getenv("SMTP_PASSWORD"),
	From:     "My App <no-reply@example.com>",
	TLSMode:  mail.TLSStartTLS,   // the default (zero value)
	Timeout:  10 * time.Second,   // 0 means 10s
})
```

| Field | Meaning |
| --- | --- |
| `Host` | SMTP server hostname or IP. Required. |
| `Port` | Server port; `0` defaults to `587`. |
| `Username`, `Password` | Enable `AUTH PLAIN` when `Username` is set. Credentials are only ever sent over TLS; under `TLSDisabled` authentication is skipped. |
| `From` | Default sender. Used as the envelope sender (`MAIL FROM`) and as the `From` header of messages that do not set their own. May include a display name. |
| `TLSMode` | See [TLS modes](#tls-modes). |
| `Timeout` | Bounds the connection attempt and every single SMTP command; `0` defaults to 10 seconds. |

Each `Send` opens one connection and runs one SMTP dialog: connect, `EHLO`,
TLS, `AUTH`, `MAIL FROM`, `RCPT TO` (every To, Cc and Bcc address), `DATA`,
`QUIT`. The context is checked before connecting and between commands, so a
cancelled request stops the delivery. A message without any sender address
(neither `Message.From` nor `SMTPConfig.From`) is rejected before
connecting.

### LogSender (development)

```go
sender := mail.NewLogSender(nil) // nil uses slog.Default()
```

`LogSender` delivers nothing. It validates the message and logs **one
compact Info line** per message, so you can click the verification or
password reset link straight from your terminal without any extra setup:

```text
2026/10/24 12:00:00 INFO mail: outgoing message to=ada@example.com subject="Verify your email for MyApp" links="http://localhost:8080/auth/verify-email?email=ada%40example.com&token=..."
```

| Attribute | Content |
| --- | --- |
| `from` | `Message.From`; **omitted** when empty (the auth `DefaultMailer` leaves it to the sender). |
| `to` | Recipients, comma-separated. |
| `subject` | The subject. |
| `links` | Every `http`/`https` URL in the text body, deduplicated and space-separated. When the text body is empty or has no URL, the `href` targets of the HTML body are used instead (HTML entities such as `&amp;` decoded; `mailto:` and relative links skipped). Omitted when there are none. |

Trailing sentence punctuation and unbalanced closing brackets are not part
of a link (`(see https://example.com/a).` yields `https://example.com/a`).
The full text and HTML bodies are logged in a second record at **Debug**
level (`to`, `subject`, `text`, `html`, plus `from` when set). Enable Debug
for the logger you pass in to see them:

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
sender := mail.NewLogSender(logger)
```

> **Warning: development only.** The logged links and bodies contain live,
> single-use tokens (verification and password reset), and the links are
> logged at **Info** level, which production log pipelines keep. Never use
> `LogSender` as your production mailer: anyone who can read the logs could
> take over accounts. Use `SMTPSender` (or your own `Sender`) in
> production.

### CaptureSender (tests)

```go
capture := mail.NewCaptureSender()

// ... exercise the code under test with capture as its sender ...

msgs := capture.Messages()  // copies of every message, in send order
last, ok := capture.Last()  // the most recent message
capture.Reset()             // start over
```

`CaptureSender` records messages in memory and always returns nil. It does
not validate (it records exactly what your code produced, even invalid
messages) and ignores the context. Typical auth test:

```go
capture := mail.NewCaptureSender()
mailer := auth.NewDefaultMailer(capture, auth.DefaultMailerConfig{
	VerifyEmailURL:   "https://api.example.test/auth/verify-email",
	ResetPasswordURL: "https://app.example.test/reset-password",
})
// ... sign a user up, wait for background deliveries (svc.Shutdown) ...
last, _ := capture.Last()
// assert on last.To, last.Subject and the link in last.Text
```

## Message

```go
msg := mail.Message{
	From:    "My App <no-reply@example.com>", // optional, senders fill a default
	To:      []string{"Ada Lovelace <ada@example.com>", "grace@example.com"},
	Cc:      []string{"team@example.com"},
	Bcc:     []string{"audit@example.com"},
	ReplyTo: "Support <support@example.com>",
	Subject: "Your invoice",
	Text:    "Plain text version",
	HTML:    "<p>HTML version</p>",
}
```

| Field | Required | Notes |
| --- | --- | --- |
| `From` | no | Sender address. Empty means the sender's default (`SMTPConfig.From`). |
| `To` | yes, at least one | Recipients. |
| `Cc` | no | Visible copy recipients. |
| `Bcc` | no | Hidden recipients: they receive the message, but never appear in any header. |
| `ReplyTo` | no | Where replies go. |
| `Subject` | yes | Must not be blank. |
| `Text` | one of `Text`/`HTML` | Plain text body. |
| `HTML` | one of `Text`/`HTML` | HTML body. With both set, mail clients show the HTML and fall back to the text. |

Every address may be bare (`ada@example.com`) or carry a display name
(`Ada Lovelace <ada@example.com>`, `"Doe, John" <john@example.com>`,
`José Núñez <jose@example.com>`). One entry holds exactly one address;
use separate slice elements for several recipients.

`msg.Validate()` checks a message before sending (all senders except
`CaptureSender` call it for you) and returns the first problem as
`mail: <field>: <problem>`, where list entries are named with their index
(`to[1]`, `cc[0]`, `bcc[2]`) and the offending value is never included
(for example `mail: from: invalid address`):

- `From` and `ReplyTo`, when set, and every `To`, `Cc` and `Bcc` entry must
  be a valid address.
- `To` must not be empty, `Subject` must not be blank, and at least one of
  `Text` or `HTML` must be set.
- No header field (`From`, `To`, `Cc`, `Bcc`, `ReplyTo`, `Subject`) may
  contain a line break. This blocks header injection such as a subject of
  `"hi\r\nBcc: attacker@example.com"` coming from user input.

### What goes on the wire

You never need to encode anything yourself:

- Bodies are sent as UTF-8 with `Content-Transfer-Encoding:
  quoted-printable`. Any line length, any language and characters such as
  `=` or a line consisting of a single `.` arrive intact; `\n` line breaks
  are converted to the CRLF line breaks email requires.
- With both `Text` and `HTML` the message is `multipart/alternative`;
  otherwise a single `text/plain` or `text/html` part.
- A non-ASCII `Subject` or display name is encoded per RFC 2047
  (`=?utf-8?q?...?=`); names containing commas or quotes are quoted
  correctly. Long headers are folded.
- `Date`, `Message-ID` and `MIME-Version` headers are added automatically.

## TLS modes

| Mode | Behavior | Use it for |
| --- | --- | --- |
| `mail.TLSStartTLS` (default) | Connects in plaintext and upgrades with `STARTTLS` before anything else is sent. If the server does not offer `STARTTLS`, sending **fails** instead of continuing unencrypted. | Port **587**, the standard submission port. Most providers. |
| `mail.TLSImplicit` | Speaks TLS from the first byte. | Port **465** ("SMTPS"). |
| `mail.TLSDisabled` | Plaintext for the whole session; never authenticates. | Local development servers only (MailHog, Mailpit, smtp4dev on port 1025). |

Server certificates are verified against the system roots using `Host` as
the server name.

Local development with Mailpit, for example:

```go
sender := mail.NewSMTPSender(mail.SMTPConfig{
	Host:    "localhost",
	Port:    1025,
	From:    "dev@example.test",
	TLSMode: mail.TLSDisabled,
})
```

## Using mail with auth

The auth service sends emails through an `auth.Mailer`. `auth.DefaultMailer`
turns any `mail.Sender` into one that writes the verification and password
reset emails (plain text and HTML) for you:

```go
mailer := auth.NewDefaultMailer(sender, auth.DefaultMailerConfig{
	AppName:          "My App",                                      // optional, used in subjects
	VerifyEmailURL:   "https://api.example.com/auth/verify-email",   // the API's GET /auth/verify-email
	ResetPasswordURL: "https://app.example.com/reset-password",      // your frontend's reset page
})

core, err := corego.New(&corego.Config{
	Auth: &corego.AuthOptions{Service: auth.Config{Mailer: mailer}},
})
```

`token` and `email` query parameters are appended to both URLs.

With the facade you usually do not build the `DefaultMailer` yourself.
Hand `corego.New` a sender, or nothing at all, and it builds one whose
verification link is `APP_URL` + the auth base path + `/verify-email`
(`/auth/verify-email` by default) and whose reset link is
`COREGO_RESET_PASSWORD_URL` (default `APP_URL` + `/reset-password`). The
mailer is chosen in this order:

| Configured | Auth emails go through |
| --- | --- |
| `AuthOptions.Service.Mailer` | your `auth.Mailer`, exactly as given |
| `AuthOptions.MailSender` | that `mail.Sender`, wrapped in the facade's `DefaultMailer` |
| `SMTP_HOST` (environment) | an `SMTPSender` built from the `SMTP_*` variables, wrapped the same way |
| none of them | nothing is sent; `corego.New` logs a warning |

`MailSender` is the easy way to use a non-SMTP backend or, in
development, to read the emails in your terminal (the links appear in the
Info line; pass a Debug logger instead of `nil` to see the full bodies too):

```go
core, err := corego.New(&corego.Config{
	Auth: &corego.AuthOptions{MailSender: mail.NewLogSender(nil)}, // never in production
})
```

Set `APP_URL` to your public URL so the links work outside your machine. See
[environment.md](environment.md) for all variables and
[authentication.md](authentication.md) for when emails are sent.

## Troubleshooting

**Gmail: "Username and Password not accepted" (535).** Gmail does not accept
your normal account password over SMTP. Turn on 2-Step Verification, create
an *app password* in your Google account (Security, App passwords) and use
it as `SMTP_PASSWORD`, with `SMTP_HOST=smtp.gmail.com`, `SMTP_PORT=587` and
your full Gmail address as `SMTP_USERNAME`. Gmail sends from the
authenticated account; use that address (or a configured alias) in
`SMTP_FROM`.

**Port 465 vs 587.** Port 587 starts unencrypted and upgrades with STARTTLS
(`TLSStartTLS`, the default). Port 465 expects TLS immediately
(`TLSImplicit`). A mismatch shows up as a timeout or a TLS handshake error:

- `TLSStartTLS` against port 465: the client waits for a greeting the server
  never sends in plaintext, and `Send` times out.
- `TLSImplicit` against port 587: the TLS handshake fails.

The facade's `SMTP_*` configuration picks the mode from `SMTP_PORT`:
`SMTP_PORT=465` selects `TLSImplicit`, every other port (587 by default)
selects `TLSStartTLS`, so both common setups work without code. Only a
server that offers implicit TLS on a port other than 465 needs the sender
built in code with `TLSMode: mail.TLSImplicit`, passed through
`auth.NewDefaultMailer` as shown above (or as `AuthOptions.MailSender`).

**"server ... does not advertise STARTTLS; refusing to send over
plaintext".** You are talking to a server without TLS support on that port.
For a local development server use `TLSDisabled`; for a real provider check
the host and port.

**"no sender address".** Set `SMTPConfig.From` (`SMTP_FROM`) or
`Message.From`.

**Emails arrive in spam or not at all.** Send from a domain you control and
configure SPF, DKIM and DMARC for it at your DNS provider; many providers
silently drop mail whose `From` domain does not match the authenticated
account.

**Nothing is sent and nothing fails in development.** You are probably
using `LogSender` (or no mailer at all). Check the logs for
`mail: outgoing message`: its `links` attribute holds the verification or
reset link. Enable Debug logging to see the full body.
