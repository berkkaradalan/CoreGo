package authhttp

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

func TestErrorMappingTable(t *testing.T) {
	tests := []struct {
		err     error
		code    int
		message string
	}{
		{auth.ErrInvalidCredentials, 401, msgInvalidCredentials},
		{auth.ErrSessionNotFound, 401, msgSessionNotFound},
		{auth.ErrSessionExpired, 401, msgSessionExpired},
		{auth.ErrUserBanned, 403, msgUserBanned},
		{auth.ErrEmailNotVerified, 403, msgEmailNotVerified},
		{auth.ErrUserNotFound, 404, msgUserNotFound},
		{auth.ErrAccountNotFound, 409, msgAccountNotFound},
		{auth.ErrUserAlreadyExists, 409, msgUserAlreadyExists},
		{auth.ErrEmailAlreadyVerified, 409, msgEmailAlreadyVerified},
		{auth.ErrConflict, 409, msgConflict},
		{auth.ErrNameRequired, 400, msgNameRequired},
		{auth.ErrNameTooLong, 400, msgNameTooLong},
		{auth.ErrInvalidImageURL, 400, msgInvalidImageURL},
		{auth.ErrEmailRequired, 400, msgEmailRequired},
		{auth.ErrInvalidEmail, 400, msgInvalidEmail},
		{auth.ErrPasswordRequired, 400, msgPasswordRequired},
		{auth.ErrWeakPassword, 400, msgWeakPassword},
		{auth.ErrInvalidCurrentPassword, 400, msgInvalidCurrentPassword},
		{auth.ErrInvalidToken, 400, msgInvalidToken},
		{auth.ErrTokenExpired, 400, msgTokenExpired},
		{auth.ErrMailerNotConfigured, 503, msgMailerNotConfigured},
		{auth.ErrEmailDeliveryFailed, 502, msgEmailDeliveryFailed},
	}
	if len(tests) != len(errorMappings) {
		t.Fatalf("the test covers %d sentinels, the table maps %d", len(tests), len(errorMappings))
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			for _, err := range []error{tt.err, fmt.Errorf("context: %w", tt.err)} {
				he := httpErrorFor(err)
				if he == nil || he.Code != tt.code || he.Message != tt.message {
					t.Fatalf("httpErrorFor(%v) = %+v, want %d %q", err, he, tt.code, tt.message)
				}
				if !errors.Is(he, tt.err) {
					t.Fatal("the mapped error must keep the sentinel as its cause")
				}
				// Messages are user facing: no package prefixes or internals.
				if strings.Contains(he.Message, "auth:") || strings.Contains(he.Message, "web:") {
					t.Fatalf("message %q leaks an internal prefix", he.Message)
				}
			}
		})
	}
}

// The body-parsing messages of the auth routes are web's own (resolved
// through web.AsHTTPError), so they can never drift from what any other
// web route answers. The literals pin the public wording.
const (
	msgInvalidJSON          = "Request body is not valid JSON."
	msgUnsupportedMediaType = "Content-Type must be application/json."
	msgBodyTooLarge         = "Request body exceeds the configured size limit."
)

func TestWebBodySentinelsMatchTheWebFramework(t *testing.T) {
	tests := []struct {
		err     error
		code    int
		message string
	}{
		{web.ErrInvalidJSON, 400, msgInvalidJSON},
		{web.ErrUnsupportedMediaType, 415, msgUnsupportedMediaType},
		{web.ErrBodyTooLarge, 413, msgBodyTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			for _, err := range []error{tt.err, fmt.Errorf("context: %w", tt.err)} {
				he := httpErrorFor(err)
				if he == nil || he.Code != tt.code || he.Message != tt.message {
					t.Fatalf("httpErrorFor(%v) = %+v, want %d %q", err, he, tt.code, tt.message)
				}
				if !errors.Is(he, tt.err) {
					t.Fatal("the mapped error must keep the sentinel as its cause")
				}
				webErr, ok := web.AsHTTPError(err)
				if !ok || webErr.Code != he.Code || webErr.Message != he.Message {
					t.Fatalf("auth mapping %d %q differs from web.AsHTTPError %+v", he.Code, he.Message, webErr)
				}
			}
		})
	}
}

func TestHTTPErrorForKeepsExistingHTTPErrors(t *testing.T) {
	teapot := web.NewHTTPError(http.StatusTeapot, "short and stout")
	for _, err := range []error{teapot, fmt.Errorf("ctx: %w", teapot)} {
		if got := httpErrorFor(err); got != teapot {
			t.Fatalf("httpErrorFor(%v) = %+v, want the existing *web.HTTPError", err, got)
		}
	}
}

func TestMapErrorPassesUnknownErrorsThrough(t *testing.T) {
	if mapError(nil) != nil {
		t.Fatal("mapError(nil) must be nil")
	}
	unknown := errors.New("boom")
	if got := mapError(unknown); got != unknown {
		t.Fatalf("mapError(unknown) = %v, want the same error", got)
	}
	if httpErrorFor(unknown) != nil {
		t.Fatal("unknown errors have no mapping")
	}
	// Store-level sentinels are translated by the service, never here.
	for _, err := range []error{auth.ErrNotFound, auth.ErrDuplicate, auth.ErrInvalidConfig} {
		if httpErrorFor(err) != nil {
			t.Fatalf("%v must not be mapped", err)
		}
	}
}

func TestUnknownErrorsBecomeLogged500(t *testing.T) {
	store := &failingStore{MemoryStore: auth.NewMemoryStore()}
	app := newTestApp(t, appOptions{store: store})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	store.failGetUserByEmail.Store(true)

	rec := app.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
		"email": "ada@example.com", "password": testPassword,
	}})
	expectError(t, rec, http.StatusInternalServerError, "Internal Server Error")
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Fatal("internal error details leaked to the client")
	}
}

func TestBodyErrors(t *testing.T) {
	app := newTestApp(t, appOptions{server: func(s *web.Server) { s.MaxBodyBytes = 64 }})
	tests := []struct {
		name    string
		r       req
		code    int
		message string
	}{
		{"malformed JSON", req{raw: `{"email":`, ctype: "application/json"}, 400, msgInvalidJSON},
		{"wrong JSON type", req{raw: `{"email":42}`, ctype: "application/json"}, 400, msgInvalidJSON},
		{"empty body", req{ctype: "application/json"}, 400, msgInvalidJSON},
		{"no content type", req{raw: `{}`}, 415, msgUnsupportedMediaType},
		{"form content type", req{raw: `email=a`, ctype: "application/x-www-form-urlencoded"}, 415, msgUnsupportedMediaType},
		{"charset is accepted", req{raw: `{"email":"","password":""}`, ctype: "application/json; charset=utf-8"}, 401, msgInvalidCredentials},
		{"too large", req{raw: `{"email":"` + strings.Repeat("a", 100) + `"}`, ctype: "application/json"}, 413, msgBodyTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.method, tt.r.path = http.MethodPost, "/signin"
			expectError(t, app.do(t, tt.r), tt.code, tt.message)
		})
	}
}

func TestWriteHTTPErrorMatchesTheWebErrorHandler(t *testing.T) {
	// writeHTTPError logs unknown errors through slog.Default().
	previous := slog.Default()
	slog.SetDefault(discardLogger())
	t.Cleanup(func() { slog.SetDefault(previous) })

	errs := []error{
		errMissingToken(),
		auth.ErrSessionExpired,
		auth.ErrUserBanned,
		web.NewHTTPError(http.StatusTeapot, ""),
		errors.New("unknown"),
	}
	for _, err := range errs {
		t.Run(err.Error(), func(t *testing.T) {
			s := web.New()
			s.Logger = discardLogger()
			s.Get("/", func(c *web.Context) error { return mapError(err) })
			webRec := httptest.NewRecorder()
			s.ServeHTTP(webRec, httptest.NewRequest(http.MethodGet, "/", nil))

			plainRec := httptest.NewRecorder()
			writeHTTPError(plainRec, httptest.NewRequest(http.MethodGet, "/", nil), err)

			if webRec.Code != plainRec.Code || webRec.Body.String() != plainRec.Body.String() ||
				webRec.Header().Get("Content-Type") != plainRec.Header().Get("Content-Type") {
				t.Fatalf("web %d %q %q vs net/http %d %q %q",
					webRec.Code, webRec.Header().Get("Content-Type"), webRec.Body,
					plainRec.Code, plainRec.Header().Get("Content-Type"), plainRec.Body)
			}
		})
	}
}
