package auth

import (
	"context"
	"errors"
)

// maxUpdateAttempts is how often a read-modify-write of a user or an
// account is tried before the service gives up with ErrConflict. Every
// attempt re-reads the record, so a lost race only costs one more
// round trip; five attempts make a persistent failure practically
// impossible without a pathological write storm on one record.
const maxUpdateAttempts = 5

// updateUser applies mutate to the freshly read user with the given ID
// and writes the result with the store's optimistic concurrency check.
// When a concurrent write wins (ErrConflict) the user is read again and
// mutate is applied again to the new state, up to maxUpdateAttempts
// times; mutate must therefore be safe to call repeatedly and derive
// its changes from the record it receives.
//
// mutate reports whether anything must be written; returning false
// skips the write and returns the user as read. An error from mutate
// aborts and is returned unchanged.
//
// Errors: ErrUserNotFound when the user does not exist (or was deleted
// meanwhile), ErrUserAlreadyExists when the write violates the unique
// email constraint, ErrConflict when every attempt lost the race, and
// store errors as they are.
func (a *Auth) updateUser(ctx context.Context, userID string, mutate func(*User) (bool, error)) (*User, error) {
	for attempt := 0; attempt < maxUpdateAttempts; attempt++ {
		user, err := a.store.GetUser(ctx, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrUserNotFound
			}
			return nil, err
		}
		write, err := mutate(user)
		if err != nil {
			return nil, err
		}
		if !write {
			return user, nil
		}
		user.UpdatedAt = a.now()
		err = a.store.UpdateUser(ctx, user)
		switch {
		case err == nil:
			return user, nil
		case errors.Is(err, ErrConflict):
			continue
		case errors.Is(err, ErrNotFound):
			return nil, ErrUserNotFound
		case errors.Is(err, ErrDuplicate):
			return nil, ErrUserAlreadyExists
		default:
			return nil, err
		}
	}
	return nil, ErrConflict
}

// updateAccount is updateUser for the user's account of the given
// provider. mutate follows the same rules.
//
// Errors: ErrAccountNotFound when the account does not exist (or was
// deleted meanwhile), ErrConflict when every attempt lost the race,
// and store errors (including ErrDuplicate) as they are.
func (a *Auth) updateAccount(ctx context.Context, userID, provider string, mutate func(*Account) (bool, error)) (*Account, error) {
	for attempt := 0; attempt < maxUpdateAttempts; attempt++ {
		account, err := a.store.GetAccountByUser(ctx, userID, provider)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrAccountNotFound
			}
			return nil, err
		}
		write, err := mutate(account)
		if err != nil {
			return nil, err
		}
		if !write {
			return account, nil
		}
		account.UpdatedAt = a.now()
		err = a.store.UpdateAccount(ctx, account)
		switch {
		case err == nil:
			return account, nil
		case errors.Is(err, ErrConflict):
			continue
		case errors.Is(err, ErrNotFound):
			return nil, ErrAccountNotFound
		default:
			return nil, err
		}
	}
	return nil, ErrConflict
}
