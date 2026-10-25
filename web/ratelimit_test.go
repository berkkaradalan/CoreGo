package web

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock for rate limiter tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// limitedServer builds a server whose GET /r is protected by a limiter
// driven by clock, keyed by the X-Key header when keyed is true.
func limitedServer(cfg RateLimitConfig, clock *fakeClock) (*Server, *RateLimiter) {
	l := newRateLimiter(cfg, clock.Now)
	s := New()
	s.Get("/r", text("ok"), rateLimitMiddleware(l, cfg.Key, cfg.Message))
	return s, l
}

// hit sends GET /r as key (used as the client address).
func hit(s *Server, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/r", nil)
	req.RemoteAddr = key + ":1234"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestRateLimitBasicLimitAndHeaders(t *testing.T) {
	clock := newFakeClock()
	s, _ := limitedServer(RateLimitConfig{Limit: 3, Window: time.Minute}, clock)
	for i, wantRemaining := range []string{"2", "1", "0"} {
		rec := hit(s, "10.0.0.1")
		if rec.Code != 200 {
			t.Fatalf("request %d code = %d, want 200", i+1, rec.Code)
		}
		if got := rec.Header().Get("RateLimit-Limit"); got != "3" {
			t.Errorf("request %d RateLimit-Limit = %q, want 3", i+1, got)
		}
		if got := rec.Header().Get("RateLimit-Remaining"); got != wantRemaining {
			t.Errorf("request %d RateLimit-Remaining = %q, want %s", i+1, got, wantRemaining)
		}
		if rec.Header().Get("Retry-After") != "" {
			t.Errorf("request %d has Retry-After", i+1)
		}
	}
	rec := hit(s, "10.0.0.1")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request code = %d, want 429", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"Too many requests. Please try again later."}` {
		t.Errorf("429 body = %q", got)
	}
	if rec.Header().Get("RateLimit-Remaining") != "0" || rec.Header().Get("RateLimit-Limit") != "3" {
		t.Errorf("429 headers = %v", rec.Header())
	}
	// Window fully elapsed for this window, then 2/3 of the previous one
	// must slide out: 60s + 20s.
	if got := rec.Header().Get("Retry-After"); got != "80" {
		t.Errorf("Retry-After = %q, want 80", got)
	}
}

func TestRateLimitRetryAfterIsAccurate(t *testing.T) {
	clock := newFakeClock()
	s, _ := limitedServer(RateLimitConfig{Limit: 3, Window: time.Minute}, clock)
	for i := 0; i < 3; i++ {
		hit(s, "a")
	}
	retry, _ := strconv.Atoi(hit(s, "a").Header().Get("Retry-After"))
	clock.Advance(time.Duration(retry-1) * time.Second)
	rec := hit(s, "a")
	if rec.Code != 429 {
		t.Fatalf("one second before Retry-After: code = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want 1", got)
	}
	clock.Advance(time.Second)
	if rec := hit(s, "a"); rec.Code != 200 {
		t.Errorf("at Retry-After: code = %d, want 200", rec.Code)
	}
}

func TestRateLimitRetryAfterMinimumOneSecond(t *testing.T) {
	clock := newFakeClock()
	s, _ := limitedServer(RateLimitConfig{Limit: 1, Window: 100 * time.Millisecond}, clock)
	hit(s, "a")
	rec := hit(s, "a")
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "1" {
		t.Errorf("code/Retry-After = %d/%q, want 429/1", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestRateLimitWindowReset(t *testing.T) {
	clock := newFakeClock()
	s, _ := limitedServer(RateLimitConfig{Limit: 2, Window: time.Minute}, clock)
	hit(s, "a")
	hit(s, "a")
	if hit(s, "a").Code != 429 {
		t.Fatal("limit not enforced")
	}
	// Two full windows later nothing is left of the old requests.
	clock.Advance(2 * time.Minute)
	for i, want := range []string{"1", "0"} {
		rec := hit(s, "a")
		if rec.Code != 200 || rec.Header().Get("RateLimit-Remaining") != want {
			t.Errorf("after reset request %d = %d remaining %q, want 200 remaining %s", i+1, rec.Code, rec.Header().Get("RateLimit-Remaining"), want)
		}
	}
	if hit(s, "a").Code != 429 {
		t.Error("limit not enforced after reset")
	}
}

func TestRateLimitSlidingWindow(t *testing.T) {
	clock := newFakeClock()
	s, _ := limitedServer(RateLimitConfig{Limit: 4, Window: time.Minute}, clock)
	for i := 0; i < 4; i++ {
		hit(s, "a")
	}
	steps := []struct {
		advance  time.Duration
		wantCode int
	}{
		// At the next window boundary the previous window still weighs 100%.
		{advance: time.Minute, wantCode: 429},
		// Half way through: 4*0.5 = 2 counted, so two more fit.
		{advance: 30 * time.Second, wantCode: 200},
		{wantCode: 200},
		{wantCode: 429},
		// Rejected requests are not counted; after 15s more: 4*0.25+2 = 3.
		{advance: 15 * time.Second, wantCode: 200},
		{wantCode: 429},
	}
	for i, step := range steps {
		clock.Advance(step.advance)
		if got := hit(s, "a").Code; got != step.wantCode {
			t.Errorf("step %d code = %d, want %d", i, got, step.wantCode)
		}
	}
}

func TestRateLimitIndependentKeys(t *testing.T) {
	clock := newFakeClock()
	cfg := RateLimitConfig{Limit: 1, Window: time.Minute, Key: func(c *Context) string { return c.GetHeader("X-Key") }}
	s, _ := limitedServer(cfg, clock)
	do := func(key string) int {
		req := httptest.NewRequest(http.MethodGet, "/r", nil)
		req.Header.Set("X-Key", key)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}
	if do("alice") != 200 || do("bob") != 200 {
		t.Fatal("first request per key must pass")
	}
	if do("alice") != 429 || do("bob") != 429 {
		t.Error("second request per key must be limited")
	}
	if do("carol") != 200 {
		t.Error("new key affected by other keys")
	}
	if do("") != 200 || do("") != 429 {
		t.Error("empty key must be limited like any other key")
	}
}

func TestRateLimitDefaultKeyIsRemoteIP(t *testing.T) {
	clock := newFakeClock()
	s, _ := limitedServer(RateLimitConfig{Limit: 1, Window: time.Minute}, clock)
	if hit(s, "10.0.0.1").Code != 200 || hit(s, "10.0.0.2").Code != 200 {
		t.Fatal("different IPs must have separate budgets")
	}
	// Same IP, different port: same budget.
	req := httptest.NewRequest(http.MethodGet, "/r", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 429 {
		t.Errorf("same IP other port code = %d, want 429", rec.Code)
	}
}

func TestRateLimitInstancesAreIndependent(t *testing.T) {
	s := New()
	s.Get("/a", text("a"), RateLimit(RateLimitConfig{Limit: 1, Window: time.Hour}))
	s.Get("/b", text("b"), RateLimit(RateLimitConfig{Limit: 1, Window: time.Hour}))
	shared := s.Group("/g", RateLimit(RateLimitConfig{Limit: 1, Window: time.Hour}))
	shared.Get("/x", text("x"))
	shared.Get("/y", text("y"))
	codes := []int{
		serveRequest(t, s, "GET", "/a", nil).Code,
		serveRequest(t, s, "GET", "/b", nil).Code,
		serveRequest(t, s, "GET", "/a", nil).Code,
		serveRequest(t, s, "GET", "/g/x", nil).Code,
		serveRequest(t, s, "GET", "/g/y", nil).Code, // same limiter as /g/x
	}
	want := []int{200, 200, 429, 200, 429}
	if fmt.Sprint(codes) != fmt.Sprint(want) {
		t.Errorf("codes = %v, want %v", codes, want)
	}
}

func TestRateLimitCustomMessageAndErrorHandler(t *testing.T) {
	s := New()
	s.Get("/r", text("ok"), RateLimit(RateLimitConfig{Limit: 1, Window: time.Hour, Message: "slow down"}))
	serveRequest(t, s, "GET", "/r", nil)
	rec := serveRequest(t, s, "GET", "/r", nil)
	if rec.Code != 429 || rec.Body.String() != `{"error":"slow down"}` {
		t.Errorf("code/body = %d/%q", rec.Code, rec.Body.String())
	}
}

func TestRateLimitEvictsStaleKeys(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 5, Window: time.Minute}, clock.Now)
	for i := 0; i < 1000; i++ {
		l.Allow(fmt.Sprintf("key-%d", i))
	}
	if got := l.size(); got != 1000 {
		t.Fatalf("size = %d, want 1000", got)
	}
	// 1.5 windows later the keys still matter for the sliding estimate.
	clock.Advance(90 * time.Second)
	l.Allow("fresh")
	if got := l.size(); got != 1001 {
		t.Errorf("size after 1.5 windows = %d, want 1001 (nothing evictable yet)", got)
	}
	// Sweeps run at most once per window: 30s later nothing is swept even
	// though the old keys are now two windows old.
	clock.Advance(30 * time.Second)
	l.Allow("fresh")
	if got := l.size(); got != 1001 {
		t.Errorf("size = %d, want 1001 (sweep ran twice within one window)", got)
	}
	clock.Advance(30 * time.Second)
	l.Allow("fresh")
	if got := l.size(); got != 1 {
		t.Errorf("size after sweep = %d, want 1", got)
	}
}

func TestRateLimitClockGoingBackwards(t *testing.T) {
	clock := newFakeClock()
	s, _ := limitedServer(RateLimitConfig{Limit: 1, Window: time.Minute}, clock)
	hit(s, "a")
	clock.Advance(-time.Hour)
	if rec := hit(s, "a"); rec.Code != 429 {
		t.Errorf("code = %d, want 429 (no reset by a clock jump backwards)", rec.Code)
	}
	clock.Advance(time.Hour + 2*time.Minute)
	if rec := hit(s, "a"); rec.Code != 200 {
		t.Errorf("code = %d, want 200 once time catches up", rec.Code)
	}
}

func TestRateLimitConcurrent(t *testing.T) {
	const limit = 50
	s := New()
	s.Get("/r", text("ok"), RateLimit(RateLimitConfig{Limit: limit, Window: time.Hour}))
	var allowed, limited atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				// Two keys share the load.
				switch hit(s, fmt.Sprintf("10.0.0.%d", g%2)).Code {
				case 200:
					allowed.Add(1)
				case 429:
					limited.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()
	if allowed.Load() != 2*limit {
		t.Errorf("allowed = %d, want exactly %d", allowed.Load(), 2*limit)
	}
	if allowed.Load()+limited.Load() != 400 {
		t.Errorf("allowed+limited = %d, want 400", allowed.Load()+limited.Load())
	}
}

func TestRateLimitInvalidConfigPanics(t *testing.T) {
	tests := []struct {
		name string
		cfg  RateLimitConfig
		want string
	}{
		{name: "zero limit", cfg: RateLimitConfig{Limit: 0, Window: time.Minute}, want: "RateLimit: Limit must be greater than zero, got 0"},
		{name: "negative limit", cfg: RateLimitConfig{Limit: -1, Window: time.Minute}, want: "Limit must be greater than zero, got -1"},
		{name: "zero window", cfg: RateLimitConfig{Limit: 1}, want: "RateLimit: Window must be greater than zero, got 0s"},
		{name: "negative window", cfg: RateLimitConfig{Limit: 1, Window: -time.Second}, want: "Window must be greater than zero, got -1s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustPanic(t, tt.want, func() { RateLimit(tt.cfg) })
		})
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want int64
	}{
		{0, 1}, {-time.Second, 1}, {time.Millisecond, 1}, {time.Second, 1},
		{time.Second + time.Nanosecond, 2}, {80 * time.Second, 80},
	}
	for _, tt := range tests {
		if got := retryAfterSeconds(tt.in); got != tt.want {
			t.Errorf("retryAfterSeconds(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestNewRateLimiterAllow(t *testing.T) {
	clock := newFakeClock()
	// Key and Message are ignored by the limiter itself.
	l := newRateLimiter(RateLimitConfig{Limit: 2, Window: time.Minute, Key: func(*Context) string { panic("unused") }, Message: "unused"}, clock.Now)
	steps := []struct {
		key           string
		advance       time.Duration
		wantAllowed   bool
		wantRetry     time.Duration
		wantRemaining int
	}{
		{key: "a", wantAllowed: true, wantRemaining: 1},
		{key: "a", wantAllowed: true, wantRemaining: 0},
		// Full: the window must end (60s) and half of the previous
		// window must slide out (30s).
		{key: "a", wantRetry: 90 * time.Second},
		{key: "b", wantAllowed: true, wantRemaining: 1},
		{key: "", wantAllowed: true, wantRemaining: 1},
		{key: "a", advance: 30 * time.Second, wantRetry: 60 * time.Second},
		{key: "a", advance: 60 * time.Second, wantAllowed: true, wantRemaining: 0},
	}
	for i, st := range steps {
		clock.Advance(st.advance)
		allowed, retry, remaining := l.Allow(st.key)
		if allowed != st.wantAllowed || retry != st.wantRetry || remaining != st.wantRemaining {
			t.Errorf("step %d Allow(%q) = %v, %v, %d; want %v, %v, %d", i, st.key, allowed, retry, remaining, st.wantAllowed, st.wantRetry, st.wantRemaining)
		}
	}
}

func TestNewRateLimiterDefaultsAndPanics(t *testing.T) {
	l := NewRateLimiter(RateLimitConfig{Limit: 1, Window: time.Second})
	if l.maxKeys != DefaultRateLimitMaxKeys || DefaultRateLimitMaxKeys != 100000 {
		t.Errorf("maxKeys = %d, want %d (100000)", l.maxKeys, DefaultRateLimitMaxKeys)
	}
	if ok, _, _ := l.Allow("k"); !ok {
		t.Error("first request rejected")
	}
	if ok, retry, rem := l.Allow("k"); ok || retry <= 0 || rem != 0 {
		t.Errorf("second request = %v, %v, %d; want rejected with a positive retry", ok, retry, rem)
	}
	if got := NewRateLimiter(RateLimitConfig{Limit: 1, Window: time.Second, MaxKeys: 7}).maxKeys; got != 7 {
		t.Errorf("explicit MaxKeys = %d, want 7", got)
	}

	tests := []struct {
		name string
		cfg  RateLimitConfig
		want string
	}{
		{name: "zero limit", cfg: RateLimitConfig{Window: time.Minute}, want: "web: RateLimit: Limit must be greater than zero, got 0"},
		{name: "zero window", cfg: RateLimitConfig{Limit: 1}, want: "web: RateLimit: Window must be greater than zero, got 0s"},
		{name: "negative max keys", cfg: RateLimitConfig{Limit: 1, Window: time.Minute, MaxKeys: -1}, want: "web: RateLimit: MaxKeys must not be negative, got -1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustPanic(t, tt.want, func() { NewRateLimiter(tt.cfg) })
			mustPanic(t, tt.want, func() { RateLimit(tt.cfg) })
		})
	}
}

// Regression: rotating keys (for example addresses) once grew the map
// without bound and gave every key a fresh budget. With MaxKeys reached,
// new keys share one overflow bucket.
func TestRateLimiterMaxKeysOverflow(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 3, Window: time.Minute, MaxKeys: 10}, clock.Now)
	for i := 0; i < 10; i++ {
		if ok, _, _ := l.Allow(fmt.Sprintf("tracked-%d", i)); !ok {
			t.Fatalf("tracked key %d rejected", i)
		}
	}
	// 100 rotating keys: only the overflow bucket's Limit gets through.
	allowed := 0
	for i := 0; i < 100; i++ {
		if ok, _, _ := l.Allow(fmt.Sprintf("rotating-%d", i)); ok {
			allowed++
		}
	}
	if allowed != 3 {
		t.Errorf("rotating keys allowed = %d, want 3 (one shared bucket)", allowed)
	}
	if got := l.size(); got != 10 {
		t.Errorf("tracked keys = %d, want 10 (bounded by MaxKeys)", got)
	}
	// Tracked keys keep their own budget.
	if ok, _, rem := l.Allow("tracked-0"); !ok || rem != 1 {
		t.Errorf("tracked key = %v remaining %d, want allowed remaining 1", ok, rem)
	}
	// The overflow bucket refills like any other.
	clock.Advance(2 * time.Minute)
	if ok, _, _ := l.Allow("rotating-new"); !ok {
		t.Error("overflow bucket did not refill (or room did not free up)")
	}
	// The sweep (due after one window) evicted the idle keys, so new keys
	// are tracked individually again.
	if got := l.size(); got != 1 {
		t.Errorf("tracked keys after sweep = %d, want 1", got)
	}
	for i := 0; i < 3; i++ {
		if ok, _, _ := l.Allow("rotating-new"); ok != (i < 2) {
			t.Errorf("rotating-new request %d allowed = %v", i+2, ok)
		}
	}
}

func TestRateLimiterMaxKeysOne(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 1, Window: time.Minute, MaxKeys: 1}, clock.Now)
	if ok, _, _ := l.Allow("a"); !ok {
		t.Fatal("first tracked request rejected")
	}
	if ok, _, _ := l.Allow("b"); !ok {
		t.Error("first overflow request rejected")
	}
	if ok, _, _ := l.Allow("c"); ok {
		t.Error("overflow bucket not shared between b and c")
	}
	if l.size() != 1 {
		t.Errorf("size = %d, want 1", l.size())
	}
}

func TestRateLimiterConcurrentRotatingKeys(t *testing.T) {
	l := NewRateLimiter(RateLimitConfig{Limit: 5, Window: time.Hour, MaxKeys: 50})
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if ok, _, _ := l.Allow(fmt.Sprintf("%d-%d", g, i)); ok {
					allowed.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()
	// 50 tracked keys get one request each, the overflow bucket 5.
	if got := allowed.Load(); got != 55 {
		t.Errorf("allowed = %d, want 55", got)
	}
	if got := l.size(); got != 50 {
		t.Errorf("size = %d, want 50", got)
	}
}

// Regression for the IPv6 bypass: one client rotating addresses inside
// its /64 got a fresh budget per address.
func TestRateLimitIPv6RotationWithinPrefix(t *testing.T) {
	clock := newFakeClock()
	s, _ := limitedServer(RateLimitConfig{Limit: 5, Window: time.Minute}, clock)
	allowed := 0
	for i := 0; i < 30; i++ {
		if hit(s, fmt.Sprintf("[2001:db8:0:1::%x]", i+100)).Code == 200 {
			allowed++
		}
	}
	if allowed != 5 {
		t.Errorf("rotating within one /64: %d/30 allowed, want 5", allowed)
	}
	// Another /64 (even inside the same /48) is an independent client.
	for _, other := range []string{"[2001:db8:0:2::1]", "[2001:db8:1:1::1]", "10.0.0.1"} {
		if code := hit(s, other).Code; code != 200 {
			t.Errorf("%s code = %d, want 200 (independent budget)", other, code)
		}
	}
}

func TestClientKey(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		trust   bool
		header  string // X-Forwarded-For value
		wantKey string
	}{
		{name: "ipv4", remote: "203.0.113.7:1234", wantKey: "203.0.113.7"},
		{name: "ipv4 without port", remote: "203.0.113.7", wantKey: "203.0.113.7"},
		{name: "ipv6 masked to /64", remote: "[2001:db8:1:2:3:4:5:6]:443", wantKey: "2001:db8:1:2::/64"},
		{name: "ipv6 already on prefix boundary", remote: "[2001:db8:1:2::]:443", wantKey: "2001:db8:1:2::/64"},
		{name: "ipv6 loopback", remote: "[::1]:80", wantKey: "::/64"},
		{name: "ipv4-mapped ipv6 is ipv4", remote: "[::ffff:192.0.2.1]:80", wantKey: "192.0.2.1"},
		{name: "ipv6 zone dropped", remote: "[fe80::1%eth0]:80", wantKey: "fe80::/64"},
		{name: "not an ip", remote: "@unix-socket", wantKey: "@unix-socket"},
		{name: "empty remote", remote: "", wantKey: ""},
		{name: "trusted proxy ipv6", remote: "10.0.0.1:1", trust: true, header: "2001:db8:aa:bb:cc::1", wantKey: "2001:db8:aa:bb::/64"},
		{name: "trusted proxy ipv4", remote: "10.0.0.1:1", trust: true, header: "198.51.100.9", wantKey: "198.51.100.9"},
		{name: "untrusted proxy header ignored", remote: "[2001:db8::1]:1", header: "198.51.100.9", wantKey: "2001:db8::/64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			s.TrustProxyHeaders = tt.trust
			var got string
			s.Get("/", func(c *Context) error {
				got = ClientKey(c)
				return nil
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			if tt.header != "" {
				req.Header.Set("X-Forwarded-For", tt.header)
			}
			s.ServeHTTP(httptest.NewRecorder(), req)
			if got != tt.wantKey {
				t.Errorf("ClientKey = %q, want %q", got, tt.wantKey)
			}
		})
	}
}
