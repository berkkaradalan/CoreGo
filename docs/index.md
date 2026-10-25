# CoreGo Documentation

CoreGo is a backend SDK for Go: authentication with server-side sessions,
a web framework, MongoDB and PostgreSQL access, email and environment
configuration behind one facade, `corego.New`. New here? Start with
[Getting started](getting-started.md): a working API in one file, then
short recipes for everything else.

## I want to...

| Task | Where |
| --- | --- |
| Build my first CoreGo app | [Getting started: hello world](getting-started.md#hello-world) |
| Add an endpoint | [Getting started: recipe 1](getting-started.md#1-add-an-endpoint), [Web framework: adding an endpoint](web-framework.md#adding-an-endpoint) |
| Protect routes and read the signed-in user | [Getting started: recipe 2](getting-started.md#2-protect-routes-and-read-the-current-user), [HTTP API: protecting your own routes](http-api.md#protecting-your-own-routes) |
| Serve guests and signed-in users on one route | [HTTP API: optional sign-in](http-api.md#optional-sign-in) |
| Return a 404 or 400 from a handler | [Web framework: errors](web-framework.md#errors) |
| Write middleware | [Web framework: writing your own](web-framework.md#writing-your-own) |
| Store my data in PostgreSQL or MongoDB | [Getting started: recipe 3](getting-started.md#3-connect-a-database-and-store-your-data), [Database](database.md) |
| Choose where users and sessions are stored | [Auth storage](auth-storage.md) |
| Send verification and password reset emails | [Getting started: recipe 4](getting-started.md#4-send-email), [Mail](mail.md#using-mail-with-auth) |
| Send my own emails | [Mail](mail.md) |
| Connect a browser frontend (CORS, cookies) | [Getting started: recipe 5](getting-started.md#5-a-browser-frontend-cors-and-cookies), [HTTP API: bearer tokens or cookies](http-api.md#bearer-tokens-or-cookies), [CSRF protection](http-api.md#csrf-protection) |
| Know every auth route, body and status code | [HTTP API](http-api.md) |
| Call the auth service from my own code | [Authentication](authentication.md) |
| Configure through environment variables | [Environment](environment.md) |
| Deploy to production | [Getting started: deploy checklist](getting-started.md#6-deploy-checklist) |
| Test my application | [Testing](testing.md#testing-your-application) |
| Upgrade from 0.1 | [Migrating from 0.1](migrating-from-0.1.md) |
| Look up an exact signature | [API reference](api-reference.md) |

## Pages

| Page | What it covers |
| --- | --- |
| [Getting started](getting-started.md) | Install, hello world, how the pieces fit, a project layout, and six recipes: endpoints, protected routes, databases, email, browser frontends, deployment. |
| [Web framework](web-framework.md) | The `web` package: routing, groups, middleware (CORS, rate limiting, logging, recovery), the `Context`, errors, `net/http` interop, running and configuring the server. |
| [Authentication](authentication.md) | The `auth` service: configuration, sign-up and sign-in, sessions, reading the user in handlers, email verification, password reset, email and password changes, admin operations, concurrent updates, background email delivery, mailers, errors, security notes. |
| [HTTP API](http-api.md) | The `auth/authhttp` routes: route table, curl examples, bearer tokens and cookies, CSRF protection, the email link flows, rate limits, error messages, `RequireAuth`, `OptionalAuth`, plain `net/http`. |
| [Auth storage](auth-storage.md) | Choosing an auth store (including a custom prefix or your own store through the facade), the memory store, the MongoDB and PostgreSQL stores (setup, prefixes, indexes, migrations, data model) and writing your own store. |
| [Database](database.md) | The `database` package: connecting, MongoDB CRUD, PostgreSQL queries, typed scanning, transactions, "not found" handling, raw drivers. |
| [Mail](mail.md) | The `mail` package: senders (SMTP, log, capture), messages, TLS modes, auth emails, troubleshooting. |
| [Environment](environment.md) | Every environment variable CoreGo reads, precedence rules, the `env` package and the `.env` format. |
| [Testing](testing.md) | Testing an application built on CoreGo, and running CoreGo's own test suite and integration tests. |
| [Migrating from 0.1](migrating-from-0.1.md) | Before and after code for every API that changed in 0.2.0. |
| [API reference](api-reference.md) | Every exported identifier of every package, with its signature. |

## Outside this directory

- [README](../README.md): overview and the hello world.
- [Quickstart example](../examples/quickstart/README.md): a runnable app
  with a recorded curl session.
- [Changelog](../CHANGELOG.md): what changed in each release.
- [Roadmap](../ROADMAP.md): what is next and what is explicitly not
  planned.

## Keeping the docs honest

The documentation is written against the code and checked against it:
code samples compile against the current release, and every route,
status code, default and environment variable is taken from the source.
If you change a public API, update the affected page and the
[API reference](api-reference.md) in the same change.
