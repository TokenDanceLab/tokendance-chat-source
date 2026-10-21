package handler

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"tokendancechat/backend/security"
)

// ValidateTurnstileConfig rejects a start that asks for Turnstile but has no
// secret. Without this check a missing secret would silently disable the only
// server-side bot verification on the login path.
func ValidateTurnstileConfig() error {
	if !turnstileRequired() {
		return nil
	}
	if strings.TrimSpace(os.Getenv("CHAT_TURNSTILE_SECRET")) == "" {
		return errors.New("CHAT_TURNSTILE_REQUIRED is true but CHAT_TURNSTILE_SECRET is empty")
	}
	return nil
}

// loadTurnstileVerifier builds a verifier from CHAT_TURNSTILE_* environment
// variables. An empty CHAT_TURNSTILE_SECRET disables verification unless
// CHAT_TURNSTILE_REQUIRED is set, in which case startup fails first.
func loadTurnstileVerifier() *security.Turnstile {
	secret := strings.TrimSpace(os.Getenv("CHAT_TURNSTILE_SECRET"))
	var hostnames []string
	if raw := strings.TrimSpace(os.Getenv("CHAT_TURNSTILE_HOSTNAMES")); raw != "" {
		hostnames = strings.Split(raw, ",")
	}
	action := strings.TrimSpace(os.Getenv("CHAT_TURNSTILE_ACTION"))
	return security.NewTurnstile(secret, hostnames, action)
}

func turnstileRequired() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("CHAT_TURNSTILE_REQUIRED")))
	return value == "1" || value == "true" || value == "yes"
}

// verifyTurnstile validates a token against the configured verifier. It is a
// no-op when Turnstile is disabled (no secret configured).
func (h *Handler) verifyTurnstile(r *http.Request, token string) error {
	if h.turnstile == nil {
		return nil
	}
	return h.turnstile.Verify(r.Context(), token, requestIP(r))
}
