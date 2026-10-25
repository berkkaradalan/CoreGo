package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// newBlockedAuth returns a service whose mailer blocks every send
// until a value is sent on the returned gate (one value per send).
func newBlockedAuth(t *testing.T) (*Auth, *recordingMailer, chan struct{}) {
	t.Helper()
	gate := make(chan struct{})
	mailer := &recordingMailer{gate: gate, started: make(chan struct{}, 16)}
	a, _ := newTestAuth(t, Config{Mailer: mailer})
	t.Cleanup(func() { close(gate) }) // runs before the drain registered by newTestAuth
	return a, mailer, gate
}

func TestShutdownWithNothingInFlight(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	canceled, cancel := context.WithCancel(testContext)
	cancel()
	if err := a.Shutdown(canceled); err != nil {
		t.Fatalf("Shutdown with nothing in flight = %v, want nil even with a done context", err)
	}
	if err := a.Shutdown(testContext); err != nil {
		t.Fatalf("Shutdown = %v", err)
	}
}

func TestShutdownTimesOutWhileDeliveryIsBlocked(t *testing.T) {
	a, mailer, gate := newBlockedAuth(t)
	signUpUser(t, a, "blocked@example.com")
	<-mailer.started

	canceled, cancel := context.WithCancel(testContext)
	cancel()
	if err := a.Shutdown(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown = %v, want context.Canceled", err)
	}
	expired, cancelExpired := context.WithTimeout(testContext, 0)
	defer cancelExpired()
	if err := a.Shutdown(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want context.DeadlineExceeded", err)
	}

	// The delivery keeps running after the timeout and completes once
	// released.
	gate <- struct{}{}
	waitForDeliveries(t, a)
	if calls := mailer.verificationCalls(); len(calls) != 1 {
		t.Fatalf("verification calls = %d, want 1", len(calls))
	}
}

func TestShutdownWaitsForEveryDelivery(t *testing.T) {
	a, mailer, gate := newBlockedAuth(t)
	for _, email := range []string{"w1@example.com", "w2@example.com", "w3@example.com"} {
		signUpUser(t, a, email)
	}
	for i := 0; i < 3; i++ {
		<-mailer.started
	}

	done := make(chan error, 1)
	go func() { done <- a.Shutdown(testContext) }()

	// Two of three deliveries finish: Shutdown cannot have returned,
	// because the third is still blocked in the mailer.
	gate <- struct{}{}
	gate <- struct{}{}
	select {
	case err := <-done:
		t.Fatalf("Shutdown returned %v while a delivery was still running", err)
	default:
	}

	gate <- struct{}{}
	if err := <-done; err != nil {
		t.Fatalf("Shutdown = %v", err)
	}
	if calls := mailer.verificationCalls(); len(calls) != 3 {
		t.Fatalf("verification calls = %d, want 3 after Shutdown", len(calls))
	}
}

func TestServiceUsableAfterShutdown(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	if err := a.Shutdown(testContext); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	signUpUser(t, a, "after@example.com")
	waitForDeliveries(t, a)
	if calls := mailer.verificationCalls(); len(calls) != 1 {
		t.Fatalf("verification calls = %d, want 1 after Shutdown", len(calls))
	}
}

// TestShutdownConcurrentWithNewDeliveries exercises Shutdown while
// deliveries keep starting; the race detector checks the tracker.
func TestShutdownConcurrentWithNewDeliveries(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	signUpUser(t, a, "concurrent@example.com")

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = a.RequestPasswordReset(testContext, "concurrent@example.com")
		}()
		go func() {
			defer wg.Done()
			if err := a.Shutdown(testContext); err != nil {
				t.Errorf("Shutdown: %v", err)
			}
		}()
	}
	wg.Wait()
	waitForDeliveries(t, a)
	if calls := mailer.resetCalls(); len(calls) != 4 {
		t.Fatalf("reset calls = %d, want 4", len(calls))
	}
}

func TestInflightTracker(t *testing.T) {
	var f inflight
	if err := f.wait(testContext); err != nil {
		t.Fatalf("wait on the zero value = %v", err)
	}

	if !f.tryAdd(2) || !f.tryAdd(2) {
		t.Fatal("tryAdd below the limit = false, want true")
	}
	if f.tryAdd(2) {
		t.Fatal("tryAdd at the limit = true, want false")
	}
	canceled, cancel := context.WithCancel(testContext)
	cancel()
	if err := f.wait(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait with two tasks = %v, want context.Canceled", err)
	}
	f.done()
	if err := f.wait(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait with one task = %v, want context.Canceled", err)
	}
	f.done()
	if err := f.wait(canceled); err != nil {
		t.Fatalf("wait when idle = %v, want nil", err)
	}

	// Reusable after becoming idle.
	if !f.tryAdd(1) {
		t.Fatal("tryAdd after becoming idle = false, want true")
	}
	waited := make(chan error, 1)
	go func() { waited <- f.wait(testContext) }()
	f.done()
	if err := <-waited; err != nil {
		t.Fatalf("wait after reuse = %v", err)
	}
}

// TestMaxPendingEmailsDropsInsteadOfBlocking: with every slot taken by
// a hanging mailer, further email jobs are dropped and logged at Warn
// without blocking or failing the request, and without persisting a
// token nobody will receive.
func TestMaxPendingEmailsDropsInsteadOfBlocking(t *testing.T) {
	logger, logs := newLogCapture()
	gate := make(chan struct{})
	mailer := &recordingMailer{gate: gate, started: make(chan struct{}, 16)}
	a, _ := newTestAuth(t, Config{Mailer: mailer, MaxPendingEmails: 2, Logger: logger})
	t.Cleanup(func() { close(gate) })

	first := signUpUser(t, a, "first@example.com") // slot 1: verification email
	<-mailer.started
	if err := a.RequestPasswordReset(testContext, "first@example.com"); err != nil { // slot 2
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	<-mailer.started
	store := memStore(t, a)
	tokensBefore := tokenCount(store)

	// Every further job is dropped. None of these calls may block: the
	// mailer is stuck, so a queued job would hang Shutdown below.
	for i := 0; i < 50; i++ {
		if err := a.RequestPasswordReset(testContext, "first@example.com"); err != nil {
			t.Fatalf("RequestPasswordReset with a full queue = %v, want nil", err)
		}
		if err := a.RequestEmailVerification(testContext, "first@example.com"); err != nil {
			t.Fatalf("RequestEmailVerification with a full queue = %v, want nil", err)
		}
	}
	second := signUpUser(t, a, "second@example.com") // registers, email dropped
	if second.SessionToken == "" {
		t.Error("SignUp with a full queue did not complete")
	}
	if got := tokenCount(store); got != tokensBefore {
		t.Errorf("tokens = %d, want %d (dropped jobs persist nothing)", got, tokensBefore)
	}

	output := logs.String()
	for _, want := range []string{"level=WARN", "background email dropped", "max_pending_emails=2",
		"email=password_reset", "email=email_verification", "user_id=" + first.User.ID, "user_id=" + second.User.ID} {
		if !strings.Contains(output, want) {
			t.Errorf("log output lacks %q", want)
		}
	}
	if strings.Contains(output, "@example.com") {
		t.Errorf("drop log contains an address:\n%s", output)
	}

	// Release the two running jobs; nothing else was started.
	gate <- struct{}{}
	gate <- struct{}{}
	waitForDeliveries(t, a)
	if got := len(mailer.attemptedCalls()); got != 2 {
		t.Fatalf("send attempts = %d, want 2", got)
	}

	// Freed slots accept work again.
	if err := a.RequestPasswordReset(testContext, "first@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset after draining: %v", err)
	}
	<-mailer.started
	gate <- struct{}{}
	waitForDeliveries(t, a)
	if got := len(mailer.resetCalls()); got != 2 {
		t.Errorf("reset emails = %d, want 2", got)
	}
}

// TestDefaultMaxPendingEmails checks the default bound end to end:
// exactly DefaultMaxPendingEmails jobs run concurrently.
func TestDefaultMaxPendingEmails(t *testing.T) {
	gate := make(chan struct{})
	mailer := &recordingMailer{gate: gate, started: make(chan struct{}, DefaultMaxPendingEmails+1)}
	a, _ := newTestAuth(t, Config{Mailer: mailer})
	t.Cleanup(func() { close(gate) })
	signUpUser(t, a, "bound@example.com")
	for i := 0; i < DefaultMaxPendingEmails+10; i++ {
		if err := a.RequestPasswordReset(testContext, "bound@example.com"); err != nil {
			t.Fatalf("RequestPasswordReset: %v", err)
		}
	}
	for i := 0; i < DefaultMaxPendingEmails; i++ {
		<-mailer.started
	}
	for i := 0; i < DefaultMaxPendingEmails; i++ {
		gate <- struct{}{}
	}
	waitForDeliveries(t, a)
	if got := len(mailer.attemptedCalls()); got != DefaultMaxPendingEmails {
		t.Fatalf("send attempts = %d, want %d", got, DefaultMaxPendingEmails)
	}
}

// blockingTokenStore parks every CreateVerificationToken call until
// release is closed, and reports each call on entered.
type blockingTokenStore struct {
	Store
	entered chan struct{}
	release chan struct{}
}

// CreateVerificationToken implements Store.
func (s *blockingTokenStore) CreateVerificationToken(ctx context.Context, token *VerificationToken) error {
	s.entered <- struct{}{}
	<-s.release
	return s.Store.CreateVerificationToken(ctx, token)
}

// TestPublicEmailRequestsDoNotWaitForTheTokenWrite: for a registered
// address the public request flows must do nothing synchronously
// beyond the user lookup that every address gets, so response time
// does not reveal registration. The token write is parked; the
// request must return regardless.
func TestPublicEmailRequestsDoNotWaitForTheTokenWrite(t *testing.T) {
	flows := []struct {
		name string
		call func(a *Auth) error
		sent func(m *recordingMailer) int
	}{
		{"RequestPasswordReset", func(a *Auth) error { return a.RequestPasswordReset(testContext, "timing@example.com") },
			func(m *recordingMailer) int { return len(m.resetCalls()) }},
		{"RequestEmailVerification", func(a *Auth) error { return a.RequestEmailVerification(testContext, "timing@example.com") },
			func(m *recordingMailer) int { return len(m.verificationCalls()) - 1 }}, // minus the sign-up email
	}
	for _, flow := range flows {
		t.Run(flow.name, func(t *testing.T) {
			a, _, mailer := newTestAuthWithMailer(t, Config{})
			signUpUser(t, a, "timing@example.com")
			waitForDeliveries(t, a)
			store := &blockingTokenStore{Store: a.store, entered: make(chan struct{}, 1), release: make(chan struct{})}
			a.store = store
			released := false
			t.Cleanup(func() {
				if !released {
					close(store.release)
				}
			})

			returned := make(chan error, 1)
			go func() { returned <- flow.call(a) }()
			select {
			case err := <-returned:
				if err != nil {
					t.Fatalf("%s = %v", flow.name, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("%s waited for the token write (timing oracle)", flow.name)
			}

			<-store.entered // the background job does write the token
			close(store.release)
			released = true
			waitForDeliveries(t, a)
			if got := flow.sent(mailer); got != 1 {
				t.Errorf("emails sent = %d, want 1", got)
			}
		})
	}
}
