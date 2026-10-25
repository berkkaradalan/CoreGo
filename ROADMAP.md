# CoreGo Roadmap

CoreGo is a Go backend SDK: authentication, database access, email and
the web layer behind one API.

Principles that apply to every item below:

- Clean, readable Go; no code ported or copied from other libraries.
- Tests for every feature, edge cases included; `go test ./...` stays green.
- Documentation ships with the feature and stays in sync with the code.
- English only across code, comments, tests and docs.

## Done in 0.2.0

See the [changelog](CHANGELOG.md) for the complete list.

- [x] Web framework: router with parameters and wildcards, groups,
      middleware, JSON binding, CORS, rate limiting, request logging,
      panic recovery, server timeouts, graceful shutdown (`web`)
- [x] Auth service: sign-up, sign-in, server-side sessions with
      remember-me, email verification, password reset, password and
      email change, account deletion, bans with expiry, user listing,
      cleanup (`auth`)
- [x] Auth HTTP layer: 17 routes, bearer and cookie transport,
      `RequireAuth`, per-IP rate limiting, `auth.MustUser`
      (`auth/authhttp`)
- [x] Auth stores: memory, MongoDB with unique and TTL indexes,
      PostgreSQL with versioned migrations, prefixed tables and
      collections (`auth`, `auth/mongostore`, `auth/pgstore`)
- [x] Security hardening: token purpose separation, no stale tokens after
      email changes, enforced email verification, enumeration-safe and
      timing-safe email flows, password-confirmed sensitive changes,
      spoof-resistant client IP
- [x] Mail: SMTP (STARTTLS, implicit TLS), log and capture senders
      (`mail`)
- [x] Databases: context-aware MongoDB helpers, typed PostgreSQL queries,
      transactions (`database`)
- [x] Environment: `.env` plus process environment with typed getters
      (`env`)
- [x] Facade with auto-configuration: `corego.New`, `Core.Server`,
      `Core.RequireAuth`, `Core.Run`
- [x] In-memory rate limiting (`web.RateLimit`)
- [x] Quickstart example and complete documentation

## Next

Ordered roughly by expected value for a typical application.

- [ ] **Social sign-in (OAuth)**: Google, Apple, Facebook, GitHub and
      Microsoft. Authorization-code flow with PKCE, account linking by
      verified email, provider configuration from code or environment.
      The `auth.Account` model already carries provider fields.
- [ ] **Two-factor authentication**: TOTP apps, backup codes and email
      one-time codes, enforced per user.
- [ ] **Organizations and teams**: membership, invitations, roles inside
      an organization, permission checks.
- [ ] **RBAC**: global roles and permissions on top of the user model,
      middleware helpers for route protection.
- [ ] **Magic links and email OTP sign-in**: passwordless sign-in reusing
      the verification-token infrastructure.
- [ ] **Redis-backed rate limiting**: limits shared by every instance
      behind a load balancer (today's limiter is per process).
- [ ] **MySQL support**: an `auth.Store` adapter and a database wrapper.

## Later

- [ ] API keys for machine clients
- [ ] Audit logging hooks for security events
- [ ] Admin HTTP handlers (user management, session overview)
- [ ] Redis caching utilities
- [ ] WebAuthn / passkeys

## Explicitly not planned

Niche providers and integrations (Kakao, Waku, SIWE, payment plugins,
SCIM/SAML provisioning and similar). CoreGo targets the widely used core;
anything exotic belongs in user land, built on the public interfaces
(`auth.Store`, `auth.Mailer`, `mail.Sender`, `web.MiddlewareFunc`).
