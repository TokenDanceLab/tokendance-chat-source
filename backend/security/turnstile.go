package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	turnstileSiteVerifyURL  = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	turnstileMaxTokenLength = 2048
)

// ErrTurnstileExpired means the token was already used or is older than five
// minutes. The widget must issue a fresh challenge.
var ErrTurnstileExpired = errors.New("turnstile: token expired or already used")

// ErrTurnstileUnavailable means the verification service could not be reached
// or answered with an unusable response. Callers should fail closed.
var ErrTurnstileUnavailable = errors.New("turnstile: verification unavailable")

// Turnstile validates Cloudflare Turnstile tokens server-side.
type Turnstile struct {
	secret    string
	hostnames map[string]struct{}
	action    string
	client    *http.Client
	endpoint  string
}

// NewTurnstile builds a verifier. An empty secret disables verification so
// local development keeps working.
func NewTurnstile(secret string, hostnames []string, action string) *Turnstile {
	set := make(map[string]struct{}, len(hostnames))
	for _, hostname := range hostnames {
		if hostname = strings.TrimSpace(hostname); hostname != "" {
			set[hostname] = struct{}{}
		}
	}
	if action == "" {
		action = "login"
	}
	return &Turnstile{
		secret:    secret,
		hostnames: set,
		action:    action,
		client:    &http.Client{Timeout: 8 * time.Second},
		endpoint:  turnstileSiteVerifyURL,
	}
}

// Enabled reports whether a secret is configured.
func (t *Turnstile) Enabled() bool {
	return t != nil && t.secret != ""
}

type verifyResponse struct {
	Success    bool     `json:"success"`
	Action     string   `json:"action"`
	Hostname   string   `json:"hostname"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify checks one token. Tokens are single-use and valid for 300 seconds, so
// every verification carries its own idempotency key for safe retries.
func (t *Turnstile) Verify(ctx context.Context, token, remoteIP string) error {
	if !t.Enabled() {
		return nil
	}
	if token == "" || len(token) > turnstileMaxTokenLength {
		return errors.New("turnstile: missing or invalid token")
	}

	form := url.Values{}
	form.Set("secret", t.secret)
	form.Set("response", token)
	form.Set("idempotency_key", uuid.NewString())
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrTurnstileUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: siteverify request: %v", ErrTurnstileUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return fmt.Errorf("%w: read response: %v", ErrTurnstileUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: siteverify status %d", ErrTurnstileUnavailable, resp.StatusCode)
	}

	var parsed verifyResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrTurnstileUnavailable, err)
	}
	if !parsed.Success {
		for _, code := range parsed.ErrorCodes {
			if code == "timeout-or-duplicate" {
				return ErrTurnstileExpired
			}
		}
		return fmt.Errorf("turnstile: verification failed: %s", strings.Join(parsed.ErrorCodes, ", "))
	}
	if parsed.Action != t.action {
		return fmt.Errorf("turnstile: unexpected action %q", parsed.Action)
	}
	if len(t.hostnames) > 0 {
		if _, ok := t.hostnames[parsed.Hostname]; !ok {
			return fmt.Errorf("turnstile: unexpected hostname %q", parsed.Hostname)
		}
	}
	return nil
}
