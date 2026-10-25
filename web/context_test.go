package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestContext builds a white-box Context around a recorder.
func newTestContext(method, target string, body io.Reader) (*Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, target, body)
	rec := httptest.NewRecorder()
	return newContext(rec, req, New()), rec
}

// Compile-time check: *Context is a context.Context.
var _ context.Context = (*Context)(nil)

func TestContextParam(t *testing.T) {
	c, _ := newTestContext(http.MethodGet, "/", nil)
	c.params = map[string]string{"id": "42", "rest": ""}
	tests := map[string]string{"id": "42", "rest": "", "missing": ""}
	for name, want := range tests {
		if got := c.Param(name); got != want {
			t.Errorf("Param(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestContextQuery(t *testing.T) {
	c, _ := newTestContext(http.MethodGet, "/search?q=corego&lang=go&lang=rs&empty=&enc=a%20b", nil)
	tests := []struct {
		name string
		want string
	}{
		{"q", "corego"},
		{"empty", ""},
		{"missing", ""},
		{"lang", "go"}, // first value
		{"enc", "a b"},
	}
	for _, tt := range tests {
		if got := c.Query(tt.name); got != tt.want {
			t.Errorf("Query(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestContextDefaultQuery(t *testing.T) {
	c, _ := newTestContext(http.MethodGet, "/search?q=&page=2", nil)
	tests := []struct {
		name, def, want string
	}{
		{"q", "fallback", ""}, // present but empty
		{"missing", "fallback", "fallback"},
		{"page", "1", "2"},
	}
	for _, tt := range tests {
		if got := c.DefaultQuery(tt.name, tt.def); got != tt.want {
			t.Errorf("DefaultQuery(%q, %q) = %q, want %q", tt.name, tt.def, got, tt.want)
		}
	}
}

func TestContextQueries(t *testing.T) {
	c, _ := newTestContext(http.MethodGet, "/x?a=1&b=2&b=3", nil)
	all := c.Queries()
	if got := all.Get("a"); got != "1" {
		t.Errorf("Queries().Get(a) = %q, want 1", got)
	}
	if len(all["b"]) != 2 || all["b"][0] != "2" || all["b"][1] != "3" {
		t.Errorf("Queries()[b] = %v, want [2 3]", all["b"])
	}
	if len(all) != 2 {
		t.Errorf("len(Queries()) = %d, want 2", len(all))
	}
}

func TestContextPathAndMethod(t *testing.T) {
	c, _ := newTestContext(http.MethodPost, "/users/a%20b/posts?page=1", nil)
	if got := c.Path(); got != "/users/a b/posts" {
		t.Errorf("Path() = %q, want decoded path without query", got)
	}
	if got := c.Method(); got != http.MethodPost {
		t.Errorf("Method() = %q, want POST", got)
	}
}

func TestContextImplementsContext(t *testing.T) {
	type key struct{}
	deadline := time.Now().Add(time.Hour)
	base, cancel := context.WithDeadline(context.WithValue(context.Background(), key{}, "v1"), deadline)
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(base)
	c := newContext(httptest.NewRecorder(), req, New())

	if got, ok := c.Deadline(); !ok || !got.Equal(deadline) {
		t.Errorf("Deadline() = %v, %v; want %v, true", got, ok, deadline)
	}
	if got := c.Value(key{}); got != "v1" {
		t.Errorf("Value(key) = %v, want v1", got)
	}
	if got := c.Value("unknown"); got != nil {
		t.Errorf("Value(unknown) = %v, want nil", got)
	}
	c.Set("stored", "x")
	if got := c.Value("stored"); got != nil {
		t.Errorf("Value sees Set values (%v); it must only delegate to the request context", got)
	}
	select {
	case <-c.Done():
		t.Fatal("Done() closed before cancellation")
	default:
	}
	if c.Err() != nil {
		t.Fatalf("Err() = %v before cancellation", c.Err())
	}

	cancel()
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() not closed after cancellation")
	}
	if !errors.Is(c.Err(), context.Canceled) {
		t.Errorf("Err() = %v, want context.Canceled", c.Err())
	}

	// Derived contexts see the cancellation too.
	child, stop := context.WithTimeout(c, time.Hour)
	defer stop()
	if !errors.Is(child.Err(), context.Canceled) {
		t.Errorf("child Err() = %v, want context.Canceled", child.Err())
	}
}

func TestContextNoDeadlineByDefault(t *testing.T) {
	c, _ := newTestContext(http.MethodGet, "/", nil)
	if _, ok := c.Deadline(); ok {
		t.Error("Deadline() ok = true for a plain request")
	}
}

func TestContextSetRequest(t *testing.T) {
	type key struct{}
	c, _ := newTestContext(http.MethodGet, "/original", nil)
	c.params = map[string]string{"id": "7"}
	c.Set("k", "v")

	ctx, cancel := context.WithCancel(context.WithValue(c.Request().Context(), key{}, "attached"))
	c.SetRequest(c.Request().WithContext(ctx))
	if got := c.Value(key{}); got != "attached" {
		t.Errorf("Value after SetRequest = %v, want attached", got)
	}
	cancel()
	if !errors.Is(c.Err(), context.Canceled) {
		t.Errorf("Err() = %v, want the new request's cancellation", c.Err())
	}
	if c.Param("id") != "7" {
		t.Error("SetRequest dropped path parameters")
	}
	if v, _ := c.Get("k"); v != "v" {
		t.Error("SetRequest dropped Set values")
	}

	other := httptest.NewRequest(http.MethodPut, "/other?x=1", nil)
	c.SetRequest(other)
	if c.Request() != other || c.Path() != "/other" || c.Method() != http.MethodPut || c.Query("x") != "1" {
		t.Error("accessors do not follow the replaced request")
	}
	mustPanic(t, "SetRequest called with a nil", func() { c.SetRequest(nil) })
}

func TestContextSetRequestVisibleDownstream(t *testing.T) {
	type key struct{}
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			c.SetRequest(c.Request().WithContext(context.WithValue(c, key{}, "from middleware")))
			return next(c)
		}
	})
	s.Get("/", func(c *Context) error {
		v, _ := c.Value(key{}).(string)
		return c.String(200, v)
	})
	if rec := serveRequest(t, s, http.MethodGet, "/", nil); rec.Body.String() != "from middleware" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestContextCanceledByClientDisconnect(t *testing.T) {
	canceled := make(chan error, 1)
	started := make(chan struct{})
	s := New()
	s.Get("/wait", func(c *Context) error {
		close(started)
		select {
		case <-c.Done():
			canceled <- c.Err()
		case <-time.After(5 * time.Second):
			canceled <- nil
		}
		return nil
	})
	ts := httptest.NewServer(s)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/wait", nil)
	go func() {
		<-started
		cancel()
	}()
	if resp, err := ts.Client().Do(req); err == nil {
		resp.Body.Close()
	}
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Errorf("handler saw %v, want context.Canceled", err)
	}
}

func TestContextRemoteIP(t *testing.T) {
	tests := []struct {
		name       string
		trust      bool
		header     string // ClientIPHeader; "" keeps the default
		remoteAddr string
		set        map[string][]string
		want       string
	}{
		// Trust off: proxy headers never consulted.
		{name: "untrusted ignores xff", remoteAddr: "1.2.3.4:5678", set: map[string][]string{"X-Forwarded-For": {"8.8.8.8, 7.7.7.7"}}, want: "1.2.3.4"},
		{name: "untrusted ignores x-real-ip", remoteAddr: "1.2.3.4:5678", set: map[string][]string{"X-Real-IP": {"9.9.9.9"}}, want: "1.2.3.4"},
		{name: "remote addr without port", remoteAddr: "1.2.3.4", want: "1.2.3.4"},
		{name: "ipv6 remote addr", remoteAddr: "[::1]:8080", want: "::1"},
		{name: "ipv6 remote addr uppercase canonicalized", remoteAddr: "[2001:DB8::1]:443", want: "2001:db8::1"},
		{name: "ipv4-mapped ipv6 unmapped", remoteAddr: "[::ffff:10.0.0.1]:80", want: "10.0.0.1"},
		{name: "empty remote addr", remoteAddr: "", want: ""},
		{name: "non-ip remote addr kept", remoteAddr: "@", want: "@"},
		{name: "non-ip remote addr with port", remoteAddr: "localhost:80", want: "localhost"},

		// Trust on, default X-Forwarded-For: rightmost non-empty entry.
		{name: "xff single entry", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"5.6.7.8"}}, want: "5.6.7.8"},
		{name: "xff rightmost wins over spoofed left", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"6.6.6.6, 5.6.7.8"}}, want: "5.6.7.8"},
		{name: "xff trailing empty entries skipped", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"6.6.6.6, 5.6.7.8, , "}}, want: "5.6.7.8"},
		{name: "xff multiple header lines", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"6.6.6.6", "5.6.7.8"}}, want: "5.6.7.8"},
		{name: "xff ipv6 entry", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"1.1.1.1, 2001:db8::7"}}, want: "2001:db8::7"},
		{name: "xff entry with port", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"5.6.7.8:4711"}}, want: "5.6.7.8"},
		{name: "xff bracketed ipv6 with port", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"[2001:db8::7]:4711"}}, want: "2001:db8::7"},
		{name: "xff malformed rightmost falls back", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"5.6.7.8, garbage"}}, want: "10.0.0.1"},
		{name: "xff only commas falls back", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {" , ,"}}, want: "10.0.0.1"},
		{name: "xff empty falls back", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {""}}, want: "10.0.0.1"},
		{name: "xff absent falls back", trust: true, remoteAddr: "10.0.0.1:1", want: "10.0.0.1"},
		{name: "x-real-ip not consulted implicitly", trust: true, remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Real-IP": {"9.9.9.9"}}, want: "10.0.0.1"},
		{name: "empty header name means xff", trust: true, header: "-", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"1.1.1.1, 5.6.7.8"}}, want: "5.6.7.8"},
		{name: "lowercase xff name still rightmost", trust: true, header: "x-forwarded-for", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"1.1.1.1, 5.6.7.8"}}, want: "5.6.7.8"},

		// Trust on, custom header: whole trimmed value.
		{name: "x-real-ip used", trust: true, header: "X-Real-IP", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Real-IP": {"9.9.9.9"}}, want: "9.9.9.9"},
		{name: "x-real-ip trimmed", trust: true, header: "X-Real-IP", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Real-IP": {"  9.9.9.9 "}}, want: "9.9.9.9"},
		{name: "custom header ignores xff", trust: true, header: "CF-Connecting-IP", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"5.6.7.8"}, "Cf-Connecting-Ip": {"2001:db8::9"}}, want: "2001:db8::9"},
		{name: "custom header absent falls back", trust: true, header: "X-Real-IP", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Forwarded-For": {"5.6.7.8"}}, want: "10.0.0.1"},
		{name: "custom header empty falls back", trust: true, header: "X-Real-IP", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Real-IP": {"   "}}, want: "10.0.0.1"},
		{name: "custom header list is malformed", trust: true, header: "X-Real-IP", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Real-IP": {"1.1.1.1, 2.2.2.2"}}, want: "10.0.0.1"},
		{name: "custom header garbage falls back", trust: true, header: "X-Real-IP", remoteAddr: "10.0.0.1:1", set: map[string][]string{"X-Real-IP": {"<script>"}}, want: "10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.TrustProxyHeaders = tt.trust
			switch tt.header {
			case "":
			case "-":
				s.ClientIPHeader = ""
			default:
				s.ClientIPHeader = tt.header
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for name, values := range tt.set {
				for _, v := range values {
					req.Header.Add(name, v)
				}
			}
			c := newContext(httptest.NewRecorder(), req, s)
			if got := c.RemoteIP(); got != tt.want {
				t.Errorf("RemoteIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContextHeaders(t *testing.T) {
	c, rec := newTestContext(http.MethodGet, "/", nil)
	c.r.Header.Set("X-Custom", "request-value")
	if got := c.GetHeader("x-custom"); got != "request-value" {
		t.Errorf("GetHeader = %q, want request-value", got)
	}
	if got := c.GetHeader("X-Missing"); got != "" {
		t.Errorf("GetHeader(missing) = %q, want empty", got)
	}
	c.SetHeader("X-Response", "first")
	c.SetHeader("X-Response", "response-value")
	if got := rec.Header().Values("X-Response"); len(got) != 1 || got[0] != "response-value" {
		t.Errorf("response header = %v, want [response-value]", got)
	}
}

func TestContextBodyReadOnce(t *testing.T) {
	c, _ := newTestContext(http.MethodPost, "/", strings.NewReader("hello body"))
	b1, err := c.Body()
	if err != nil || string(b1) != "hello body" {
		t.Fatalf("Body() = %q, %v", b1, err)
	}
	// A second read must not touch the drained stream: replace it with a
	// reader that fails to prove the cache is used.
	c.r.Body = io.NopCloser(errReader{})
	b2, err := c.Body()
	if err != nil || string(b2) != "hello body" {
		t.Errorf("cached Body() = %q, %v", b2, err)
	}
}

// errReader always fails, to exercise body read error paths.
type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("read boom") }

func TestContextBodyReadError(t *testing.T) {
	c, _ := newTestContext(http.MethodPost, "/", errReader{})
	for i := 0; i < 2; i++ { // the error is cached
		if _, err := c.Body(); err == nil || err.Error() != "read boom" {
			t.Fatalf("Body() call %d error = %v, want read boom", i, err)
		}
	}
}

func TestContextBodyLimit(t *testing.T) {
	tests := []struct {
		name    string
		maxBody int64
		bodyLen int
		wantErr error
	}{
		{name: "under limit", maxBody: 16, bodyLen: 10},
		{name: "exactly at limit", maxBody: 16, bodyLen: 16},
		{name: "one over limit", maxBody: 16, bodyLen: 17, wantErr: ErrBodyTooLarge},
		{name: "far over limit", maxBody: 4, bodyLen: 100, wantErr: ErrBodyTooLarge},
		{name: "zero means unlimited", maxBody: 0, bodyLen: 4096},
		{name: "default limit exact", maxBody: DefaultMaxBodyBytes, bodyLen: int(DefaultMaxBodyBytes)},
		{name: "default limit plus one", maxBody: DefaultMaxBodyBytes, bodyLen: int(DefaultMaxBodyBytes) + 1, wantErr: ErrBodyTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.MaxBodyBytes = tt.maxBody
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", tt.bodyLen)))
			c := newContext(httptest.NewRecorder(), req, s)
			b, err := c.Body()
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("Body() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && len(b) != tt.bodyLen {
				t.Errorf("len(Body()) = %d, want %d", len(b), tt.bodyLen)
			}
		})
	}
}

func TestContextNilBody(t *testing.T) {
	c, _ := newTestContext(http.MethodGet, "/", nil)
	c.r.Body = nil
	b, err := c.Body()
	if err != nil || len(b) != 0 {
		t.Fatalf("Body() = %q, %v; want empty, nil", b, err)
	}
}

type jsonPayload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestContextBindJSON(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     error
		wantPayload jsonPayload
	}{
		{name: "valid", contentType: "application/json", body: `{"name":"ada","age":36}`, wantPayload: jsonPayload{Name: "ada", Age: 36}},
		{name: "valid with charset", contentType: "application/json; charset=utf-8", body: `{"name":"bob","age":1}`, wantPayload: jsonPayload{Name: "bob", Age: 1}},
		{name: "uppercase media type", contentType: "Application/JSON", body: `{"name":"c"}`, wantPayload: jsonPayload{Name: "c"}},
		{name: "empty body", contentType: "application/json", body: "", wantErr: ErrInvalidJSON},
		{name: "malformed body", contentType: "application/json", body: `{"name":`, wantErr: ErrInvalidJSON},
		{name: "type mismatch", contentType: "application/json", body: `[1,2,3]`, wantErr: ErrInvalidJSON},
		{name: "wrong content type", contentType: "text/plain", body: `{"name":"x"}`, wantErr: ErrUnsupportedMediaType},
		{name: "missing content type", contentType: "", body: `{"name":"x"}`, wantErr: ErrUnsupportedMediaType},
		{name: "garbage content type", contentType: "not a media type ;;", body: `{}`, wantErr: ErrUnsupportedMediaType},
		{name: "json with vendor suffix", contentType: "application/vnd.api+json", body: `{}`, wantErr: ErrUnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestContext(http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.contentType != "" {
				c.r.Header.Set("Content-Type", tt.contentType)
			}
			var got jsonPayload
			err := c.BindJSON(&got)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("BindJSON() error = %v, want nil", err)
				}
				if got != tt.wantPayload {
					t.Errorf("payload = %+v, want %+v", got, tt.wantPayload)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("BindJSON() error = %v, want %v", err, tt.wantErr)
			}
			if err.Error() == tt.wantErr.Error() {
				t.Errorf("BindJSON() returned the bare sentinel; want a wrapped error with detail")
			}
		})
	}
}

func TestContextBindJSONBodyTooLarge(t *testing.T) {
	s := New()
	s.MaxBodyBytes = 8
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"way too long"}`))
	req.Header.Set("Content-Type", "application/json")
	c := newContext(httptest.NewRecorder(), req, s)
	var got jsonPayload
	if err := c.BindJSON(&got); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("BindJSON() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestContextBindJSONUsesBodyCache(t *testing.T) {
	c, _ := newTestContext(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
	c.r.Header.Set("Content-Type", "application/json")
	var p1, p2 jsonPayload
	if err := c.BindJSON(&p1); err != nil {
		t.Fatal(err)
	}
	c.r.Body = io.NopCloser(errReader{})
	if err := c.BindJSON(&p2); err != nil {
		t.Fatalf("second BindJSON() error = %v, want cached success", err)
	}
	if p1 != p2 {
		t.Errorf("second payload = %+v, want %+v", p2, p1)
	}
}

func TestContextWriters(t *testing.T) {
	tests := []struct {
		name     string
		write    func(c *Context) error
		wantCode int
		wantBody string
		wantCT   string
	}{
		{name: "json", write: func(c *Context) error { return c.JSON(201, map[string]int{"a": 1}) }, wantCode: 201, wantBody: `{"a":1}`, wantCT: "application/json; charset=utf-8"},
		{name: "json nil", write: func(c *Context) error { return c.JSON(200, nil) }, wantCode: 200, wantBody: `null`, wantCT: "application/json; charset=utf-8"},
		{name: "json html escaped", write: func(c *Context) error { return c.JSON(200, "<b>") }, wantCode: 200, wantBody: "\"\\u003cb\\u003e\"", wantCT: "application/json; charset=utf-8"},
		{name: "string", write: func(c *Context) error { return c.String(200, "hello") }, wantCode: 200, wantBody: "hello", wantCT: "text/plain; charset=utf-8"},
		{name: "empty string", write: func(c *Context) error { return c.String(202, "") }, wantCode: 202, wantBody: "", wantCT: "text/plain; charset=utf-8"},
		{name: "html", write: func(c *Context) error { return c.HTML(200, "<h1>hi</h1>") }, wantCode: 200, wantBody: "<h1>hi</h1>", wantCT: "text/html; charset=utf-8"},
		{name: "data", write: func(c *Context) error { return c.Data(200, "application/octet-stream", []byte{0, 1}) }, wantCode: 200, wantBody: "\x00\x01", wantCT: "application/octet-stream"},
		{name: "data empty content type", write: func(c *Context) error { return c.Data(200, "", []byte("x")) }, wantCode: 200, wantBody: "x", wantCT: ""},
		{name: "no content", write: func(c *Context) error { return c.NoContent() }, wantCode: 204},
		{name: "status only", write: func(c *Context) error { return c.Status(418) }, wantCode: 418},
		{name: "first status wins", write: func(c *Context) error {
			_ = c.Status(200)
			return c.String(500, "late")
		}, wantCode: 200, wantBody: "late", wantCT: "text/plain; charset=utf-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newTestContext(http.MethodGet, "/", nil)
			if c.Written() {
				t.Fatal("Written() = true before any write")
			}
			if err := tt.write(c); err != nil {
				t.Fatalf("writer returned %v, want nil", err)
			}
			if !c.Written() {
				t.Error("Written() = false after writing")
			}
			if rec.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if ct := rec.Header().Get("Content-Type"); ct != tt.wantCT {
				t.Errorf("Content-Type = %q, want %q", ct, tt.wantCT)
			}
		})
	}
}

// failingWriter is a ResponseWriter whose body writes fail.
type failingWriter struct {
	header http.Header
	code   int
}

func (w *failingWriter) Header() http.Header         { return w.header }
func (w *failingWriter) WriteHeader(code int)        { w.code = code }
func (w *failingWriter) Write(b []byte) (int, error) { return 0, errWriteFailed }

var errWriteFailed = errors.New("connection reset")

func TestContextWritersReturnWriteErrors(t *testing.T) {
	writers := map[string]func(c *Context) error{
		"JSON":   func(c *Context) error { return c.JSON(200, map[string]string{"a": "b"}) },
		"String": func(c *Context) error { return c.String(200, "x") },
		"HTML":   func(c *Context) error { return c.HTML(200, "<p>") },
		"Data":   func(c *Context) error { return c.Data(200, "text/csv", []byte("a,b")) },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			w := &failingWriter{header: http.Header{}}
			c := newContext(w, httptest.NewRequest(http.MethodGet, "/", nil), New())
			if err := write(c); !errors.Is(err, errWriteFailed) {
				t.Errorf("error = %v, want the ResponseWriter's write error", err)
			}
		})
	}
}

func TestContextJSONMarshalFailure(t *testing.T) {
	c, rec := newTestContext(http.MethodGet, "/", nil)
	err := c.JSON(200, make(chan int))
	if err == nil || !strings.HasPrefix(err.Error(), "web: encode JSON response") {
		t.Fatalf("error = %v, want wrapped encode error", err)
	}
	if c.Written() {
		t.Error("Written() = true after a failed marshal; nothing may be written")
	}
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Errorf("partial response written: body %q, Content-Type %q", rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}

func TestContextRedirect(t *testing.T) {
	tests := []struct {
		code    int
		wantErr bool
	}{
		{code: 301}, {code: 302}, {code: 303}, {code: 307}, {code: 308},
		{code: 200, wantErr: true}, {code: 299, wantErr: true}, {code: 400, wantErr: true}, {code: 0, wantErr: true},
	}
	for _, tt := range tests {
		c, rec := newTestContext(http.MethodGet, "/", nil)
		err := c.Redirect(tt.code, "https://example.com/new")
		if tt.wantErr {
			if err == nil || !strings.Contains(err.Error(), "invalid redirect status code") {
				t.Errorf("Redirect(%d) error = %v, want invalid status error", tt.code, err)
			}
			if c.Written() || rec.Header().Get("Location") != "" {
				t.Errorf("Redirect(%d) wrote a response", tt.code)
			}
			continue
		}
		if err != nil {
			t.Errorf("Redirect(%d) error = %v", tt.code, err)
		}
		if rec.Code != tt.code || rec.Header().Get("Location") != "https://example.com/new" {
			t.Errorf("Redirect(%d) = %d, Location %q", tt.code, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestContextCookies(t *testing.T) {
	c, rec := newTestContext(http.MethodGet, "/", nil)
	c.r.AddCookie(&http.Cookie{Name: "existing", Value: "yes"})
	if got, err := c.Cookie("existing"); err != nil || got != "yes" {
		t.Errorf("Cookie(existing) = %q, %v; want yes, nil", got, err)
	}
	if _, err := c.Cookie("missing"); !errors.Is(err, http.ErrNoCookie) {
		t.Errorf("Cookie(missing) error = %v, want http.ErrNoCookie", err)
	}
	c.SetCookie(&http.Cookie{Name: "session", Value: "abc", Path: "/", MaxAge: 3600, HttpOnly: true})
	c.SetCookie(nil) // ignored
	got := rec.Header().Values("Set-Cookie")
	if len(got) != 1 || !strings.Contains(got[0], "session=abc") || !strings.Contains(got[0], "HttpOnly") {
		t.Errorf("Set-Cookie = %v, want one session=abc HttpOnly cookie", got)
	}
}

func TestContextSetGet(t *testing.T) {
	c, _ := newTestContext(http.MethodGet, "/", nil)
	if _, ok := c.Get("user"); ok {
		t.Error("Get(user) ok before Set")
	}
	c.Set("user", "ada")
	c.Set("count", 7)
	c.Set("nil", nil)
	if v, ok := c.Get("user"); !ok || v != "ada" {
		t.Errorf("Get(user) = %v, %v; want ada, true", v, ok)
	}
	if v, ok := c.Get("count"); !ok || v != 7 {
		t.Errorf("Get(count) = %v, %v; want 7, true", v, ok)
	}
	if v, ok := c.Get("nil"); !ok || v != nil {
		t.Errorf("Get(nil) = %v, %v; want nil, true", v, ok)
	}
	c.Set("user", "grace")
	if v, _ := c.Get("user"); v != "grace" {
		t.Errorf("Get(user) after overwrite = %v, want grace", v)
	}
}

func TestContextResponseTracksWrites(t *testing.T) {
	tests := []struct {
		name       string
		write      func(w http.ResponseWriter)
		wantStatus int
		wantCode   int
	}{
		{name: "raw write implies 200", write: func(w http.ResponseWriter) { _, _ = w.Write([]byte("x")) }, wantStatus: 200, wantCode: 200},
		{name: "raw WriteHeader", write: func(w http.ResponseWriter) { w.WriteHeader(201) }, wantStatus: 201, wantCode: 201},
		{name: "second WriteHeader ignored", write: func(w http.ResponseWriter) { w.WriteHeader(202); w.WriteHeader(500) }, wantStatus: 202, wantCode: 202},
		// The recorder keeps the informational code; real connections send
		// it as an interim response before the final 200.
		{name: "1xx does not start response", write: func(w http.ResponseWriter) { w.WriteHeader(103); w.WriteHeader(200) }, wantStatus: 200, wantCode: 103},
		{name: "flush implies 200", write: func(w http.ResponseWriter) { w.(http.Flusher).Flush() }, wantStatus: 200, wantCode: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newTestContext(http.MethodGet, "/", nil)
			tt.write(c.Response())
			if !c.Written() {
				t.Error("Written() = false")
			}
			if got := c.w.statusCode(); got != tt.wantStatus {
				t.Errorf("tracked status = %d, want %d", got, tt.wantStatus)
			}
			if rec.Code != tt.wantCode {
				t.Errorf("recorder code = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestContextResponseController(t *testing.T) {
	// http.ResponseController reaches the underlying writer via Unwrap.
	c, rec := newTestContext(http.MethodGet, "/", nil)
	if err := http.NewResponseController(c.Response()).Flush(); err != nil {
		t.Errorf("Flush through ResponseController: %v", err)
	}
	if !rec.Flushed {
		t.Error("underlying recorder was not flushed")
	}
	// The recorder cannot be hijacked; the error surfaces and nothing is
	// marked as written.
	c2, _ := newTestContext(http.MethodGet, "/", nil)
	if _, _, err := c2.Response().(http.Hijacker).Hijack(); err == nil {
		t.Error("Hijack on a recorder succeeded, want error")
	}
	if c2.Written() {
		t.Error("failed Hijack marked the response as written")
	}
}

func TestContextHijackMarksWritten(t *testing.T) {
	s := New()
	result := make(chan bool, 1)
	s.Get("/ws", func(c *Context) error {
		conn, buf, err := c.Response().(http.Hijacker).Hijack()
		if err != nil {
			return err
		}
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\n")
		_ = buf.Flush()
		conn.Close()
		result <- c.Written()
		return errors.New("must only be logged, never written to the hijacked connection")
	})
	ts := httptest.NewServer(s)
	defer ts.Close()
	resp, err := ts.Client().Get(ts.URL + "/ws")
	if err == nil {
		resp.Body.Close()
	}
	if written := <-result; !written {
		t.Error("Written() = false after Hijack")
	}
}

func TestContextResponseUnwrap(t *testing.T) {
	c, rec := newTestContext(http.MethodGet, "/", nil)
	u, ok := c.Response().(interface{ Unwrap() http.ResponseWriter })
	if !ok || u.Unwrap() != http.ResponseWriter(rec) {
		t.Error("Response() does not unwrap to the underlying writer")
	}
}

type strictPayload struct {
	Name    string `json:"name"`
	Age     int    `json:"age"`
	Address struct {
		City string `json:"city"`
	} `json:"address"`
	Tags []string `json:"tags"`
}

// TestContextBindJSONStrictVersusLenient runs every body through both
// binders and documents where they differ: only BindJSONStrict rejects
// unknown fields; both reject trailing data and the same malformed input.
func TestContextBindJSONStrictVersusLenient(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		lenientErr  error
		strictErr   error
		strictMsg   string // substring of the strict error, when it fails
		want        string // expected Name after a successful strict bind
	}{
		{name: "exact shape", contentType: "application/json", body: `{"name":"ada","age":36}`, want: "ada"},
		{name: "subset of fields", contentType: "application/json", body: `{"name":"ada"}`, want: "ada"},
		{name: "empty object", contentType: "application/json", body: `{}`},
		{name: "null", contentType: "application/json", body: `null`},
		{name: "surrounding whitespace", contentType: "application/json", body: " \n\t{\"name\":\"ada\"}\r\n ", want: "ada"},
		{name: "case-insensitive field name", contentType: "application/json", body: `{"NAME":"ada"}`, want: "ada"},
		{name: "nested known fields", contentType: "application/json", body: `{"address":{"city":"London"},"tags":["a"]}`},
		{name: "unknown top-level field", contentType: "application/json", body: `{"name":"ada","nmae":"x"}`, strictErr: ErrInvalidJSON, strictMsg: `unknown field "nmae"`},
		{name: "unknown nested field", contentType: "application/json", body: `{"address":{"zip":"123"}}`, strictErr: ErrInvalidJSON, strictMsg: `unknown field "zip"`},
		{name: "unknown field with null value", contentType: "application/json", body: `{"extra":null}`, strictErr: ErrInvalidJSON, strictMsg: `unknown field "extra"`},
		{name: "trailing garbage", contentType: "application/json", body: `{"name":"ada"} garbage`, lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON, strictMsg: "after the JSON value"},
		{name: "second JSON value", contentType: "application/json", body: `{"name":"ada"}{"name":"bob"}`, lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON, strictMsg: "after the JSON value"},
		{name: "trailing closing brace", contentType: "application/json", body: `{"name":"ada"}}`, lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON, strictMsg: "after the JSON value"},
		{name: "trailing number", contentType: "application/json", body: `{"name":"ada"} 1`, lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON, strictMsg: "after the JSON value"},
		{name: "empty body", contentType: "application/json", body: "", lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON, strictMsg: "body is empty"},
		{name: "whitespace only", contentType: "application/json", body: "  \n ", lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON, strictMsg: "body is empty"},
		{name: "truncated", contentType: "application/json", body: `{"name":`, lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON},
		{name: "type mismatch", contentType: "application/json", body: `{"age":"old"}`, lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON},
		{name: "array instead of object", contentType: "application/json", body: `[1]`, lenientErr: ErrInvalidJSON, strictErr: ErrInvalidJSON},
		{name: "wrong content type", contentType: "text/plain", body: `{}`, lenientErr: ErrUnsupportedMediaType, strictErr: ErrUnsupportedMediaType},
		{name: "missing content type", body: `{}`, lenientErr: ErrUnsupportedMediaType, strictErr: ErrUnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newCtx := func() *Context {
				c, _ := newTestContext(http.MethodPost, "/", strings.NewReader(tt.body))
				if tt.contentType != "" {
					c.r.Header.Set("Content-Type", tt.contentType)
				}
				return c
			}

			var lenient strictPayload
			if err := newCtx().BindJSON(&lenient); !errorMatches(err, tt.lenientErr) {
				t.Errorf("BindJSON() error = %v, want %v", err, tt.lenientErr)
			}

			var strict strictPayload
			err := newCtx().BindJSONStrict(&strict)
			if !errorMatches(err, tt.strictErr) {
				t.Fatalf("BindJSONStrict() error = %v, want %v", err, tt.strictErr)
			}
			if err != nil {
				if tt.strictMsg != "" && !strings.Contains(err.Error(), tt.strictMsg) {
					t.Errorf("BindJSONStrict() error = %q, want it to contain %q", err, tt.strictMsg)
				}
				return
			}
			if strict.Name != tt.want {
				t.Errorf("Name = %q, want %q", strict.Name, tt.want)
			}
		})
	}
}

// errorMatches reports whether err satisfies want: nil when want is nil,
// errors.Is otherwise.
func errorMatches(err, want error) bool {
	if want == nil {
		return err == nil
	}
	return errors.Is(err, want)
}

func TestContextBindJSONStrictMapAcceptsAnyKey(t *testing.T) {
	c, _ := newTestContext(http.MethodPost, "/", strings.NewReader(`{"anything":1,"else":"x"}`))
	c.r.Header.Set("Content-Type", "application/json")
	var got map[string]any
	if err := c.BindJSONStrict(&got); err != nil {
		t.Fatalf("BindJSONStrict(map) error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("map = %v, want two keys", got)
	}
}

func TestContextBindJSONStrictBodyTooLarge(t *testing.T) {
	s := New()
	s.MaxBodyBytes = 8
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"way too long"}`))
	req.Header.Set("Content-Type", "application/json")
	c := newContext(httptest.NewRecorder(), req, s)
	var got jsonPayload
	if err := c.BindJSONStrict(&got); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("BindJSONStrict() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestContextBindJSONStrictSharesBodyCache(t *testing.T) {
	c, _ := newTestContext(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
	c.r.Header.Set("Content-Type", "application/json")
	var p1, p2 jsonPayload
	if err := c.BindJSON(&p1); err != nil {
		t.Fatal(err)
	}
	c.r.Body = io.NopCloser(errReader{})
	if err := c.BindJSONStrict(&p2); err != nil {
		t.Fatalf("BindJSONStrict() after BindJSON error = %v, want cached success", err)
	}
	if p1 != p2 {
		t.Errorf("strict payload = %+v, want %+v", p2, p1)
	}
}

func TestBindJSONStrictErrorResponse(t *testing.T) {
	s := New()
	s.Post("/users", func(c *Context) error {
		var p jsonPayload
		if err := c.BindJSONStrict(&p); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, p)
	})
	tests := []struct {
		name     string
		body     string
		wantCode int
		wantBody string
	}{
		{name: "valid", body: `{"name":"ada"}`, wantCode: 200, wantBody: `{"name":"ada","age":0}`},
		{name: "unknown field", body: `{"name":"ada","admin":true}`, wantCode: 400, wantBody: `{"error":"Request body is not valid JSON."}`},
		{name: "trailing data", body: `{"name":"ada"} x`, wantCode: 400, wantBody: `{"error":"Request body is not valid JSON."}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode || rec.Body.String() != tt.wantBody {
				t.Fatalf("response = %d %q, want %d %q", rec.Code, rec.Body.String(), tt.wantCode, tt.wantBody)
			}
			if strings.Contains(rec.Body.String(), "admin") {
				t.Fatal("the unknown field name must not be echoed to the client")
			}
		})
	}
}
