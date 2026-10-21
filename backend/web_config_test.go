package main

import (
	"strings"
	"testing"
)

func TestWebSearchExplicitConfigRetainsModel(t *testing.T) {
	t.Setenv("CHAT_LLM_PROVIDER", "openai")
	t.Setenv("CHAT_LLM_MODEL", "deepseek-v4.1-flash")
	t.Setenv("CHAT_LLM_BASE_URL", "https://gateway.example.com/v1")
	t.Setenv("CHAT_LLM_API_KEY", "model-key-never-reused-implicitly")
	t.Setenv("CHAT_WEB_SEARCH_ENABLED", "false")
	cfg, err := loadLLMConfig()
	if err != nil || cfg.WebSearch != nil || cfg.Model != "deepseek-v4.1-flash" {
		t.Fatalf("disabled config %v", err)
	}
	t.Setenv("CHAT_WEB_SEARCH_ENABLED", "true")
	t.Setenv("CHAT_WEB_SEARCH_API_KEY", "")
	if _, err = loadLLMConfig(); err == nil || !strings.Contains(err.Error(), "CHAT_WEB_SEARCH_API_KEY") {
		t.Fatal("model key silently forwarded to tool provider")
	}
	t.Setenv("CHAT_WEB_SEARCH_API_KEY", "tool-key")
	t.Setenv("CHAT_WEB_SEARCH_BASE_URL", "https://gateway.example.com/v1")
	cfg, err = loadLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "deepseek-v4.1-flash" || cfg.WebSearch.APIKey != "tool-key" || cfg.WebSearch.DailyMicroCNY != 5000000 {
		t.Fatal("model changed or budget missing")
	}
}

func TestWebSearchConfigRejectsUnsafeEndpointAndUnboundedMoney(t *testing.T) {
	t.Setenv("CHAT_WEB_SEARCH_ENABLED", "true")
	t.Setenv("CHAT_WEB_SEARCH_API_KEY", "test")
	for _, base := range []string{"http://example.com/v1", "https://user:pass@example.com/v1", "https://example.com/v1?secret=query", "https://example.com/v1#fragment", "https://example.com"} { // # leak-guard-allow:LG-F4C6A155 synthetic credentials rejected by this test
		t.Setenv("CHAT_WEB_SEARCH_BASE_URL", base)
		if _, err := loadWebSearchConfig(); err == nil {
			t.Errorf("unsafe base accepted %s", base)
		}
	}
	t.Setenv("CHAT_WEB_SEARCH_BASE_URL", "https://api.deepseek.com/v1")
	for _, cap := range []string{"0", "-1", "4999", "1000000001", "NaN"} {
		t.Setenv("CHAT_WEB_SEARCH_DAILY_MICRO_CNY", cap)
		if _, err := loadWebSearchConfig(); err == nil {
			t.Errorf("unbounded daily cap %s", cap)
		}
	}
}
