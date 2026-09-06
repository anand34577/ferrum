package api

import (
	"sync"
	"time"
)

// loginLimiter throttles authentication attempts per client IP + username.
// After too many failures inside the window the key is locked out until the
// window elapses, blunting online password guessing without new
// dependencies. State is in-memory: per-instance throttling is the right
// scope for a single-binary deployment.
type loginLimiter struct {
	mu     sync.Mutex
	failed map[string]*failRecord

	maxFailures   int
	window        time.Duration
	sweepInterval time.Duration
	lastSweep     time.Time
}

type failRecord struct {
	failures     int
	firstFailure time.Time
	lockedUntil  time.Time
}

const (
	loginMaxFailures   = 5
	loginWindow        = 15 * time.Minute
	loginSweepInterval = 5 * time.Minute

	// aiChatMaxPerMinute bounds how many /ai/chat completions one user can
	// start per minute — the endpoint proxies to a paid/rate-limited LLM
	// provider, so an unbounded caller (buggy client, or a compromised
	// session/API key) could otherwise run up cost or exhaust the
	// provider's own rate limit for every other user.
	aiChatMaxPerMinute = 20
)

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		failed:        map[string]*failRecord{},
		maxFailures:   loginMaxFailures,
		window:        loginWindow,
		sweepInterval: loginSweepInterval,
		lastSweep:     time.Now(),
	}
}

// Allowed reports whether an attempt for key may proceed. When locked, the
// returned retryAfter tells the client how long to wait.
func (l *loginLimiter) Allowed(key string) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked()

	rec, ok := l.failed[key]
	if !ok {
		return true, 0
	}
	now := time.Now()
	if now.Before(rec.lockedUntil) {
		return false, rec.lockedUntil.Sub(now)
	}
	// Window elapsed since the failures began — start fresh.
	if now.Sub(rec.firstFailure) > l.window {
		delete(l.failed, key)
		return true, 0
	}
	if rec.failures >= l.maxFailures {
		rec.lockedUntil = rec.firstFailure.Add(l.window)
		return false, time.Until(rec.lockedUntil)
	}
	return true, 0
}

// RecordFailure counts a failed attempt, engaging the lockout once
// maxFailures is reached inside the window.
func (l *loginLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked()

	now := time.Now()
	rec, ok := l.failed[key]
	if !ok || now.Sub(rec.firstFailure) > l.window {
		l.failed[key] = &failRecord{failures: 1, firstFailure: now}
		return
	}
	rec.failures++
	if rec.failures >= l.maxFailures {
		rec.lockedUntil = rec.firstFailure.Add(l.window)
	}
}

// RecordSuccess clears the failure history after a successful login.
func (l *loginLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failed, key)
}

// SetPolicy changes the failure threshold and window — applied to attempts
// evaluated from this point on; a key already locked out keeps its existing
// lockedUntil rather than being retroactively reinterpreted.
func (l *loginLimiter) SetPolicy(maxFailures int, window time.Duration) {
	l.mu.Lock()
	l.maxFailures = maxFailures
	l.window = window
	l.mu.Unlock()
}

// sweepLocked prunes stale entries so the map can't grow without bound.
// Caller must hold l.mu.
func (l *loginLimiter) sweepLocked() {
	now := time.Now()
	if now.Sub(l.lastSweep) < l.sweepInterval {
		return
	}
	l.lastSweep = now
	for key, rec := range l.failed {
		if now.Sub(rec.firstFailure) > l.window && now.After(rec.lockedUntil) {
			delete(l.failed, key)
		}
	}
}

// requestLimiter caps how many requests one key (typically a user ID) can
// make inside a fixed window — a hard ceiling, not smooth throttling, which
// is all endpoints proxying to a paid/rate-limited upstream (the AI chat's
// LLM provider) need: it bounds the cost/DoS blast radius of one runaway
// caller without needing a token-bucket's extra bookkeeping.
type requestLimiter struct {
	mu            sync.Mutex
	counts        map[string]*windowCount
	max           int
	window        time.Duration
	sweepInterval time.Duration
	lastSweep     time.Time
}

type windowCount struct {
	count      int
	windowFrom time.Time
}

func newRequestLimiter(max int, window time.Duration) *requestLimiter {
	return &requestLimiter{counts: map[string]*windowCount{}, max: max, window: window, sweepInterval: window, lastSweep: time.Now()}
}

// Allow reports whether another request for key may proceed inside the
// current window, incrementing its count if so.
func (l *requestLimiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Sub(l.lastSweep) >= l.sweepInterval {
		l.lastSweep = now
		for k, c := range l.counts {
			if now.Sub(c.windowFrom) > l.window {
				delete(l.counts, k)
			}
		}
	}

	c, ok := l.counts[key]
	if !ok || now.Sub(c.windowFrom) > l.window {
		l.counts[key] = &windowCount{count: 1, windowFrom: now}
		return true, 0
	}
	if c.count >= l.max {
		return false, l.window - now.Sub(c.windowFrom)
	}
	c.count++
	return true, 0
}
