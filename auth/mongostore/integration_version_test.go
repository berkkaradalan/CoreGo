package mongostore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/berkkaradalan/CoreGo/auth"
)

// concurrentWriters is the number of goroutines racing for the same
// record version in the optimistic concurrency tests.
const concurrentWriters = 16

func TestIntegrationUserVersioning(t *testing.T) {
	store, db := newIntegrationStore(t)
	ctx := context.Background()

	// The caller's Version is ignored on create.
	user := &auth.User{Name: "Ada", Email: "ada@example.com", Version: 42}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Version != 1 {
		t.Fatalf("CreateUser set Version = %d, want 1", user.Version)
	}
	if n := countDocs(t, db, store.names.users, bson.D{{Key: "_id", Value: user.ID}, {Key: "version", Value: int64(1)}}); n != 1 {
		t.Fatalf("stored document with version 1: %d, want 1", n)
	}
	got := mustGetUser(t, store, user.ID)
	if got.Version != 1 {
		t.Fatalf("GetUser Version = %d, want 1", got.Version)
	}
	byEmail, err := store.GetUserByEmail(ctx, user.Email)
	if err != nil || byEmail.Version != 1 {
		t.Fatalf("GetUserByEmail = %+v, %v; want Version 1", byEmail, err)
	}

	// A stale copy taken before the update below.
	stale := *got

	user.Name = "Ada King"
	previousUpdate := user.UpdatedAt
	time.Sleep(2 * time.Millisecond) // UpdatedAt has millisecond precision.
	if err := store.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if user.Version != 2 || !user.UpdatedAt.After(previousUpdate) {
		t.Fatalf("UpdateUser left Version %d, UpdatedAt %v (was %v)", user.Version, user.UpdatedAt, previousUpdate)
	}
	if got := mustGetUser(t, store, user.ID); !reflect.DeepEqual(got, user) {
		t.Fatalf("GetUser after update = %+v, want %+v", got, user)
	}

	// The stale copy is rejected and neither the document nor the
	// struct changes.
	stale.Name = "Lost Update"
	staleBefore := stale
	if err := store.UpdateUser(ctx, &stale); !errors.Is(err, auth.ErrConflict) {
		t.Fatalf("UpdateUser(stale) error = %v, want ErrConflict", err)
	}
	if !reflect.DeepEqual(stale, staleBefore) {
		t.Fatalf("failed UpdateUser modified the struct: %+v -> %+v", staleBefore, stale)
	}
	if got := mustGetUser(t, store, user.ID); !reflect.DeepEqual(got, user) {
		t.Fatalf("conflicting update changed the document to %+v, want %+v", got, user)
	}

	// A version ahead of the stored one conflicts as well.
	ahead := *user
	ahead.Version = user.Version + 5
	if err := store.UpdateUser(ctx, &ahead); !errors.Is(err, auth.ErrConflict) {
		t.Fatalf("UpdateUser(future version) error = %v, want ErrConflict", err)
	}

	// A missing record is reported as such, whatever the version.
	for _, version := range []int64{0, 1, 7} {
		missing := &auth.User{ID: "missing", Email: fmt.Sprintf("missing-%d@example.com", version), Version: version}
		if err := store.UpdateUser(ctx, missing); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("UpdateUser(missing, version %d) error = %v, want ErrNotFound", version, err)
		}
		if missing.Version != version {
			t.Fatalf("failed UpdateUser changed Version %d to %d", version, missing.Version)
		}
	}

	// A unique violation with the current version stays a duplicate
	// and does not consume a version.
	other := &auth.User{Name: "Bob", Email: "bob@example.com"}
	if err := store.CreateUser(ctx, other); err != nil {
		t.Fatalf("CreateUser(other): %v", err)
	}
	taken := *user
	taken.Email = other.Email
	if err := store.UpdateUser(ctx, &taken); !errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("UpdateUser(taken email) error = %v, want ErrDuplicate", err)
	}
	if taken.Version != user.Version {
		t.Fatalf("failed UpdateUser changed Version to %d", taken.Version)
	}
	current := mustGetUser(t, store, user.ID)
	if current.Version != user.Version {
		t.Fatalf("stored Version = %d after a failed update, want %d", current.Version, user.Version)
	}
	// The version check comes first: a stale write that would also
	// collide on the email is a conflict, like in auth.MemoryStore.
	staleTaken := stale
	staleTaken.Email = other.Email
	if err := store.UpdateUser(ctx, &staleTaken); !errors.Is(err, auth.ErrConflict) {
		t.Fatalf("UpdateUser(stale, taken email) error = %v, want ErrConflict", err)
	}

	// Every update increments by exactly one.
	for want := user.Version + 1; want <= user.Version+3; want++ {
		if err := store.UpdateUser(ctx, current); err != nil {
			t.Fatalf("UpdateUser(repeat): %v", err)
		}
		if current.Version != want {
			t.Fatalf("Version = %d, want %d", current.Version, want)
		}
	}

	// ListUsers returns the stored versions.
	list, err := store.ListUsers(ctx, auth.ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	versions := map[string]int64{}
	for _, u := range list {
		versions[u.ID] = u.Version
	}
	if want := map[string]int64{user.ID: current.Version, other.ID: 1}; !reflect.DeepEqual(versions, want) {
		t.Fatalf("ListUsers versions = %v, want %v", versions, want)
	}
}

func TestIntegrationAccountVersioning(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()

	account := &auth.Account{UserID: "user-1", Provider: "credential", ProviderAccountID: "ada@example.com", Password: "hash", Version: -3}
	if err := store.CreateAccount(ctx, account); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if account.Version != 1 {
		t.Fatalf("CreateAccount set Version = %d, want 1", account.Version)
	}
	for name, get := range map[string]func() (*auth.Account, error){
		"GetAccount":       func() (*auth.Account, error) { return store.GetAccount(ctx, "credential", "ada@example.com") },
		"GetAccountByUser": func() (*auth.Account, error) { return store.GetAccountByUser(ctx, "user-1", "credential") },
	} {
		got, err := get()
		if err != nil || got.Version != 1 {
			t.Fatalf("%s = %+v, %v; want Version 1", name, got, err)
		}
	}

	stale := *account

	account.Password = "new-hash"
	if err := store.UpdateAccount(ctx, account); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if account.Version != 2 {
		t.Fatalf("UpdateAccount set Version = %d, want 2", account.Version)
	}

	stale.Password = "lost-update"
	staleBefore := stale
	if err := store.UpdateAccount(ctx, &stale); !errors.Is(err, auth.ErrConflict) {
		t.Fatalf("UpdateAccount(stale) error = %v, want ErrConflict", err)
	}
	if !reflect.DeepEqual(stale, staleBefore) {
		t.Fatalf("failed UpdateAccount modified the struct: %+v -> %+v", staleBefore, stale)
	}
	list, err := store.ListAccountsByUser(ctx, "user-1")
	if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0], account) {
		t.Fatalf("ListAccountsByUser after conflict = %+v, %v; want [%+v]", list, err, account)
	}

	missing := &auth.Account{ID: "missing", UserID: "user-1", Provider: "x", ProviderAccountID: "y", Version: 1}
	if err := store.UpdateAccount(ctx, missing); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("UpdateAccount(missing) error = %v, want ErrNotFound", err)
	}
	if missing.Version != 1 {
		t.Fatalf("failed UpdateAccount changed Version to %d", missing.Version)
	}
}

// TestIntegrationConcurrentUpdatesHaveOneWinner starts many writers
// from the same version at once: exactly one may succeed and every
// other one must see auth.ErrConflict.
func TestIntegrationConcurrentUpdatesHaveOneWinner(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()

	user := &auth.User{Name: "Ada", Email: "ada@example.com"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	account := &auth.Account{UserID: user.ID, Provider: "credential", ProviderAccountID: user.Email, Password: "hash"}
	if err := store.CreateAccount(ctx, account); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	t.Run("users", func(t *testing.T) {
		winner := raceUpdates(t, func(i int) error {
			copied := *user
			copied.Name = fmt.Sprintf("writer-%d", i)
			return store.UpdateUser(ctx, &copied)
		})
		got := mustGetUser(t, store, user.ID)
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
		if err != nil {
			t.Fatalf("GetAccountByUser: %v", err)
		}
		if got.Version != 2 || got.Password != fmt.Sprintf("hash-%d", winner) {
			t.Fatalf("after the race: Version %d, Password %q; want 2 and hash-%d", got.Version, got.Password, winner)
		}
	})
}

// TestIntegrationRetriedUpdatesLoseNothing has every writer re-read and
// retry on auth.ErrConflict: all of them must eventually land, so the
// final version counts every update.
func TestIntegrationRetriedUpdatesLoseNothing(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	user := &auth.User{Name: "Ada", Email: "ada@example.com"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

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
		if err != nil {
			t.Fatalf("retried UpdateUser: %v", err)
		}
	}
	if got, want := mustGetUser(t, store, user.ID).Version, int64(1+concurrentWriters); got != want {
		t.Fatalf("Version = %d after %d retried updates, want %d", got, concurrentWriters, want)
	}
}

// mustGetUser reads a user or fails the test.
func mustGetUser(t *testing.T, store *Store, id string) *auth.User {
	t.Helper()
	user, err := store.GetUser(context.Background(), id)
	if err != nil {
		t.Fatalf("GetUser(%s): %v", id, err)
	}
	return user
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
