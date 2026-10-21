package security

import (
	"testing"
	"time"
)

func TestWindowLimiterBudgetAndRecovery(t *testing.T) {
	limiter := NewWindowLimiter()
	now := &testClock{at: 1_700_000_000}
	limiter.now = now.Now
	window := time.Minute

	for i := 0; i < 3; i++ {
		if !limiter.Allow("auth", "1.2.3.4", 3, window) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if limiter.Allow("auth", "1.2.3.4", 3, window) {
		t.Fatal("fourth request should be denied")
	}

	now.Advance(window)
	if !limiter.Allow("auth", "1.2.3.4", 3, window) {
		t.Fatal("request after the window should be allowed")
	}
}

func TestWindowLimiterKeepsBucketsIndependent(t *testing.T) {
	limiter := NewWindowLimiter()
	now := &testClock{at: 1_700_000_000}
	limiter.now = now.Now
	window := time.Minute

	if !limiter.Allow("ws", "1.2.3.4", 1, window) {
		t.Fatal("first WS request should pass")
	}
	if limiter.Allow("ws", "1.2.3.4", 1, window) {
		t.Fatal("second WS request should be denied")
	}
	if !limiter.Allow("api", "1.2.3.4", 1, window) {
		t.Fatal("API bucket must not consume the WS budget")
	}
	if !limiter.Allow("api", "5.6.7.8", 1, window) {
		t.Fatal("a different key must have its own budget")
	}
}
