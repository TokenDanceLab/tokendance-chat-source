package hub

import (
	"testing"
	"time"
)

func fixedBudgetClock(day int) func() time.Time {
	return func() time.Time {
		return time.Date(2026, 10, day, 12, 0, 0, 0, botBudgetZone)
	}
}

func TestBotBudgetPerUserAndIP(t *testing.T) {
	b := newBotBudget()
	b.now = fixedBudgetClock(20)
	b.limits = botBudgetLimits{perUser: 2, perIP: 3, globalReplies: 100}

	release, reason := b.Begin("alice", "1.1.1.1")
	if release == nil {
		t.Fatalf("first reply denied: %s", reason)
	}
	release(10)
	release, reason = b.Begin("alice", "1.1.1.1")
	if release == nil {
		t.Fatalf("second reply denied: %s", reason)
	}
	release(10)

	if _, reason := b.Begin("alice", "1.1.1.1"); reason != "user" {
		t.Fatalf("alice should hit the per-user cap, got %q", reason)
	}

	// A new name from the same address still counts against the IP cap.
	release, reason = b.Begin("bob", "1.1.1.1")
	if release == nil {
		t.Fatalf("bob's first reply denied: %s", reason)
	}
	release(0)
	if _, reason := b.Begin("carol", "1.1.1.1"); reason != "ip" {
		t.Fatalf("same IP should hit the per-IP cap, got %q", reason)
	}
	if release, reason := b.Begin("carol", "2.2.2.2"); release == nil {
		t.Fatalf("a different IP should still be allowed: %s", reason)
	} else {
		release(0)
	}
}

func TestBotBudgetGlobalRepliesAndTokens(t *testing.T) {
	b := newBotBudget()
	b.now = fixedBudgetClock(20)
	b.limits = botBudgetLimits{globalReplies: 1, globalTokens: 100}

	release, reason := b.Begin("alice", "1.1.1.1")
	if release == nil {
		t.Fatalf("first reply denied: %s", reason)
	}
	if _, reason := b.Begin("bob", "2.2.2.2"); reason != "global" {
		t.Fatalf("global reply cap should deny, got %q", reason)
	}
	release(100)
	if _, reason := b.Begin("bob", "2.2.2.2"); reason != "global" {
		t.Fatalf("global token cap should deny, got %q", reason)
	}
}

func TestBotBudgetGroupsIPv6By64(t *testing.T) {
	b := newBotBudget()
	b.now = fixedBudgetClock(20)
	b.limits = botBudgetLimits{perIP: 1}

	release, reason := b.Begin("alice", "2001:db8:1:2::1")
	if release == nil {
		t.Fatalf("first reply denied: %s", reason)
	}
	release(0)

	if _, reason := b.Begin("bob", "2001:db8:1:2::ffff"); reason != "ip" {
		t.Fatalf("same /64 should share the quota, got %q", reason)
	}
	if release, reason := b.Begin("carol", "2001:db8:1:3::1"); release == nil {
		t.Fatalf("a different /64 should be allowed: %s", reason)
	} else {
		release(0)
	}
}

func TestBotBudgetConcurrency(t *testing.T) {
	b := newBotBudget()
	b.now = fixedBudgetClock(20)
	b.limits = botBudgetLimits{maxConcurrent: 1}

	release, reason := b.Begin("alice", "1.1.1.1")
	if release == nil {
		t.Fatalf("first reply denied: %s", reason)
	}
	if _, reason := b.Begin("bob", "2.2.2.2"); reason != "busy" {
		t.Fatalf("second concurrent reply should be busy, got %q", reason)
	}
	release(0)
	release2, reason := b.Begin("bob", "2.2.2.2")
	if release2 == nil {
		t.Fatalf("slot should be free after release: %s", reason)
	}
	release2(0)
}

func TestBotBudgetResetsAtMidnightHKT(t *testing.T) {
	b := newBotBudget()
	b.now = fixedBudgetClock(20)
	b.limits = botBudgetLimits{perUser: 1}

	release, reason := b.Begin("alice", "1.1.1.1")
	if release == nil {
		t.Fatalf("first reply denied: %s", reason)
	}
	release(0)
	if _, reason := b.Begin("alice", "1.1.1.1"); reason != "user" {
		t.Fatalf("expected per-user cap, got %q", reason)
	}

	b.now = fixedBudgetClock(21)
	if release, reason := b.Begin("alice", "1.1.1.1"); release == nil {
		t.Fatalf("quota should reset on the next HKT day: %s", reason)
	} else {
		release(0)
	}
}

func TestBotBudgetReadsEnv(t *testing.T) {
	t.Setenv("CHAT_BOT_DAILY_PER_USER", "5")
	t.Setenv("CHAT_BOT_DAILY_PER_IP", "9")
	t.Setenv("CHAT_BOT_DAILY_REPLIES", "20")
	t.Setenv("CHAT_BOT_DAILY_TOKENS", "1234")
	t.Setenv("CHAT_BOT_MAX_CONCURRENT", "2")

	limits := newBotBudget().Limits()
	if limits.DailyPerUser != 5 || limits.DailyPerIP != 9 || limits.DailyReplies != 20 ||
		limits.DailyTokens != 1234 || limits.MaxConcurrent != 2 {
		t.Fatalf("limits = %+v", limits)
	}
}
