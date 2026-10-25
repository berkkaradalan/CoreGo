package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// This file holds deterministic regression tests for concurrency
// bugs (lost updates, double spending). Instead of hoping for an unlucky
// schedule, pausingStore parks one request right after a read so a
// competing request can run to completion in between; the test then
// lets the parked request continue and checks the final state.

// pausePoint parks one store call. reached is closed when the call has
// been answered by the wrapped store and is parked; closing release
// lets it return.
type pausePoint struct {
	method  string
	skip    int
	reached chan struct{}
	release chan struct{}
}

// pausingStore wraps a Store and parks selected read calls right after
// the wrapped store answered them.
type pausingStore struct {
	Store

	mu     sync.Mutex
	points []*pausePoint
}

// pauseAfter arms a pause point for method: the call after the next
// skip calls of method parks. The point fires once.
func (s *pausingStore) pauseAfter(t *testing.T, method string, skip int) *pausePoint {
	t.Helper()
	p := &pausePoint{method: method, skip: skip, reached: make(chan struct{}), release: make(chan struct{})}
	s.mu.Lock()
	s.points = append(s.points, p)
	s.mu.Unlock()
	// Never leave a goroutine parked when a test fails early.
	t.Cleanup(func() {
		select {
		case <-p.release:
		default:
			close(p.release)
		}
	})
	return p
}

// park blocks the calling store method when an armed point matches.
func (s *pausingStore) park(method string) {
	s.mu.Lock()
	var hit *pausePoint
	for i, p := range s.points {
		if p.method != method {
			continue
		}
		if p.skip > 0 {
			p.skip--
			break
		}
		hit = p
		s.points = append(s.points[:i], s.points[i+1:]...)
		break
	}
	s.mu.Unlock()
	if hit != nil {
		close(hit.reached)
		<-hit.release
	}
}

// GetUser implements Store.
func (s *pausingStore) GetUser(ctx context.Context, id string) (*User, error) {
	user, err := s.Store.GetUser(ctx, id)
	s.park("GetUser")
	return user, err
}

// GetAccountByUser implements Store.
func (s *pausingStore) GetAccountByUser(ctx context.Context, userID, provider string) (*Account, error) {
	account, err := s.Store.GetAccountByUser(ctx, userID, provider)
	s.park("GetAccountByUser")
	return account, err
}

// GetVerificationToken implements Store.
func (s *pausingStore) GetVerificationToken(ctx context.Context, identifier, valueHash string) (*VerificationToken, error) {
	token, err := s.Store.GetVerificationToken(ctx, identifier, valueHash)
	s.park("GetVerificationToken")
	return token, err
}

// newPausingAuth builds a test service on a pausingStore over a fresh
// MemoryStore.
func newPausingAuth(t *testing.T, cfg Config) (*Auth, *pausingStore, *recordingMailer) {
	t.Helper()
	a, _, mailer := newTestAuthWithMailer(t, cfg)
	store := &pausingStore{Store: a.store}
	a.store = store
	return a, store, mailer
}

// runParked starts op on a new goroutine, waits until it is parked at
// p and returns a function that releases it and returns op's error.
func runParked(t *testing.T, p *pausePoint, op func() error) func() error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- op() }()
	select {
	case <-p.reached:
	case err := <-result:
		t.Fatalf("operation finished (%v) without reaching the pause point %s", err, p.method)
	case <-time.After(10 * time.Second):
		t.Fatalf("operation never reached the pause point %s", p.method)
	}
	return func() error {
		close(p.release)
		return <-result
	}
}

// TestRegressionBanSurvivesConcurrentProfileUpdate: a profile update
// that read the user before an admin ban must not write the stale,
// unbanned record back.
func TestRegressionBanSurvivesConcurrentProfileUpdate(t *testing.T) {
	a, store, _ := newPausingAuth(t, Config{})
	out := signUpUser(t, a, "mallory@example.com")

	p := store.pauseAfter(t, "GetUser", 0)
	finishUpdate := runParked(t, p, func() error {
		name := "Renamed"
		_, err := a.UpdateUser(testContext, out.User.ID, UpdateUserInput{Name: &name})
		return err
	})
	if err := a.BanUser(testContext, out.User.ID, "abuse", nil); err != nil {
		t.Fatalf("BanUser: %v", err)
	}
	if err := finishUpdate(); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	user, err := a.GetUser(testContext, out.User.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !user.Banned || user.BanReason != "abuse" {
		t.Fatalf("ban lost: Banned=%v BanReason=%q", user.Banned, user.BanReason)
	}
	if user.Name != "Renamed" {
		t.Errorf("Name = %q, want the profile change applied on top of the ban", user.Name)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "mallory@example.com", Password: testPassword}); !errors.Is(err, ErrUserBanned) {
		t.Errorf("SignIn after the ban = %v, want ErrUserBanned", err)
	}
}

// TestRegressionChangeEmailSurvivesConcurrentProfileUpdate: a profile
// update that read the user before an email change must not revert the
// address while the credential account keeps the new one, which would
// squat the new address.
func TestRegressionChangeEmailSurvivesConcurrentProfileUpdate(t *testing.T) {
	a, store, _ := newPausingAuth(t, Config{})
	out := signUpUser(t, a, "mallory@example.com")

	p := store.pauseAfter(t, "GetUser", 0)
	finishUpdate := runParked(t, p, func() error {
		name := "Spam"
		_, err := a.UpdateUser(testContext, out.User.ID, UpdateUserInput{Name: &name})
		return err
	})
	if _, err := a.ChangeEmail(testContext, out.User.ID, "victim@example.com", testPassword); err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if err := finishUpdate(); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	assertEmailConsistent(t, a, out.User.ID, "victim@example.com")
	user, _ := a.GetUser(testContext, out.User.ID)
	if user.Name != "Spam" {
		t.Errorf("Name = %q, want the profile change applied on top", user.Name)
	}
	// The old address is free again and the new one belongs to the
	// user who changed to it.
	signUpUser(t, a, "mallory@example.com")
	if _, err := a.SignIn(testContext, SignInInput{Email: "victim@example.com", Password: testPassword}); err != nil {
		t.Errorf("SignIn with the new address: %v", err)
	}
}

// TestRegressionPasswordWriteSurvivesConcurrentEmailChange: a password
// change that read the credential account before an email change must
// not write the old provider id back.
func TestRegressionPasswordWriteSurvivesConcurrentEmailChange(t *testing.T) {
	a, store, _ := newPausingAuth(t, Config{})
	out := signUpUser(t, a, "old@example.com")

	// Skip the account read of the password check; park the read that
	// precedes the password write.
	p := store.pauseAfter(t, "GetAccountByUser", 1)
	finishChange := runParked(t, p, func() error {
		return a.ChangePassword(testContext, out.User.ID, testPassword, "brand-new-password")
	})
	if _, err := a.ChangeEmail(testContext, out.User.ID, "new@example.com", testPassword); err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if err := finishChange(); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	assertEmailConsistent(t, a, out.User.ID, "new@example.com")
	if _, err := a.SignIn(testContext, SignInInput{Email: "new@example.com", Password: "brand-new-password"}); err != nil {
		t.Errorf("SignIn with the new address and password: %v", err)
	}
}

// TestRegressionResetTokenCannotBeSpentTwice: two concurrent
// redemptions of one password reset token must not both succeed.
func TestRegressionResetTokenCannotBeSpentTwice(t *testing.T) {
	a, store, mailer := newPausingAuth(t, Config{})
	signUpUser(t, a, "ada@example.com")
	if err := a.RequestPasswordReset(testContext, "ada@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	token := lastResetToken(t, a, mailer)

	p := store.pauseAfter(t, "GetVerificationToken", 0)
	finishFirst := runParked(t, p, func() error {
		return a.ResetPassword(testContext, "ada@example.com", token, "first-new-password")
	})
	if err := a.ResetPassword(testContext, "ada@example.com", token, "second-new-password"); err != nil {
		t.Fatalf("second ResetPassword: %v", err)
	}
	if err := finishFirst(); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("first (parked) ResetPassword = %v, want ErrInvalidToken", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "ada@example.com", Password: "second-new-password"}); err != nil {
		t.Errorf("SignIn with the winning password: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "ada@example.com", Password: "first-new-password"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("SignIn with the losing password = %v, want ErrInvalidCredentials", err)
	}
}

// TestRegressionVerificationTokenCannotBeSpentTwice is the email
// verification counterpart of the double spend test.
func TestRegressionVerificationTokenCannotBeSpentTwice(t *testing.T) {
	a, store, mailer := newPausingAuth(t, Config{})
	signUpUser(t, a, "ada@example.com")
	token := lastVerificationToken(t, a, mailer)

	p := store.pauseAfter(t, "GetVerificationToken", 0)
	finishFirst := runParked(t, p, func() error {
		return a.VerifyEmail(testContext, "ada@example.com", token)
	})
	if err := a.VerifyEmail(testContext, "ada@example.com", token); err != nil {
		t.Fatalf("second VerifyEmail: %v", err)
	}
	if err := finishFirst(); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("first (parked) VerifyEmail = %v, want ErrInvalidToken", err)
	}
}

// TestRegressionVerifyEmailAfterConcurrentEmailChange: a verification
// token proves ownership of the address it was sent to only. When the
// address changes between redemption and the write, the new address
// must stay unverified.
func TestRegressionVerifyEmailAfterConcurrentEmailChange(t *testing.T) {
	a, store, mailer := newPausingAuth(t, Config{})
	out := signUpUser(t, a, "first@example.com")
	token := lastVerificationToken(t, a, mailer)

	// VerifyEmail reads the token, then (inside the retry helper) the
	// user by ID; park it there.
	p := store.pauseAfter(t, "GetUser", 0)
	finishVerify := runParked(t, p, func() error {
		return a.VerifyEmail(testContext, "first@example.com", token)
	})
	if _, err := a.ChangeEmail(testContext, out.User.ID, "second@example.com", testPassword); err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if err := finishVerify(); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyEmail = %v, want ErrInvalidToken", err)
	}
	user, _ := a.GetUser(testContext, out.User.ID)
	if user.Email != "second@example.com" || user.EmailVerified {
		t.Errorf("user = %q verified=%v, want second@example.com unverified", user.Email, user.EmailVerified)
	}
}

// TestRegressionConcurrentUserWritesKeepEveryChange hammers one user
// with profile updates, a ban and an email change from many
// goroutines. ErrConflict (retries exhausted) is a legitimate outcome
// under contention, but whatever the schedule, a write that reported
// success is never lost and user and credential account always agree
// on the address.
func TestRegressionConcurrentUserWritesKeepEveryChange(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "hammer@example.com")
	id := out.User.ID

	var wg sync.WaitGroup
	unexpected := make(chan error, 64)
	check := func(err error) {
		if err != nil && !errors.Is(err, ErrConflict) {
			unexpected <- err
		}
	}
	var renamed, banned, moved bool
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "Renamed"
			_, err := a.UpdateUser(testContext, id, UpdateUserInput{Name: &name})
			check(err)
			mu.Lock()
			renamed = renamed || err == nil
			mu.Unlock()
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		err := a.BanUser(testContext, id, "abuse", nil)
		check(err)
		mu.Lock()
		banned = err == nil
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		_, err := a.ChangeEmail(testContext, id, "moved@example.com", testPassword)
		check(err)
		mu.Lock()
		moved = err == nil
		mu.Unlock()
	}()
	wg.Wait()
	close(unexpected)
	for err := range unexpected {
		t.Errorf("unexpected error: %v", err)
	}

	user, _ := a.GetUser(testContext, id)
	if banned && !user.Banned {
		t.Error("a successful ban was lost under concurrent writes")
	}
	if renamed && user.Name != "Renamed" {
		t.Error("a successful rename was lost under concurrent writes")
	}
	want := "hammer@example.com"
	if moved {
		want = "moved@example.com"
	}
	assertEmailConsistent(t, a, id, want)
}

// assertEmailConsistent checks that the user and its credential
// account both carry email.
func assertEmailConsistent(t *testing.T, a *Auth, userID, email string) {
	t.Helper()
	user, err := a.GetUser(testContext, userID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	account, err := a.store.GetAccountByUser(testContext, userID, CredentialProvider)
	if err != nil {
		t.Fatalf("GetAccountByUser: %v", err)
	}
	if user.Email != email || account.ProviderAccountID != email {
		t.Fatalf("users.email = %q, account.provider_account_id = %q, want both %q",
			user.Email, account.ProviderAccountID, email)
	}
}
