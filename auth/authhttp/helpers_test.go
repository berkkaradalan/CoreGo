package authhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// testPassword satisfies the default password policy (8+ bytes).
const testPassword = "correct-horse-battery"

// sentEmail is one email recorded by recordingMailer.
type sentEmail struct {
	to    string
	token string
}

// recordingMailer is an auth.Mailer test double that records every email
// instead of delivering it. It is safe for concurrent use; the service
// calls it from background goroutines.
type recordingMailer struct {
	mu     sync.Mutex
	verify []sentEmail
	reset  []sentEmail
}

func (m *recordingMailer) SendVerificationEmail(_ context.Context, to, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verify = append(m.verify, sentEmail{to: to, token: token})
	return nil
}

func (m *recordingMailer) SendPasswordResetEmail(_ context.Context, to, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reset = append(m.reset, sentEmail{to: to, token: token})
	return nil
}

// counts returns the number of verification and reset emails recorded.
func (m *recordingMailer) counts() (verify, reset int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.verify), len(m.reset)
}

// lastToken returns the token of the most recent email of the given
// list sent to to, or "" when there is none.
func lastToken(list []sentEmail, to string) string {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].to == to {
			return list[i].token
		}
	}
	return ""
}

// verificationToken returns the newest verification token sent to to.
func (m *recordingMailer) verificationToken(to string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return lastToken(m.verify, to)
}

// resetToken returns the newest password reset token sent to to.
func (m *recordingMailer) resetToken(to string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return lastToken(m.reset, to)
}

// failingStore wraps a MemoryStore and fails selected operations with
// errStoreDown, to exercise the 500 paths.
type failingStore struct {
	*auth.MemoryStore
	failGetUserByEmail atomic.Bool
	failGetSession     atomic.Bool
}

// errStoreDown is the error injected by failingStore.
var errStoreDown = fmt.Errorf("test store: connection refused")

func (s *failingStore) GetUserByEmail(ctx context.Context, email string) (*auth.User, error) {
	if s.failGetUserByEmail.Load() {
		return nil, errStoreDown
	}
	return s.MemoryStore.GetUserByEmail(ctx, email)
}

func (s *failingStore) GetSessionByTokenHash(ctx context.Context, hash string) (*auth.Session, error) {
	if s.failGetSession.Load() {
		return nil, errStoreDown
	}
	return s.MemoryStore.GetSessionByTokenHash(ctx, hash)
}

// appOptions customizes the test app built by newTestApp.
type appOptions struct {
	// noMailer leaves auth.Config.Mailer nil.
	noMailer bool
	// authCfg mutates the auth.Config before the service is created.
	authCfg func(*auth.Config)
	// httpCfg is the authhttp.Config; the zero value by default.
	httpCfg Config
	// store backs the service; a MemoryStore by default.
	store auth.Store
	// server mutates the web.Server before the routes are mounted.
	server func(*web.Server)
}

// testApp bundles a mounted Handlers with the pieces tests need to drive
// and inspect it.
type testApp struct {
	server   *web.Server
	service  *auth.Auth
	handlers *Handlers
	store    auth.Store
	mailer   *recordingMailer
	base     string

	// nextIP hands every request without an explicit address its own
	// client IP, so the rate limiters never interfere with tests that
	// are not about rate limiting.
	nextIP atomic.Uint32
}

// newTestApp builds a web.Server with the auth routes mounted.
func newTestApp(t *testing.T, opts appOptions) *testApp {
	t.Helper()
	store := opts.store
	if store == nil {
		store = auth.NewMemoryStore()
	}
	mailer := &recordingMailer{}
	cfg := auth.Config{Logger: slog.New(slog.DiscardHandler)}
	if !opts.noMailer {
		cfg.Mailer = mailer
	}
	if opts.authCfg != nil {
		opts.authCfg(&cfg)
	}
	service, err := auth.New(cfg, store)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	t.Cleanup(func() { waitForEmails(t, service) })

	h := New(service, opts.httpCfg)
	server := web.New()
	server.Logger = slog.New(slog.DiscardHandler)
	if opts.server != nil {
		opts.server(server)
	}
	h.Mount(server)
	return &testApp{server: server, service: service, handlers: h, store: store, mailer: mailer, base: h.cfg.BasePath}
}

// waitForEmails blocks until the service's background deliveries are
// done, so mailer assertions are deterministic.
func waitForEmails(t *testing.T, service *auth.Auth) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatalf("waiting for background emails: %v", err)
	}
}

// waitEmails waits for the app's background deliveries.
func (a *testApp) waitEmails(t *testing.T) {
	t.Helper()
	waitForEmails(t, a.service)
}

// req describes one test request. Path is relative to the app's base
// path unless absolute is set.
type req struct {
	method   string
	path     string
	absolute bool
	body     any    // marshaled as JSON with Content-Type application/json
	raw      string // raw body, used when body is nil
	ctype    string // Content-Type for raw bodies
	token    string // Bearer token
	cookie   *http.Cookie
	headers  map[string]string
	remote   string // RemoteAddr; a unique address when empty
}

// do performs r against the app's server.
func (a *testApp) do(t *testing.T, r req) *httptest.ResponseRecorder {
	t.Helper()
	path := r.path
	if !r.absolute {
		path = a.base + r.path
	}
	var body io.Reader = strings.NewReader(r.raw)
	if r.body != nil {
		encoded, err := json.Marshal(r.body)
		if err != nil {
			t.Fatalf("marshaling the request body: %v", err)
		}
		body = strings.NewReader(string(encoded))
	}
	httpReq := httptest.NewRequest(r.method, path, body)
	switch {
	case r.body != nil:
		httpReq.Header.Set("Content-Type", "application/json")
	case r.ctype != "":
		httpReq.Header.Set("Content-Type", r.ctype)
	}
	if r.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.token)
	}
	if r.cookie != nil {
		httpReq.AddCookie(r.cookie)
	}
	for name, value := range r.headers {
		httpReq.Header.Set(name, value)
	}
	httpReq.RemoteAddr = r.remote
	if httpReq.RemoteAddr == "" {
		n := a.nextIP.Add(1)
		httpReq.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:4321", byte(n>>16), byte(n>>8), byte(n))
	}
	rec := httptest.NewRecorder()
	a.server.ServeHTTP(rec, httpReq)
	return rec
}

// signUp registers a user through the API and returns the response
// body; it fails the test unless the status is 201.
func (a *testApp) signUp(t *testing.T, name, email, password string) signUpResult {
	t.Helper()
	rec := a.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": name, "email": email, "password": password,
	}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup %s: status %d, body %s", email, rec.Code, rec.Body)
	}
	var out signUpResult
	decode(t, rec, &out)
	return out
}

// signIn signs a user in through the API and returns the session token.
func (a *testApp) signIn(t *testing.T, email, password string) string {
	t.Helper()
	rec := a.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
		"email": email, "password": password,
	}})
	if rec.Code != http.StatusOK {
		t.Fatalf("signin %s: status %d, body %s", email, rec.Code, rec.Body)
	}
	var out signUpResult
	decode(t, rec, &out)
	return out.Token
}

// signUpResult decodes sign-up and sign-in responses, keeping the
// optional fields distinguishable from their zero values.
type signUpResult struct {
	User                 userResult `json:"user"`
	Token                string     `json:"token"`
	ExpiresAt            *time.Time `json:"expires_at"`
	VerificationRequired *bool      `json:"verification_required"`
}

// userResult decodes a user object.
type userResult struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Image         string `json:"image"`
}

// decode unmarshals the response body into v.
func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decoding response body %q: %v", rec.Body, err)
	}
}

// errorMessage returns the "error" field of a JSON error body.
func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error *string `json:"error"`
	}
	decode(t, rec, &body)
	if body.Error == nil {
		t.Fatalf("response body %q has no error field", rec.Body)
	}
	return *body.Error
}

// expectError asserts an error response with the given status and
// message.
func expectError(t *testing.T, rec *httptest.ResponseRecorder, code int, message string) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	if got := errorMessage(t, rec); got != message {
		t.Fatalf("error message = %q, want %q", got, message)
	}
}

// expectStatus asserts the response status.
func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, code int) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, code, rec.Body)
	}
}

// responseCookie returns the Set-Cookie entry with the given name, or
// nil.
func responseCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// expectClearedCookie asserts that the response deletes the session
// cookie.
func expectClearedCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) {
	t.Helper()
	c := responseCookie(rec, name)
	if c == nil {
		t.Fatalf("no %s cookie in the response; Set-Cookie: %v", name, rec.Header().Values("Set-Cookie"))
	}
	if c.Value != "" || c.MaxAge >= 0 {
		t.Fatalf("cookie %s not cleared: value %q, MaxAge %d", name, c.Value, c.MaxAge)
	}
	if !c.HttpOnly {
		t.Fatal("the clearing cookie must be HttpOnly as well")
	}
}

// userIDByEmail looks the user up in the store.
func (a *testApp) userIDByEmail(t *testing.T, email string) string {
	t.Helper()
	user, err := a.store.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetUserByEmail(%s): %v", email, err)
	}
	return user.ID
}

// updateUser applies fn to the stored user, bypassing the service (for
// states the service API cannot produce directly, such as a banned user
// who still has a session).
func (a *testApp) updateUser(t *testing.T, email string, fn func(*auth.User)) {
	t.Helper()
	user, err := a.store.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetUserByEmail(%s): %v", email, err)
	}
	fn(user)
	if err := a.store.UpdateUser(context.Background(), user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
}

// expireSession moves the expiry of the session identified by token into
// the past.
func (a *testApp) expireSession(t *testing.T, token string) {
	t.Helper()
	session, err := a.store.GetSessionByTokenHash(context.Background(), auth.HashToken(token))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	session.ExpiresAt = time.Now().Add(-time.Minute)
	if err := a.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
}

// expectPanic runs fn and asserts it panics with a message containing
// want.
func expectPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic containing %q", want)
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, want) {
			t.Fatalf("panic %q does not contain %q", msg, want)
		}
	}()
	fn()
}

// discardLogger returns a logger that drops every record.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
