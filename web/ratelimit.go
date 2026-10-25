package web

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"
)

// DefaultRateLimitMessage is the 429 error message used when
// RateLimitConfig.Message is empty.
const DefaultRateLimitMessage = "Too many requests. Please try again later."

// DefaultRateLimitMaxKeys is the number of keys a rate limiter tracks
// individually when RateLimitConfig.MaxKeys is zero.
const DefaultRateLimitMaxKeys = 100000

// RateLimitConfig configures the RateLimit middleware and NewRateLimiter.
type RateLimitConfig struct {
	// Limit is the maximum number of requests per Window for one key.
	// Must be greater than zero.
	Limit int

	// Window is the length of the rate window. Must be greater than zero.
	Window time.Duration

	// Key derives the bucket a request counts against. Default:
	// ClientKey, the client IP with IPv6 addresses grouped by their /64
	// prefix. Behind a reverse proxy, enable Server.TrustProxyHeaders so
	// the real client address is used. Ignored by NewRateLimiter.
	Key func(*Context) string

	// Message is the error message of the 429 response. Default
	// DefaultRateLimitMessage. Ignored by NewRateLimiter.
	Message string

	// MaxKeys bounds the number of keys tracked individually. When that
	// many keys are tracked and none is idle enough to be evicted, every
	// further new key shares one overflow bucket with the same Limit and
	// Window until room frees up. Memory therefore stays bounded, and a
	// client rotating keys (for example addresses) is limited
	// collectively; the trade-off is that new legitimate keys arriving
	// while the limiter is full share that bucket too. Keys already
	// tracked are unaffected. Default DefaultRateLimitMaxKeys (100000);
	// must not be negative.
	MaxKeys int
}

// RateLimit returns an in-memory rate limiting middleware built on a
// RateLimiter. Each call creates an independent limiter; a limiter used
// as group middleware is shared by all routes of the group.
//
//	auth := s.Group("/auth", web.RateLimit(web.RateLimitConfig{
//		Limit:  10,
//		Window: time.Minute,
//	}))
//
// Requests are counted per RateLimitConfig.Key, by default ClientKey.
// Every response carries RateLimit-Limit and RateLimit-Remaining headers.
// A rejected request is answered 429 through TooManyRequests(Message) with
// a Retry-After header (whole seconds, at least 1) telling the client when
// its next request will be accepted.
//
// See RateLimiter for the algorithm, eviction and the MaxKeys bound.
// RateLimit panics if Limit or Window is not greater than zero or MaxKeys
// is negative.
func RateLimit(cfg RateLimitConfig) MiddlewareFunc {
	return rateLimitMiddleware(newRateLimiter(cfg, time.Now), cfg.Key, cfg.Message)
}

// RateLimiter is an in-memory, per-key rate limiter. RateLimit is built
// on it; use it directly to limit by something other than a request
// attribute, or to combine several limits in one handler (for example
// per client and per account):
//
//	perEmail := web.NewRateLimiter(web.RateLimitConfig{Limit: 5, Window: time.Minute})
//	if ok, retry, _ := perEmail.Allow(email); !ok {
//		c.SetHeader("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
//		return web.TooManyRequests("")
//	}
//
// The algorithm is a sliding-window counter: each key counts the requests
// accepted in the current fixed window and the previous one, and a request
// is accepted while
//
//	previous * (unelapsed fraction of the current window) + current < Limit
//
// This approximates "at most Limit requests in any Window-long span"
// without storing timestamps, and avoids the double burst fixed windows
// allow around a window boundary. Rejected requests are not counted.
//
// Keys idle for two windows are evicted (swept lazily, at most once per
// Window). At most MaxKeys keys are tracked; once that many are tracked
// and the sweep frees nothing, new keys share one overflow bucket (see
// RateLimitConfig.MaxKeys). State is per process: multiple instances each
// enforce their own limit. A RateLimiter is safe for concurrent use.
type RateLimiter struct {
	limit   int
	window  time.Duration
	maxKeys int
	now     func() time.Time

	mu        sync.Mutex
	counters  map[string]*rateCounter
	overflow  rateCounter // shared by new keys while counters is full
	lastSweep time.Time
}

// NewRateLimiter returns a RateLimiter enforcing cfg.Limit requests per
// cfg.Window for each key, tracking at most cfg.MaxKeys keys. cfg.Key and
// cfg.Message are ignored. NewRateLimiter panics if Limit or Window is not
// greater than zero or MaxKeys is negative.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	return newRateLimiter(cfg, time.Now)
}

// rateCounter is the sliding-window state of one key.
type rateCounter struct {
	start    time.Time // start of the current fixed window
	current  int       // requests accepted in the current window
	previous int       // requests accepted in the window before it
}

// newRateLimiter validates cfg and builds a limiter reading time from now.
func newRateLimiter(cfg RateLimitConfig, now func() time.Time) *RateLimiter {
	if cfg.Limit <= 0 {
		panic(fmt.Sprintf("web: RateLimit: Limit must be greater than zero, got %d", cfg.Limit))
	}
	if cfg.Window <= 0 {
		panic(fmt.Sprintf("web: RateLimit: Window must be greater than zero, got %v", cfg.Window))
	}
	if cfg.MaxKeys < 0 {
		panic(fmt.Sprintf("web: RateLimit: MaxKeys must not be negative, got %d", cfg.MaxKeys))
	}
	maxKeys := cfg.MaxKeys
	if maxKeys == 0 {
		maxKeys = DefaultRateLimitMaxKeys
	}
	return &RateLimiter{
		limit:    cfg.Limit,
		window:   cfg.Window,
		maxKeys:  maxKeys,
		now:      now,
		counters: make(map[string]*rateCounter),
	}
}

// rateLimitMiddleware returns the MiddlewareFunc enforcing l, counting
// requests per key (default ClientKey) and answering 429 with message
// (default DefaultRateLimitMessage).
func rateLimitMiddleware(l *RateLimiter, key func(*Context) string, message string) MiddlewareFunc {
	if key == nil {
		key = ClientKey
	}
	if message == "" {
		message = DefaultRateLimitMessage
	}
	limit := strconv.Itoa(l.limit)
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			allowed, retryAfter, remaining := l.Allow(key(c))
			h := c.Response().Header()
			h.Set("RateLimit-Limit", limit)
			h.Set("RateLimit-Remaining", strconv.Itoa(remaining))
			if !allowed {
				h.Set("Retry-After", strconv.FormatInt(retryAfterSeconds(retryAfter), 10))
				return TooManyRequests(message)
			}
			return next(c)
		}
	}
}

// Allow records one request for key if it fits in the limit. It reports
// whether the request is allowed, how long until the next request for
// key would be accepted (zero when allowed; assuming no other request is
// accepted meanwhile) and how many more requests fit right now (zero when
// rejected). Any string is a valid key, including "".
func (l *RateLimiter) Allow(key string) (allowed bool, retryAfter time.Duration, remaining int) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)
	rc := l.counters[key]
	if rc == nil {
		if len(l.counters) >= l.maxKeys {
			rc = &l.overflow
			if rc.start.IsZero() {
				rc.start = now
			}
		} else {
			rc = &rateCounter{start: now}
			l.counters[key] = rc
		}
	}
	rc.advance(now, l.window)

	estimate := rc.estimate(now, l.window)
	// The epsilon absorbs floating point noise in the weighted estimate.
	if estimate+1 > float64(l.limit)+1e-9 {
		return false, rc.retryAfter(now, l.window, l.limit), 0
	}
	rc.current++
	remaining = int(math.Floor(float64(l.limit) - estimate - 1 + 1e-9))
	if remaining < 0 {
		remaining = 0
	}
	return true, 0, remaining
}

// sweep evicts keys whose counters have been idle for two full windows (a
// fresh counter would be equivalent). It runs at most once per window.
func (l *RateLimiter) sweep(now time.Time) {
	if !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for key, rc := range l.counters {
		if now.Sub(rc.start) >= 2*l.window {
			delete(l.counters, key)
		}
	}
}

// size returns the number of individually tracked keys (for tests).
func (l *RateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.counters)
}

// advance rolls the counter forward so that start is the beginning of the
// fixed window containing now. Windows stay aligned to the key's first
// request. A clock that moved backwards leaves the counter unchanged.
func (rc *rateCounter) advance(now time.Time, window time.Duration) {
	elapsed := now.Sub(rc.start)
	if elapsed < window {
		return
	}
	periods := elapsed / window
	if periods == 1 {
		rc.previous = rc.current
	} else {
		rc.previous = 0
	}
	rc.current = 0
	rc.start = rc.start.Add(periods * window)
}

// estimate returns the weighted request count for the sliding window
// ending at now.
func (rc *rateCounter) estimate(now time.Time, window time.Duration) float64 {
	return float64(rc.previous)*rc.previousWeight(now, window) + float64(rc.current)
}

// previousWeight is the fraction of the previous window still covered by
// the sliding window ending at now.
func (rc *rateCounter) previousWeight(now time.Time, window time.Duration) float64 {
	elapsed := now.Sub(rc.start)
	if elapsed < 0 {
		elapsed = 0
	}
	return float64(window-elapsed) / float64(window)
}

// retryAfter returns how long, from now, until one more request fits,
// assuming no other request is accepted meanwhile.
func (rc *rateCounter) retryAfter(now time.Time, window time.Duration, limit int) time.Duration {
	windowEnd := rc.start.Add(window)
	if rc.current <= limit-1 {
		if rc.previous == 0 {
			return 0
		}
		// The current window has room; wait until enough of the previous
		// window's weight has slid out: previous*(1-e/W) <= limit-1-current.
		room := limit - 1 - rc.current
		e := windowFraction(window, rc.previous-room, rc.previous)
		return max(rc.start.Add(e).Sub(now), 0)
	}
	// The current window alone is full: wait for it to become the previous
	// window and slide out far enough: current*(1-e/W) <= limit-1.
	e := windowFraction(window, rc.current-(limit-1), rc.current)
	return max(windowEnd.Sub(now), 0) + e
}

// windowFraction returns window*num/den rounded up to the nanosecond. A
// tiny tolerance keeps floating point noise from rounding an exact result
// up by one nanosecond (and so Retry-After up by a whole second).
func windowFraction(window time.Duration, num, den int) time.Duration {
	if num <= 0 || den <= 0 {
		return 0
	}
	return time.Duration(math.Ceil(float64(window)*float64(num)/float64(den) - 1e-3))
}

// retryAfterSeconds rounds d up to whole seconds, with a minimum of 1.
func retryAfterSeconds(d time.Duration) int64 {
	secs := int64(math.Ceil(d.Seconds()))
	if secs < 1 {
		return 1
	}
	return secs
}
