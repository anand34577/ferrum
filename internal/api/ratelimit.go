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
