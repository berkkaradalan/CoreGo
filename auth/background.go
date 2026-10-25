package auth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Email kinds used as the "email" attribute of delivery log records.
const (
	emailKindVerification  = "email_verification"
	emailKindPasswordReset = "password_reset"
)

// deliverInBackground runs job on a new goroutine so the caller's
// response time and result never depend on the mail backend (or, for
// the public request flows, on the token write either). It is
// reserveBackground followed by runReserved; when every slot is taken
// the job is dropped (and logged) without blocking the caller.
func (a *Auth) deliverInBackground(ctx context.Context, kind, userID string, job func(context.Context) error) {
	if a.reserveBackground(ctx, kind, userID) {
		a.runReserved(ctx, kind, userID, job)
	}
}

// issueAndDeliverInBackground starts a background job that persists a
// fresh token of the given purpose for the user's address and emails
// it. It backs the public request flows, whose response must not
// depend on the token write. The caller must have checked that a
// mailer is configured.
func (a *Auth) issueAndDeliverInBackground(ctx context.Context, kind string, user *User, purpose tokenPurpose, ttl time.Duration) {
	mailer, userID, to := a.cfg.Mailer, user.ID, user.Email
	a.deliverInBackground(ctx, kind, userID, func(ctx context.Context) error {
		token, err := a.issueToken(ctx, purpose, to, ttl)
		if err != nil {
			return fmt.Errorf("issuing the token: %w", err)
		}
		if purpose == purposePasswordReset {
			return mailer.SendPasswordResetEmail(ctx, to, token)
		}
		return mailer.SendVerificationEmail(ctx, to, token)
	})
}

// reserveBackground claims one of the Config.MaxPendingEmails
// background slots. When all are in use it logs the dropped email at
// Warn level (kind and user ID only) and returns false; it never
// blocks. A successful reservation must be handed to runReserved or
// returned with releaseBackground.
func (a *Auth) reserveBackground(ctx context.Context, kind, userID string) bool {
	if a.deliveries.tryAdd(a.cfg.MaxPendingEmails) {
		return true
	}
	a.logger().WarnContext(ctx, "auth: background email dropped: too many emails in flight",
		"email", kind, "user_id", userID, "max_pending_emails", a.cfg.MaxPendingEmails)
	return false
}

// releaseBackground returns a slot claimed by reserveBackground that
// will not run a job.
func (a *Auth) releaseBackground() {
	a.deliveries.done()
}

// runReserved runs job on a new goroutine using a slot claimed by
// reserveBackground, and frees the slot when the job returns.
//
// The goroutine receives context.WithoutCancel(ctx): request scoped
// values (trace IDs, ...) stay available while the request's
// cancellation does not abort the job. Failures and panics are logged
// at Error level with the email kind and the user ID only; the token,
// the recipient address and the message body are never logged.
// Shutdown waits for every job started here.
func (a *Auth) runReserved(ctx context.Context, kind, userID string, job func(context.Context) error) {
	ctx = context.WithoutCancel(ctx)
	go func() {
		defer a.deliveries.done()
		defer func() {
			if r := recover(); r != nil {
				a.logger().ErrorContext(ctx, "auth: background email delivery panicked",
					"email", kind, "user_id", userID, "panic", fmt.Sprint(r))
			}
		}()
		if err := job(ctx); err != nil {
			a.logger().ErrorContext(ctx, "auth: background email delivery failed",
				"email", kind, "user_id", userID, "error", err.Error())
		}
	}()
}

// inflight counts running background tasks and lets callers wait,
// with a context, until none is running.
//
// It plays the role of a sync.WaitGroup but, unlike a WaitGroup, it
// allows new tasks to start while another goroutine is waiting with
// the counter at zero. Shutdown can therefore be called while requests
// are still being served without tripping the WaitGroup reuse rules.
// The zero value is ready to use.
type inflight struct {
	mu    sync.Mutex
	count int
	idle  chan struct{} // closed when count drops back to zero
}

// tryAdd registers one running task unless limit tasks are already
// running, and reports whether it did.
func (f *inflight) tryAdd(limit int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.count >= limit {
		return false
	}
	if f.count == 0 {
		f.idle = make(chan struct{})
	}
	f.count++
	return true
}

// done marks one task as finished.
func (f *inflight) done() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count--
	if f.count == 0 {
		close(f.idle)
	}
}

// wait blocks until no task is running or ctx is done, whichever
// happens first, and returns nil or ctx.Err() respectively.
func (f *inflight) wait(ctx context.Context) error {
	f.mu.Lock()
	if f.count == 0 {
		f.mu.Unlock()
		return nil
	}
	idle := f.idle
	f.mu.Unlock()

	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
