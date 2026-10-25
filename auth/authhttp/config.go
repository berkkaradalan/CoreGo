package authhttp

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Default configuration values applied by New when the corresponding
// Config field is left zero.
const (
	// DefaultBasePath prefixes every mounted route when Config.BasePath
	// is empty.
	DefaultBasePath = "/auth"

	// DefaultCookieName is the session cookie name used when
	// CookieConfig.Name is empty.
	DefaultCookieName = "corego_session"

	// DefaultCookiePath is the session cookie path used when
	// CookieConfig.Path is empty.
	DefaultCookiePath = "/"

	// DefaultRateLimit is the number of requests one client (and, on
	// the routes limited per email address, one address) may send to
	// one rate limited route per window when RateLimitConfig.Limit is
	// zero.
	DefaultRateLimit = 10

	// DefaultRateLimitWindow is the rate limit window used when
	// RateLimitConfig.Window is zero.
	DefaultRateLimitWindow = time.Minute
)

// Config configures the HTTP layer. The zero value is ready to use:
// routes mount under DefaultBasePath, the session token travels in the
// Authorization header only, the sensitive routes are rate limited and
// GET /verify-email renders a minimal built-in page.
type Config struct {
	// BasePath prefixes every mounted route. Empty selects
	// DefaultBasePath ("/auth"); "/" mounts the routes at the root of
	// the router. A trailing slash is ignored. The path must start with
	// '/' and consist of non-empty static segments (no ":param" or
	// "*wildcard" segments, no query or fragment); New panics otherwise.
	// To mount below a dynamic prefix, Mount onto a web.Group instead.
	BasePath string

	// Cookie, when enabled, additionally transports the session token
	// as an HttpOnly cookie for browser clients. The Authorization
	// header always works, with or without cookies. Set
	// Cookie.OmitTokenFromBody to also keep the token out of the JSON
	// responses.
	Cookie CookieConfig

	// RateLimit limits how often one client IP may call each sensitive
	// route (sign-up, sign-in, the email and password flows, the email
	// change and account deletion) and how often one email address may
	// be used on sign-in, forgot-password and verify-email/request.
	// Enabled by default.
	RateLimit RateLimitConfig

	// TrustedOrigins lists the origins of other sites (typically your
	// frontend on another host, such as "https://app.example.com") that
	// may send state-changing requests authenticated with the session
	// cookie. It only matters with Cookie.Enabled: requests carrying the
	// token in the Authorization header are never checked.
	//
	// Cookie-authenticated requests with a method other than GET, HEAD
	// and OPTIONS pass the cross-site request forgery check when the
	// browser marks them same-origin, same-site or user-initiated
	// (Sec-Fetch-Site), when their Origin header matches the request's
	// Host, when they carry neither header (non-browser clients), or
	// when their Origin is listed here. Everything else is answered 403
	// {"error": "Cross-site request blocked."}.
	//
	// Entries are exact origins: an http or https scheme and a host with
	// an optional port, nothing else ("https://app.example.com",
	// "http://localhost:5173"). They are compared case-insensitively and
	// a trailing slash is ignored. New panics on any other value,
	// including "*".
	TrustedOrigins []string

	// VerifyEmailRedirectURL is where GET {BasePath}/verify-email (the
	// link in verification emails) sends the browser after verifying,
	// with a "status" query parameter set to "verified", "invalid" or
	// "expired"; existing query parameters and the fragment are kept.
	// It is either an absolute http(s) URL or an absolute path such as
	// "/email-verified". Empty renders a minimal built-in HTML page
	// instead. New panics on any other value.
	VerifyEmailRedirectURL string
}

// CookieConfig describes the optional session cookie. The cookie is
// always HttpOnly, so scripts in the page can never read it. The sign-up
// and sign-in responses still return the token in their JSON body too,
// unless OmitTokenFromBody is set: only then does the token never reach
// JavaScript at all.
type CookieConfig struct {
	// Enabled turns the cookie transport on: sign-up and sign-in set the
	// cookie, the routes that end the current session clear it, and
	// requests without an Authorization header authenticate with it.
	Enabled bool

	// Name is the cookie name. Default DefaultCookieName
	// ("corego_session").
	Name string

	// Path is the cookie path. Default "/".
	Path string

	// Domain is the cookie domain. Empty leaves the attribute unset, so
	// the cookie is sent to the exact host that set it only.
	Domain string

	// Secure restricts the cookie to HTTPS. It defaults to false so
	// local development over plain HTTP works; set it to true in
	// production.
	Secure bool

	// SameSite is the cookie's SameSite policy. The zero value selects
	// http.SameSiteLaxMode. http.SameSiteNoneMode (needed when the
	// frontend runs on another site than the API) requires Secure;
	// browsers reject such cookies otherwise, so New panics.
	SameSite http.SameSite

	// OmitTokenFromBody leaves the "token" field out of the sign-up and
	// sign-in responses (expires_at, user and verification_required
	// stay), so the session token only ever travels in the HttpOnly
	// cookie and scripts in the page, including injected ones, can never
	// read it. Set it for browser-only apps that rely on the cookie;
	// leave it false when the same API also serves clients that need the
	// bearer token (mobile apps, CLIs). It requires Enabled: without the
	// cookie the client would receive no token at all, so New panics and
	// Config.Validate reports an error.
	OmitTokenFromBody bool
}

// RateLimitConfig configures the rate limits of the sensitive routes.
// Every rate limited route gets its own independent limiter per client
// IP (IPv6 addresses grouped by their /64 prefix, see web.ClientKey), so
// a burst of sign-in attempts does not block sign-ups.
//
// POST /signin, /forgot-password and /verify-email/request are
// additionally limited per email address (trimmed and lowercased), with
// the same Limit and Window and again one independent limiter per route,
// so an attacker spreading requests over many IPs cannot guess one
// account's password or flood one inbox. The per-address limit applies
// to registered and unknown addresses alike and therefore reveals
// nothing about which addresses are registered.
type RateLimitConfig struct {
	// Disabled turns rate limiting off for every route, for example
	// when a reverse proxy or API gateway already enforces limits.
	Disabled bool

	// Limit is the maximum number of requests per Window from one
	// client IP to one route, and for one email address to one of the
	// routes limited per address. Default DefaultRateLimit (10).
	// Negative values make New panic.
	Limit int

	// Window is the length of the rate window. Default
	// DefaultRateLimitWindow (1 minute). Negative values make New
	// panic.
	Window time.Duration
}

// Validate reports the first invalid value in cfg as an error starting
// with "authhttp: ", or nil when New would accept cfg. Call it when the
// configuration comes from user input or the environment and you want
// an error instead of New's panic.
func (cfg Config) Validate() error {
	_, err := cfg.resolve()
	return err
}

// ResolvedBasePath returns the prefix Mount registers the routes under
// for this configuration: "" for the root (BasePath "/"), DefaultBasePath
// for an empty BasePath, otherwise the normalized BasePath without a
// trailing slash. It is the value Handlers.BasePath reports, available
// before the Handlers exist (for example to build email links).
func (cfg Config) ResolvedBasePath() (string, error) {
	return normalizeBasePath(cfg.BasePath)
}

// withDefaults returns the resolved configuration and panics with the
// error Validate reports on invalid values.
func (cfg Config) withDefaults() Config {
	resolved, err := cfg.resolve()
	if err != nil {
		panic(err.Error())
	}
	return resolved
}

// resolve validates cfg and returns a copy with zero fields replaced by
// the package defaults and the base path normalized.
func (cfg Config) resolve() (Config, error) {
	base, err := normalizeBasePath(cfg.BasePath)
	if err != nil {
		return Config{}, err
	}
	cfg.BasePath = base

	if cfg.Cookie.Name == "" {
		cfg.Cookie.Name = DefaultCookieName
	}
	if cfg.Cookie.Path == "" {
		cfg.Cookie.Path = DefaultCookiePath
	}
	if cfg.Cookie.SameSite == 0 {
		cfg.Cookie.SameSite = http.SameSiteLaxMode
	}
	if cfg.Cookie.Enabled {
		if err := validateCookie(cfg.Cookie); err != nil {
			return Config{}, err
		}
	} else if cfg.Cookie.OmitTokenFromBody {
		return Config{}, fmt.Errorf("authhttp: invalid Cookie configuration: OmitTokenFromBody requires Enabled=true (otherwise sign-up and sign-in would hand the client no session token at all)")
	}

	if cfg.RateLimit.Limit < 0 {
		return Config{}, fmt.Errorf("authhttp: RateLimit.Limit must not be negative, got %d", cfg.RateLimit.Limit)
	}
	if cfg.RateLimit.Window < 0 {
		return Config{}, fmt.Errorf("authhttp: RateLimit.Window must not be negative, got %v", cfg.RateLimit.Window)
	}
	if cfg.RateLimit.Limit == 0 {
		cfg.RateLimit.Limit = DefaultRateLimit
	}
	if cfg.RateLimit.Window == 0 {
		cfg.RateLimit.Window = DefaultRateLimitWindow
	}

	if err := validateRedirectURL(cfg.VerifyEmailRedirectURL); err != nil {
		return Config{}, err
	}

	trusted, err := normalizeTrustedOrigins(cfg.TrustedOrigins)
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedOrigins = trusted
	return cfg, nil
}

// normalizeBasePath validates a raw BasePath and returns the prefix the
// routes are registered under: "" (empty) selects DefaultBasePath, "/"
// becomes "" (the root) and one trailing slash is dropped.
func normalizeBasePath(raw string) (string, error) {
	switch raw {
	case "":
		return DefaultBasePath, nil
	case "/":
		return "", nil
	}
	invalid := func(reason string) error {
		return fmt.Errorf("authhttp: invalid BasePath %q: %s", raw, reason)
	}
	if raw[0] != '/' {
		return "", invalid("must start with '/'")
	}
	base := strings.TrimSuffix(raw, "/")
	for _, seg := range strings.Split(base, "/")[1:] {
		switch {
		case seg == "":
			return "", invalid("must not contain empty segments")
		case seg[0] == ':' || seg[0] == '*':
			return "", invalid("must not contain parameter or wildcard segments; mount onto a web.Group for a dynamic prefix")
		case strings.ContainsAny(seg, "?#"):
			return "", invalid("must not contain a query or fragment")
		case strings.IndexFunc(seg, isSpaceOrControl) >= 0:
			return "", invalid("must not contain whitespace or control characters")
		}
		if _, err := url.PathUnescape(seg); err != nil {
			return "", invalid("malformed percent-encoding")
		}
	}
	return base, nil
}

// isSpaceOrControl reports whether r is an ASCII space, a control
// character or a non-ASCII space; none of them belong in a path.
func isSpaceOrControl(r rune) bool {
	return r <= ' ' || r == 0x7f || r == '\u00a0' || r == '\u2028' || r == '\u2029'
}

// validateCookie reports cookie settings browsers or net/http would
// reject.
func validateCookie(cc CookieConfig) error {
	probe := &http.Cookie{Name: cc.Name, Value: "probe", Path: cc.Path, Domain: cc.Domain}
	if err := probe.Valid(); err != nil {
		return fmt.Errorf("authhttp: invalid Cookie configuration: %v", err)
	}
	if cc.SameSite == http.SameSiteNoneMode && !cc.Secure {
		return fmt.Errorf("authhttp: invalid Cookie configuration: SameSite=None requires Secure=true (browsers reject the cookie otherwise)")
	}
	return nil
}

// validateRedirectURL accepts "" (no redirect), an absolute http(s) URL
// with a host, or an absolute path ("/done", not the protocol-relative
// "//host").
func validateRedirectURL(raw string) error {
	if raw == "" {
		return nil
	}
	invalid := func(reason string) error {
		return fmt.Errorf("authhttp: invalid VerifyEmailRedirectURL %q: %s", raw, reason)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return invalid("not a valid URL")
	}
	switch {
	case u.Scheme == "http" || u.Scheme == "https":
		if u.Host == "" {
			return invalid("an absolute URL needs a host")
		}
	case u.Scheme != "":
		return invalid("only http and https URLs are allowed")
	case u.Host != "" || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\"):
		return invalid("protocol-relative URLs are not allowed; use an absolute http(s) URL")
	case !strings.HasPrefix(u.Path, "/"):
		return invalid("must be an absolute http(s) URL or a path starting with '/'")
	}
	return nil
}

// normalizeTrustedOrigins validates every entry of TrustedOrigins and
// returns a new slice holding their normalized form (see
// normalizeOrigin). The caller's slice is never modified.
func normalizeTrustedOrigins(origins []string) ([]string, error) {
	if len(origins) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(origins))
	for _, origin := range origins {
		if err := validateTrustedOrigin(origin); err != nil {
			return nil, err
		}
		normalized = append(normalized, normalizeOrigin(origin))
	}
	return normalized, nil
}

// validateTrustedOrigin accepts an absolute http(s) origin: a scheme and
// a host with an optional port, optionally followed by a single slash.
func validateTrustedOrigin(raw string) error {
	invalid := func(reason string) error {
		return fmt.Errorf("authhttp: invalid TrustedOrigins entry %q: %s", raw, reason)
	}
	if raw == "*" {
		return invalid("wildcards are not allowed; list each origin")
	}
	if strings.TrimSpace(raw) != raw || strings.IndexFunc(raw, isSpaceOrControl) >= 0 {
		return invalid("must not contain whitespace or control characters")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return invalid("not a valid URL")
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return invalid("must be an absolute http or https origin such as \"https://app.example.com\"")
	case u.Host == "" || u.Hostname() == "":
		return invalid("must contain a host")
	case u.User != nil:
		return invalid("must not contain user information")
	case u.Path != "" && u.Path != "/", u.RawQuery != "" || u.ForceQuery, u.Fragment != "" || strings.Contains(raw, "#"):
		return invalid("must be an origin only, without a path, query or fragment")
	}
	return nil
}

// normalizeOrigin returns the form origins are compared in: lowercase
// and without a trailing slash, matching how browsers send the Origin
// header.
func normalizeOrigin(origin string) string {
	return strings.ToLower(strings.TrimSuffix(origin, "/"))
}
