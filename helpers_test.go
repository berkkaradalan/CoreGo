package corego

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// facadeEnvVars lists every environment variable the facade reads.
var facadeEnvVars = []string{
	"APP_URL", "URL", "PORT",
	"MONGODB_CONNECTION_URL", "MONGODB_DATABASE", "POSTGRES_CONNECTION_URL",
	"COREGO_AUTH_STORE", "COREGO_AUTH_TABLE_PREFIX",
	"COREGO_REQUIRE_VERIFIED_EMAIL", "COREGO_AUTH_COOKIE", "COREGO_MAIL",
	"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM",
	"CORS_ALLOWED_ORIGINS", "TRUST_PROXY_HEADERS",
	"COREGO_RESET_PASSWORD_URL", "COREGO_VERIFY_EMAIL_REDIRECT_URL",
	"COREGO_BANNER", "NO_COLOR", "FORCE_COLOR",
}

// isolateEnv runs the test in an empty working directory (so no .env
// file is picked up) with every facade variable set to the empty
// string, which the env package treats as "not configured". Nothing is
// dialed unless the test sets a connection variable itself.
func isolateEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, key := range facadeEnvVars {
		t.Setenv(key, "")
	}
}

// logBuffer is a goroutine-safe log sink: background email deliveries
// and servers log from other goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newTestLogger returns a Debug level text logger writing into a fresh
// logBuffer.
func newTestLogger() (*slog.Logger, *logBuffer) {
	buf := &logBuffer{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// newCore calls New and registers Close as cleanup.
func newCore(t *testing.T, cfg *Config) *Core {
	t.Helper()
	core, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := core.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return core
}

// request describes one HTTP call against a handler.
type request struct {
	method  string
	path    string
	body    any
	token   string
	headers map[string]string
}

// do serves req on h and returns the recorded response.
func do(t *testing.T, h http.Handler, req request) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if req.body != nil {
		raw, err := json.Marshal(req.body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(req.method, req.path, body)
	if req.body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if req.token != "" {
		r.Header.Set("Authorization", "Bearer "+req.token)
	}
	for k, v := range req.headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// decode unmarshals a JSON response body.
func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
}

// signUp creates an account through POST /auth/signup and returns the
// session token.
func signUp(t *testing.T, h http.Handler, email string) string {
	t.Helper()
	w := do(t, h, request{method: http.MethodPost, path: "/auth/signup", body: map[string]any{
		"name": "Ada Lovelace", "email": email, "password": "analytical-engine",
	}})
	if w.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, body %s", w.Code, w.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	decode(t, w, &out)
	if out.Token == "" {
		t.Fatalf("signup returned no token: %s", w.Body.String())
	}
	return out.Token
}

// missingSubstrings returns the wanted substrings s does not contain.
func missingSubstrings(s string, want ...string) []string {
	var missing []string
	for _, w := range want {
		if !strings.Contains(s, w) {
			missing = append(missing, w)
		}
	}
	return missing
}
