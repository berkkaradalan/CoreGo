package web

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// record returns a middleware appending name to trace before calling next.
func record(trace *[]string, name string) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			*trace = append(*trace, name)
			return next(c)
		}
	}
}

// testContext builds a white-box Context for a fresh server.
func testContext(method, target string) *Context {
	return newContext(httptest.NewRecorder(), httptest.NewRequest(method, target, nil), New())
}

func TestChainOrder(t *testing.T) {
	var trace []string
	leaf := func(c *Context) error {
		trace = append(trace, "leaf")
		return nil
	}
	composed := chain(leaf, record(&trace, "first"), record(&trace, "second"), record(&trace, "third"))
	if err := composed(testContext(http.MethodGet, "/")); err != nil {
		t.Fatalf("composed error = %v", err)
	}
	if got := strings.Join(trace, ","); got != "first,second,third,leaf" {
		t.Errorf("trace = %q", got)
	}
}

func TestChainEmpty(t *testing.T) {
	called := false
	composed := chain(func(c *Context) error { called = true; return nil })
	if err := composed(testContext(http.MethodGet, "/")); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("leaf not called through empty chain")
	}
}

func TestMiddlewareFullOrder(t *testing.T) {
	s := New()
	var trace []string
	s.Use(record(&trace, "global1"), record(&trace, "global2"))
	api := s.Group("/api", record(&trace, "api"))
	v1 := api.Group("/v1", record(&trace, "v1"))
	v1.Get("/thing", func(c *Context) error {
		trace = append(trace, "handler")
		return c.String(200, "ok")
	}, record(&trace, "route1"), record(&trace, "route2"))
	// Global middleware registered after the route still applies.
	s.Use(record(&trace, "global3"))

	rec := serveRequest(t, s, http.MethodGet, "/api/v1/thing", nil)
	if rec.Code != 200 {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	want := "global1,global2,global3,api,v1,route1,route2,handler"
	if got := strings.Join(trace, ","); got != want {
		t.Errorf("trace = %q, want %q", got, want)
	}
}

func TestMiddlewareConstructedOncePerChain(t *testing.T) {
	s := New()
	constructed := 0
	counting := func(next HandlerFunc) HandlerFunc {
		constructed++
		return next
	}
	s.Use(counting)
	s.Use(counting)
	g := s.Group("/g", counting)
	g.Get("/a", text("a"))
	g.Get("/b", text("b"))
	if constructed != 2 {
		t.Errorf("constructions at registration = %d, want 2 (one per group route)", constructed)
	}
	for i := 0; i < 5; i++ {
		serveRequest(t, s, http.MethodGet, "/g/a", nil)
		serveRequest(t, s, http.MethodGet, "/missing", nil)
	}
	if constructed != 4 {
		t.Errorf("constructions after serving = %d, want 4 (global chain composed once)", constructed)
	}
	// A later Use recomposes the global chain once.
	s.Use(counting)
	for i := 0; i < 3; i++ {
		serveRequest(t, s, http.MethodGet, "/g/b", nil)
	}
	if constructed != 7 {
		t.Errorf("constructions after a late Use = %d, want 7", constructed)
	}
}

func TestGroupUseSemantics(t *testing.T) {
	s := New()
	var trace []string
	api := s.Group("/api", record(&trace, "api"))
	child := api.Group("/child", record(&trace, "child"))
	api.Get("/before", text("before"))
	child.Get("/before", text("child before"))

	api.Use(record(&trace, "late"))
	api.Get("/after", text("after"))
	child.Get("/after", text("child after")) // child created before Use
	grandchild := child.Group("/gc")
	grandchild.Get("/x", text("gc"))
	child.Use(record(&trace, "child-late"))
	child.Get("/after-child-use", text("x"))

	tests := []struct {
		target string
		want   string
	}{
		{target: "/api/before", want: "api"},
		{target: "/api/child/before", want: "api,child"},
		{target: "/api/after", want: "api,late"},
		{target: "/api/child/after", want: "api,late,child"},
		{target: "/api/child/gc/x", want: "api,late,child"},
		{target: "/api/child/after-child-use", want: "api,late,child,child-late"},
	}
	for _, tt := range tests {
		trace = nil
		rec := serveRequest(t, s, http.MethodGet, tt.target, nil)
		if rec.Code != 200 {
			t.Errorf("GET %s code = %d", tt.target, rec.Code)
		}
		if got := strings.Join(trace, ","); got != tt.want {
			t.Errorf("GET %s trace = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestGlobalMiddlewareRunsForFallbacks(t *testing.T) {
	s := New()
	var trace []string
	var routeTrace []string
	s.Use(record(&trace, "global"))
	s.Group("/api", record(&routeTrace, "group")).Get("/r", text("r"), record(&routeTrace, "route"))
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			c.SetHeader("X-Global", "yes")
			return next(c)
		}
	})
	tests := []struct {
		name     string
		method   string
		target   string
		wantCode int
	}{
		{name: "matched route", method: "GET", target: "/api/r", wantCode: 200},
		{name: "404", method: "GET", target: "/nope", wantCode: 404},
		{name: "405", method: "DELETE", target: "/api/r", wantCode: 405},
		{name: "auto OPTIONS", method: "OPTIONS", target: "/api/r", wantCode: 204},
		{name: "HEAD fallback", method: "HEAD", target: "/api/r", wantCode: 200},
		{name: "HEAD 404", method: "HEAD", target: "/nope", wantCode: 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trace, routeTrace = nil, nil
			rec := serveRequest(t, s, tt.method, tt.target, nil)
			if rec.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if strings.Join(trace, ",") != "global" {
				t.Errorf("global trace = %v, want [global]", trace)
			}
			if rec.Header().Get("X-Global") != "yes" {
				t.Error("global middleware header missing")
			}
			matched := tt.wantCode == 200
			if ran := len(routeTrace) > 0; ran != matched {
				t.Errorf("group/route middleware ran = %v, want %v", ran, matched)
			}
		})
	}
}

func TestGlobalMiddlewareCanAnswerFallbacks(t *testing.T) {
	// A global middleware may short-circuit before the 404 fallback.
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			if c.Path() == "/health" {
				return c.String(200, "healthy")
			}
			return next(c)
		}
	})
	if rec := serveRequest(t, s, http.MethodGet, "/health", nil); rec.Code != 200 || rec.Body.String() != "healthy" {
		t.Errorf("code/body = %d/%q", rec.Code, rec.Body.String())
	}
}

func TestMiddlewareShortCircuit(t *testing.T) {
	s := New()
	nextCalled := false
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			return c.String(http.StatusUnauthorized, "stop")
		}
	})
	s.Get("/x", func(c *Context) error {
		nextCalled = true
		return nil
	})
	rec := serveRequest(t, s, http.MethodGet, "/x", nil)
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != "stop" {
		t.Errorf("code/body = %d/%q", rec.Code, rec.Body.String())
	}
	if nextCalled {
		t.Error("next was called after short-circuit")
	}
}

func TestMiddlewareErrorAbortsChain(t *testing.T) {
	s := New()
	nextCalled := false
	s.Get("/x", func(c *Context) error {
		nextCalled = true
		return nil
	}, func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error { return Unauthorized("login required") }
	})
	rec := serveRequest(t, s, http.MethodGet, "/x", nil)
	if rec.Code != 401 || rec.Body.String() != `{"error":"login required"}` {
		t.Errorf("code/body = %d/%q", rec.Code, rec.Body.String())
	}
	if nextCalled {
		t.Error("handler ran after middleware error")
	}
}

func TestMiddlewareErrorPropagation(t *testing.T) {
	s := New()
	boom := errors.New("middleware boom")
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			if err := next(c); err != nil {
				return err
			}
			return boom
		}
	})
	var gotErr error
	s.OnError(func(c *Context, err error) {
		gotErr = err
		_ = c.String(http.StatusInternalServerError, "handled")
	})
	s.Get("/x", func(c *Context) error { return nil })
	rec := serveRequest(t, s, http.MethodGet, "/x", nil)
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != "handled" {
		t.Errorf("code/body = %d/%q", rec.Code, rec.Body.String())
	}
	if !errors.Is(gotErr, boom) {
		t.Errorf("OnError received %v, want %v", gotErr, boom)
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	sentinel := errors.New("ordinary error")
	tests := []struct {
		name     string
		handler  HandlerFunc
		wantCode int // 0 = expect the handler's own error/nil
		wantErr  error
	}{
		{name: "string panic", handler: func(c *Context) error { panic("handler exploded") }, wantCode: 500},
		{name: "error panic", handler: func(c *Context) error { panic(errors.New("runtime failure")) }, wantCode: 500},
		{name: "nil map write panic", handler: func(c *Context) error {
			var m map[string]int
			m["x"] = 1
			return nil
		}, wantCode: 500},
		{name: "ordinary error passes through", handler: func(c *Context) error { return sentinel }, wantErr: sentinel},
		{name: "success passes through", handler: func(c *Context) error { return nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, logs := bufferLogger(slog.LevelError)
			s := New()
			s.Logger = logger
			c := newContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil), s)
			got := Recovery()(tt.handler)(c)
			if tt.wantCode == 0 {
				if !errors.Is(got, tt.wantErr) || (tt.wantErr == nil && got != nil) {
					t.Errorf("error = %v, want %v", got, tt.wantErr)
				}
				if logs.String() != "" {
					t.Errorf("unexpected log: %s", logs.String())
				}
				return
			}
			var httpErr *HTTPError
			if !errors.As(got, &httpErr) || httpErr.Code != tt.wantCode {
				t.Fatalf("recovered error = %#v, want *HTTPError(%d)", got, tt.wantCode)
			}
			if httpErr.Message != "" {
				t.Errorf("message = %q, want empty (no panic details leak)", httpErr.Message)
			}
			if !strings.Contains(logs.String(), "panic recovered") || !strings.Contains(logs.String(), `"path":"/p"`) {
				t.Errorf("panic not logged with request info: %s", logs.String())
			}
		})
	}
}

func TestRecoveryReRaisesAbortHandler(t *testing.T) {
	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", r)
		}
	}()
	_ = Recovery()(func(c *Context) error { panic(http.ErrAbortHandler) })(testContext(http.MethodGet, "/"))
	t.Error("Recovery swallowed http.ErrAbortHandler")
}

func TestRecoveryAsGlobalMiddleware(t *testing.T) {
	s := New()
	s.Use(Recovery())
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error { panic("global panic") }
	})
	s.Get("/x", text("x"))
	rec := serveRequest(t, s, http.MethodGet, "/x", nil)
	if rec.Code != 500 || rec.Body.String() != `{"error":"Internal Server Error"}` {
		t.Errorf("code/body = %d/%q", rec.Code, rec.Body.String())
	}
}

func TestSafetyNetUsesCustomErrorHandler(t *testing.T) {
	s := New()
	s.OnError(func(c *Context, err error) {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			_ = c.String(httpErr.Code, "custom")
		}
	})
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error { panic("global panic") }
	})
	rec := serveRequest(t, s, http.MethodGet, "/anything", nil)
	if rec.Code != 500 || rec.Body.String() != "custom" {
		t.Errorf("code/body = %d/%q, want 500 custom", rec.Code, rec.Body.String())
	}
}
