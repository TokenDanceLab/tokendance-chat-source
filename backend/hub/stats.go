package hub

import (
	"sync/atomic"
)

// cacheMetrics aggregates provider prompt-cache usage for operators. Exact
// numbers come from the LLM adapter (prompt_tokens_details.cached_tokens or
// DeepSeek prompt_cache_hit_tokens), never from local estimation.
type cacheMetrics struct {
	runs         atomic.Int64
	promptTokens atomic.Int64
	cachedTokens atomic.Int64
	compactions  atomic.Int64
}

func (m *cacheMetrics) record(promptTokens, cachedTokens int64) {
	m.runs.Add(1)
	m.promptTokens.Add(promptTokens)
	m.cachedTokens.Add(cachedTokens)
}

func (m *cacheMetrics) addCompaction() { m.compactions.Add(1) }

// CacheStats is the public snapshot returned by GET /api/metrics.
type CacheStats struct {
	Runs         int64   `json:"runs"`
	PromptTokens int64   `json:"prompt_tokens"`
	CachedTokens int64   `json:"cached_tokens"`
	HitRate      float64 `json:"hit_rate"`
	Compactions  int64   `json:"context_compactions"`
}

func (m *cacheMetrics) snapshot() CacheStats {
	prompt := m.promptTokens.Load()
	cached := m.cachedTokens.Load()
	rate := 0.0
	if prompt > 0 {
		rate = float64(cached) / float64(prompt)
	}
	return CacheStats{
		Runs:         m.runs.Load(),
		PromptTokens: prompt,
		CachedTokens: cached,
		HitRate:      rate,
		Compactions:  m.compactions.Load(),
	}
}

// CacheStats returns a copy of the live counters.
func (h *Hub) CacheStats() CacheStats { return h.metrics.snapshot() }

// ContextPolicyView exposes the effective policy for the metrics endpoint.
type ContextPolicyView struct {
	MaxTokens         int64   `json:"max_tokens"`
	CompressAt        float64 `json:"compress_at"`
	MaxMessages       int     `json:"max_messages"`
	SummaryMaxTokens  int64   `json:"summary_max_tokens"`
	RecentMinMessages int     `json:"recent_min_messages"`
	ReserveTokens     int64   `json:"reserve_tokens"`
}

// ContextPolicy returns the effective token-budget policy.
func (h *Hub) ContextPolicy() ContextPolicyView {
	p := h.contextPolicy()
	return ContextPolicyView{
		MaxTokens:         p.MaxTokens,
		CompressAt:        p.CompressAt,
		MaxMessages:       p.MaxMessages,
		SummaryMaxTokens:  p.SummaryMaxTokens,
		RecentMinMessages: p.RecentMinMessages,
		ReserveTokens:     p.ReserveTokens,
	}
}

// SetContextPolicy replaces the policy. Values are normalised so an invalid
// environment value degrades to the default rather than silently disabling
// compaction.
func (h *Hub) SetContextPolicy(policy ContextPolicy) {
	h.contextMu.Lock()
	h.policy = policy.normalized()
	h.contextDigests = make(map[string]contextDigest)
	h.contextMu.Unlock()
}

func (h *Hub) contextPolicy() ContextPolicy {
	h.contextMu.Lock()
	defer h.contextMu.Unlock()
	if h.policy.MaxTokens <= 0 {
		h.policy = DefaultContextPolicy()
	}
	return h.policy
}
