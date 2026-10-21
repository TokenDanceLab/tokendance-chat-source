package handler

import (
	"encoding/json"
	"net/http"

	"tokendancechat/backend/hub"
)

// ConfigHandler returns the public, non-sensitive runtime config the frontend
// needs to render the active model and bot. It deliberately omits API keys,
// base URLs, and any other credential material.
func (h *Handler) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	botName := ""
	model := ""
	llmEnabled := false
	if h.hub != nil {
		botName = h.hub.BotName()
		model = h.hub.LLMModel()
		llmEnabled = h.hub.LLMEnabled()
	}

	resp := map[string]any{
		"bot_name":           botName,
		"model":              model,
		"llm_enabled":        llmEnabled,
		"capabilities":       map[string]bool{"run_control": h.hub != nil && h.hub.RunControlEnabled(), "web_search": h.hub != nil && h.hub.WebSearchEnabled()},
		"oidc_enabled":       h.oidcEnabled,
		"local_auth_enabled": !h.oidcEnabled,
		"max_message_length": hub.MaxContentLength,
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
