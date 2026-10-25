package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain silences the framework's default logging (panic recovery,
// error fallbacks) so test output stays readable.
func TestMain(m *testing.M) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	code := m.Run()
	slog.SetDefault(previous)
	os.Exit(code)
}

// serveRequest runs a single request through the server's ServeHTTP and
// returns the recorder.
func serveRequest(t *testing.T, s *Server, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// text returns a handler answering 200 with body.
func text(body string) HandlerFunc {
	return func(c *Context) error { return c.String(http.StatusOK, body) }
}

// bufferLogger returns a logger writing JSON lines to a buffer.
func bufferLogger(level slog.Level) (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level})), buf
}

// syncBuffer is a bytes.Buffer safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestNewDefaults(t *testing.T) {
	s := New()
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"MaxBodyBytes", s.MaxBodyBytes, int64(1 << 20)},
		{"TrustProxyHeaders", s.TrustProxyHeaders, false},
		{"ClientIPHeader", s.ClientIPHeader, "X-Forwarded-For"},
		{"ReadHeaderTimeout", s.ReadHeaderTimeout, 10 * time.Second},
		{"ReadTimeout", s.ReadTimeout, 30 * time.Second},
		{"WriteTimeout", s.WriteTimeout, 30 * time.Second},
		{"IdleTimeout", s.IdleTimeout, 120 * time.Second},
		{"ShutdownTimeout", s.ShutdownTimeout, 10 * time.Second},
		{"Logger", s.Logger, (*slog.Logger)(nil)},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if len(s.Routes()) != 0 {
		t.Errorf("Routes() = %v, want empty", s.Routes())
	}
}

func TestServerRoutingMatrix(t *testing.T) {
	s := New()
	render := func(marker string, c *Context) string {
		r := strings.NewReplacer(
			"<id>", c.Param("id"),
			"<pid>", c.Param("pid"),
			"<path>", c.Param("path"),
			"<p>", c.Param("p"),
			"<oid>", c.Param("oid"),
		)
		return r.Replace(marker)
	}
	mk := func(method, path, marker string) {
		s.Handle(method, path, func(c *Context) error {
			return c.String(200, render(marker, c))
		})
	}
	mk(http.MethodGet, "/", "root")
	mk(http.MethodGet, "/users", "list")
	mk(http.MethodPost, "/users", "create")
	mk(http.MethodGet, "/users/new", "static-new")
	mk(http.MethodGet, "/users/:id", "id=<id>")
	mk(http.MethodGet, "/users/:id/posts/:pid", "id=<id> pid=<pid>")
	mk(http.MethodGet, "/files/*path", "path=<path>")
	mk(http.MethodGet, "/files/report.txt", "static-report")
	mk(http.MethodGet, "/a/:p/b", "p=<p>")
	mk(http.MethodGet, "/a/x/c", "static-axc")
	mk(http.MethodGet, "/café", "cafe")
	mk(http.MethodGet, "/Users", "capital-users")
	mk(http.MethodGet, "/orders/:oid", "oid=<oid>") // no /orders route registered

	tests := []struct {
		name      string
		method    string
		target    string
		wantCode  int
		wantBody  string
		wantAllow string
	}{
		{name: "root", method: "GET", target: "/", wantCode: 200, wantBody: "root"},
		{name: "root trailing slash", method: "GET", target: "//", wantCode: 200, wantBody: "root"},
		{name: "static list", method: "GET", target: "/users", wantCode: 200, wantBody: "list"},
		{name: "trailing slash equivalence", method: "GET", target: "/users/", wantCode: 200, wantBody: "list"},
		{name: "static beats param", method: "GET", target: "/users/new", wantCode: 200, wantBody: "static-new"},
		{name: "param captures one segment", method: "GET", target: "/users/42", wantCode: 200, wantBody: "id=42"},
		{name: "param non-empty required 404", method: "GET", target: "/orders", wantCode: 404, wantBody: `{"error":"Not Found"}`},
		{name: "param non-empty required 404 trailing slash", method: "GET", target: "/orders/", wantCode: 404, wantBody: `{"error":"Not Found"}`},
		{name: "param-only route matches with value", method: "GET", target: "/orders/3", wantCode: 200, wantBody: "oid=3"},
		{name: "interior double slash 404", method: "GET", target: "/users//42", wantCode: 404, wantBody: `{"error":"Not Found"}`},
		{name: "double param depth", method: "GET", target: "/users/7/posts/9", wantCode: 200, wantBody: "id=7 pid=9"},
		{name: "wildcard captures rest", method: "GET", target: "/files/a/b/c.txt", wantCode: 200, wantBody: "path=a/b/c.txt"},
		{name: "wildcard single segment", method: "GET", target: "/files/x", wantCode: 200, wantBody: "path=x"},
		{name: "wildcard empty tail", method: "GET", target: "/files", wantCode: 200, wantBody: "path="},
		{name: "wildcard empty tail with slash", method: "GET", target: "/files/", wantCode: 200, wantBody: "path="},
		{name: "static beats wildcard", method: "GET", target: "/files/report.txt", wantCode: 200, wantBody: "static-report"},
		{name: "backtracking into param", method: "GET", target: "/a/x/b", wantCode: 200, wantBody: "p=x"},
		{name: "static path wins when exact", method: "GET", target: "/a/x/c", wantCode: 200, wantBody: "static-axc"},
		{name: "backtracking miss", method: "GET", target: "/a/y/z", wantCode: 404, wantBody: `{"error":"Not Found"}`},
		{name: "unicode static route", method: "GET", target: "/café", wantCode: 200, wantBody: "cafe"},
		{name: "percent-encoded unicode static route", method: "GET", target: "/caf%C3%A9", wantCode: 200, wantBody: "cafe"},
		{name: "percent-encoded param decoded once", method: "GET", target: "/users/a%20b", wantCode: 200, wantBody: "id=a b"},
		{name: "encoded percent in param", method: "GET", target: "/users/100%25", wantCode: 200, wantBody: "id=100%"},
		{name: "double-encoded param decoded only once", method: "GET", target: "/users/a%2520b", wantCode: 200, wantBody: "id=a%20b"},
		{name: "non-ascii encoded param", method: "GET", target: "/users/%E4%B8%AD%E6%96%87", wantCode: 200, wantBody: "id=中文"},
		{name: "encoded slash stays in segment", method: "GET", target: "/users/a%2Fb", wantCode: 200, wantBody: "id=a/b"},
		{name: "static case differs 404", method: "GET", target: "/uSers", wantCode: 404, wantBody: `{"error":"Not Found"}`},
		{name: "capital route matches itself", method: "GET", target: "/Users", wantCode: 200, wantBody: "capital-users"},
		{name: "unknown path 404", method: "GET", target: "/definitely/missing", wantCode: 404, wantBody: `{"error":"Not Found"}`},
		{name: "method not allowed", method: "DELETE", target: "/users", wantCode: 405, wantBody: `{"error":"Method Not Allowed"}`, wantAllow: "GET, POST"},
		{name: "wrong method on param route 405", method: "PUT", target: "/users/3", wantCode: 405, wantBody: `{"error":"Method Not Allowed"}`, wantAllow: "GET"},
		{name: "post works", method: "POST", target: "/users", wantCode: 200, wantBody: "create"},
		{name: "post trailing slash", method: "POST", target: "/users/", wantCode: 200, wantBody: "create"},
		{name: "lowercase request method matches", method: "post", target: "/users", wantCode: 200, wantBody: "create"},
		{name: "double slash anywhere 404", method: "GET", target: "//users", wantCode: 404, wantBody: `{"error":"Not Found"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveRequest(t, s, tt.method, tt.target, nil)
			if rec.Code != tt.wantCode {
				t.Errorf("code = %d, want %d (body %q)", rec.Code, tt.wantCode, rec.Body.String())
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantAllow != "" {
				if got := rec.Header().Get("Allow"); got != tt.wantAllow {
					t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
				}
			} else if tt.wantCode != 405 && rec.Header().Get("Allow") != "" {
				t.Errorf("unexpected Allow header %q", rec.Header().Get("Allow"))
			}
		})
	}
}

func TestServerNotFoundResponseShape(t *testing.T) {
	s := New()
	rec := serveRequest(t, s, http.MethodGet, "/nothing", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"Not Found"}` {
		t.Errorf("body = %q, want 404 JSON envelope", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestServerMethodNotAllowed(t *testing.T) {
	s := New()
	for _, method := range []string{"GET", "POST"} {
		s.Handle(method, "/resource", text(method))
	}
	rec := serveRequest(t, s, http.MethodDelete, "/resource", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, POST" {
		t.Errorf("Allow = %q, want %q", got, "GET, POST")
	}
	if got := rec.Body.String(); got != `{"error":"Method Not Allowed"}` {
		t.Errorf("body = %q", got)
	}
	// Trailing slash keeps the 405 semantics.
	rec = serveRequest(t, s, http.MethodDelete, "/resource/", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("trailing slash code = %d, want 405", rec.Code)
	}
	// A path that matches no method at all stays a 404.
	rec = serveRequest(t, s, http.MethodDelete, "/resource/sub", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
}

func TestServerFallbacksUseCustomErrorHandler(t *testing.T) {
	s := New()
	s.Get("/only-get", text("ok"))
	s.OnError(func(c *Context, err error) {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			_ = c.String(httpErr.Code, fmt.Sprintf("custom %d", httpErr.Code))
			return
		}
		_ = c.String(500, "custom 500")
	})
	rec := serveRequest(t, s, http.MethodGet, "/missing", nil)
	if rec.Code != 404 || rec.Body.String() != "custom 404" {
		t.Errorf("404 = %d %q, want custom rendering", rec.Code, rec.Body.String())
	}
	rec = serveRequest(t, s, http.MethodPost, "/only-get", nil)
	if rec.Code != 405 || rec.Body.String() != "custom 405" {
		t.Errorf("405 = %d %q, want custom rendering", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Errorf("Allow = %q, want GET even with a custom error handler", got)
	}
}

func TestServerAutoOptions(t *testing.T) {
	s := New()
	s.Get("/r", text("get"))
	s.Post("/r", text("post"))
	rec := serveRequest(t, s, http.MethodOptions, "/r", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, OPTIONS, POST" {
		t.Errorf("Allow = %q, want %q", got, "GET, OPTIONS, POST")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	// OPTIONS for an entirely unknown path is a plain 404.
	rec = serveRequest(t, s, http.MethodOptions, "/unknown", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
	if rec.Header().Get("Allow") != "" {
		t.Errorf("Allow = %q, want empty", rec.Header().Get("Allow"))
	}
}

func TestServerExplicitOptionsRoute(t *testing.T) {
	s := New()
	s.Get("/r", text("get"))
	s.Options("/r", func(c *Context) error {
		c.SetHeader("Allow", "GET, OPTIONS")
		return c.NoContent()
	})
	rec := serveRequest(t, s, http.MethodOptions, "/r", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, OPTIONS" {
		t.Errorf("Allow = %q, want the handler-provided value", got)
	}
}

func TestServerHeadFallbackToGet(t *testing.T) {
	s := New()
	s.Get("/data", func(c *Context) error {
		return c.JSON(200, map[string]string{"message": "hello head"})
	})
	rec := serveRequest(t, s, http.MethodHead, "/data", nil)
	if rec.Code != 200 {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty (suppressed)", rec.Body.String())
	}
	want := `{"message":"hello head"}`
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(want)) {
		t.Errorf("Content-Length = %q, want %d", got, len(want))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestServerHeadExplicitRoute(t *testing.T) {
	s := New()
	s.Head("/data", text("explicit head body"))
	s.Get("/data", text("get body"))
	rec := serveRequest(t, s, http.MethodHead, "/data", nil)
	if rec.Code != 200 {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	// The explicit HEAD handler ran (its body length is reported).
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len("explicit head body")) {
		t.Errorf("Content-Length = %q, want %d", got, len("explicit head body"))
	}
}

func TestServerHeadEdgeCases(t *testing.T) {
	s := New()
	s.Get("/empty", func(c *Context) error { return c.NoContent() })
	s.Get("/explicit-length", func(c *Context) error {
		c.SetHeader("Content-Length", "99")
		return c.String(200, "abc")
	})
	tests := []struct {
		name       string
		target     string
		wantCode   int
		wantLength string
	}{
		{name: "no content has no length", target: "/empty", wantCode: 204, wantLength: ""},
		{name: "handler-set length kept", target: "/explicit-length", wantCode: 200, wantLength: "99"},
		{name: "unknown path 404 without body", target: "/missing", wantCode: 404, wantLength: strconv.Itoa(len(`{"error":"Not Found"}`))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveRequest(t, s, http.MethodHead, tt.target, nil)
			if rec.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Body.Len() != 0 {
				t.Errorf("body = %q, want empty", rec.Body.String())
			}
			if got := rec.Header().Get("Content-Length"); got != tt.wantLength {
				t.Errorf("Content-Length = %q, want %q", got, tt.wantLength)
			}
		})
	}
}

func TestServerPanicRecoveryAndSurvival(t *testing.T) {
	logger, logs := bufferLogger(slog.LevelError)
	s := New()
	s.Logger = logger
	s.Get("/boom", func(c *Context) error { panic("handler panic") })
	s.Get("/ok", text("still alive"))
	rec := serveRequest(t, s, http.MethodGet, "/boom", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"Internal Server Error"}` {
		t.Errorf("body = %q, want generic 500 envelope", got)
	}
	if !strings.Contains(logs.String(), "handler panic") || !strings.Contains(logs.String(), "panic recovered") {
		t.Errorf("panic not logged to the server Logger: %s", logs.String())
	}
	// The server keeps serving afterwards.
	rec = serveRequest(t, s, http.MethodGet, "/ok", nil)
	if rec.Code != 200 || rec.Body.String() != "still alive" {
		t.Errorf("after panic: code=%d body=%q, want 200 'still alive'", rec.Code, rec.Body.String())
	}

	// Panics inside global middleware, outside the per-route recovery,
	// are contained by the server's safety net.
	s2 := New()
	s2.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error { panic("middleware panic") }
	})
	s2.Get("/x", text("x"))
	for i := 0; i < 2; i++ {
		rec = serveRequest(t, s2, http.MethodGet, "/x", nil)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("middleware panic run %d code = %d, want 500", i, rec.Code)
		}
	}
	// The safety net also covers the framework fallbacks.
	rec = serveRequest(t, s2, http.MethodGet, "/missing", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("middleware panic on 404 path code = %d, want 500", rec.Code)
	}
}

func TestServerPanicAfterWriteKeepsResponse(t *testing.T) {
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			_ = next(c)
			panic("late middleware panic")
		}
	})
	s.Get("/x", text("written first"))
	rec := serveRequest(t, s, http.MethodGet, "/x", nil)
	if rec.Code != 200 || rec.Body.String() != "written first" {
		t.Errorf("code/body = %d/%q, want the already written response", rec.Code, rec.Body.String())
	}
}

func TestServerAbortHandlerPanicPropagates(t *testing.T) {
	tests := []struct {
		name  string
		setup func(s *Server)
	}{
		{name: "from handler", setup: func(s *Server) {
			s.Get("/x", func(c *Context) error { panic(http.ErrAbortHandler) })
		}},
		{name: "from global middleware", setup: func(s *Server) {
			s.Use(func(next HandlerFunc) HandlerFunc {
				return func(c *Context) error { panic(http.ErrAbortHandler) }
			})
			s.Get("/x", text("x"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			tt.setup(s)
			defer func() {
				if r := recover(); r != http.ErrAbortHandler {
					t.Errorf("recovered %v, want http.ErrAbortHandler to reach net/http", r)
				}
			}()
			serveRequest(t, s, http.MethodGet, "/x", nil)
		})
	}
}

func TestServerCustomOnError(t *testing.T) {
	s := New()
	s.OnError(func(c *Context, err error) {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			_ = c.String(httpErr.Code, "custom:"+httpErr.Message)
			return
		}
		_ = c.String(500, "custom:unknown")
	})
	s.Get("/e", func(c *Context) error { return NewHTTPError(422, "bad value") })
	rec := serveRequest(t, s, http.MethodGet, "/e", nil)
	if rec.Code != 422 || rec.Body.String() != "custom:bad value" {
		t.Errorf("code/body = %d/%q, want 422 'custom:bad value'", rec.Code, rec.Body.String())
	}
	// Restoring the default handler with nil.
	s.OnError(nil)
	rec = serveRequest(t, s, http.MethodGet, "/e", nil)
	if rec.Code != 422 || rec.Body.String() != `{"error":"bad value"}` {
		t.Errorf("default restore code/body = %d/%q", rec.Code, rec.Body.String())
	}
}

func TestServerDefaultErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
		wantLog  bool
	}{
		{name: "bare invalid json", err: ErrInvalidJSON, wantCode: 400, wantBody: `{"error":"Request body is not valid JSON."}`},
		{name: "bare unsupported media", err: ErrUnsupportedMediaType, wantCode: 415, wantBody: `{"error":"Content-Type must be application/json."}`},
		{name: "bare body too large", err: ErrBodyTooLarge, wantCode: 413, wantBody: `{"error":"Request body exceeds the configured size limit."}`},
		{name: "wrapped invalid json", err: fmt.Errorf("decode: %w", ErrInvalidJSON), wantCode: 400, wantBody: `{"error":"Request body is not valid JSON."}`},
		{name: "wrapped body too large", err: fmt.Errorf("read: %w", ErrBodyTooLarge), wantCode: 413, wantBody: `{"error":"Request body exceeds the configured size limit."}`},
		{name: "wrapped unsupported media", err: fmt.Errorf("ct: %w", ErrUnsupportedMediaType), wantCode: 415, wantBody: `{"error":"Content-Type must be application/json."}`},
		{name: "http error", err: NotFound("user not found"), wantCode: 404, wantBody: `{"error":"user not found"}`},
		{name: "wrapped http error", err: fmt.Errorf("query: %w", NewHTTPError(502, "upstream unavailable")), wantCode: 502, wantBody: `{"error":"upstream unavailable"}`},
		{name: "http error empty message", err: BadRequest(""), wantCode: 400, wantBody: `{"error":"Bad Request"}`},
		{name: "http error with hidden cause", err: &HTTPError{Code: 409, Message: "taken", Err: errors.New("secret detail")}, wantCode: 409, wantBody: `{"error":"taken"}`},
		{name: "unknown error is a logged 500", err: errors.New("database password is hunter2"), wantCode: 500, wantBody: `{"error":"Internal Server Error"}`, wantLog: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, logs := bufferLogger(slog.LevelError)
			s := New()
			s.Logger = logger
			returned := tt.err
			s.Get("/e", func(c *Context) error { return returned })
			rec := serveRequest(t, s, http.MethodGet, "/e", nil)
			if rec.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if logged := strings.Contains(logs.String(), "request handling error"); logged != tt.wantLog {
				t.Errorf("logged = %v, want %v (%s)", logged, tt.wantLog, logs.String())
			}
		})
	}
}

func TestServerErrorAfterResponseWritten(t *testing.T) {
	logger, logs := bufferLogger(slog.LevelError)
	s := New()
	s.Logger = logger
	s.Get("/late", func(c *Context) error {
		_ = c.String(200, "first wins")
		return errors.New("too late")
	})
	rec := serveRequest(t, s, http.MethodGet, "/late", nil)
	if rec.Code != 200 || rec.Body.String() != "first wins" {
		t.Errorf("code/body = %d/%q, want 200 'first wins'", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "after response was written") || !strings.Contains(logs.String(), "too late") {
		t.Errorf("late error not logged: %s", logs.String())
	}
}

func TestServerErrorAfterRawWriteIsDetected(t *testing.T) {
	// Writes through Response() must count as written too.
	s := New()
	s.Get("/raw", func(c *Context) error {
		_, _ = c.Response().Write([]byte("raw body"))
		return errors.New("after raw write")
	})
	rec := serveRequest(t, s, http.MethodGet, "/raw", nil)
	if rec.Code != 200 || rec.Body.String() != "raw body" {
		t.Errorf("code/body = %d/%q, want untouched raw response", rec.Code, rec.Body.String())
	}
}

func TestServerJSONMarshalFailureIs500WithoutPartialBody(t *testing.T) {
	s := New()
	s.Get("/bad", func(c *Context) error {
		c.SetHeader("X-Before", "kept")
		return c.JSON(http.StatusOK, map[string]any{"ok": true, "ch": make(chan int)})
	})
	rec := serveRequest(t, s, http.MethodGet, "/bad", nil)
	if rec.Code != 500 {
		t.Errorf("code = %d, want 500", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"Internal Server Error"}` {
		t.Errorf("body = %q, want only the error envelope", got)
	}
}

func TestServerMaxBodyBytesConfig(t *testing.T) {
	s := New()
	s.MaxBodyBytes = 10
	s.Post("/echo", func(c *Context) error {
		b, err := c.Body()
		if err != nil {
			return err
		}
		return c.String(200, string(b))
	})
	rec := serveRequest(t, s, http.MethodPost, "/echo", strings.NewReader("short"))
	if rec.Code != 200 || rec.Body.String() != "short" {
		t.Fatalf("in-limit code/body = %d/%q", rec.Code, rec.Body.String())
	}
	rec = serveRequest(t, s, http.MethodPost, "/echo", strings.NewReader("this is longer than ten"))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit code = %d, want 413", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"Request body exceeds the configured size limit."}` {
		t.Errorf("body = %q", got)
	}
	// Unlimited.
	s.MaxBodyBytes = 0
	rec = serveRequest(t, s, http.MethodPost, "/echo", strings.NewReader(strings.Repeat("a", 100_000)))
	if rec.Code != 200 || rec.Body.Len() != 100_000 {
		t.Errorf("unlimited code/len = %d/%d", rec.Code, rec.Body.Len())
	}
}

func TestServerBindJSONEndToEnd(t *testing.T) {
	s := New()
	s.Post("/users", func(c *Context) error {
		var in struct {
			Name string `json:"name"`
		}
		if err := c.BindJSON(&in); err != nil {
			return err
		}
		return c.JSON(201, map[string]string{"name": in.Name})
	})
	tests := []struct {
		name        string
		contentType string
		body        string
		wantCode    int
		wantBody    string
	}{
		{name: "valid", contentType: "application/json", body: `{"name":"ada"}`, wantCode: 201, wantBody: `{"name":"ada"}`},
		{name: "valid with charset", contentType: "application/json; charset=utf-8", body: `{"name":"bob"}`, wantCode: 201, wantBody: `{"name":"bob"}`},
		{name: "empty body", contentType: "application/json", body: ``, wantCode: 400, wantBody: `{"error":"Request body is not valid JSON."}`},
		{name: "malformed", contentType: "application/json", body: `{oops`, wantCode: 400, wantBody: `{"error":"Request body is not valid JSON."}`},
		{name: "wrong content type", contentType: "text/plain", body: `{"name":"x"}`, wantCode: 415, wantBody: `{"error":"Content-Type must be application/json."}`},
		{name: "missing content type", contentType: "", body: `{"name":"x"}`, wantCode: 415, wantBody: `{"error":"Content-Type must be application/json."}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestServerConvenienceMethods(t *testing.T) {
	s := New()
	regs := []struct {
		register func(path string, h HandlerFunc, mw ...MiddlewareFunc)
		method   string
	}{
		{s.Get, http.MethodGet},
		{s.Post, http.MethodPost},
		{s.Put, http.MethodPut},
		{s.Patch, http.MethodPatch},
		{s.Delete, http.MethodDelete},
		{s.Head, http.MethodHead},
		{s.Options, http.MethodOptions},
	}
	for _, r := range regs {
		method := r.method
		r.register("/thing", func(c *Context) error {
			c.SetHeader("X-Method", method)
			return c.String(200, method)
		})
		rec := serveRequest(t, s, method, "/thing", nil)
		if rec.Code != 200 || rec.Header().Get("X-Method") != method {
			t.Errorf("%s code/X-Method = %d/%q", method, rec.Code, rec.Header().Get("X-Method"))
		}
	}
}

func TestServerHandlerReturningNilWritesResponse(t *testing.T) {
	s := New()
	s.Get("/nil", func(c *Context) error { return nil })
	rec := serveRequest(t, s, http.MethodGet, "/nil", nil)
	if rec.Code != 200 {
		t.Errorf("code = %d, want 200 (implicit)", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}

func TestServerRoutes(t *testing.T) {
	s := New()
	s.Post("/users", text(""))
	s.Get("/users/", text("")) // trailing slash canonicalized
	s.Get("/", text(""))
	api := s.Group("/api")
	api.Delete("/items/:id", text(""))
	api.Get("/items/:id", text(""))
	s.Handle("purge", "/cache", text("")) // method normalized
	want := []Route{
		{Method: "GET", Path: "/"},
		{Method: "DELETE", Path: "/api/items/:id"},
		{Method: "GET", Path: "/api/items/:id"},
		{Method: "PURGE", Path: "/cache"},
		{Method: "GET", Path: "/users"},
		{Method: "POST", Path: "/users"},
	}
	got := s.Routes()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Routes() = %v, want %v", got, want)
	}
	// The returned slice is a copy.
	got[0].Path = "/mutated"
	if s.Routes()[0].Path != "/" {
		t.Error("Routes() exposes internal state")
	}
}

func TestServerConcurrentRequests(t *testing.T) {
	s := New()
	var processed atomic.Int64
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			c.Set("n", processed.Add(1))
			return next(c)
		}
	})
	s.Get("/users/:id", func(c *Context) error {
		n, _ := c.Get("n")
		return c.JSON(200, map[string]any{
			"id":     c.Param("id"),
			"n":      n,
			"method": c.Method(),
			"path":   c.Path(),
		})
	})
	ts := httptest.NewServer(s)
	defer ts.Close()

	const goroutines = 50
	const perGoroutine = 4
	var wg sync.WaitGroup
	failures := make(chan string, goroutines*perGoroutine)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				id := fmt.Sprintf("%d-%d", g, i)
				resp, err := ts.Client().Get(ts.URL + "/users/" + id)
				if err != nil {
					failures <- fmt.Sprintf("request error: %v", err)
					return
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 {
					failures <- fmt.Sprintf("status %d for id %s", resp.StatusCode, id)
					return
				}
				if want := `{"id":"` + id + `","method":"GET","n":`; !strings.HasPrefix(string(body), want) {
					failures <- fmt.Sprintf("body %q for id %s", body, id)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(failures)
	for f := range failures {
		t.Error(f)
	}
	if got := processed.Load(); got != goroutines*perGoroutine {
		t.Errorf("middleware processed %d requests, want %d", got, goroutines*perGoroutine)
	}
}

func TestServerRegistrationWhileServing(t *testing.T) {
	// Registration is synchronized with dispatch; -race verifies it.
	s := New()
	s.Get("/0", text("0"))
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				serveRequest(t, s, http.MethodGet, "/0", nil)
				serveRequest(t, s, http.MethodGet, "/missing", nil)
			}
		}
	}()
	g := s.Group("/g")
	for i := 1; i <= 50; i++ {
		s.Get(fmt.Sprintf("/%d", i), text(strconv.Itoa(i)))
		g.Use(func(next HandlerFunc) HandlerFunc { return next })
		g.Get(fmt.Sprintf("/%d", i), text(strconv.Itoa(i)))
		s.Use(func(next HandlerFunc) HandlerFunc { return next })
		s.OnError(nil)
		_ = s.Routes()
	}
	close(stop)
	wg.Wait()
	if rec := serveRequest(t, s, http.MethodGet, "/g/50", nil); rec.Body.String() != "50" {
		t.Errorf("body = %q, want 50", rec.Body.String())
	}
}

func TestServerSlowHandlerNotInterrupted(t *testing.T) {
	// Guards that nothing in the request path imposes hidden timeouts.
	s := New()
	s.Get("/slow", func(c *Context) error {
		time.Sleep(50 * time.Millisecond)
		return c.String(200, "done")
	})
	ts := httptest.NewServer(s)
	defer ts.Close()
	start := time.Now()
	resp, err := ts.Client().Get(ts.URL + "/slow")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("handler returned in %v, want at least the handler delay", elapsed)
	}
}

func TestServerHeadWriterEdgeCases(t *testing.T) {
	s := New()
	s.Get("/double-status", func(c *Context) error {
		c.Response().(http.Flusher).Flush() // no-op for HEAD, implies 200
		_ = c.Status(http.StatusAccepted)   // ignored: the first status wins
		_, err := c.Response().Write([]byte("body"))
		return err
	})
	s.Head("/raw-head", func(c *Context) error {
		_, err := c.Response().Write([]byte("abc")) // implicit 200
		return err
	})
	tests := []struct {
		target     string
		wantCode   int
		wantLength string
	}{
		{target: "/double-status", wantCode: 200, wantLength: "4"},
		{target: "/raw-head", wantCode: 200, wantLength: "3"},
	}
	for _, tt := range tests {
		rec := serveRequest(t, s, http.MethodHead, tt.target, nil)
		if rec.Code != tt.wantCode || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != tt.wantLength {
			t.Errorf("HEAD %s = %d body %q length %q, want %d empty %s",
				tt.target, rec.Code, rec.Body.String(), rec.Header().Get("Content-Length"), tt.wantCode, tt.wantLength)
		}
	}
}

func TestServerEmptyRequestPathIsRoot(t *testing.T) {
	s := New()
	s.Get("/", text("root"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path, req.URL.RawPath = "", ""
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "root" {
		t.Errorf("code/body = %d/%q, want 200 root", rec.Code, rec.Body.String())
	}
}

// proxyHeaderRequest sends GET / with the given headers.
func proxyHeaderRequest(s *Server, headers map[string]string) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	s.ServeHTTP(httptest.NewRecorder(), req)
}

func TestUntrustedProxyHeaderWarning(t *testing.T) {
	const warning = "Server.TrustProxyHeaders is false"
	tests := []struct {
		name     string
		trust    bool
		header   string // ClientIPHeader; "" keeps the default
		requests []map[string]string
		wantName string // header named in the warning; "" means no warning
	}{
		{name: "x-forwarded-for", requests: []map[string]string{{"X-Forwarded-For": "198.51.100.1"}}, wantName: "X-Forwarded-For"},
		{name: "configured header", header: "cf-connecting-ip", requests: []map[string]string{{"CF-Connecting-IP": "198.51.100.1"}}, wantName: "Cf-Connecting-Ip"},
		{name: "x-forwarded-for with other configured header", header: "X-Real-IP", requests: []map[string]string{{"X-Forwarded-For": "198.51.100.1"}}, wantName: "X-Forwarded-For"},
		{name: "empty configured header means default", header: "", requests: []map[string]string{{"X-Forwarded-For": "1.2.3.4"}}, wantName: "X-Forwarded-For"},
		{name: "no proxy header", requests: []map[string]string{{}, {"X-Real-IP": "1.2.3.4"}}},
		{name: "empty header value", requests: []map[string]string{{"X-Forwarded-For": ""}}},
		{name: "trusted proxy headers", trust: true, requests: []map[string]string{{"X-Forwarded-For": "198.51.100.1"}}},
		{name: "only once", requests: []map[string]string{{}, {"X-Forwarded-For": "1.1.1.1"}, {"X-Forwarded-For": "2.2.2.2"}, {}}, wantName: "X-Forwarded-For"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			logger, buf := bufferLogger(slog.LevelDebug)
			s.Logger = logger
			s.TrustProxyHeaders = tt.trust
			s.ClientIPHeader = tt.header
			s.Get("/", text("ok"))
			for _, h := range tt.requests {
				proxyHeaderRequest(s, h)
			}
			out := buf.String()
			count := strings.Count(out, warning)
			if tt.wantName == "" {
				if count != 0 {
					t.Errorf("unexpected warning: %s", out)
				}
				return
			}
			if count != 1 {
				t.Fatalf("warning logged %d times, want 1: %s", count, out)
			}
			if !strings.Contains(out, `"level":"WARN"`) || !strings.Contains(out, `"header":"`+tt.wantName+`"`) {
				t.Errorf("warning = %s, want level WARN and header %q", out, tt.wantName)
			}
			for _, v := range []string{"198.51.100.1", "1.1.1.1", "1.2.3.4"} {
				if strings.Contains(out, v) {
					t.Errorf("warning leaks header value %s: %s", v, out)
				}
			}
		})
	}
}

func TestUntrustedProxyHeaderWarningOncePerServerConcurrent(t *testing.T) {
	logger, buf := bufferLogger(slog.LevelWarn)
	servers := []*Server{New(), New()}
	for _, s := range servers {
		s.Logger = logger
		s.Get("/", text("ok"))
	}
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				proxyHeaderRequest(servers[g%2], map[string]string{"X-Forwarded-For": fmt.Sprintf("10.0.%d.%d", g, i)})
			}
		}(g)
	}
	wg.Wait()
	if got := strings.Count(buf.String(), "Server.TrustProxyHeaders is false"); got != 2 {
		t.Errorf("warning logged %d times for two servers, want exactly 2 (once each)", got)
	}
}
