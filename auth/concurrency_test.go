package auth

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// TestConcurrentSignUpSignInSignOut drives the full happy path from
// many goroutines at once; the -race detector guards the store and the
// service against data races.
func TestConcurrentSignUpSignInSignOut(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	const workers = 8
	const iterations = 2

	var wg sync.WaitGroup
	errs := make(chan error, workers*iterations)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				email := fmt.Sprintf("worker%d-%d@example.com", worker, i)
				out, err := a.SignUp(testContext, SignUpInput{
					Name:     fmt.Sprintf("Worker %d", worker),
					Email:    email,
					Password: testPassword,
				})
				if err != nil {
					errs <- fmt.Errorf("SignUp(%s): %w", email, err)
					return
				}

				signIn, err := a.SignIn(testContext, SignInInput{Email: email, Password: testPassword})
				if err != nil {
					errs <- fmt.Errorf("SignIn(%s): %w", email, err)
					return
				}
				if _, _, err := a.GetSession(testContext, signIn.SessionToken); err != nil {
					errs <- fmt.Errorf("GetSession(%s): %w", email, err)
					return
				}
				sessions, err := a.ListSessions(testContext, out.User.ID)
				if err != nil {
					errs <- fmt.Errorf("ListSessions(%s): %w", email, err)
					return
				}
				if len(sessions) != 2 {
					errs <- fmt.Errorf("ListSessions(%s) = %d sessions, want 2", email, len(sessions))
					return
				}
				if err := a.SignOut(testContext, out.SessionToken); err != nil {
					errs <- fmt.Errorf("SignOut(%s): %w", email, err)
					return
				}
				if _, _, err := a.GetSession(testContext, out.SessionToken); !errors.Is(err, ErrSessionNotFound) {
					errs <- fmt.Errorf("GetSession after SignOut(%s): %w", email, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	if n, _ := a.CountUsers(testContext); n != workers*iterations {
		t.Errorf("CountUsers = %d, want %d", n, workers*iterations)
	}
}

// TestConcurrentDuplicateSignUp checks the race-safe duplicate
// mapping: when several sign-ups with the same email run in parallel,
// exactly one wins and the rest get ErrUserAlreadyExists.
func TestConcurrentDuplicateSignUp(t *testing.T) {
	for round := 0; round < 3; round++ {
		a, _ := newTestAuth(t, Config{})
		email := fmt.Sprintf("dupe%d@example.com", round)

		const contenders = 4
		var wg sync.WaitGroup
		results := make([]error, contenders)
		for i := 0; i < contenders; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, results[i] = a.SignUp(testContext, SignUpInput{
					Name:     "Contender",
					Email:    email,
					Password: testPassword,
				})
			}(i)
		}
		wg.Wait()

		var successes, duplicates int
		for _, err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrUserAlreadyExists):
				duplicates++
			default:
				t.Fatalf("round %d: unexpected error: %v", round, err)
			}
		}
		if successes != 1 || duplicates != contenders-1 {
			t.Fatalf("round %d: successes = %d, duplicates = %d, want 1 and %d",
				round, successes, duplicates, contenders-1)
		}
	}
}

// TestConcurrentMemoryStoreMixed hammers the store with writers and
// readers across all entity types.
func TestConcurrentMemoryStoreMixed(t *testing.T) {
	store := NewMemoryStore()
	const writers = 4
	const perWriter = 25

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				user := &User{
					Name:  fmt.Sprintf("User %d-%d", worker, i),
					Email: fmt.Sprintf("mixed-%d-%d@example.com", worker, i),
				}
				if err := store.CreateUser(testContext, user); err != nil {
					t.Errorf("CreateUser: %v", err)
					return
				}
				account := &Account{
					UserID:            user.ID,
					Provider:          "credential",
					ProviderAccountID: user.Email,
				}
				if err := store.CreateAccount(testContext, account); err != nil {
					t.Errorf("CreateAccount: %v", err)
					return
				}
				session := &Session{UserID: user.ID, TokenHash: HashToken(fmt.Sprintf("mixed-%d-%d", worker, i))}
				if err := store.CreateSession(testContext, session); err != nil {
					t.Errorf("CreateSession: %v", err)
					return
				}
				token := &VerificationToken{
					Identifier: user.Email,
					ValueHash:  HashToken(fmt.Sprintf("mixedtok-%d-%d", worker, i)),
				}
				if err := store.CreateVerificationToken(testContext, token); err != nil {
					t.Errorf("CreateVerificationToken: %v", err)
					return
				}
				if _, err := store.ListUsers(testContext, ListOptions{Limit: 10}); err != nil {
					t.Errorf("ListUsers: %v", err)
					return
				}
				if _, err := store.ListSessionsByUser(testContext, user.ID); err != nil {
					t.Errorf("ListSessionsByUser: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	if n, _ := store.CountUsers(testContext); n != writers*perWriter {
		t.Errorf("CountUsers = %d, want %d", n, writers*perWriter)
	}
	if got := sessionCount(store); got != writers*perWriter {
		t.Errorf("sessionCount = %d, want %d", got, writers*perWriter)
	}
	if got := tokenCount(store); got != writers*perWriter {
		t.Errorf("tokenCount = %d, want %d", got, writers*perWriter)
	}
	if got := accountCount(store); got != writers*perWriter {
		t.Errorf("accountCount = %d, want %d", got, writers*perWriter)
	}
}
