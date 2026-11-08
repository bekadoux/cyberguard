package auth

import (
	"sync"
	"time"
)

type loginBucket struct {
	attempts int
	reset    time.Time
}

// LoginLimiter is intentionally process-local. It stores bounded per-peer state,
// not account lockouts, and never needs a background goroutine or a broker.
type LoginLimiter struct {
	mu        sync.Mutex
	buckets   map[string]loginBucket
	limit     int
	window    time.Duration
	capacity  int
	lastSweep time.Time
	now       func() time.Time
}

func NewLoginLimiter(limit int, window time.Duration, capacity int) *LoginLimiter {
	if limit < 1 || window <= 0 || capacity < 1 {
		panic("login limiter requires positive limit, window and capacity")
	}
	return &LoginLimiter{
		buckets: make(map[string]loginBucket), limit: limit, window: window,
		capacity: capacity, lastSweep: time.Now(), now: time.Now,
	}
}

func (l *LoginLimiter) Allow(peer string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= l.window {
		for peer, bucket := range l.buckets {
			if !now.Before(bucket.reset) {
				delete(l.buckets, peer)
			}
		}
		l.lastSweep = now
	}
	bucket, exists := l.buckets[peer]
	if !exists || !now.Before(bucket.reset) {
		if !exists && len(l.buckets) >= l.capacity {
			return false, l.window
		}
		bucket = loginBucket{reset: now.Add(l.window)}
	}
	if bucket.attempts >= l.limit {
		return false, bucket.reset.Sub(now)
	}
	bucket.attempts++
	l.buckets[peer] = bucket
	return true, 0
}
