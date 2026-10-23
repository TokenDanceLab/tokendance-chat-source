package hub

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Defaults for the public demo. They bound both a single guest and the total
// spend of the instance; 0 disables daily caps. Nonpositive concurrency uses
// defaultBotMaxConcurrent so model execution always remains bounded.
const (
	defaultBotDailyPerUser  = 50
	defaultBotDailyPerIP    = 200
	defaultBotDailyReplies  = 800
	defaultBotDailyTokens   = int64(3_000_000)
	defaultBotMaxConcurrent = 4
	defaultMsgPerIPPer10s   = 80
	botBudgetDayLayout      = "2006-01-02"
)

var botBudgetZone = time.FixedZone("HKT", 8*60*60)

// BotLimits is the public, non-secret view of the assistant budget.
type BotLimits struct {
	DailyPerUser  int   `json:"daily_per_user"`
	DailyPerIP    int   `json:"daily_per_ip"`
	DailyReplies  int   `json:"daily_replies"`
	DailyTokens   int64 `json:"daily_tokens"`
	MaxConcurrent int   `json:"max_concurrent"`
}

type botBudgetLimits struct {
	perUser       int
	perIP         int
	globalReplies int
	globalTokens  int64
	maxConcurrent int
}

// botBudget contains admission configuration. The durable ledger lives in
// store.bot_runs; run scheduling owns concurrency separately.
type botBudget struct{ limits botBudgetLimits }

func newBotBudget() *botBudget {
	return &botBudget{
		limits: botBudgetLimits{
			perUser:       envInt("CHAT_BOT_DAILY_PER_USER", defaultBotDailyPerUser),
			perIP:         envInt("CHAT_BOT_DAILY_PER_IP", defaultBotDailyPerIP),
			globalReplies: envInt("CHAT_BOT_DAILY_REPLIES", defaultBotDailyReplies),
			globalTokens:  envInt64("CHAT_BOT_DAILY_TOKENS", defaultBotDailyTokens),
			maxConcurrent: envInt("CHAT_BOT_MAX_CONCURRENT", defaultBotMaxConcurrent),
		},
	}
}

func (b *botBudget) Limits() BotLimits {
	return BotLimits{
		DailyPerUser:  b.limits.perUser,
		DailyPerIP:    b.limits.perIP,
		DailyReplies:  b.limits.globalReplies,
		DailyTokens:   b.limits.globalTokens,
		MaxConcurrent: b.limits.maxConcurrent,
	}
}

// ipQuotaKey groups IPv6 clients by /64. Rotating the host bits inside one
// /64 must not mint a fresh per-IP budget.
func ipQuotaKey(raw string) string {
	raw = strings.TrimSpace(raw)
	ip := net.ParseIP(raw)
	if ip == nil {
		return raw
	}
	if ip.To4() != nil {
		return ip.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func envInt64(name string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
