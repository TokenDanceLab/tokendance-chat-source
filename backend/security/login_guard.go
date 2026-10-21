package security

import (
	"strings"
	"sync"
	"time"
)

// LoginGuard slows repeated failures per account and per source address. It is
// a delay, not a hard permanent lock: the cap keeps a hostile client from
// locking a known account forever, while Turnstile and the per-IP request
// limiter make each attempt expensive.
type LoginGuard struct {
	mu         sync.Mutex
	entries    map[string]*attempts
	maxEntries int
	now        func() time.Time
	lastPrune  time.Time
}

type attempts struct {
	failures    int
	lockedUntil time.Time
	last        time.Time
}

const (
	loginFailureWindow = 15 * time.Minute
	loginEntryTTL      = 30 * time.Minute
	loginMaxEntries    = 20000
	loginPruneInterval = 30 * time.Second
)

// NewLoginGuard returns a guard with production defaults.
func NewLoginGuard() *LoginGuard {
	return &LoginGuard{entries: make(map[string]*attempts), maxEntries: loginMaxEntries, now: time.Now}
}

// lockFor maps a failure count to a delay. The fifth consecutive failure
// starts a 30s pause; the cap stays at five minutes.
func lockFor(failures int) time.Duration {
	switch {
	case failures < 5:
		return 0
	case failures < 8:
		return 30 * time.Second
	case failures < 12:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// Allow reports whether the account and address may attempt a login now, and
// how long to wait when they may not.
func (g *LoginGuard) Allow(username, ip string) (time.Duration, bool) {
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pruneLocked(now)

	var wait time.Duration
	for _, key := range loginKeys(username, ip) {
		if entry, ok := g.entries[key]; ok && entry.lockedUntil.After(now) {
			if remaining := entry.lockedUntil.Sub(now); remaining > wait {
				wait = remaining
			}
		}
	}
	return wait, wait == 0
}

// Fail records one failed credential check.
func (g *LoginGuard) Fail(username, ip string) {
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pruneLocked(now)
	for _, key := range loginKeys(username, ip) {
		entry, ok := g.entries[key]
		if !ok {
			if len(g.entries) >= g.maxEntries {
				g.evictOneLocked()
			}
			entry = &attempts{}
			g.entries[key] = entry
		}
		if now.Sub(entry.last) > loginFailureWindow {
			entry.failures = 0
		}
		entry.failures++
		entry.last = now
		if delay := lockFor(entry.failures); delay > 0 {
			entry.lockedUntil = now.Add(delay)
		}
	}
}

// Succeed clears the account and address failures after a correct password,
// so a user who mistyped repeatedly is not left waiting.
func (g *LoginGuard) Succeed(username, ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, key := range loginKeys(username, ip) {
		delete(g.entries, key)
	}
}

func loginKeys(username, ip string) []string {
	keys := make([]string, 0, 2)
	if username = strings.ToLower(strings.TrimSpace(username)); username != "" {
		keys = append(keys, "u:"+username)
	}
	if ip != "" {
		keys = append(keys, "i:"+ip)
	}
	return keys
}

func (g *LoginGuard) pruneLocked(now time.Time) {
	if now.Sub(g.lastPrune) < loginPruneInterval {
		return
	}
	g.lastPrune = now
	for key, entry := range g.entries {
		if now.Sub(entry.last) > loginEntryTTL && !entry.lockedUntil.After(now) {
			delete(g.entries, key)
		}
	}
}

func (g *LoginGuard) evictOneLocked() {
	for key := range g.entries {
		delete(g.entries, key)
		return
	}
}
