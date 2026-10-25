package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// logLine is one decoded JSON log record written by RequestLogger.
type logLine struct {
	Level    string `json:"level"`
	Msg      string `json:"msg"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Status   int    `json:"status"`
	Duration int64  `json:"duration"`
	RemoteIP string `json:"remote_ip"`
}

// decodeLines parses every JSON log line in out.
func decodeLines(t *testing.T, out string) []logLine {
	t.Helper()
	var lines []logLine
	for _, raw := range strings.Split(strings.TrimSpace(out), "\n") {
		if raw == "" {
			continue
		}
		var l logLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("invalid log line %q: %v", raw, err)
		}
		lines = append(lines, l)
	}
	return lines
}

func TestRequestLogger(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantLevel  string
	}{
		{name: "ok", method: "GET", target: "/ok?token=secret", wantStatus: 200, wantLevel: "INFO"},
		{name: "implicit 200", method: "GET", target: "/implicit", wantStatus: 200, wantLevel: "INFO"},
		{name: "created", method: "POST", target: "/created", wantStatus: 201, wantLevel: "INFO"},
		{name: "http error 4xx", method: "GET", target: "/unauthorized", wantStatus: 401, wantLevel: "INFO"},
		{name: "unknown error 500", method: "GET", target: "/error", wantStatus: 500, wantLevel: "ERROR"},
		{name: "explicit 503", method: "GET", target: "/unavailable", wantStatus: 503, wantLevel: "ERROR"},
		{name: "panic 500", method: "GET", target: "/panic", wantStatus: 500, wantLevel: "ERROR"},
		{name: "404 fallback", method: "GET", target: "/missing", wantStatus: 404, wantLevel: "INFO"},
		{name: "405 fallback", method: "DELETE", target: "/ok", wantStatus: 405, wantLevel: "INFO"},
		{name: "auto options", method: "OPTIONS", target: "/ok", wantStatus: 204, wantLevel: "INFO"},
		{name: "head", method: "HEAD", target: "/ok", wantStatus: 200, wantLevel: "INFO"},
		{name: "raw response writer", method: "GET", target: "/raw", wantStatus: 202, wantLevel: "INFO"},
	}
	logger, logs := bufferLogger(slog.LevelDebug)
	s := New()
	s.Use(RequestLogger(logger))
	s.Get("/ok", text("ok"))
	s.Get("/implicit", func(c *Context) error { return nil })
	s.Post("/created", func(c *Context) error { return c.JSON(201, map[string]int{"id": 1}) })
	s.Get("/unauthorized", func(c *Context) error { return Unauthorized("") })
	s.Get("/error", func(c *Context) error { return errors.New("boom") })
	s.Get("/unavailable", func(c *Context) error { return c.Status(503) })
	s.Get("/panic", func(c *Context) error { panic("kaboom") })
	s.Get("/raw", func(c *Context) error {
		c.Response().WriteHeader(202)
		return nil
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(decodeLines(t, logs.String()))
			req := httptest.NewRequest(tt.method, tt.target, nil)
			req.RemoteAddr = "203.0.113.9:4000"
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("response code = %d, want %d", rec.Code, tt.wantStatus)
			}
			var access []logLine
			for _, l := range decodeLines(t, logs.String())[before:] {
				if l.Msg == "http request" {
					access = append(access, l)
				}
			}
			if len(access) != 1 {
				t.Fatalf("access log lines = %d, want 1: %s", len(access), logs.String())
			}
			l := access[0]
			wantPath := strings.SplitN(tt.target, "?", 2)[0]
			if l.Method != tt.method || l.Path != wantPath || l.Status != tt.wantStatus || l.RemoteIP != "203.0.113.9" || l.Level != tt.wantLevel {
				t.Errorf("log = %+v, want method %s path %s status %d remote_ip 203.0.113.9 level %s", l, tt.method, wantPath, tt.wantStatus, tt.wantLevel)
			}
			if l.Duration < 0 {
				t.Errorf("duration = %d, want >= 0", l.Duration)
			}
		})
	}
	if strings.Contains(logs.String(), "secret") {
		t.Error("query string leaked into the access log")
	}
}

func TestRequestLoggerResolvesErrorsOnce(t *testing.T) {
	logger, _ := bufferLogger(slog.LevelDebug)
	s := New()
	var outerErr error
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			outerErr = next(c)
			return outerErr
		}
	})
	s.Use(RequestLogger(logger))
	calls := 0
	s.OnError(func(c *Context, err error) {
		calls++
		_ = c.String(418, "handled")
	})
	s.Get("/e", func(c *Context) error { return errors.New("x") })
	rec := serveRequest(t, s, http.MethodGet, "/e", nil)
	if rec.Code != 418 || calls != 1 {
		t.Errorf("code = %d, error handler calls = %d; want 418, 1", rec.Code, calls)
	}
	if outerErr != nil {
		t.Errorf("middleware outside RequestLogger saw %v, want nil", outerErr)
	}
}

func TestRequestLoggerNilUsesDefault(t *testing.T) {
	logger, logs := bufferLogger(slog.LevelDebug)
	previous := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(previous)

	s := New()
	s.Use(RequestLogger(nil))
	s.Get("/x", text("x"))
	serveRequest(t, s, http.MethodGet, "/x", nil)
	if !strings.Contains(logs.String(), `"msg":"http request"`) {
		t.Errorf("default logger not used: %q", logs.String())
	}
}

func TestRequestLoggerPanicInGlobalMiddleware(t *testing.T) {
	logger, logs := bufferLogger(slog.LevelDebug)
	s := New()
	s.Use(RequestLogger(logger))
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error { panic("inner global panic") }
	})
	s.Get("/x", text("x"))
	rec := serveRequest(t, s, http.MethodGet, "/x", nil)
	if rec.Code != 500 {
		t.Errorf("code = %d, want 500", rec.Code)
	}
	var found bool
	for _, l := range decodeLines(t, logs.String()) {
		if l.Msg == "http request" && l.Status == 500 && l.Level == "ERROR" {
			found = true
		}
	}
	if !found {
		t.Errorf("panicking request not logged as 500: %s", logs.String())
	}
}

func TestRequestLoggerDuration(t *testing.T) {
	logger, logs := bufferLogger(slog.LevelDebug)
	s := New()
	s.Use(RequestLogger(logger))
	s.Get("/slow", func(c *Context) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	serveRequest(t, s, http.MethodGet, "/slow", nil)
	lines := decodeLines(t, logs.String())
	if len(lines) != 1 || time.Duration(lines[0].Duration) < 20*time.Millisecond {
		t.Errorf("lines = %+v, want one line with duration >= 20ms", lines)
	}
}
