package hub

import (
	"fmt"
	"strings"
	"testing"
)

func testContextPolicy() ContextPolicy {
	return ContextPolicy{
		MaxTokens:         4096,
		CompressAt:        0.1,
		MaxMessages:       400,
		SummaryMaxTokens:  256,
		RecentMinMessages: 2,
		ReserveTokens:     1024,
	}
}

func storedTranscript(n int) []StoredMessage {
	out := make([]StoredMessage, 0, n)
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("message %03d %s", i, strings.Repeat("x", 90))
		out = append(out, StoredMessage{
			ID:       fmt.Sprintf("msg-%03d", i),
			Username: "alice",
			Content:  content,
		})
	}
	return out
}

func TestEstimateTokensSeparatesWideAndNarrowText(t *testing.T) {
	wide := estimateTokens("你好世界")
	narrow := estimateTokens("hello")
	if wide < 4 || narrow > 2 {
		t.Fatalf("estimate wide=%d narrow=%d", wide, narrow)
	}
}

func TestCompactHistoryFoldsOldTurnsAndFreezesDigest(t *testing.T) {
	h := New(&mockStore{}, nil, "TokenBot")
	h.SetContextPolicy(testContextPolicy())
	stored := storedTranscript(200)

	recent, digest := h.compactHistory("dm:alice:", stored)
	if digest == "" {
		t.Fatal("expected a digest for an oversized transcript")
	}
	if len(recent) >= len(stored) {
		t.Fatalf("recent window = %d, want fewer than %d", len(recent), len(stored))
	}
	if !strings.Contains(digest, "message 000") {
		t.Fatalf("digest lost the earliest message: %q", digest[:200])
	}
	if !strings.Contains(digest, "message") || !strings.Contains(digest, "omitted") {
		t.Fatalf("digest shape = %q", digest[:200])
	}
	if estimateTokens(digest) > 256 {
		t.Fatalf("digest over budget: %d", estimateTokens(digest))
	}
	if recent[len(recent)-1].ID != "msg-199" {
		t.Fatalf("newest message not kept verbatim: %s", recent[len(recent)-1].ID)
	}

	// Appending one message must reuse the frozen digest instead of rebuilding
	// the prefix (that would cost a provider cache miss every turn).
	compactions := h.CacheStats().Compactions
	stored = append(stored, StoredMessage{ID: "msg-200", Username: "alice", Content: "one more"})
	recent2, digest2 := h.compactHistory("dm:alice:", stored)
	if digest2 != digest {
		t.Fatal("digest changed after a single append")
	}
	if got := h.CacheStats().Compactions; got != compactions {
		t.Fatalf("compactions changed on append: %d -> %d", compactions, got)
	}
	if recent2[len(recent2)-1].ID != "msg-200" {
		t.Fatalf("new message missing: %s", recent2[len(recent2)-1].ID)
	}
}

func TestCompactHistoryLeavesSmallTranscriptsAlone(t *testing.T) {
	h := New(&mockStore{}, nil, "TokenBot")
	h.SetContextPolicy(testContextPolicy())
	stored := storedTranscript(10)
	recent, digest := h.compactHistory("dm:alice:", stored)
	if digest != "" || len(recent) != len(stored) {
		t.Fatalf("small transcript was compacted: %d/%d digest=%q", len(recent), len(stored), digest)
	}
}

func TestCacheMetricsRecordUsage(t *testing.T) {
	h := New(&mockStore{}, nil, "TokenBot")
	h.metrics.record(1000, 800)
	h.metrics.record(500, 100)
	stats := h.CacheStats()
	if stats.Runs != 2 || stats.PromptTokens != 1500 || stats.CachedTokens != 900 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.HitRate < 0.59 || stats.HitRate > 0.61 {
		t.Fatalf("hit rate = %f", stats.HitRate)
	}
}
