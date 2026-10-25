package auth

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// conflictingStore wraps a Store and makes the first conflicts calls
// of UpdateUser and UpdateAccount (each counted separately) fail with
// ErrConflict, as if another writer always won the race. A negative
// count conflicts forever.
type conflictingStore struct {
	Store
	userConflicts    int64
	accountConflicts int64

	userCalls    atomic.Int64
	accountCalls atomic.Int64
}

// UpdateUser implements Store.
func (s *conflictingStore) UpdateUser(ctx context.Context, user *User) error {
	n := s.userCalls.Add(1)
	if s.userConflicts < 0 || n <= s.userConflicts {
		return ErrConflict
	}
	return s.Store.UpdateUser(ctx, user)
}

// UpdateAccount implements Store.
func (s *conflictingStore) UpdateAccount(ctx context.Context, account *Account) error {
	n := s.accountCalls.Add(1)
	if s.accountConflicts < 0 || n <= s.accountConflicts {
		return ErrConflict
	}
	return s.Store.UpdateAccount(ctx, account)
}

func TestUpdatesReturnErrConflictWhenRetriesAreExhausted(t *testing.T) {
	name := "New Name"
	tests := []struct {
		name    string
		account bool // the operation's contended write is the account
		prepare func(t *testing.T, a *Auth, m *recordingMailer, out *SignUpOutput) func() error
	}{
		{name: "UpdateUser", prepare: func(t *testing.T, a *Auth, _ *recordingMailer, out *SignUpOutput) func() error {
			return func() error {
				_, err := a.UpdateUser(testContext, out.User.ID, UpdateUserInput{Name: &name})
				return err
			}
		}},
		{name: "BanUser", prepare: func(t *testing.T, a *Auth, _ *recordingMailer, out *SignUpOutput) func() error {
			return func() error { return a.BanUser(testContext, out.User.ID, "abuse", nil) }
		}},
		{name: "UnbanUser", prepare: func(t *testing.T, a *Auth, _ *recordingMailer, out *SignUpOutput) func() error {
			return func() error { return a.UnbanUser(testContext, out.User.ID) }
		}},
		{name: "ChangeEmail", prepare: func(t *testing.T, a *Auth, _ *recordingMailer, out *SignUpOutput) func() error {
			return func() error {
				_, err := a.ChangeEmail(testContext, out.User.ID, "other@example.com", testPassword)
				return err
			}
		}},
		{name: "VerifyEmail", prepare: func(t *testing.T, a *Auth, m *recordingMailer, out *SignUpOutput) func() error {
			token := lastVerificationToken(t, a, m)
			return func() error { return a.VerifyEmail(testContext, out.User.Email, token) }
		}},
		{name: "ChangePassword", account: true, prepare: func(t *testing.T, a *Auth, _ *recordingMailer, out *SignUpOutput) func() error {
			return func() error { return a.ChangePassword(testContext, out.User.ID, testPassword, "another-password") }
		}},
		{name: "ResetPassword", account: true, prepare: func(t *testing.T, a *Auth, m *recordingMailer, out *SignUpOutput) func() error {
			if err := a.RequestPasswordReset(testContext, out.User.Email); err != nil {
				t.Fatalf("RequestPasswordReset: %v", err)
			}
			token := lastResetToken(t, a, m)
			return func() error { return a.ResetPassword(testContext, out.User.Email, token, "another-password") }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _, mailer := newTestAuthWithMailer(t, Config{})
			out := signUpUser(t, a, "contended@example.com")
			op := tt.prepare(t, a, mailer, out)

			store := &conflictingStore{Store: a.store, userConflicts: -1, accountConflicts: -1}
			a.store = store
			if err := op(); !errors.Is(err, ErrConflict) {
				t.Fatalf("error = %v, want ErrConflict", err)
			}
			calls := store.userCalls.Load()
			if tt.account {
				calls = store.accountCalls.Load()
			}
			if calls != maxUpdateAttempts {
				t.Errorf("write attempts = %d, want %d", calls, maxUpdateAttempts)
			}

			// Nothing was changed: the user is still as created and can
			// sign in with the old password.
			user, err := store.Store.GetUser(testContext, out.User.ID)
			if err != nil {
				t.Fatalf("GetUser: %v", err)
			}
			if user.Version != 1 || user.Banned || user.Email != "contended@example.com" || user.Name != out.User.Name {
				t.Errorf("user changed despite ErrConflict: %+v", user)
			}
			a.store = store.Store
			if _, err := a.SignIn(testContext, SignInInput{Email: "contended@example.com", Password: testPassword}); err != nil {
				t.Errorf("SignIn with the old password: %v", err)
			}
		})
	}
}

func TestUpdatesSucceedOnTheLastRetry(t *testing.T) {
	a, _, _ := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "eventually@example.com")
	store := &conflictingStore{Store: a.store, userConflicts: maxUpdateAttempts - 1, accountConflicts: maxUpdateAttempts - 1}
	a.store = store

	if err := a.BanUser(testContext, out.User.ID, "late", nil); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	if got := store.userCalls.Load(); got != maxUpdateAttempts {
		t.Errorf("UpdateUser calls = %d, want %d", got, maxUpdateAttempts)
	}
	user, _ := a.GetUser(testContext, out.User.ID)
	if !user.Banned || user.BanReason != "late" || user.Version != 2 {
		t.Errorf("user = banned %v reason %q v%d, want banned, %q, v2", user.Banned, user.BanReason, user.Version, "late")
	}

	if err := a.ChangePassword(testContext, out.User.ID, testPassword, "eventually-new"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if got := store.accountCalls.Load(); got != maxUpdateAttempts {
		t.Errorf("UpdateAccount calls = %d, want %d", got, maxUpdateAttempts)
	}
}

func TestUpdateUserHelperEdgeCases(t *testing.T) {
	t.Run("unknown user", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		_, err := a.updateUser(testContext, "missing", func(*User) (bool, error) {
			t.Fatal("mutate called for a missing user")
			return false, nil
		})
		if !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("error = %v, want ErrUserNotFound", err)
		}
	})
	t.Run("deleted between read and write", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		out := signUpUser(t, a, "vanishing@example.com")
		_, err := a.updateUser(testContext, out.User.ID, func(u *User) (bool, error) {
			if err := a.store.DeleteUser(testContext, u.ID); err != nil {
				t.Fatalf("DeleteUser: %v", err)
			}
			return true, nil
		})
		if !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("error = %v, want ErrUserNotFound", err)
		}
	})
	t.Run("mutate error aborts without writing", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		out := signUpUser(t, a, "abort@example.com")
		sentinel := errors.New("rejected")
		if _, err := a.updateUser(testContext, out.User.ID, func(*User) (bool, error) { return true, sentinel }); !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want the mutate error", err)
		}
		if user, _ := a.GetUser(testContext, out.User.ID); user.Version != 1 {
			t.Errorf("Version = %d, want 1 (no write)", user.Version)
		}
	})
	t.Run("skip write returns the record as read", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		out := signUpUser(t, a, "skip@example.com")
		user, err := a.updateUser(testContext, out.User.ID, func(*User) (bool, error) { return false, nil })
		if err != nil || user.ID != out.User.ID || user.Version != 1 {
			t.Fatalf("updateUser = %+v, %v; want the unchanged user", user, err)
		}
	})
	t.Run("duplicate email maps to ErrUserAlreadyExists", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		signUpUser(t, a, "taken@example.com")
		out := signUpUser(t, a, "mover@example.com")
		_, err := a.updateUser(testContext, out.User.ID, func(u *User) (bool, error) {
			u.Email = "taken@example.com"
			return true, nil
		})
		if !errors.Is(err, ErrUserAlreadyExists) {
			t.Fatalf("error = %v, want ErrUserAlreadyExists", err)
		}
	})
}

func TestUpdateAccountHelperEdgeCases(t *testing.T) {
	t.Run("no credential account", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		_, err := a.updateAccount(testContext, "missing", CredentialProvider, func(*Account) (bool, error) {
			t.Fatal("mutate called for a missing account")
			return false, nil
		})
		if !errors.Is(err, ErrAccountNotFound) {
			t.Fatalf("error = %v, want ErrAccountNotFound", err)
		}
	})
	t.Run("deleted between read and write", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		out := signUpUser(t, a, "gone@example.com")
		_, err := a.updateAccount(testContext, out.User.ID, CredentialProvider, func(acc *Account) (bool, error) {
			if err := a.store.DeleteAccount(testContext, acc.ID); err != nil {
				t.Fatalf("DeleteAccount: %v", err)
			}
			return true, nil
		})
		if !errors.Is(err, ErrAccountNotFound) {
			t.Fatalf("error = %v, want ErrAccountNotFound", err)
		}
	})
	t.Run("store failure is returned unchanged", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		out := signUpUser(t, a, "fail@example.com")
		a.store = &faultyStore{Store: a.store, err: errStoreFailure, updateAccount: true}
		_, err := a.updateAccount(testContext, out.User.ID, CredentialProvider, func(*Account) (bool, error) { return true, nil })
		if !errors.Is(err, errStoreFailure) {
			t.Fatalf("error = %v, want the store failure", err)
		}
	})
}

// TestChangeEmailSyncFailureRevertsOnlyItsOwnChange: when the account
// sync fails, the user is put back on the old address, unless the
// address was changed again meanwhile.
func TestChangeEmailSyncFailureRevertsOnlyItsOwnChange(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "revert@example.com")
	a.store = &faultyStore{Store: a.store, err: errStoreFailure, updateAccount: true}
	if _, err := a.ChangeEmail(testContext, out.User.ID, "next@example.com", testPassword); !errors.Is(err, errStoreFailure) {
		t.Fatalf("ChangeEmail = %v, want the store failure", err)
	}
	user, _ := a.GetUser(testContext, out.User.ID)
	if user.Email != "revert@example.com" || user.Version != 3 {
		t.Errorf("user = %q v%d, want the old address after change and revert (v3)", user.Email, user.Version)
	}
}
