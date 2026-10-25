package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Default values applied by CORS to an empty CORSConfig field.
var (
	defaultCORSMethods = []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	}
	defaultCORSHeaders = []string{"Content-Type", "Authorization"}
)

// defaultCORSMaxAge is the preflight cache duration used when
// CORSConfig.MaxAge is zero.
const defaultCORSMaxAge = 10 * time.Minute

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	// AllowedOrigins lists the origins allowed to make cross-origin
	// requests, exactly as browsers send them ("https://app.example.com",
	// scheme and host compared case-insensitively, a trailing slash
	// ignored). "*" allows any origin except the opaque "null" origin,
	// which must be listed explicitly. Empty allows no origin unless
	// AllowOriginFunc does.
	AllowedOrigins []string

	// AllowOriginFunc, when set, is consulted for origins that
	// AllowedOrigins does not match; returning true allows the origin.
	AllowOriginFunc func(origin string) bool

	// AllowedMethods is sent in Access-Control-Allow-Methods. Default:
	// GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS.
	AllowedMethods []string

	// AllowedHeaders is sent in Access-Control-Allow-Headers. Default:
	// Content-Type, Authorization. "*" allows whatever headers the
	// preflight asks for (the requested list is echoed back).
	AllowedHeaders []string

	// ExposedHeaders lists response headers browsers may expose to the
	// calling script (Access-Control-Expose-Headers). Default none.
	ExposedHeaders []string

	// AllowCredentials allows cookies and HTTP authentication on
	// cross-origin requests (Access-Control-Allow-Credentials: true).
	// It cannot be combined with the "*" origin: that would let every
	// website make authenticated requests with the visitor's cookies, so
	// CORS panics. List the allowed origins explicitly, or decide per
	// origin with AllowOriginFunc.
	AllowCredentials bool

	// MaxAge is how long browsers may cache a preflight result
	// (Access-Control-Max-Age, whole seconds). Default 10 minutes; a
	// negative value omits the header.
	MaxAge time.Duration
}

// corsPolicy is a CORSConfig resolved into ready-to-send header values.
type corsPolicy struct {
	anyOrigin   bool
	origins     map[string]struct{}
	originFunc  func(string) bool
	methods     string
	headers     string
	echoHeaders bool
	exposed     string
	credentials bool
	maxAge      string
}

// corsCredentialsWildcardPanic is the panic message of CORS for a
// configuration combining AllowCredentials with the "*" origin.
const corsCredentialsWildcardPanic = `web: CORS: AllowCredentials cannot be combined with the "*" origin; list the allowed origins or use AllowOriginFunc`

// CORS returns a middleware implementing Cross-Origin Resource Sharing.
// Install it globally with Server.Use so it also answers preflight
// requests for paths that have no explicit OPTIONS route:
//
//	s.Use(web.CORS(web.CORSConfig{
//		AllowedOrigins:   []string{"https://app.example.com"},
//		AllowCredentials: true,
//	}))
//
// Requests without an Origin header pass through untouched. Every request
// with one gets "Vary: Origin".
//
// A preflight (OPTIONS with Origin and Access-Control-Request-Method) from
// an allowed origin is answered 204 with the Allow-Origin, Allow-Methods,
// Allow-Headers, Max-Age (and Allow-Credentials) headers; one from a
// disallowed origin is answered 403. Either way the chain stops there.
//
// Other requests from an allowed origin get Access-Control-Allow-Origin
// (plus Allow-Credentials and Expose-Headers when configured) and continue
// down the chain. Requests from a disallowed origin continue without CORS
// headers, so the browser withholds the response from the calling script.
// With "*" (and no credentials) the literal "*" is sent as
// Access-Control-Allow-Origin.
//
// CORS panics when AllowCredentials is combined with the "*" origin.
func CORS(cfg CORSConfig) MiddlewareFunc {
	p := newCORSPolicy(cfg)
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			origin := c.GetHeader("Origin")
			if origin == "" {
				return next(c)
			}
			h := c.Response().Header()
			addVary(h, "Origin")
			allowed := p.allows(origin)

			if c.Method() == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
				if !allowed {
					return Forbidden("")
				}
				p.setAllowOrigin(h, origin)
				h.Set("Access-Control-Allow-Methods", p.methods)
				if p.echoHeaders {
					addVary(h, "Access-Control-Request-Headers")
					if requested := c.GetHeader("Access-Control-Request-Headers"); requested != "" {
						h.Set("Access-Control-Allow-Headers", requested)
					}
				} else if p.headers != "" {
					h.Set("Access-Control-Allow-Headers", p.headers)
				}
				if p.maxAge != "" {
					h.Set("Access-Control-Max-Age", p.maxAge)
				}
				return c.NoContent()
			}

			if allowed {
				p.setAllowOrigin(h, origin)
				if p.exposed != "" {
					h.Set("Access-Control-Expose-Headers", p.exposed)
				}
			}
			return next(c)
		}
	}
}

// newCORSPolicy resolves cfg, applying defaults.
func newCORSPolicy(cfg CORSConfig) *corsPolicy {
	p := &corsPolicy{
		origins:     make(map[string]struct{}),
		originFunc:  cfg.AllowOriginFunc,
		credentials: cfg.AllowCredentials,
	}
	for _, o := range cfg.AllowedOrigins {
		o = strings.TrimSpace(o)
		switch o {
		case "":
		case "*":
			p.anyOrigin = true
		default:
			p.origins[normalizeOrigin(o)] = struct{}{}
		}
	}
	if p.anyOrigin && p.credentials {
		panic(corsCredentialsWildcardPanic)
	}

	methods := cfg.AllowedMethods
	if len(methods) == 0 {
		methods = defaultCORSMethods
	}
	upper := make([]string, 0, len(methods))
	for _, m := range methods {
		if m = strings.TrimSpace(m); m != "" {
			upper = append(upper, strings.ToUpper(m))
		}
	}
	p.methods = strings.Join(upper, ", ")

	headers := cfg.AllowedHeaders
	if len(headers) == 0 {
		headers = defaultCORSHeaders
	}
	var named []string
	for _, h := range headers {
		switch h = strings.TrimSpace(h); h {
		case "":
		case "*":
			p.echoHeaders = true
		default:
			named = append(named, h)
		}
	}
	p.headers = strings.Join(named, ", ")
	p.exposed = strings.Join(trimEmpty(cfg.ExposedHeaders), ", ")

	switch maxAge := cfg.MaxAge; {
	case maxAge == 0:
		p.maxAge = strconv.Itoa(int(defaultCORSMaxAge / time.Second))
	case maxAge > 0:
		p.maxAge = strconv.FormatInt(int64(maxAge/time.Second), 10)
	}
	return p
}

// allows reports whether origin may make cross-origin requests.
func (p *corsPolicy) allows(origin string) bool {
	normalized := normalizeOrigin(origin)
	if _, ok := p.origins[normalized]; ok {
		return true
	}
	if p.anyOrigin && normalized != "null" {
		return true
	}
	return p.originFunc != nil && p.originFunc(origin)
}

// setAllowOrigin writes Access-Control-Allow-Origin (and
// Allow-Credentials) for an allowed origin.
func (p *corsPolicy) setAllowOrigin(h http.Header, origin string) {
	if p.anyOrigin {
		h.Set("Access-Control-Allow-Origin", "*")
	} else {
		h.Set("Access-Control-Allow-Origin", origin)
	}
	if p.credentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
}

// normalizeOrigin lowercases an origin and drops a trailing slash so that
// configuration typos such as "https://App.example.com/" still match.
func normalizeOrigin(origin string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(origin), "/"))
}

// addVary adds token to the Vary header unless it is already listed.
func addVary(h http.Header, token string) {
	for _, value := range h.Values("Vary") {
		for _, existing := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(existing), token) {
				return
			}
		}
	}
	h.Add("Vary", token)
}

// trimEmpty returns the trimmed, non-empty entries of values.
func trimEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
