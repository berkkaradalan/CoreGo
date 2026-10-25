package auth

import (
	"context"
	"errors"
)

// UpdateUserInput describes changes to a user profile. Nil fields are
// left unchanged. The email address is not part of the profile: it is
// a credential and changes through ChangeEmail, which requires the
// current password.
type UpdateUserInput struct {
	// Name replaces the display name. Surrounding whitespace is
	// trimmed; an empty name is rejected with ErrNameRequired and a
	// name longer than 256 characters with ErrNameTooLong.
	Name *string

	// Image replaces the avatar URL. Surrounding whitespace is
	// trimmed; the result must be empty (removes the image) or an
	// absolute http or https URL of at most 2048 bytes, otherwise
	// ErrInvalidImageURL is returned.
	Image *string
}

// GetUser returns the user with the given ID. Unknown IDs yield
// ErrUserNotFound.
func (a *Auth) GetUser(ctx context.Context, userID string) (*User, error) {
	user, err := a.store.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return user, nil
}

// UpdateUser applies the non-nil fields of in to the user with the
// given ID and returns the updated record.
//
// The input is validated first (ErrNameRequired, ErrNameTooLong,
// ErrInvalidImageURL; see UpdateUserInput). Unknown IDs yield
// ErrUserNotFound. The change is applied to the freshly read record
// and written with optimistic concurrency, so it never reverts a
// concurrent change such as a ban or an email change; when the user
// keeps changing underneath, ErrConflict is returned after a few
// retries.
func (a *Auth) UpdateUser(ctx context.Context, userID string, in UpdateUserInput) (*User, error) {
	var name, image string
	if in.Name != nil {
		var err error
		if name, err = normalizeName(*in.Name); err != nil {
			return nil, err
		}
	}
	if in.Image != nil {
		var err error
		if image, err = normalizeImage(*in.Image); err != nil {
			return nil, err
		}
	}
	return a.updateUser(ctx, userID, func(user *User) (bool, error) {
		if in.Name != nil {
			user.Name = name
		}
		if in.Image != nil {
			user.Image = image
		}
		return true, nil
	})
}

// DeleteUser removes the user with the given ID together with
// everything that belongs to it: the service first deletes every
// outstanding verification and password reset token of the user's
// address, then the store deletes the user and cascades to its
// accounts and sessions. Unknown IDs yield ErrUserNotFound.
//
// DeleteUser does not ask for a password; it is the building block for
// both admin deletions and self-service deletion. For the latter,
// confirm the password with CheckPassword first (the HTTP layer does).
func (a *Auth) DeleteUser(ctx context.Context, userID string) error {
	user, err := a.store.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if err := a.deleteTokensFor(ctx, user.Email, allTokenPurposes...); err != nil {
		return err
	}
	if err := a.store.DeleteUser(ctx, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

// ListAccounts returns all provider accounts linked to the user,
// oldest first.
func (a *Auth) ListAccounts(ctx context.Context, userID string) ([]*Account, error) {
	accounts, err := a.store.ListAccountsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if accounts == nil {
		accounts = []*Account{}
	}
	return accounts, nil
}
