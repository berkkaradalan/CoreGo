package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Context carries per-request state through the middleware and handler
// chain. The Server creates one for every request; handlers never construct
// one themselves.
//
// Context implements context.Context by delegating Deadline, Done, Err and
// Value to the current request's context, so it can be passed straight to
// anything that takes a context.Context:
//
//	user, err := users.Find(c, c.Param("id"))
//
// A Context is not safe for concurrent use by multiple goroutines, and it
// must not be retained after the handler returns.
type Context struct {
	w       *responseWriter
	r       *http.Request
	params  map[string]string
	server  *Server
	target  HandlerFunc  // route handler or framework fallback
	onError ErrorHandler // snapshot of Server.OnError (nil = default)

	maxBody        int64
	trustProxy     bool
	clientIPHeader string

	body     []byte
	bodyRead bool
	bodyErr  error
	keys     map[string]any
}

// newContext builds a Context, snapshotting the server's request-related
// settings at request start.
func newContext(w http.ResponseWriter, r *http.Request, s *Server) *Context {
	return &Context{
		w:              newResponseWriter(w),
		r:              r,
		params:         map[string]string{},
		server:         s,
		maxBody:        s.MaxBodyBytes,
		trustProxy:     s.TrustProxyHeaders,
		clientIPHeader: s.ClientIPHeader,
	}
}

// logger returns the owning server's logger.
func (c *Context) logger() *slog.Logger { return c.server.logger() }

// handleError renders err through the configured error handler, or only
// logs it when the response has already been started and can no longer be
// changed. It is how errors returned through the chain become responses.
func (c *Context) handleError(err error) {
	if err == nil {
		return
	}
	if c.Written() {
		c.logger().Error("web: handler error after response was written",
			"method", c.Method(),
			"path", c.Path(),
			"error", err,
		)
		return
	}
	fn := c.onError
	if fn == nil {
		fn = defaultOnError
	}
	fn(c, err)
}

// Deadline implements context.Context by delegating to the request context.
func (c *Context) Deadline() (time.Time, bool) { return c.r.Context().Deadline() }

// Done implements context.Context by delegating to the request context. It
// is closed when the client disconnects, the request times out or the
// server shuts down hard.
func (c *Context) Done() <-chan struct{} { return c.r.Context().Done() }

// Err implements context.Context by delegating to the request context.
func (c *Context) Err() error { return c.r.Context().Err() }

// Value implements context.Context by delegating to the request context,
// so values attached with SetRequest(r.WithContext(...)) are visible. It
// does not see values stored with Set.
func (c *Context) Value(key any) any { return c.r.Context().Value(key) }

// Request returns the current *http.Request.
func (c *Context) Request() *http.Request { return c.r }

// SetRequest replaces the current request, typically to attach values to
// its context so downstream handlers (and Value) see them:
//
//	ctx := context.WithValue(c.Request().Context(), key, value)
//	c.SetRequest(c.Request().WithContext(ctx))
//
// Path parameters and values stored with Set are kept. It panics if r is
// nil.
func (c *Context) SetRequest(r *http.Request) {
	if r == nil {
		panic("web: SetRequest called with a nil *http.Request")
	}
	c.r = r
}

// Response returns the http.ResponseWriter for the current request. Writes
// made through it are tracked, so Written stays accurate. It supports
// http.Flusher, http.Hijacker and http.ResponseController when the
// underlying connection does.
func (c *Context) Response() http.ResponseWriter { return c.w }

// Param returns the value of the named path parameter (":name" or "*name"
// segments), or the empty string when no such parameter was captured.
// Values are percent-decoded exactly once.
func (c *Context) Param(name string) string { return c.params[name] }

// Query returns the first URL query parameter with the given name, or the
// empty string when absent.
func (c *Context) Query(name string) string { return c.r.URL.Query().Get(name) }

// DefaultQuery returns the first URL query parameter with the given name,
// or def when the parameter is absent. A parameter that is present but
// empty ("?q=") returns the empty string.
func (c *Context) DefaultQuery(name, def string) string {
	values := c.r.URL.Query()
	if values.Has(name) {
		return values.Get(name)
	}
	return def
}

// Queries returns all URL query parameters, keyed by name.
func (c *Context) Queries() url.Values { return c.r.URL.Query() }

// Path returns the decoded request path, without the query string.
func (c *Context) Path() string { return c.r.URL.Path }

// Method returns the request's HTTP method.
func (c *Context) Method() string { return c.r.Method }

// RemoteIP returns the client IP address.
//
// With Server.TrustProxyHeaders false (the default) it is the host part of
// the connection's remote address. With it true, the address comes from
// Server.ClientIPHeader: for "X-Forwarded-For" the RIGHTMOST non-empty
// entry is used (the address appended by your own proxy; entries further
// left are supplied by the client and can be forged); any other header,
// such as "X-Real-IP" or "CF-Connecting-IP", is used whole. When the header
// is absent, empty or does not hold an IP address, the remote address is
// used instead. Addresses are returned in canonical form: an optional port
// is dropped and IPv4-mapped IPv6 addresses are unmapped.
func (c *Context) RemoteIP() string {
	if c.trustProxy {
		if ip, ok := c.proxyIP(); ok {
			return ip
		}
	}
	if ip, ok := parseIP(c.r.RemoteAddr); ok {
		return ip
	}
	// Not an IP address (e.g. a Unix socket peer): return it as-is, minus
	// any port.
	if host, _, err := net.SplitHostPort(c.r.RemoteAddr); err == nil {
		return host
	}
	return strings.TrimSpace(c.r.RemoteAddr)
}

// ClientKey returns a key identifying the client of c for per-client
// limits such as RateLimit (its default key). It is c.RemoteIP() with IPv6
// addresses reduced to their /64 prefix ("2001:db8:1:2::/64"): a single
// IPv6 subscriber usually controls a whole /64 and can rotate through its
// addresses at will, so per-address keys would not limit it at all. IPv4
// addresses (including IPv4-mapped IPv6 addresses, which RemoteIP unmaps)
// are returned unchanged, as is a remote address that is not an IP
// address. IPv6 zones are dropped.
func ClientKey(c *Context) string {
	ip := c.RemoteIP()
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is6() {
		return ip
	}
	prefix, err := addr.WithZone("").Prefix(64)
	if err != nil {
		return ip
	}
	return prefix.String()
}

// proxyIP extracts the client IP from the trusted proxy header.
func (c *Context) proxyIP() (string, bool) {
	name := c.clientIPHeader
	if name == "" {
		name = DefaultClientIPHeader
	}
	values := c.r.Header.Values(name)
	if len(values) == 0 {
		return "", false
	}
	if http.CanonicalHeaderKey(name) != DefaultClientIPHeader {
		return parseIP(values[0])
	}
	// A proxy may append its entry to the existing header line or add a
	// new line; either way the last entry overall is the trusted one.
	entries := strings.Split(strings.Join(values, ","), ",")
	for i := len(entries) - 1; i >= 0; i-- {
		if entry := strings.TrimSpace(entries[i]); entry != "" {
			return parseIP(entry)
		}
	}
	return "", false
}

// parseIP parses an IP address, optionally with a port ("1.2.3.4:80",
// "[::1]:80"), and returns it in canonical, unmapped form.
func parseIP(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		return addr.Unmap().String(), true
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr.Unmap().String(), true
		}
	}
	return "", false
}

// GetHeader returns the first request header with the given
// (case-insensitive) name, or the empty string when absent.
func (c *Context) GetHeader(name string) string { return c.r.Header.Get(name) }

// SetHeader sets a response header, replacing any existing value. Headers
// must be set before the response is written.
func (c *Context) SetHeader(name, value string) { c.w.Header().Set(name, value) }

// Body reads and returns the request body. The body is read at most once
// and cached; subsequent calls return the same result. When the server's
// MaxBodyBytes is positive (the default is 1 MiB) and the body exceeds it,
// Body returns ErrBodyTooLarge (mapped to 413 by the default error
// handler). A MaxBodyBytes of 0 means unlimited.
func (c *Context) Body() ([]byte, error) {
	if c.bodyRead {
		return c.body, c.bodyErr
	}
	c.bodyRead = true
	if c.r.Body == nil {
		return nil, nil
	}
	var reader io.Reader = c.r.Body
	if c.maxBody > 0 {
		reader = io.LimitReader(c.r.Body, c.maxBody+1)
	}
	b, err := io.ReadAll(reader)
	if err != nil {
		c.bodyErr = err
		return nil, err
	}
	if c.maxBody > 0 && int64(len(b)) > c.maxBody {
		c.bodyErr = ErrBodyTooLarge
		return nil, ErrBodyTooLarge
	}
	c.body = b
	return b, nil
}

// BindJSON decodes the request body into v. The Content-Type header must be
// application/json (parameters such as charset are ignored), otherwise
// ErrUnsupportedMediaType is returned. An empty or malformed body returns
// ErrInvalidJSON, and an oversized one ErrBodyTooLarge. All are detectable
// with errors.Is even when wrapped, and the default error handler maps them
// to 415, 400 and 413, so handlers can simply return the error.
//
// BindJSON follows encoding/json.Unmarshal: object keys that match no field
// of v are ignored, and anything but whitespace after the JSON value makes
// the body malformed. Use BindJSONStrict to reject unknown fields as well.
func (c *Context) BindJSON(v any) error {
	body, err := c.jsonBody()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	return nil
}

// BindJSONStrict is BindJSON for clients that must send exactly the
// expected shape: besides everything BindJSON rejects, an object key that
// matches no field of the destination struct (at any nesting level)
// returns ErrInvalidJSON wrapped with a message naming the field, for
// example `web: invalid JSON body: json: unknown field "nmae"`, and data
// after the JSON value returns ErrInvalidJSON too. Field names are matched
// like encoding/json does (exact match preferred, then case-insensitive).
// Maps and other non-struct destinations accept any key.
//
// The detailed message is only in the returned error; the default error
// handler answers 400 "Request body is not valid JSON." without echoing
// it. Return web.BadRequest(...) yourself to show the field to clients.
// As with BindJSON, v may be partially filled when an error is returned.
func (c *Context) BindJSONStrict(v any) error {
	body, err := c.jsonBody()
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: body is empty", ErrInvalidJSON)
		}
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: unexpected data after the JSON value", ErrInvalidJSON)
	}
	return nil
}

// jsonBody checks the Content-Type of a JSON request and returns its
// non-empty body, or the sentinel error describing why it cannot be
// decoded.
func (c *Context) jsonBody() ([]byte, error) {
	contentType := c.r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return nil, fmt.Errorf("%w: Content-Type %q", ErrUnsupportedMediaType, contentType)
	}
	body, err := c.Body()
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%w: body is empty", ErrInvalidJSON)
	}
	return body, nil
}

// Written reports whether the response has been started (a status code or
// body bytes were written, through the Context writers or Response()).
func (c *Context) Written() bool { return c.w.wroteHeader }

// write sets the Content-Type (when non-empty), the status and the body,
// returning the body write error.
func (c *Context) write(code int, contentType string, b []byte) error {
	if contentType != "" {
		c.w.Header().Set("Content-Type", contentType)
	}
	c.w.WriteHeader(code)
	if len(b) == 0 {
		return nil
	}
	_, err := c.w.Write(b)
	return err
}

// JSON serializes v as JSON and writes it with the given status code and a
// "application/json; charset=utf-8" Content-Type. When v cannot be
// serialized nothing is written and a wrapped error is returned, so the
// error handler answers 500. Otherwise it returns the write error, if any:
//
//	return c.JSON(http.StatusOK, user)
func (c *Context) JSON(code int, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("web: encode JSON response: %w", err)
	}
	return c.write(code, "application/json; charset=utf-8", payload)
}

// String writes s with the given status code and a
// "text/plain; charset=utf-8" Content-Type.
func (c *Context) String(code int, s string) error {
	return c.write(code, "text/plain; charset=utf-8", []byte(s))
}

// HTML writes s with the given status code and a
// "text/html; charset=utf-8" Content-Type. s is written verbatim: escape
// any untrusted data (for example with html/template) first.
func (c *Context) HTML(code int, s string) error {
	return c.write(code, "text/html; charset=utf-8", []byte(s))
}

// Data writes raw bytes with the given status code and Content-Type. An
// empty contentType leaves the Content-Type header unset.
func (c *Context) Data(code int, contentType string, b []byte) error {
	return c.write(code, contentType, b)
}

// NoContent writes a 204 No Content status with no body.
func (c *Context) NoContent() error {
	c.w.WriteHeader(http.StatusNoContent)
	return nil
}

// Status writes the response status code without a body.
func (c *Context) Status(code int) error {
	c.w.WriteHeader(code)
	return nil
}

// Redirect writes a redirection response with the given 3xx status code
// (commonly 301, 302, 303, 307 or 308) and Location header. A code outside
// 300-399 writes nothing and returns an error.
func (c *Context) Redirect(code int, location string) error {
	if code < 300 || code > 399 {
		return fmt.Errorf("web: invalid redirect status code %d", code)
	}
	c.w.Header().Set("Location", location)
	c.w.WriteHeader(code)
	return nil
}

// SetCookie adds a Set-Cookie header to the response. A nil cookie is
// ignored; an invalid one is dropped by net/http.
func (c *Context) SetCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}
	http.SetCookie(c.w, cookie)
}

// Cookie returns the value of the named request cookie. It returns
// http.ErrNoCookie when the cookie is absent.
func (c *Context) Cookie(name string) (string, error) {
	cookie, err := c.r.Cookie(name)
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

// Set stores a per-request value for communication between middleware and
// handlers. For values that services reading a context.Context must see,
// use SetRequest with a derived request context instead.
func (c *Context) Set(key string, value any) {
	if c.keys == nil {
		c.keys = make(map[string]any)
	}
	c.keys[key] = value
}

// Get returns the per-request value stored under key with Set and whether
// it was present.
func (c *Context) Get(key string) (any, bool) {
	value, ok := c.keys[key]
	return value, ok
}
