package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitMiddlewareBlocksAfterThirtyAPICalls(t *testing.T) {
	ResetRateLimiter()
	wrapped := RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 30; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.RemoteAddr = "198.51.100.99:12345"
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = "198.51.100.99:12345"
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on request 31, got %d", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
}

func TestRateLimitMiddlewareSkipsStaticAssets(t *testing.T) {
	ResetRateLimiter()
	wrapped := RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 80; i++ {
		req := httptest.NewRequest(http.MethodGet, "/assets/index.js", nil)
		req.RemoteAddr = "198.51.100.100:12345"
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("static request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimitMiddlewareUsesBackchannelBudget(t *testing.T) {
	ResetRateLimiter()
	t.Setenv("CHAT_API_RATE_LIMIT_PER_MINUTE", "1")
	wrapped := RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// /api/session has its own bootstrap budget; use another normal API here.
	normalRequest := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
		req.RemoteAddr = "203.0.113.77:4000"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := normalRequest(); got != http.StatusNoContent {
		t.Fatalf("first normal request status = %d", got)
	}
	if got := normalRequest(); got != http.StatusTooManyRequests {
		t.Fatalf("second normal request status = %d, want 429", got)
	}

	backchannelRequest := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/oidc/backchannel-logout", nil)
		req.RemoteAddr = "203.0.113.77:4000"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < backchannelMaxPerWindow; i++ {
		if got := backchannelRequest(); got != http.StatusNoContent {
			t.Fatalf("backchannel request %d status = %d", i+1, got)
		}
	}
	if got := backchannelRequest(); got != http.StatusTooManyRequests {
		t.Fatalf("backchannel over-budget status = %d, want 429", got)
	}
}

func TestBodyLimitMiddlewareCapsJSON(t *testing.T) {
	var readErr error
	wrapped := BodyLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("x"), maxJSONBodySize+1024)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if readErr == nil {
		t.Fatal("expected the body read to hit the limit")
	}
}

func TestBodyLimitMiddlewareAllowsImageUploadWithinProductLimit(t *testing.T) {
	var readErr error
	wrapped := BodyLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("x"), maxJSONBodySize+1024)
	req := httptest.NewRequest(http.MethodPost, "/api/upload/image", bytes.NewReader(body))
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if readErr != nil {
		t.Fatalf("image upload body below the image limit was rejected: %v", readErr)
	}
}

func TestBodyLimitMiddlewareCapsImageUploadEnvelope(t *testing.T) {
	var readErr error
	wrapped := BodyLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("x"), maxImageUploadSize+maxImageMultipartOverhead+1)
	req := httptest.NewRequest(http.MethodPost, "/api/upload/image", bytes.NewReader(body))
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if readErr == nil {
		t.Fatal("expected the image upload envelope to hit the limit")
	}
}

func TestRequestIPPrefersCloudflareHeader(t *testing.T) {
	t.Setenv("CHAT_TRUSTED_PROXY_CIDRS", "127.0.0.1/32,192.168.0.0/16")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "192.168.32.1:40000"
	req.Header.Set("CF-Connecting-IP", "203.0.113.7")
	req.Header.Set("X-Real-IP", "173.245.48.1")
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 173.245.48.1")

	if got := requestIP(req); got != "203.0.113.7" {
		t.Fatalf("requestIP() = %q, want CF-Connecting-IP", got)
	}
}

func TestRequestIPUsesRealIPFromTrustedProxy(t *testing.T) {
	t.Setenv("CHAT_TRUSTED_PROXY_CIDRS", "192.168.0.0/16")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "192.168.32.1:40000"
	req.Header.Set("X-Real-IP", "203.0.113.8")

	if got := requestIP(req); got != "203.0.113.8" {
		t.Fatalf("requestIP() = %q, want X-Real-IP", got)
	}
}

func TestRequestIPUsesForwardedForFromTrustedProxy(t *testing.T) {
	t.Setenv("CHAT_TRUSTED_PROXY_CIDRS", "127.0.0.1/32,10.0.0.0/8")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.5")

	if got := requestIP(req); got != "203.0.113.10" {
		t.Fatalf("requestIP() = %q, want forwarded client IP", got)
	}
}

func TestRequestIPIgnoresSpoofedForwardedForPrefix(t *testing.T) {
	t.Setenv("CHAT_TRUSTED_PROXY_CIDRS", "127.0.0.1/32")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.250, 203.0.113.10")

	if got := requestIP(req); got != "203.0.113.10" {
		t.Fatalf("requestIP() = %q, want rightmost untrusted forwarded client IP", got)
	}
}

func TestRequestIPIgnoresForwardedHeadersFromUntrustedRemote(t *testing.T) {
	t.Setenv("CHAT_TRUSTED_PROXY_CIDRS", "")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "198.51.100.20:54321"
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	req.Header.Set("X-Real-IP", "203.0.113.11")
	req.Header.Set("X-Forwarded-For", "203.0.113.12")

	if got := requestIP(req); got != "198.51.100.20" {
		t.Fatalf("requestIP() = %q, want the direct peer", got)
	}
}

func TestOIDCAllowBudgetsFourCompleteRedirectFlows(t *testing.T) {
	ResetRateLimiter()
	ip := "203.0.113.62"
	for i := 0; i < 4*3; i++ {
		if !OIDCAllow(ip) {
			t.Fatalf("OIDCAllow denied request %d; four complete redirect flows must fit", i+1)
		}
	}
}

func TestWSAllowBudget(t *testing.T) {
	ResetRateLimiter()
	for i := 0; i < 50; i++ {
		if !WSAllow("203.0.113.42") {
			t.Fatalf("WSAllow denied request %d", i+1)
		}
	}
	if WSAllow("203.0.113.42") {
		t.Fatal("expected WSAllow to deny request 51")
	}
}

func TestAuthAllowBudget(t *testing.T) {
	ResetRateLimiter()
	for i := 0; i < 5; i++ {
		if !AuthAllow("203.0.113.60") {
			t.Fatalf("AuthAllow denied request %d", i+1)
		}
	}
	if AuthAllow("203.0.113.60") {
		t.Fatal("expected AuthAllow to deny request 6")
	}
}

func TestRateLimitMiddlewareKeepsSessionBootstrapAvailable(t *testing.T) {
	ResetRateLimiter()
	wrapped := RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	const remote = "198.51.100.101:12345"
	call := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = remote
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		return w.Code
	}

	for i := 0; i < 30; i++ {
		if code := call("/api/test"); code != http.StatusOK {
			t.Fatalf("api call %d = %d", i+1, code)
		}
	}
	if code := call("/api/test"); code != http.StatusTooManyRequests {
		t.Fatalf("generic bucket not exhausted: %d", code)
	}

	// Identity bootstrap has its own budget, so a client whose API budget was
	// consumed can still obtain its session/guest cookie and reconnect.
	for i := 0; i < 60; i++ {
		if code := call("/api/session"); code != http.StatusOK {
			t.Fatalf("session call %d = %d", i+1, code)
		}
	}
	if code := call("/api/session"); code != http.StatusTooManyRequests {
		t.Fatalf("session bucket ceiling missing: %d", code)
	}
}
