package pgstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
)

// concurrentWriters is the number of goroutines racing for the same
// record version in the optimistic concurrency tests.
const concurrentWriters = 16

func TestIntegrationUserVersioning(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	// The caller's Version is ignored on create.
	user := &auth.User{Name: "Ada", Email: uniqueEmail(t), Version: 42}
	wantNoErr(t, "CreateUser", store.CreateUser(ctx, user))
	if user.Version != 1 {
		t.Fatalf("CreateUser set Version = %d, want 1", user.Version)
	}
	got, err := store.GetUser(ctx, user.ID)
	wantNoErr(t, "GetUser", err)
	if got.Version != 1 {
		t.Fatalf("stored Version = %d, want 1", got.Version)
	}
	byEmail, err := store.GetUserByEmail(ctx, user.Email)
	wantNoErr(t, "GetUserByEmail", err)
	if byEmail.Version != 1 {
		t.Fatalf("GetUserByEmail Version = %d, want 1", byEmail.Version)
	}

	// A stale copy taken before the update below.
	stale := *got

	user.Name = "Ada King"
	previousUpdate := user.UpdatedAt
	wantNoErr(t, "UpdateUser", store.UpdateUser(ctx, user))
	if user.Version != 2 {
		t.Fatalf("UpdateUser set Version = %d, want 2", user.Version)
	}
	if !user.UpdatedAt.After(previousUpdate) {
		t.Fatalf("UpdatedAt not bumped: %v -> %v", previousUpdate, user.UpdatedAt)
	}
	got, err = store.GetUser(ctx, user.ID)
	wantNoErr(t, "GetUser after update", err)
	if !reflect.DeepEqual(got, user) {
		t.Fatalf("GetUser after update = %+v, want %+v", got, user)
	}

	// The stale copy is rejected and neither the row nor the struct
	// changes.
	stale.Name = "Lost Update"
	staleBefore := stale
	wantErrIs(t, "UpdateUser(stale)", store.UpdateUser(ctx, &stale), auth.ErrConflict)
	if !reflect.DeepEqual(stale, staleBefore) {
		t.Fatalf("failed UpdateUser modified the struct: %+v -> %+v", staleBefore, stale)
	}
	got, err = store.GetUser(ctx, user.ID)
	wantNoErr(t, "GetUser after conflict", err)
	if !reflect.DeepEqual(got, user) {
		t.Fatalf("conflicting update changed the row to %+v, want %+v", got, user)
	}

	// A version ahead of the stored one conflicts as well.
	ahead := *user
	ahead.Version = user.Version + 5
	wantErrIs(t, "UpdateUser(future version)", store.UpdateUser(ctx, &ahead), auth.ErrConflict)

	// A missing record is reported as such, whatever the version.
	for _, version := range []int64{0, 1, 7} {
		missing := &auth.User{ID: "missing", Email: uniqueEmail(t), Version: version}
		wantErrIs(t, fmt.Sprintf("UpdateUser(missing, version %d)", version), store.UpdateUser(ctx, missing), auth.ErrNotFound)
		if missing.Version != version {
			t.Fatalf("failed UpdateUser changed Version %d to %d", version, missing.Version)
		}
	}

	// A unique violation with the current version stays a duplicate
	// and does not consume a version.
	other := mustCreateUser(t, store, uniqueEmail(t))
	taken := *user
	taken.Email = other.Email
	wantErrIs(t, "UpdateUser(taken email)", store.UpdateUser(ctx, &taken), auth.ErrDuplicate)
	if taken.Version != user.Version {
		t.Fatalf("failed UpdateUser changed Version to %d", taken.Version)
	}
	got, err = store.GetUser(ctx, user.ID)
	wantNoErr(t, "GetUser after duplicate", err)
	if got.Version != user.Version {
		t.Fatalf("stored Version = %d after a failed update, want %d", got.Version, user.Version)
	}
	// The version check comes first: a stale write that would also
	// collide on the email is a conflict, like in auth.MemoryStore.
	staleTaken := stale
	staleTaken.Email = other.Email
	wantErrIs(t, "UpdateUser(stale, taken email)", store.UpdateUser(ctx, &staleTaken), auth.ErrConflict)

	// Every update increments by exactly one.
	for want := user.Version + 1; want <= user.Version+3; want++ {
		wantNoErr(t, "UpdateUser(repeat)", store.UpdateUser(ctx, got))
		if got.Version != want {
			t.Fatalf("Version = %d, want %d", got.Version, want)
		}
	}

	// ListUsers returns the stored versions.
	list, err := store.ListUsers(ctx, auth.ListOptions{})
	wantNoErr(t, "ListUsers", err)
	versions := map[string]int64{}
	for _, u := range list {
		versions[u.ID] = u.Version
	}
	if want := map[string]int64{user.ID: got.Version, other.ID: 1}; !reflect.DeepEqual(versions, want) {
		t.Fatalf("ListUsers versions = %v, want %v", versions, want)
	}
}

func TestIntegrationAccountVersioning(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	user := mustCreateUser(t, store, uniqueEmail(t))
	account := &auth.Account{UserID: user.ID, Provider: "credential", ProviderAccountID: user.Email, Password: "hash", Version: -3}
	wantNoErr(t, "CreateAccount", store.CreateAccount(ctx, account))
	if account.Version != 1 {
		t.Fatalf("CreateAccount set Version = %d, want 1", account.Version)
	}
	for name, get := range map[string]func() (*auth.Account, error){
		"GetAccount":       func() (*auth.Account, error) { return store.GetAccount(ctx, "credential", user.Email) },
		"GetAccountByUser": func() (*auth.Account, error) { return store.GetAccountByUser(ctx, user.ID, "credential") },
	} {
		got, err := get()
		wantNoErr(t, name, err)
		if got.Version != 1 {
			t.Fatalf("%s Version = %d, want 1", name, got.Version)
		}
	}

	stale := *account

	account.Password = "new-hash"
	previousUpdate := account.UpdatedAt
	wantNoErr(t, "UpdateAccount", store.UpdateAccount(ctx, account))
	if account.Version != 2 || !account.UpdatedAt.After(previousUpdate) {
		t.Fatalf("UpdateAccount left Version %d, UpdatedAt %v (was %v)", account.Version, account.UpdatedAt, previousUpdate)
	}

	stale.Password = "lost-update"
	staleBefore := stale
	wantErrIs(t, "UpdateAccount(stale)", store.UpdateAccount(ctx, &stale), auth.ErrConflict)
	if !reflect.DeepEqual(stale, staleBefore) {
		t.Fatalf("failed UpdateAccount modified the struct: %+v -> %+v", staleBefore, stale)
	}
	list, err := store.ListAccountsByUser(ctx, user.ID)
	wantNoErr(t, "ListAccountsByUser", err)
	if len(list) != 1 || !reflect.DeepEqual(list[0], account) {
		t.Fatalf("ListAccountsByUser after conflict = %+v, want [%+v]", list, account)
	}

	missing := &auth.Account{ID: "missing", UserID: user.ID, Provider: "x", ProviderAccountID: "y", Version: 1}
	wantErrIs(t, "UpdateAccount(missing)", store.UpdateAccount(ctx, missing), auth.ErrNotFound)
	if missing.Version != 1 {
		t.Fatalf("failed UpdateAccount changed Version to %d", missing.Version)
	}
}

// TestIntegrationConcurrentUpdatesHaveOneWinner starts many writers
// from the same version at once: exactly one may succeed and every
// other one must see auth.ErrConflict.
func TestIntegrationConcurrentUpdatesHaveOneWinner(t *testing.T) {
	t.Parallel()
	store := migratedStore(t, newSchemaPool(t, concurrentWriters), "")
	ctx := testContext(t)

	user := mustCreateUser(t, store, uniqueEmail(t))
	account := &auth.Account{UserID: user.ID, Provider: "credential", ProviderAccountID: user.Email, Password: "hash"}
	wantNoErr(t, "CreateAccount", store.CreateAccount(ctx, account))

	t.Run("users", func(t *testing.T) {
		winner := raceUpdates(t, func(i int) error {
			copied := *user
			copied.Name = fmt.Sprintf("writer-%d", i)
			return store.UpdateUser(ctx, &copied)
		})
		got, err := store.GetUser(ctx, user.ID)
		wantNoErr(t, "GetUser", err)
		if got.Version != 2 || got.Name != fmt.Sprintf("writer-%d", winner) {
			t.Fatalf("after the race: Version %d, Name %q; want 2 and writer-%d", got.Version, got.Name, winner)
		}
	})

	t.Run("accounts", func(t *testing.T) {
		winner := raceUpdates(t, func(i int) error {
			copied := *account
			copied.Password = fmt.Sprintf("hash-%d", i)
			return store.UpdateAccount(ctx, &copied)
		})
		got, err := store.GetAccountByUser(ctx, user.ID, "credential")
		wantNoErr(t, "GetAccountByUser", err)
		if got.Version != 2 || got.Password != fmt.Sprintf("hash-%d", winner) {
			t.Fatalf("after the race: Version %d, Password %q; want 2 and hash-%d", got.Version, got.Password, winner)
		}
	})
}

// TestIntegrationRetriedUpdatesLoseNothing has every writer re-read and
// retry on auth.ErrConflict: all of them must eventually land, so the
// final version counts every update.
func TestIntegrationRetriedUpdatesLoseNothing(t *testing.T) {
	t.Parallel()
	store := migratedStore(t, newSchemaPool(t, concurrentWriters), "")
	ctx := testContext(t)
	user := mustCreateUser(t, store, uniqueEmail(t))

	var wg sync.WaitGroup
	errs := make(chan error, concurrentWriters)
	for i := 0; i < concurrentWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- retryOnConflict(ctx, func() error {
				current, err := store.GetUser(ctx, user.ID)
				if err != nil {
					return err
				}
				current.Image = fmt.Sprintf("https://cdn.example.com/%d.png", current.Version)
				return store.UpdateUser(ctx, current)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		wantNoErr(t, "retried UpdateUser", err)
	}
	got, err := store.GetUser(ctx, user.ID)
	wantNoErr(t, "GetUser", err)
	if want := int64(1 + concurrentWriters); got.Version != want {
		t.Fatalf("Version = %d after %d retried updates, want %d", got.Version, concurrentWriters, want)
	}
}

// raceUpdates runs update(i) for concurrentWriters goroutines released
// at the same moment. It fails the test unless exactly one call
// succeeded and all others returned auth.ErrConflict, and returns the
// index of the winner.
func raceUpdates(t *testing.T, update func(i int) error) int {
	t.Helper()
	start := make(chan struct{})
	results := make([]error, concurrentWriters)
	var wg sync.WaitGroup
	for i := 0; i < concurrentWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = update(i)
		}()
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, err := range results {
		switch {
		case err == nil:
			if winner >= 0 {
				t.Fatalf("writers %d and %d both succeeded from the same version", winner, i)
			}
			winner = i
		case !errors.Is(err, auth.ErrConflict):
			t.Fatalf("writer %d: error = %v, want nil or auth.ErrConflict", i, err)
		}
	}
	if winner < 0 {
		t.Fatal("no writer succeeded")
	}
	return winner
}

// retryOnConflict calls attempt until it returns something other than
// auth.ErrConflict or ctx ends.
func retryOnConflict(ctx context.Context, attempt func() error) error {
	for {
		err := attempt()
		if !errors.Is(err, auth.ErrConflict) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}
