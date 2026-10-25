package auth

import (
	"context"
	"errors"
)

// RequestPasswordReset starts the password reset flow for the given
// email address.
//
// It returns nil in every non-exceptional case so that neither the
// result nor the response time reveals whether an address is
// registered: for malformed or unknown addresses and when no mailer is
// configured (an error there would turn a deployment without SMTP
// into an enumeration oracle). The only synchronous store call is the
// user lookup, made for every valid address alike; for a registered
// address a background job (bounded by Config.MaxPendingEmails)
// persists a single use reset token with PasswordResetTokenTTL and
// delivers the email, so neither the token write nor SMTP latency and
// failures reach the caller. Background failures are logged. Only a
// failing user lookup is returned.
//
// Multiple outstanding tokens are allowed until one of them is used:
// a successful reset (or ChangePassword) invalidates all of them.
func (a *Auth) RequestPasswordReset(ctx context.Context, email string) error {
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
	a.issueAndDeliverInBackground(ctx, emailKindPasswordReset, user, purposePasswordReset, a.cfg.PasswordResetTokenTTL)
	return nil
}

// ResetPassword consumes a password reset token and sets a new
// password for the matching credential account.
//
// The new password is checked against the policy first
// (ErrPasswordRequired / ErrWeakPassword); a rejected password does
// not consume the token. Only tokens issued for password reset are
// accepted: an email verification token yields ErrInvalidToken, as do
// unknown tokens and tokens whose address now belongs to nobody or to
// an account created after the token was issued. An expired token
// yields ErrTokenExpired.
//
// The token is consumed (deleted) before the password is changed, so
// two concurrent attempts with the same token can never both succeed:
// the second yields ErrInvalidToken. A failure after consumption (a
// user without a credential account yields ErrAccountNotFound, a
// store error, ErrConflict after a few lost retries) leaves the token
// consumed; request a new one.
//
// On success the bcrypt hash is replaced, every session of the user is
// revoked (so all clients must sign in again) and every outstanding
// reset token of the address is deleted.
func (a *Auth) ResetPassword(ctx context.Context, email, token, newPassword string) error {
	if err := a.validatePassword(newPassword); err != nil {
		return err
	}
	user, err := a.redeemToken(ctx, purposePasswordReset, email, token)
	if err != nil {
		return err
	}
	if err := a.setCredentialPassword(ctx, user.ID, newPassword); err != nil {
		return err
	}
	if err := a.store.DeleteSessionsByUser(ctx, user.ID); err != nil {
		return err
	}
	return a.deleteTokensFor(ctx, user.Email, purposePasswordReset)
}
