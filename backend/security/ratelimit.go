package security

import (
	"sync"
	"time"
)

// WindowLimiter counts events per key inside a fixed window. Each named bucket
// keeps its own counters, so WebSocket upgrades, REST calls, and auth attempts
// never consume each other's budget.
type WindowLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	entries   map[string]*windowEntry
	lastPrune time.Time
}

type windowEntry struct {
	mu         sync.Mutex
	timestamps []time.Time
}

const pruneInterval = 30 * time.Second

// NewWindowLimiter returns an empty limiter.
func NewWindowLimiter() *WindowLimiter {
	return &WindowLimiter{buckets: make(map[string]*bucket), now: time.Now}
}

// Allow records one event and reports whether the key is still inside its
// budget for the window.
func (l *WindowLimiter) Allow(name, key string, max int, window time.Duration) bool {
	if max <= 0 || window <= 0 {
		return true
	}
	now := l.now()

	l.mu.Lock()
	b, ok := l.buckets[name]
	if !ok {
		b = &bucket{entries: make(map[string]*windowEntry)}
		l.buckets[name] = b
	}
	if now.Sub(b.lastPrune) >= pruneInterval {
		b.lastPrune = now
		pruneBucket(b, window, now)
	}
	entry, ok := b.entries[key]
	if !ok {
		entry = &windowEntry{}
		b.entries[key] = entry
	}
	l.mu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()
	cutoff := now.Add(-window)
	kept := entry.timestamps[:0]
	for _, ts := range entry.timestamps {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	entry.timestamps = kept
	if len(entry.timestamps) >= max {
		return false
	}
	entry.timestamps = append(entry.timestamps, now)
	return true
}

func pruneBucket(b *bucket, window time.Duration, now time.Time) {
	cutoff := now.Add(-window)
	for key, entry := range b.entries {
		entry.mu.Lock()
		kept := entry.timestamps[:0]
		for _, ts := range entry.timestamps {
			if ts.After(cutoff) {
				kept = append(kept, ts)
			}
		}
		entry.timestamps = kept
		empty := len(entry.timestamps) == 0
		entry.mu.Unlock()
		if empty {
			delete(b.entries, key)
		}
	}
}
