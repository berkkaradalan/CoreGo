package authhttp

import (
	"math"
	"strconv"
	"strings"

	"github.com/berkkaradalan/CoreGo/web"
)

// emailRateKey is the key an email address counts against in the per
// address limiters: trimmed and lowercased, exactly like the auth
// service normalizes addresses, so "Ada@Example.com " and
// "ada@example.com" share one bucket. Malformed and empty input is
// limited like any other value.
func emailRateKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// allowEmail records one request for email on the per address limiter
// l and answers 429 exactly like the per client limiter (same message,
// Retry-After in whole seconds, RateLimit-Remaining 0) when the address
// is over the limit. A nil l (rate limiting disabled) allows everything.
// On success RateLimit-Remaining reports the stricter of the per client
// and the per address allowance.
//
// The decision depends on nothing but the submitted string, so it is
// identical for registered and unknown addresses.
func (h *Handlers) allowEmail(c *web.Context, l *web.RateLimiter, email string) error {
	if l == nil {
		return nil
	}
	allowed, retryAfter, remaining := l.Allow(emailRateKey(email))
	header := c.Response().Header()
	header.Set("RateLimit-Limit", strconv.Itoa(h.cfg.RateLimit.Limit))
	if !allowed {
		header.Set("RateLimit-Remaining", "0")
		header.Set("Retry-After", strconv.FormatInt(max(1, int64(math.Ceil(retryAfter.Seconds()))), 10))
		return web.TooManyRequests(web.DefaultRateLimitMessage)
	}
	if current, err := strconv.Atoi(header.Get("RateLimit-Remaining")); err != nil || remaining < current {
		header.Set("RateLimit-Remaining", strconv.Itoa(remaining))
	}
	return nil
}
