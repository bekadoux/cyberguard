package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterWindowsAndCapacity(t *testing.T) {
	t.Parallel()
	now := time.Now()
	limiter := NewLoginLimiter(2, time.Minute, 2)
	limiter.now = func() time.Time { return now }
	limiter.lastSweep = now
	for range 2 {
		if allowed, _ := limiter.Allow("peer-a"); !allowed {
			t.Fatal("early login attempt denied")
		}
	}
	if allowed, retry := limiter.Allow("peer-a"); allowed || retry != time.Minute {
		t.Fatal("limit did not return a retry duration")
	}
	if allowed, _ := limiter.Allow("peer-b"); !allowed {
		t.Fatal("peers do not have independent limits")
	}
	if allowed, _ := limiter.Allow("peer-c"); allowed || len(limiter.buckets) != 2 {
		t.Fatal("limiter state is unbounded")
	}
	now = now.Add(time.Minute)
	if allowed, _ := limiter.Allow("peer-c"); !allowed || len(limiter.buckets) != 1 {
		t.Fatal("expired limiter entries were not cleaned")
	}
}

func TestLimiterConcurrentRequests(t *testing.T) {
	t.Parallel()
	limiter := NewLoginLimiter(5, time.Minute, 10)
	var allowed atomic.Int32
	var group sync.WaitGroup
	for range 100 {
		group.Go(func() {
			if ok, _ := limiter.Allow("one-peer"); ok {
				allowed.Add(1)
			}
		})
	}
	group.Wait()
	if allowed.Load() != 5 {
		t.Fatal("concurrent requests bypassed the login limit")
	}
}

func TestLimiterRejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	for _, create := range []func(){
		func() { NewLoginLimiter(0, time.Minute, 1) },
		func() { NewLoginLimiter(1, 0, 1) },
		func() { NewLoginLimiter(1, time.Minute, 0) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid limits were accepted")
				}
			}()
			create()
		}()
	}
}
