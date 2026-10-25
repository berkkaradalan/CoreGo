package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type interopKey struct{}

// attachValue is a standard middleware adding a request-context value and
// a response header.
func attachValue(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Std", "1")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), interopKey{}, "std value")))
	})
}

// denyWithoutToken is a standard middleware that short-circuits.
func denyWithoutToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") == "" {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder is a standard middleware that wraps the writer to observe
// the final status, as std logging middleware does.
func statusRecorder(seen *int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &captureWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			*seen = rw.status
		})
	}
}

type captureWriter struct {
	http.ResponseWriter
	status int
}

func (w *captureWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// bufferingMiddleware writes downstream output into a buffer and then
// emits it upper-cased, proving downstream writes go to the writer the
// middleware passed.
func bufferingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(bytes.ToUpper(rec.Body.Bytes()))
	})
}

func TestWrapHandler(t *testing.T) {
	s := New()
	s.Get("/std/*rest", WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("std:" + r.URL.Path))
	})))
	s.Get("/mux/*rest", WrapHandler(http.StripPrefix("/mux", http.NotFoundHandler())))
	rec := serveRequest(t, s, http.MethodGet, "/std/a/b", nil)
	if rec.Code != 202 || rec.Body.String() != "std:/std/a/b" {
		t.Errorf("code/body = %d/%q", rec.Code, rec.Body.String())
	}
	if rec := serveRequest(t, s, http.MethodGet, "/mux/x", nil); rec.Code != 404 {
		t.Errorf("std 404 code = %d", rec.Code)
	}
	mustPanic(t, "WrapHandler called with a nil http.Handler", func() { WrapHandler(nil) })
}

func TestWrapHandlerSeesContextChanges(t *testing.T) {
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			c.SetRequest(c.Request().WithContext(context.WithValue(c, interopKey{}, "from web middleware")))
			return next(c)
		}
	})
	s.Get("/", WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := r.Context().Value(interopKey{}).(string)
		_, _ = w.Write([]byte(v))
	})))
	if rec := serveRequest(t, s, http.MethodGet, "/", nil); rec.Body.String() != "from web middleware" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestWrapMiddlewareContextVisibleDownstream(t *testing.T) {
	s := New()
	s.Use(WrapMiddleware(attachValue))
	var afterValue any
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			afterValue = c.Value(interopKey{})
			return next(c)
		}
	})
	s.Get("/", func(c *Context) error {
		v, _ := c.Value(interopKey{}).(string)
		r, _ := c.Request().Context().Value(interopKey{}).(string)
		return c.String(200, v+"|"+r)
	})
	rec := serveRequest(t, s, http.MethodGet, "/", nil)
	if rec.Body.String() != "std value|std value" {
		t.Errorf("body = %q, want the std middleware's value via Value and Request", rec.Body.String())
	}
	if afterValue != "std value" {
		t.Errorf("next web middleware saw %v", afterValue)
	}
	if rec.Header().Get("X-Std") != "1" {
		t.Error("std middleware header missing")
	}
}

func TestWrapMiddlewareShortCircuit(t *testing.T) {
	s := New()
	reached := false
	s.Get("/", func(c *Context) error {
		reached = true
		return c.String(200, "secret")
	}, WrapMiddleware(denyWithoutToken))
	rec := serveRequest(t, s, http.MethodGet, "/", nil)
	if rec.Code != 403 || strings.TrimSpace(rec.Body.String()) != "denied" || reached {
		t.Errorf("code/body/reached = %d/%q/%v, want 403 denied false", rec.Code, rec.Body.String(), reached)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Token", "t")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "secret" {
		t.Errorf("with token code/body = %d/%q", rec.Code, rec.Body.String())
	}
}

func TestWrapMiddlewareObservesRenderedErrors(t *testing.T) {
	var seen int
	s := New()
	s.Use(WrapMiddleware(statusRecorder(&seen)))
	s.Get("/fail", func(c *Context) error { return NotFound("nope") })
	s.Get("/ok", func(c *Context) error { return c.String(201, "made") })
	tests := []struct {
		target   string
		wantCode int
		wantBody string
	}{
		{target: "/fail", wantCode: 404, wantBody: `{"error":"nope"}`},
		{target: "/ok", wantCode: 201, wantBody: "made"},
		{target: "/missing", wantCode: 404, wantBody: `{"error":"Not Found"}`},
	}
	for _, tt := range tests {
		seen = 0
		rec := serveRequest(t, s, http.MethodGet, tt.target, nil)
		if rec.Code != tt.wantCode || rec.Body.String() != tt.wantBody {
			t.Errorf("GET %s = %d %q", tt.target, rec.Code, rec.Body.String())
		}
		if seen != tt.wantCode {
			t.Errorf("GET %s: std middleware observed %d, want %d", tt.target, seen, tt.wantCode)
		}
	}
}

func TestWrapMiddlewareReplacedWriter(t *testing.T) {
	s := New()
	s.Get("/", func(c *Context) error {
		if c.Written() {
			t.Error("Written() = true before writing")
		}
		_ = c.String(200, "shout")
		if !c.Written() {
			t.Error("Written() = false after writing to the replaced writer")
		}
		return errors.New("late error is logged, not written")
	}, WrapMiddleware(bufferingMiddleware))
	rec := serveRequest(t, s, http.MethodGet, "/", nil)
	if rec.Code != 200 || rec.Body.String() != "SHOUT" {
		t.Errorf("code/body = %d/%q, want 200 SHOUT", rec.Code, rec.Body.String())
	}
}

func TestWrapMiddlewareConstructedOnce(t *testing.T) {
	constructed := 0
	mw := func(next http.Handler) http.Handler {
		constructed++
		return next
	}
	s := New()
	s.Use(WrapMiddleware(mw))
	s.Get("/", text("ok"))
	for i := 0; i < 3; i++ {
		serveRequest(t, s, http.MethodGet, "/", nil)
	}
	if constructed != 1 {
		t.Errorf("std middleware constructed %d times, want 1", constructed)
	}
}

func TestWrapMiddlewareLostContext(t *testing.T) {
	// A middleware that discards the request context cannot be served.
	s := New()
	s.Get("/", text("ok"), WrapMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.Background()))
		})
	}))
	if rec := serveRequest(t, s, http.MethodGet, "/", nil); rec.Code != 500 {
		t.Errorf("code = %d, want 500", rec.Code)
	}
}

func TestWrapMiddlewarePanics(t *testing.T) {
	mustPanic(t, "WrapMiddleware called with a nil middleware", func() { WrapMiddleware(nil) })
	s := New()
	mustPanic(t, "returned a nil http.Handler", func() {
		s.Get("/", text("ok"), WrapMiddleware(func(http.Handler) http.Handler { return nil }))
	})
}
