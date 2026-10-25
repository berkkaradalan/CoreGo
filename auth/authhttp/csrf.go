package authhttp

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// Values of the Sec-Fetch-Site request header (Fetch Metadata).
const (
	fetchSiteSameOrigin = "same-origin"
	fetchSiteSameSite   = "same-site"
	fetchSiteNone       = "none"
	fetchSiteCrossSite  = "cross-site"
)

// Reasons a cookie-authenticated request fails the cross-site check,
// logged at Debug level by logCrossSiteRejection.
const (
	reasonUntrustedCrossSite = "Sec-Fetch-Site is cross-site and Origin is not a trusted origin"
	reasonOriginMismatch     = "Origin does not match the request Host and is not a trusted origin"
	reasonUnknownFetchSite   = "unknown Sec-Fetch-Site value"
)

// logMsgCrossSiteRejection is the message of the Debug record written
// for every blocked request.
const logMsgCrossSiteRejection = "authhttp: cross-site request blocked"

// crossSiteRejection reports why a request authenticated with the
// session cookie may not proceed, or "" when it may. Cookies are
// attached by the browser to requests other sites trigger (HTML forms,
// fetch with credentials), so for every method except GET, HEAD and
// OPTIONS the request must prove that it was not forged by another
// site:
//
//   - Sec-Fetch-Site "same-origin", "same-site" or "none" (typed URL,
//     bookmark) passes;
//   - Sec-Fetch-Site "cross-site" passes only when Origin is one of
//     Config.TrustedOrigins;
//   - without Sec-Fetch-Site (older browsers, non-browser clients) the
//     request passes when it has no Origin header, when the Origin's host
//     equals the request's Host, or when Origin is trusted;
//   - anything else (an unknown Sec-Fetch-Site value) is rejected.
//
// Methods are compared case-insensitively because the router routes
// them that way.
func (h *Handlers) crossSiteRejection(r *http.Request) string {
	if isSafeMethod(r.Method) {
		return ""
	}
	origin := r.Header.Get("Origin")
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case fetchSiteSameOrigin, fetchSiteSameSite, fetchSiteNone:
		return ""
	case fetchSiteCrossSite:
		if h.isTrustedOrigin(origin) {
			return ""
		}
		return reasonUntrustedCrossSite
	case "":
		if origin == "" || originMatchesHost(origin, r.Host) || h.isTrustedOrigin(origin) {
			return ""
		}
		return reasonOriginMismatch
	default:
		return reasonUnknownFetchSite
	}
}

// logCrossSiteRejection records a request blocked by the cross-site
// check at Debug level through slog.Default(), so a developer wondering
// why the frontend gets 403 "Cross-site request blocked." can enable
// Debug logging and see the deciding headers. It logs the method, path,
// Origin, Sec-Fetch-Site and the reason; never the session token or any
// other cookie.
func logCrossSiteRejection(r *http.Request, reason string) {
	slog.Default().DebugContext(r.Context(), logMsgCrossSiteRejection,
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("origin", r.Header.Get("Origin")),
		slog.String("sec_fetch_site", r.Header.Get("Sec-Fetch-Site")),
		slog.String("reason", reason),
	)
}

// isSafeMethod reports whether method is GET, HEAD or OPTIONS, in any
// letter case.
func isSafeMethod(method string) bool {
	return strings.EqualFold(method, http.MethodGet) ||
		strings.EqualFold(method, http.MethodHead) ||
		strings.EqualFold(method, http.MethodOptions)
}

// isTrustedOrigin reports whether origin is one of Config.TrustedOrigins.
func (h *Handlers) isTrustedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	_, ok := h.trustedOrigins[normalizeOrigin(origin)]
	return ok
}

// originMatchesHost reports whether the host (with port) of origin is
// host, compared case-insensitively. The opaque "null" origin and
// malformed values never match.
func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}
