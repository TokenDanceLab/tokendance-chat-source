package hub

import "testing"

func TestIPQuotaKeyGroupsIPv6By64(t *testing.T) {
	if ipQuotaKey("2001:db8:1:2::1") != ipQuotaKey("2001:db8:1:2::ffff") {
		t.Fatal("same /64 bypassed IP quota")
	}
	if ipQuotaKey("2001:db8:1:2::1") == ipQuotaKey("2001:db8:1:3::1") {
		t.Fatal("different /64 shares quota")
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
