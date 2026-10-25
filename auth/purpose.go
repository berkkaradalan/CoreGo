package auth

import (
	"context"
	"errors"
	"time"
)

// tokenPurpose binds a verification token to exactly one flow. The
// purpose is stored as a prefix of VerificationToken.Identifier, so a
// token issued for email verification is never found by a password
// reset lookup and vice versa.
type tokenPurpose string

// Token purposes. The string values are part of the persisted data
// format and must never change.
const (
	purposeEmailVerification tokenPurpose = "email-verification:"
	purposePasswordReset     tokenPurpose = "password-reset:"
)

// allTokenPurposes lists every purpose, for bulk cleanups that must
// not leave any kind of token behind.
var allTokenPurposes = []tokenPurpose{purposeEmailVerification, purposePasswordReset}

// identifier returns the stored identifier for a normalized email
// address.
func (p tokenPurpose) identifier(email string) string {
	return string(p) + email
}

// issueToken creates and persists a fresh single use token for email
// with the given purpose and lifetime, and returns the raw token. Only
// its SHA-256 hash is stored.
func (a *Auth) issueToken(ctx context.Context, purpose tokenPurpose, email string, ttl time.Duration) (string, error) {
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	now := a.now()
	stored := &VerificationToken{
		Identifier: purpose.identifier(email),
		ValueHash:  HashToken(token),
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}
	if err := a.store.CreateVerificationToken(ctx, stored); err != nil {
		return "", err
	}
	return token, nil
}

// redeemToken looks up the token for (purpose, email), consumes it
// and resolves the user it belongs to.
//
// The token is deleted right after the lookup, before anything else
// happens: of two concurrent redemptions of the same token only the
// one whose delete succeeds proceeds, the other sees ErrNotFound from
// the store and fails with ErrInvalidToken. The flip side is that a
// failure after this point (a store error, a missing credential
// account) leaves the token consumed; the user requests a new one.
//
// Errors:
//   - ErrInvalidToken: empty input, unknown or already consumed
//     token, a token whose address no longer belongs to any user, or a
//     token issued before the current owner of the address registered
//     (a leftover of a previous owner; this also guards against
//     cleanup races).
//   - ErrTokenExpired: the token existed but had expired (it is
//     consumed all the same).
func (a *Auth) redeemToken(ctx context.Context, purpose tokenPurpose, email, token string) (*User, error) {
	email = normalizeEmail(email)
	if email == "" || token == "" {
		return nil, ErrInvalidToken
	}
	stored, err := a.store.GetVerificationToken(ctx, purpose.identifier(email), HashToken(token))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if err := a.store.DeleteVerificationToken(ctx, stored.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			// A concurrent redemption consumed it first.
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if a.now().After(stored.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	user, err := a.store.GetUserByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrNotFound):
		// Nobody owns the address any more.
		return nil, ErrInvalidToken
	case err != nil:
		return nil, err
	case stored.CreatedAt.Before(user.CreatedAt):
		// Issued to a previous owner of the address.
		return nil, ErrInvalidToken
	default:
		return user, nil
	}
}

// deleteTokensFor removes every outstanding token of the given
// purposes for a normalized email address.
func (a *Auth) deleteTokensFor(ctx context.Context, email string, purposes ...tokenPurpose) error {
	for _, purpose := range purposes {
		if err := a.store.DeleteVerificationTokensByIdentifier(ctx, purpose.identifier(email)); err != nil {
			return err
		}
	}
	return nil
}
