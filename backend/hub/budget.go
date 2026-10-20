package hub

import (
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Defaults for the public demo. They bound both a single guest and the total
// spend of the instance; 0 disables the corresponding limit.
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

// botBudget caps what a free demo can spend. A guest can create a new name at
// will, so per-user limits alone are not enough: the IP and global caps are the
// real cost boundary.
type botBudget struct {
	limits botBudgetLimits
	now    func() time.Time

	mu            sync.Mutex
	day           string
	userReplies   map[string]int
	ipReplies     map[string]int
	globalReplies int
	globalTokens  int64
	inflight      int
}

func newBotBudget() *botBudget {
	return &botBudget{
		limits: botBudgetLimits{
			perUser:       envInt("CHAT_BOT_DAILY_PER_USER", defaultBotDailyPerUser),
			perIP:         envInt("CHAT_BOT_DAILY_PER_IP", defaultBotDailyPerIP),
			globalReplies: envInt("CHAT_BOT_DAILY_REPLIES", defaultBotDailyReplies),
			globalTokens:  envInt64("CHAT_BOT_DAILY_TOKENS", defaultBotDailyTokens),
			maxConcurrent: envInt("CHAT_BOT_MAX_CONCURRENT", defaultBotMaxConcurrent),
		},
		now:         time.Now,
		userReplies: make(map[string]int),
		ipReplies:   make(map[string]int),
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

// Begin reserves one assistant reply. The returned release function must be
// called exactly once with the tokens the reply consumed (0 when unknown).
// A nil release means the request was denied and reason explains why.
func (b *botBudget) Begin(username, ip string) (func(tokens int64), string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()

	if b.limits.maxConcurrent > 0 && b.inflight >= b.limits.maxConcurrent {
		return nil, "busy"
	}
	if b.limits.globalReplies > 0 && b.globalReplies >= b.limits.globalReplies {
		return nil, "global"
	}
	if b.limits.globalTokens > 0 && b.globalTokens >= b.limits.globalTokens {
		return nil, "global"
	}
	if b.limits.perUser > 0 && b.userReplies[username] >= b.limits.perUser {
		return nil, "user"
	}
	ipKey := ipQuotaKey(ip)
	if b.limits.perIP > 0 && ipKey != "" && b.ipReplies[ipKey] >= b.limits.perIP {
		return nil, "ip"
	}

	b.inflight++
	b.globalReplies++
	b.userReplies[username]++
	if ipKey != "" {
		b.ipReplies[ipKey]++
	}

	return func(tokens int64) {
		b.mu.Lock()
		if b.inflight > 0 {
			b.inflight--
		}
		if tokens > 0 {
			b.globalTokens += tokens
		}
		b.mu.Unlock()
	}, ""
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

// rollLocked starts a new quota day in HKT. In-flight replies keep their slot;
// only the daily counters reset.
func (b *botBudget) rollLocked() {
	day := b.now().In(botBudgetZone).Format(botBudgetDayLayout)
	if day == b.day {
		return
	}
	b.day = day
	b.userReplies = make(map[string]int)
	b.ipReplies = make(map[string]int)
	b.globalReplies = 0
	b.globalTokens = 0
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
