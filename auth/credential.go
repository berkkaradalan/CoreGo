package auth

import (
	"context"
	"errors"
)

// CheckPassword reports whether password is the current password of
// the user's credential account. Use it to confirm sensitive actions
// such as deleting an account.
//
// Errors: ErrUserNotFound for an unknown user, ErrAccountNotFound when
// the user has no credential account (for example an OAuth-only user)
// and ErrInvalidCurrentPassword when the password does not match.
func (a *Auth) CheckPassword(ctx context.Context, userID, password string) error {
	_, err := a.authorizeWithPassword(ctx, userID, password)
	return err
}

// authorizeWithPassword loads the user and verifies password against
// the credential account, returning the user on success. Errors match
// CheckPassword.
func (a *Auth) authorizeWithPassword(ctx context.Context, userID, password string) (*User, error) {
	user, err := a.store.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	account, err := a.store.GetAccountByUser(ctx, userID, CredentialProvider)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	if !VerifyPassword(account.Password, password) {
		return nil, ErrInvalidCurrentPassword
	}
	return user, nil
}

// ChangePassword replaces the password of the user's credential
// account after verifying the current password, then revokes all of
// the user's sessions so every client must sign in again, and
// invalidates any outstanding password reset tokens.
//
// The current password is checked first (ErrUserNotFound,
// ErrAccountNotFound, ErrInvalidCurrentPassword as in CheckPassword),
// then the new password must satisfy the policy (ErrPasswordRequired /
// ErrWeakPassword). Setting newPassword equal to the current password
// is allowed: bcrypt salts every hash so the stored value still
// changes. ErrConflict is returned when concurrent account changes win
// every retry of the write.
func (a *Auth) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	user, err := a.authorizeWithPassword(ctx, userID, currentPassword)
	if err != nil {
		return err
	}
	if err := a.validatePassword(newPassword); err != nil {
		return err
	}
	if err := a.setCredentialPassword(ctx, userID, newPassword); err != nil {
		return err
	}
	if err := a.store.DeleteSessionsByUser(ctx, userID); err != nil {
		return err
	}
	return a.deleteTokensFor(ctx, user.Email, purposePasswordReset)
}

// setCredentialPassword hashes password and stores it on the user's
// credential account through the optimistic concurrency retry helper,
// so a concurrent account change (for example the provider id sync of
// an email change) is never overwritten. A user without a credential
// account yields ErrAccountNotFound; ErrConflict is returned when
// concurrent changes win every retry.
func (a *Auth) setCredentialPassword(ctx context.Context, userID, password string) error {
	// Hash once: bcrypt is deliberately slow and the hash does not
	// depend on the account state.
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = a.updateAccount(ctx, userID, CredentialProvider, func(account *Account) (bool, error) {
		account.Password = hash
		return true, nil
	})
	return err
}
