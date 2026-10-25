package authhttp

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// Values of the "status" query parameter GET /verify-email appends to
// Config.VerifyEmailRedirectURL.
const (
	verifyStatusVerified = "verified"
	verifyStatusInvalid  = "invalid"
	verifyStatusExpired  = "expired"
)

// requestEmailVerification returns the handler of
// POST /verify-email/request, the public "send me a new verification
// link" flow. It answers 202 with the same body for registered, unknown,
// malformed and already verified addresses and whether or not a mailer
// is configured, so it cannot be used to enumerate accounts. Only a
// malformed request body (400, 413, 415), a rate limit (429, applied
// per client and, through perEmail, per address alike for every
// address) or a store failure (500) produces another status.
func (h *Handlers) requestEmailVerification(perEmail *web.RateLimiter) web.HandlerFunc {
	return func(c *web.Context) error {
		var req emailRequest
		if err := c.BindJSON(&req); err != nil {
			return mapError(err)
		}
		if err := h.allowEmail(c, perEmail, req.Email); err != nil {
			return err
		}
		if err := h.service.RequestEmailVerification(c, req.Email); err != nil {
			return mapError(err)
		}
		return c.JSON(http.StatusAccepted, messageResponse{Message: msgEmailRequestAccepted})
	}
}

// verifyEmail handles POST /verify-email, the JSON variant for clients
// that read the token from the link themselves.
func (h *Handlers) verifyEmail(c *web.Context) error {
	var req verifyEmailRequest
	if err := c.BindJSON(&req); err != nil {
		return mapError(err)
	}
	if err := h.service.VerifyEmail(c, req.Email, req.Token); err != nil {
		return mapError(err)
	}
	return c.NoContent()
}

// verifyEmailLink handles GET /verify-email?email=...&token=..., the
// target of the link in verification emails. It verifies the address,
// then either redirects (303) to Config.VerifyEmailRedirectURL with a
// status query parameter or renders the built-in result page (200 for
// verified, 400 for invalid or expired). Missing parameters count as an
// invalid link. Store failures propagate as a 500.
//
// HEAD requests (sent by some link scanners and previewers) never
// consume the token: they receive the security headers and an empty 200.
func (h *Handlers) verifyEmailLink(c *web.Context) error {
	setPageSecurityHeaders(c)
	// The router accepts methods in any letter case, so "head" must not
	// slip through to the token-consuming path either.
	if strings.EqualFold(c.Method(), http.MethodHead) {
		return c.Status(http.StatusOK)
	}

	status := verifyStatusVerified
	err := h.service.VerifyEmail(c, c.Query("email"), c.Query("token"))
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrTokenExpired):
		status = verifyStatusExpired
	case errors.Is(err, auth.ErrInvalidToken):
		status = verifyStatusInvalid
	default:
		return mapError(err)
	}

	if h.cfg.VerifyEmailRedirectURL != "" {
		return c.Redirect(http.StatusSeeOther, redirectWithStatus(h.cfg.VerifyEmailRedirectURL, status))
	}
	page := verifyPages[status]
	return c.HTML(page.code, renderVerifyPage(page))
}

// redirectWithStatus returns target with its "status" query parameter
// set, keeping every other parameter and the fragment. target was
// validated by New, so parsing cannot fail.
func redirectWithStatus(target, status string) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	query := u.Query()
	query.Set("status", status)
	u.RawQuery = query.Encode()
	return u.String()
}

// setPageSecurityHeaders sets the headers of GET /verify-email
// responses. The request URL contains a single use token, so no
// referrer may leak it, and the page itself loads nothing but inline
// styles.
func setPageSecurityHeaders(c *web.Context) {
	c.SetHeader("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	c.SetHeader("X-Content-Type-Options", "nosniff")
	c.SetHeader("Referrer-Policy", "no-referrer")
	c.SetHeader("Cache-Control", "no-store")
}
