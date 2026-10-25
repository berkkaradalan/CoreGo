package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Auth is the CoreGo authentication service. It coordinates users,
// credential accounts, sessions and verification tokens on top of a
// Store and is safe for concurrent use.
//
// Construct it with New. All methods take a context as their first
// argument and return one of the package sentinel errors on failure;
// compare with errors.Is. Call Shutdown before the process exits so
// emails that are still being delivered in the background are not
// lost.
type Auth struct {
	cfg   Config
	store Store

	// now supplies the current time. It is a field so tests can swap in
	// a deterministic clock; production instances use utcNow.
	now func() time.Time

	// deliveries tracks background email jobs for Shutdown and bounds
	// them to Config.MaxPendingEmails.
	deliveries inflight
}

// New creates an Auth service backed by store.
//
// Zero-value Config fields are replaced by the package defaults (see
// the Default* constants). A nil store, a negative duration, a
// negative MinPasswordLength or a negative MaxPendingEmails yields
// ErrInvalidConfig. Enabling
// RequireVerifiedEmail without a Mailer is allowed but logged as a
// warning, because users then have no way to verify their address.
func New(cfg Config, store Store) (*Auth, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store must not be nil", ErrInvalidConfig)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	a := &Auth{
		cfg:   cfg.withDefaults(),
		store: store,
		now:   utcNow,
	}
	if a.cfg.RequireVerifiedEmail && a.cfg.Mailer == nil {
		a.logger().Warn("auth: RequireVerifiedEmail is enabled but no Mailer is configured; users cannot verify their email address and will not be able to sign in")
	}
	return a, nil
}

// utcNow is the default clock of the service: the current time in UTC,
// so timestamps never carry the local offset of the server (for example
// in JSON responses) regardless of the store.
func utcNow() time.Time {
	return time.Now().UTC()
}

// Shutdown waits until every email that is being delivered in the
// background has been handed to the mailer (successfully or not). It
// returns nil once nothing is in flight, or ctx.Err() when ctx is done
// first; in that case the remaining deliveries keep running.
//
// Shutdown does not stop the service: deliveries started while it
// waits are waited for as well, and the service stays usable
// afterwards. Call it after the HTTP server has stopped accepting
// requests, typically with a bounded context:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	if err := svc.Shutdown(ctx); err != nil {
//		log.Printf("auth: pending emails not delivered: %v", err)
//	}
func (a *Auth) Shutdown(ctx context.Context) error {
	return a.deliveries.wait(ctx)
}

// logger returns the configured logger, falling back to slog.Default()
// at call time so a default installed after New is honored.
func (a *Auth) logger() *slog.Logger {
	if a.cfg.Logger != nil {
		return a.cfg.Logger
	}
	return slog.Default()
}
