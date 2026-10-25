package auth

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryStoreUserVersioning(t *testing.T) {
	store := NewMemoryStore()

	// Create sets Version to 1 whatever the caller passed.
	user := &User{Name: "Ada", Email: "ada@example.com", Version: 42}
	if err := store.CreateUser(testContext, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Version != 1 {
		t.Fatalf("Version after create = %d, want 1", user.Version)
	}

	// Every read carries the stored Version.
	assertUserVersion(t, store, user.ID, "ada@example.com", 1)

	// A matching Version updates and increments, in the store and on
	// the passed struct, and bumps UpdatedAt on both.
	read, _ := store.GetUser(testContext, user.ID)
	read.UpdatedAt = time.Time{}
	read.Name = "Ada Lovelace"
	if err := store.UpdateUser(testContext, read); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if read.Version != 2 {
		t.Errorf("passed Version after update = %d, want 2", read.Version)
	}
	if read.UpdatedAt.IsZero() || read.UpdatedAt.Location() != time.UTC {
		t.Errorf("passed UpdatedAt = %v, want the current UTC time", read.UpdatedAt)
	}
	assertUserVersion(t, store, user.ID, "ada@example.com", 2)

	// The stale copy from before the update conflicts and changes
	// nothing.
	user.Name = "Stale Write"
	if err := store.UpdateUser(testContext, user); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale UpdateUser = %v, want ErrConflict", err)
	}
	if user.Version != 1 {
		t.Errorf("stale struct Version = %d, want it untouched (1)", user.Version)
	}
	stored, _ := store.GetUser(testContext, user.ID)
	if stored.Name != "Ada Lovelace" || stored.Version != 2 {
		t.Errorf("stored = %q v%d after a conflict, want %q v2", stored.Name, stored.Version, "Ada Lovelace")
	}

	// A Version from the future conflicts as well.
	stored.Version = 99
	if err := store.UpdateUser(testContext, stored); !errors.Is(err, ErrConflict) {
		t.Errorf("future Version UpdateUser = %v, want ErrConflict", err)
	}

	// A missing record is ErrNotFound, never ErrConflict.
	if err := store.UpdateUser(testContext, &User{ID: "missing", Email: "x@example.com", Version: 1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateUser on a missing user = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreUserVersionCheckedBeforeUniqueness(t *testing.T) {
	store := NewMemoryStore()
	first := &User{Email: "first@example.com"}
	second := &User{Email: "second@example.com"}
	for _, u := range []*User{first, second} {
		if err := store.CreateUser(testContext, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
	}

	// Stale and duplicate at once: the stale Version wins, so the
	// service retries on fresh data instead of mapping a duplicate.
	stale := *second
	stale.Version = 0
	stale.Email = first.Email
	if err := store.UpdateUser(testContext, &stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale duplicate UpdateUser = %v, want ErrConflict", err)
	}

	// A current Version with a duplicate email is ErrDuplicate and
	// does not consume a Version.
	current := *second
	current.Email = first.Email
	if err := store.UpdateUser(testContext, &current); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate UpdateUser = %v, want ErrDuplicate", err)
	}
	if current.Version != 1 {
		t.Errorf("Version after a rejected update = %d, want 1", current.Version)
	}
	assertUserVersion(t, store, second.ID, "second@example.com", 1)
}

func TestMemoryStoreAccountVersioning(t *testing.T) {
	store := NewMemoryStore()
	account := &Account{UserID: "u1", Provider: CredentialProvider, ProviderAccountID: "ada@example.com", Version: -3}
	if err := store.CreateAccount(testContext, account); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if account.Version != 1 {
		t.Fatalf("Version after create = %d, want 1", account.Version)
	}
	assertAccountVersion(t, store, account, 1)

	read, _ := store.GetAccountByUser(testContext, "u1", CredentialProvider)
	read.Password = "hash"
	if err := store.UpdateAccount(testContext, read); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if read.Version != 2 || read.UpdatedAt.IsZero() {
		t.Errorf("passed account after update = v%d at %v, want v2 with UpdatedAt", read.Version, read.UpdatedAt)
	}
	assertAccountVersion(t, store, account, 2)

	account.ProviderAccountID = "stale@example.com"
	if err := store.UpdateAccount(testContext, account); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale UpdateAccount = %v, want ErrConflict", err)
	}
	if _, err := store.GetAccount(testContext, CredentialProvider, "stale@example.com"); !errors.Is(err, ErrNotFound) {
		t.Error("a conflicting update re-indexed the account")
	}
	if err := store.UpdateAccount(testContext, &Account{ID: "missing", Version: 1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateAccount on a missing account = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreReturnedVersionIsACopy(t *testing.T) {
	store := NewMemoryStore()
	user := &User{Email: "copy@example.com"}
	if err := store.CreateUser(testContext, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Changing the caller's struct after create, or a returned copy,
	// never moves the stored Version.
	user.Version = 50
	read, _ := store.GetUser(testContext, user.ID)
	read.Version = 60
	assertUserVersion(t, store, user.ID, "copy@example.com", 1)
}

// TestMemoryStoreCompareAndSwapIsAtomic lets many writers race with the
// same Version: exactly one may win, every other one must conflict.
func TestMemoryStoreCompareAndSwapIsAtomic(t *testing.T) {
	store := NewMemoryStore()
	user := &User{Email: "cas@example.com"}
	if err := store.CreateUser(testContext, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	account := &Account{UserID: user.ID, Provider: CredentialProvider, ProviderAccountID: user.Email}
	if err := store.CreateAccount(testContext, account); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	const writers = 16
	var wg sync.WaitGroup
	userErrs := make([]error, writers)
	accountErrs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			u := *user
			u.Name = "writer"
			userErrs[i] = store.UpdateUser(testContext, &u)
			acc := *account
			acc.Password = "hash"
			accountErrs[i] = store.UpdateAccount(testContext, &acc)
		}(i)
	}
	wg.Wait()

	for name, errs := range map[string][]error{"UpdateUser": userErrs, "UpdateAccount": accountErrs} {
		wins := 0
		for _, err := range errs {
			switch {
			case err == nil:
				wins++
			case !errors.Is(err, ErrConflict):
				t.Errorf("%s: unexpected error %v", name, err)
			}
		}
		if wins != 1 {
			t.Errorf("%s: %d writers won with the same Version, want exactly 1", name, wins)
		}
	}
	assertUserVersion(t, store, user.ID, user.Email, 2)
	assertAccountVersion(t, store, account, 2)
}

// assertUserVersion checks the Version returned by every user read.
func assertUserVersion(t *testing.T, store *MemoryStore, id, email string, want int64) {
	t.Helper()
	byID, err := store.GetUser(testContext, id)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	byEmail, err := store.GetUserByEmail(testContext, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	listed, err := store.ListUsers(testContext, ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	var fromList *User
	for _, u := range listed {
		if u.ID == id {
			fromList = u
		}
	}
	if fromList == nil {
		t.Fatalf("ListUsers lacks user %s", id)
	}
	for source, got := range map[string]int64{"GetUser": byID.Version, "GetUserByEmail": byEmail.Version, "ListUsers": fromList.Version} {
		if got != want {
			t.Errorf("%s Version = %d, want %d", source, got, want)
		}
	}
}

// assertAccountVersion checks the Version returned by every account
// read, looking the account up by its current stored keys.
func assertAccountVersion(t *testing.T, store *MemoryStore, account *Account, want int64) {
	t.Helper()
	byUser, err := store.GetAccountByUser(testContext, account.UserID, account.Provider)
	if err != nil {
		t.Fatalf("GetAccountByUser: %v", err)
	}
	byProvider, err := store.GetAccount(testContext, byUser.Provider, byUser.ProviderAccountID)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	listed, err := store.ListAccountsByUser(testContext, account.UserID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListAccountsByUser = %d accounts, %v", len(listed), err)
	}
	for source, got := range map[string]int64{"GetAccountByUser": byUser.Version, "GetAccount": byProvider.Version, "ListAccountsByUser": listed[0].Version} {
		if got != want {
			t.Errorf("%s Version = %d, want %d", source, got, want)
		}
	}
}
