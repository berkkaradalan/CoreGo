package auth

import (
	"context"
	"errors"
	"fmt"
)

// queueVerificationEmail persists a fresh verification token for the
// user's current address and delivers the email in the background.
// The caller must have checked that a mailer is configured. When
// Config.MaxPendingEmails jobs are already in flight nothing is
// persisted and the drop is logged. Only the token persistence can
// fail here; delivery failures are logged.
func (a *Auth) queueVerificationEmail(ctx context.Context, user *User) error {
	if !a.reserveBackground(ctx, emailKindVerification, user.ID) {
		return nil
	}
	token, err := a.issueToken(ctx, purposeEmailVerification, user.Email, a.cfg.EmailVerificationTokenTTL)
	if err != nil {
		a.releaseBackground()
		return err
	}
	mailer, to := a.cfg.Mailer, user.Email
	a.runReserved(ctx, emailKindVerification, user.ID, func(ctx context.Context) error {
		return mailer.SendVerificationEmail(ctx, to, token)
	})
	return nil
}

// SendVerificationEmail creates a fresh verification token for the
// user with the given ID and emails it synchronously. It is meant for
// an authenticated "resend verification email" action where the caller
// is entitled to see the outcome.
//
// Multiple outstanding tokens are allowed: every token stays valid
// until it expires or is consumed, and consuming any one of them
// verifies the address. This makes resend buttons safe.
//
// Errors: ErrUserNotFound for an unknown user, ErrEmailAlreadyVerified
// for a verified address, ErrMailerNotConfigured without a mailer, and
// an error wrapping ErrEmailDeliveryFailed (cause preserved) when the
// mailer fails. The token is persisted before sending, so it stays
// valid when delivery fails.
func (a *Auth) SendVerificationEmail(ctx context.Context, userID string) error {
	user, err := a.store.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if user.EmailVerified {
		return ErrEmailAlreadyVerified
	}
	if a.cfg.Mailer == nil {
		return ErrMailerNotConfigured
	}
	token, err := a.issueToken(ctx, purposeEmailVerification, user.Email, a.cfg.EmailVerificationTokenTTL)
	if err != nil {
		return err
	}
	if err := a.cfg.Mailer.SendVerificationEmail(ctx, user.Email, token); err != nil {
		return fmt.Errorf("%w: %w", ErrEmailDeliveryFailed, err)
	}
	return nil
}

// RequestEmailVerification is the public, unauthenticated "send me a
// new verification link" flow, keyed by email address.
//
// It returns nil in every non-exceptional case so that neither the
// result nor the response time reveals whether an address is
// registered: for malformed or unknown addresses, for addresses that
// are already verified and when no mailer is configured. The only
// synchronous store call is the user lookup, made for every valid
// address alike; for a registered, unverified address the token is
// created and the email delivered by a background job (bounded by
// Config.MaxPendingEmails), whose store and delivery failures are
// logged, never returned. Only a failing user lookup is returned.
func (a *Auth) RequestEmailVerification(ctx context.Context, email string) error {
	if a.cfg.Mailer == nil {
		return nil
	}
	email, err := normalizeAndValidateEmail(email)
	if err != nil {
		return nil
	}
	user, err := a.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if user.EmailVerified {
		return nil
	}
	a.issueAndDeliverInBackground(ctx, emailKindVerification, user, purposeEmailVerification, a.cfg.EmailVerificationTokenTTL)
	return nil
}

// VerifyEmail consumes an email verification token. The token is
// looked up by the (email, token) pair - the email is normalized
// first - and only tokens issued for email verification are accepted:
// a password reset token yields ErrInvalidToken.
//
// The token is consumed (deleted) before the user is updated, so two
// concurrent attempts with the same token can never both succeed: the
// second yields ErrInvalidToken, as does any later attempt. A failure
// after consumption (for example a store error) leaves the token
// consumed; request a new one.
//
// An unknown token, or a token whose address now belongs to nobody or
// to an account created after the token was issued, yields
// ErrInvalidToken. An expired token yields ErrTokenExpired. When the
// user's address changes between the lookup and the update, the token
// no longer matches it and ErrInvalidToken is returned. The update is
// written with optimistic concurrency (ErrConflict after a few lost
// retries). Other outstanding verification tokens stay valid, so an
// older link still confirms the address.
func (a *Auth) VerifyEmail(ctx context.Context, email, token string) error {
	user, err := a.redeemToken(ctx, purposeEmailVerification, email, token)
	if err != nil {
		return err
	}
	_, err = a.updateUser(ctx, user.ID, func(current *User) (bool, error) {
		if current.Email != user.Email {
			// The address changed after the token was redeemed: the
			// token proves ownership of the old address only.
			return false, ErrInvalidToken
		}
		if current.EmailVerified {
			return false, nil
		}
		current.EmailVerified = true
		return true, nil
	})
	if errors.Is(err, ErrUserNotFound) {
		// Deleted meanwhile: the token has nothing left to verify.
		return ErrInvalidToken
	}
	return err
}
