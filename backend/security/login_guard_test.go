package security

import (
	"testing"
	"time"
)

type testClock struct {
	at int64
}

func (c *testClock) Now() time.Time { return time.Unix(c.at, 0) }

func (c *testClock) Advance(d time.Duration) { c.at += int64(d.Seconds()) }

func TestLoginGuardDelaysAfterRepeatedFailures(t *testing.T) {
	guard := NewLoginGuard()
	now := &testClock{at: 1_700_000_000}
	guard.now = now.Now

	for i := 0; i < 4; i++ {
		guard.Fail("Alice", "203.0.113.5")
		if wait, ok := guard.Allow("alice", "203.0.113.5"); !ok {
			t.Fatalf("failure %d should not lock yet (wait=%v)", i+1, wait)
		}
	}
	guard.Fail("alice", "203.0.113.5")
	wait, ok := guard.Allow("ALICE", "203.0.113.5")
	if ok {
		t.Fatal("the fifth failure should start a delay")
	}
	if wait < 30*time.Second || wait > 31*time.Second {
		t.Fatalf("wait = %v, want about 30s", wait)
	}

	now.Advance(31 * time.Second)
	if _, ok := guard.Allow("alice", "203.0.113.5"); !ok {
		t.Fatal("the delay should expire")
	}
}

func TestLoginGuardSuccessClearsAccount(t *testing.T) {
	guard := NewLoginGuard()
	now := &testClock{at: 1_700_000_000}
	guard.now = now.Now

	for i := 0; i < 6; i++ {
		guard.Fail("alice", "203.0.113.5")
	}
	if _, ok := guard.Allow("alice", "203.0.113.5"); ok {
		t.Fatal("expected a lock before the successful login")
	}
	guard.Succeed("alice", "203.0.113.5")
	if _, ok := guard.Allow("alice", "203.0.113.5"); !ok {
		t.Fatal("a successful login must clear the account failures")
	}
}

func TestLoginGuardTracksAddressSeparately(t *testing.T) {
	guard := NewLoginGuard()
	now := &testClock{at: 1_700_000_000}
	guard.now = now.Now

	for i := 0; i < 5; i++ {
		guard.Fail("alice", "203.0.113.5")
	}
	if _, ok := guard.Allow("bob", "203.0.113.5"); ok {
		t.Fatal("the address should be delayed for every account")
	}
	if _, ok := guard.Allow("alice", "203.0.113.99"); ok {
		t.Fatal("the account should stay delayed from another address")
	}
	if _, ok := guard.Allow("bob", "203.0.113.99"); !ok {
		t.Fatal("an unrelated account and address should pass")
	}
}
