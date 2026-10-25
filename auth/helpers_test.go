package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testPassword satisfies the default password policy (>= 8 bytes).
const testPassword = "password123"

// testContext is the context used by service calls in tests.
var testContext = context.Background()

// testEpoch is the deterministic start time of every fake clock.
var testEpoch = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

// fakeClock is a deterministic clock installed on Auth.now.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the current fake time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the fake time forward by d and returns the new time.
func (c *fakeClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

// newTestAuth builds an Auth backed by a MemoryStore with a fake clock
// starting at testEpoch. Logs are discarded unless cfg.Logger is set.
// Background deliveries are drained when the test ends so no goroutine
// outlives it.
func newTestAuth(t *testing.T, cfg Config) (*Auth, *fakeClock) {
	t.Helper()
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	a, err := New(cfg, NewMemoryStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	clock := &fakeClock{t: testEpoch}
	a.now = clock.Now
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown in cleanup: %v", err)
		}
	})
	return a, clock
}

// newTestAuthWithMailer is newTestAuth with a recording mailer wired
// into the configuration.
func newTestAuthWithMailer(t *testing.T, cfg Config) (*Auth, *fakeClock, *recordingMailer) {
	t.Helper()
	mailer := &recordingMailer{}
	cfg.Mailer = mailer
	a, clock := newTestAuth(t, cfg)
	return a, clock, mailer
}

// memStore returns the MemoryStore behind a test service.
func memStore(t *testing.T, a *Auth) *MemoryStore {
	t.Helper()
	store, ok := a.store.(*MemoryStore)
	if !ok {
		t.Fatalf("store is %T, want *MemoryStore", a.store)
	}
	return store
}

// waitForDeliveries blocks until every background email delivery has
// finished. It is the deterministic synchronization point for tests
// that assert on emails sent in the background.
func waitForDeliveries(t *testing.T, a *Auth) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("waiting for background deliveries: %v", err)
	}
}

// lastVerificationToken waits for background deliveries and returns
// the token of the most recently sent verification email.
func lastVerificationToken(t *testing.T, a *Auth, m *recordingMailer) string {
	t.Helper()
	waitForDeliveries(t, a)
	calls := m.verificationCalls()
	if len(calls) == 0 {
		t.Fatal("no verification email was sent")
	}
	return calls[len(calls)-1].Token
}

// lastResetToken waits for background deliveries and returns the token
// of the most recently sent password reset email.
func lastResetToken(t *testing.T, a *Auth, m *recordingMailer) string {
	t.Helper()
	waitForDeliveries(t, a)
	calls := m.resetCalls()
	if len(calls) == 0 {
		t.Fatal("no password reset email was sent")
	}
	return calls[len(calls)-1].Token
}

// signUpUser registers a user with the given email and sensible
// defaults, failing the test on error.
func signUpUser(t *testing.T, a *Auth, email string) *SignUpOutput {
	t.Helper()
	out, err := a.SignUp(testContext, SignUpInput{
		Name:     "Test User",
		Email:    email,
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("SignUp(%q): %v", email, err)
	}
	return out
}

// verifyUser marks the user's current address as verified through the
// public flow (SendVerificationEmail + VerifyEmail).
func verifyUser(t *testing.T, a *Auth, m *recordingMailer, userID string) {
	t.Helper()
	if err := a.SendVerificationEmail(testContext, userID); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	user, err := a.GetUser(testContext, userID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if err := a.VerifyEmail(testContext, user.Email, lastVerificationToken(t, a, m)); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
}

// sentEmail records one outgoing email.
type sentEmail struct {
	To    string
	Token string
}

// recordingMailer is a Mailer that captures emails, can be told to
// fail and can be paused. It is safe for concurrent use.
type recordingMailer struct {
	mu            sync.Mutex
	verifications []sentEmail
	resets        []sentEmail
	attempts      []sentEmail // every send, including failed ones
	failErr       error
	panicValue    any
	// gate, when non-nil, makes every send block until it is closed.
	gate chan struct{}
	// started receives one value per send that reached the gate.
	started chan struct{}
}

// SendVerificationEmail implements Mailer.
func (m *recordingMailer) SendVerificationEmail(_ context.Context, to, token string) error {
	return m.record(&m.verifications, to, token)
}

// SendPasswordResetEmail implements Mailer.
func (m *recordingMailer) SendPasswordResetEmail(_ context.Context, to, token string) error {
	return m.record(&m.resets, to, token)
}

// record applies the configured behavior and captures the email.
func (m *recordingMailer) record(list *[]sentEmail, to, token string) error {
	m.mu.Lock()
	gate, started := m.gate, m.started
	m.mu.Unlock()
	if gate != nil {
		if started != nil {
			started <- struct{}{}
		}
		<-gate
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts = append(m.attempts, sentEmail{To: to, Token: token})
	if m.panicValue != nil {
		panic(m.panicValue)
	}
	if m.failErr != nil {
		return m.failErr
	}
	*list = append(*list, sentEmail{To: to, Token: token})
	return nil
}

// setFailure makes subsequent sends fail with err (nil restores
// success).
func (m *recordingMailer) setFailure(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failErr = err
}

// verificationCalls returns a copy of the captured verification
// emails.
func (m *recordingMailer) verificationCalls() []sentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentEmail(nil), m.verifications...)
}

// attemptedCalls returns a copy of every send attempt, failed or not.
func (m *recordingMailer) attemptedCalls() []sentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentEmail(nil), m.attempts...)
}

// resetCalls returns a copy of the captured password reset emails.
func (m *recordingMailer) resetCalls() []sentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentEmail(nil), m.resets...)
}

// logBuffer is a concurrency safe io.Writer that collects log output.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write implements io.Writer.
func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns everything logged so far.
func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newLogCapture returns a logger writing text records at Debug level
// and above into the returned buffer.
func newLogCapture() (*slog.Logger, *logBuffer) {
	buf := &logBuffer{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// tokenCount reports how many verification tokens the store holds.
func tokenCount(s *MemoryStore) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tokens)
}

// tokensWithIdentifier reports how many tokens carry the identifier.
func tokensWithIdentifier(s *MemoryStore, identifier string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, token := range s.tokens {
		if token.Identifier == identifier {
			n++
		}
	}
	return n
}

// sessionCount reports how many sessions the store holds.
func sessionCount(s *MemoryStore) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// accountCount reports how many accounts the store holds.
func accountCount(s *MemoryStore) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.accounts)
}

// withDummyCompareCounter replaces the package level dummyCompare with
// a counting stub and returns the counter together with a restore
// function. It must not be combined with t.Parallel.
func withDummyCompareCounter() (*atomic.Int32, func()) {
	var count atomic.Int32
	original := dummyCompare
	dummyCompare = func(_ string) {
		count.Add(1)
	}
	return &count, func() {
		dummyCompare = original
	}
}

// duplicateCreateUserStore wraps a Store and makes CreateUser always
// fail with ErrDuplicate, simulating a uniqueness race lost after the
// service's pre-check.
type duplicateCreateUserStore struct {
	Store
}

// CreateUser implements Store.
func (s duplicateCreateUserStore) CreateUser(_ context.Context, _ *User) error {
	return ErrDuplicate
}

// failingGetUserByEmailStore wraps a Store and makes GetUserByEmail
// fail with the given error, to exercise unexpected store failures.
type failingGetUserByEmailStore struct {
	Store
	err error
}

// GetUserByEmail implements Store.
func (s failingGetUserByEmailStore) GetUserByEmail(_ context.Context, _ string) (*User, error) {
	return nil, s.err
}

// faultyStore wraps a Store and fails selected methods with err. It is
// used to exercise error paths that a MemoryStore never produces.
type faultyStore struct {
	Store
	err error

	createAccount         bool
	createSession         bool
	createToken           bool
	updateUser            bool
	updateAccount         bool
	deleteTokensByIdent   bool
	deleteExpiredTokens   bool
	deleteExpiredSessions bool
}

// CreateAccount implements Store.
func (s *faultyStore) CreateAccount(ctx context.Context, account *Account) error {
	if s.createAccount {
		return s.err
	}
	return s.Store.CreateAccount(ctx, account)
}

// CreateSession implements Store.
func (s *faultyStore) CreateSession(ctx context.Context, session *Session) error {
	if s.createSession {
		return s.err
	}
	return s.Store.CreateSession(ctx, session)
}

// CreateVerificationToken implements Store.
func (s *faultyStore) CreateVerificationToken(ctx context.Context, token *VerificationToken) error {
	if s.createToken {
		return s.err
	}
	return s.Store.CreateVerificationToken(ctx, token)
}

// UpdateUser implements Store.
func (s *faultyStore) UpdateUser(ctx context.Context, user *User) error {
	if s.updateUser {
		return s.err
	}
	return s.Store.UpdateUser(ctx, user)
}

// UpdateAccount implements Store.
func (s *faultyStore) UpdateAccount(ctx context.Context, account *Account) error {
	if s.updateAccount {
		return s.err
	}
	return s.Store.UpdateAccount(ctx, account)
}

// DeleteVerificationTokensByIdentifier implements Store.
func (s *faultyStore) DeleteVerificationTokensByIdentifier(ctx context.Context, identifier string) error {
	if s.deleteTokensByIdent {
		return s.err
	}
	return s.Store.DeleteVerificationTokensByIdentifier(ctx, identifier)
}

// DeleteExpiredVerificationTokens implements Store.
func (s *faultyStore) DeleteExpiredVerificationTokens(ctx context.Context, before time.Time) error {
	if s.deleteExpiredTokens {
		return s.err
	}
	return s.Store.DeleteExpiredVerificationTokens(ctx, before)
}

// DeleteExpiredSessions implements Store.
func (s *faultyStore) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	if s.deleteExpiredSessions {
		return s.err
	}
	return s.Store.DeleteExpiredSessions(ctx, before)
}

// errStoreFailure is a sentinel stand-in for a backend failure.
var errStoreFailure = errors.New("store backend failure")
