package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestNewHTTPError(t *testing.T) {
	e := NewHTTPError(404, "user not found")
	if e == nil {
		t.Fatal("NewHTTPError returned nil")
	}
	if e.Code != 404 {
		t.Errorf("Code = %d, want 404", e.Code)
	}
	if e.Message != "user not found" {
		t.Errorf("Message = %q, want %q", e.Message, "user not found")
	}
	if e.Err != nil {
		t.Errorf("Err = %v, want nil", e.Err)
	}
}

func TestHTTPErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *HTTPError
		want string
	}{
		{
			name: "code and message",
			err:  NewHTTPError(404, "user not found"),
			want: "web: 404 user not found",
		},
		{
			name: "empty message falls back to status text",
			err:  NewHTTPError(502, ""),
			want: "web: 502 Bad Gateway",
		},
		{
			name: "wrapped cause included",
			err:  &HTTPError{Code: 500, Message: "query failed", Err: errors.New("disk on fire")},
			want: "web: 500 query failed: disk on fire",
		},
		{
			name: "unknown code with empty message",
			err:  NewHTTPError(599, ""),
			want: "web: 599 status 599",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHTTPErrorUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := &HTTPError{Code: 400, Message: "bad input", Err: cause}
	if !errors.Is(e, cause) {
		t.Error("errors.Is(e, cause) = false, want true")
	}
	if got := e.Unwrap(); !errors.Is(got, cause) {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}

	bare := NewHTTPError(418, "teapot")
	if got := bare.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
		code int
	}{
		{"ErrBodyTooLarge", ErrBodyTooLarge, 413},
		{"ErrUnsupportedMediaType", ErrUnsupportedMediaType, 415},
		{"ErrInvalidJSON", ErrInvalidJSON, 400},
	}
	for _, s := range sentinels {
		t.Run(s.name, func(t *testing.T) {
			if s.err == nil {
				t.Fatal("sentinel error is nil")
			}
			// Wrapped sentinel errors stay identifiable via errors.Is.
			wrapped := fmt.Errorf("handler failed: %w", s.err)
			if !errors.Is(wrapped, s.err) {
				t.Errorf("errors.Is(wrapped, %v) = false, want true", s.err)
			}
			// Distinctness of the sentinels.
			for _, other := range sentinels {
				if other.err == s.err {
					continue
				}
				if errors.Is(s.err, other.err) {
					t.Errorf("errors.Is(%v, %v) = true, want false", s.err, other.err)
				}
			}
			if he, ok := AsHTTPError(wrapped); !ok || he.Code != s.code {
				t.Errorf("AsHTTPError(wrapped %v) = %+v, %v; want code %d", s.err, he, ok, s.code)
			}
		})
	}
}

func TestHTTPErrorHelpers(t *testing.T) {
	tests := []struct {
		name     string
		err      *HTTPError
		wantCode int
		wantText string
	}{
		{name: "BadRequest", err: BadRequest(""), wantCode: 400, wantText: "Bad Request"},
		{name: "Unauthorized", err: Unauthorized(""), wantCode: 401, wantText: "Unauthorized"},
		{name: "Forbidden", err: Forbidden(""), wantCode: 403, wantText: "Forbidden"},
		{name: "NotFound", err: NotFound(""), wantCode: 404, wantText: "Not Found"},
		{name: "Conflict", err: Conflict(""), wantCode: 409, wantText: "Conflict"},
		{name: "TooManyRequests", err: TooManyRequests(""), wantCode: 429, wantText: "Too Many Requests"},
		{name: "custom message kept", err: NotFound("user not found"), wantCode: 404, wantText: "user not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("Code = %d, want %d", tt.err.Code, tt.wantCode)
			}
			if tt.err.Err != nil {
				t.Errorf("Err = %v, want nil", tt.err.Err)
			}
			if got := statusText(tt.err.Code, tt.err.Message); got != tt.wantText {
				t.Errorf("rendered message = %q, want %q", got, tt.wantText)
			}
			// End to end through the default error handler.
			s := New()
			returned := tt.err
			s.Get("/", func(c *Context) error { return returned })
			rec := serveRequest(t, s, "GET", "/", nil)
			if rec.Code != tt.wantCode || rec.Body.String() != `{"error":"`+tt.wantText+`"}` {
				t.Errorf("response = %d %q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAsHTTPError(t *testing.T) {
	teapot := NewHTTPError(http.StatusTeapot, "short and stout")
	tests := []struct {
		name        string
		err         error
		wantOK      bool
		wantCode    int
		wantMessage string
		wantSame    *HTTPError // when set, the exact *HTTPError must come back
	}{
		{name: "nil", err: nil, wantOK: false},
		{name: "unknown error", err: errors.New("boom"), wantOK: false},
		{name: "wrapped unknown error", err: fmt.Errorf("ctx: %w", errors.New("boom")), wantOK: false},
		{name: "http error", err: teapot, wantOK: true, wantCode: http.StatusTeapot, wantMessage: "short and stout", wantSame: teapot},
		{name: "wrapped http error", err: fmt.Errorf("ctx: %w", teapot), wantOK: true, wantCode: http.StatusTeapot, wantMessage: "short and stout", wantSame: teapot},
		{name: "http error with empty message", err: BadRequest(""), wantOK: true, wantCode: http.StatusBadRequest, wantMessage: ""},
		{name: "bare invalid JSON", err: ErrInvalidJSON, wantOK: true, wantCode: http.StatusBadRequest, wantMessage: "Request body is not valid JSON."},
		{name: "wrapped invalid JSON", err: fmt.Errorf("%w: unknown field \"x\"", ErrInvalidJSON), wantOK: true, wantCode: http.StatusBadRequest, wantMessage: "Request body is not valid JSON."},
		{name: "bare unsupported media type", err: ErrUnsupportedMediaType, wantOK: true, wantCode: http.StatusUnsupportedMediaType, wantMessage: "Content-Type must be application/json."},
		{name: "wrapped unsupported media type", err: fmt.Errorf("ct: %w", ErrUnsupportedMediaType), wantOK: true, wantCode: http.StatusUnsupportedMediaType, wantMessage: "Content-Type must be application/json."},
		{name: "bare body too large", err: ErrBodyTooLarge, wantOK: true, wantCode: http.StatusRequestEntityTooLarge, wantMessage: "Request body exceeds the configured size limit."},
		{name: "wrapped body too large", err: fmt.Errorf("read: %w", ErrBodyTooLarge), wantOK: true, wantCode: http.StatusRequestEntityTooLarge, wantMessage: "Request body exceeds the configured size limit."},
		{name: "http error wins over a sentinel", err: &HTTPError{Code: http.StatusTeapot, Message: "custom", Err: ErrInvalidJSON}, wantOK: true, wantCode: http.StatusTeapot, wantMessage: "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			he, ok := AsHTTPError(tt.err)
			if ok != tt.wantOK {
				t.Fatalf("AsHTTPError(%v) ok = %v, want %v", tt.err, ok, tt.wantOK)
			}
			if !ok {
				if he != nil {
					t.Fatalf("AsHTTPError(%v) = %+v, want nil", tt.err, he)
				}
				return
			}
			if he.Code != tt.wantCode || he.Message != tt.wantMessage {
				t.Fatalf("AsHTTPError(%v) = %d %q, want %d %q", tt.err, he.Code, he.Message, tt.wantCode, tt.wantMessage)
			}
			if tt.wantSame != nil && he != tt.wantSame {
				t.Fatal("an existing *HTTPError must be returned as-is")
			}
			if !errors.Is(he, tt.err) && he != tt.wantSame {
				t.Fatalf("the mapped error must keep %v as its cause", tt.err)
			}
			if strings.Contains(he.Message, "web:") {
				t.Fatalf("message %q leaks the package prefix", he.Message)
			}
		})
	}
}

func TestAsHTTPErrorInCustomErrorHandler(t *testing.T) {
	s := New()
	s.OnError(func(c *Context, err error) {
		if he, ok := AsHTTPError(err); ok {
			_ = c.JSON(he.Code, map[string]string{"message": he.Message})
			return
		}
		_ = c.JSON(http.StatusInternalServerError, map[string]string{"message": "oops"})
	})
	s.Post("/bind", func(c *Context) error {
		var v struct{}
		return c.BindJSON(&v)
	})
	s.Get("/boom", func(c *Context) error { return errors.New("boom") })

	rec := serveRequest(t, s, "POST", "/bind", nil)
	if rec.Code != http.StatusUnsupportedMediaType || rec.Body.String() != `{"message":"Content-Type must be application/json."}` {
		t.Fatalf("bind response = %d %q", rec.Code, rec.Body.String())
	}
	rec = serveRequest(t, s, "GET", "/boom", nil)
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != `{"message":"oops"}` {
		t.Fatalf("boom response = %d %q", rec.Code, rec.Body.String())
	}
}
