package auth

import (
	"context"
	"errors"
)

// ChangeEmail moves the user's login address to newEmail after
// verifying currentPassword, and returns the updated user.
//
// Steps and errors, in order:
//   - newEmail is normalized and validated (ErrEmailRequired,
//     ErrInvalidEmail).
//   - currentPassword is verified (ErrUserNotFound, ErrAccountNotFound,
//     ErrInvalidCurrentPassword; see CheckPassword). The password is
//     checked before the address is looked up, so the method cannot be
//     used to probe which addresses are registered.
//   - The same address (after normalization) returns the user
//     unchanged, including its verification state.
//   - An address owned by another user yields ErrUserAlreadyExists,
//     including when a concurrent sign-up or change wins the race.
//
// On success EmailVerified is reset to false, the credential account's
// provider id follows the new address, and every outstanding
// verification and password reset token of the OLD address is deleted,
// so a link sent there can never act on whoever owns that address
// next. When a mailer is configured a verification email for the new
// address is delivered in the background. Sessions are kept; with
// Config.RequireVerifiedEmail enabled they stop working
// (ErrEmailNotVerified) until the new address is verified.
//
// Both writes use optimistic concurrency, so a concurrent change of
// the same user (for example a profile update) can neither revert the
// new address nor leave the credential account pointing at a
// different address than the user. ErrConflict is returned when
// concurrent changes win every retry.
func (a *Auth) ChangeEmail(ctx context.Context, userID, newEmail, currentPassword string) (*User, error) {
	email, err := normalizeAndValidateEmail(newEmail)
	if err != nil {
		return nil, err
	}
	user, err := a.authorizeWithPassword(ctx, userID, currentPassword)
	if err != nil {
		return nil, err
	}
	if email == user.Email {
		return user, nil
	}
	if _, err := a.store.GetUserByEmail(ctx, email); err == nil {
		return nil, ErrUserAlreadyExists
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Invalidate the old address's tokens before it is released. Nothing
	// has changed yet, so a failure here simply aborts.
	if err := a.deleteTokensFor(ctx, user.Email, allTokenPurposes...); err != nil {
		return nil, err
	}

	// The retry helper may apply the change to a newer version of the
	// user than the one authorized above; remember what was replaced in
	// the version that was actually written.
	var oldEmail string
	var wasVerified bool
	updated, err := a.updateUser(ctx, userID, func(current *User) (bool, error) {
		oldEmail, wasVerified = current.Email, current.EmailVerified
		if current.Email == email {
			// A concurrent identical change already did the work.
			return false, nil
		}
		current.Email = email
		current.EmailVerified = false
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if oldEmail == email {
		return updated, nil
	}

	if err := a.syncCredentialAccountEmail(ctx, userID); err != nil {
		// Put the user back on the old address so user and credential
		// account stay consistent, unless somebody changed the address
		// again meanwhile. Best effort: the sync error is what the
		// caller needs to see.
		_, _ = a.updateUser(ctx, userID, func(current *User) (bool, error) {
			if current.Email != email {
				return false, nil
			}
			current.Email = oldEmail
			current.EmailVerified = wasVerified
			return true, nil
		})
		if errors.Is(err, ErrDuplicate) {
			return nil, ErrUserAlreadyExists
		}
		return nil, err
	}

	// A request that read the user just before the change may have
	// issued a token for the old address after the first cleanup.
	// Sweep once more; the change itself already succeeded, so a
	// failure is logged instead of returned. Tokens that still slip
	// through are rejected on use because they predate whoever
	// registers the address next (see redeemToken).
	if err := a.deleteTokensFor(ctx, oldEmail, allTokenPurposes...); err != nil {
		a.logger().ErrorContext(ctx, "auth: could not delete the previous address's tokens after an email change",
			"user_id", userID, "error", err.Error())
	}

	if a.cfg.Mailer != nil {
		if err := a.queueVerificationEmail(ctx, updated); err != nil {
			a.logger().ErrorContext(ctx, "auth: could not issue the verification token for a changed email address",
				"user_id", userID, "error", err.Error())
		}
	}
	return updated, nil
}

// syncCredentialAccountEmail aligns the provider account id of the
// user's credential account with the user's current login email, so
// the (provider, providerAccountID) uniqueness stays meaningful after
// an email change. Users without a credential account are skipped.
//
// Every attempt reads the account first and the user second and
// copies the user's address, so whichever sync writes last has seen
// the latest address: concurrent email changes cannot leave the
// account on an address the user no longer has.
func (a *Auth) syncCredentialAccountEmail(ctx context.Context, userID string) error {
	_, err := a.updateAccount(ctx, userID, CredentialProvider, func(account *Account) (bool, error) {
		user, err := a.store.GetUser(ctx, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return false, ErrUserNotFound
			}
			return false, err
		}
		if account.ProviderAccountID == user.Email {
			return false, nil
		}
		account.ProviderAccountID = user.Email
		return true, nil
	})
	if errors.Is(err, ErrAccountNotFound) {
		return nil
	}
	return err
}
