package handler

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"tokendancechat/backend/security"
)

const (
	wsMaxPerWindow            = 50
	wsWindow                  = 10 * time.Second
	apiMaxPerWindowDefault    = 30
	apiWindow                 = time.Minute
	backchannelMaxPerWindow   = 120
	backchannelWindow         = time.Minute
	authMaxPerWindow          = 5
	authWindow                = time.Minute
	oidcMaxPerWindow          = 20
	oidcWindow                = time.Minute
	maxJSONBodySize           = 16 << 10
	maxImageMultipartOverhead = 1 << 20
)

var (
	requestLimiter = security.NewWindowLimiter()
	authBackoff    = security.NewLoginGuard()

	proxyMu    sync.Mutex
	proxyRaw   string
	proxyTrust *security.ProxyTrust
)

// requestIP resolves the visitor address. Forwarded headers are honoured only
// when the direct peer is listed in CHAT_TRUSTED_PROXY_CIDRS.
func requestIP(r *http.Request) string {
	return currentProxyTrust().ClientIP(r.RemoteAddr, r.Header)
}

func currentProxyTrust() *security.ProxyTrust {
	raw := strings.TrimSpace(os.Getenv("CHAT_TRUSTED_PROXY_CIDRS"))
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if proxyTrust == nil || proxyRaw != raw {
		proxyRaw = raw
		proxyTrust = security.NewProxyTrust(raw)
	}
	return proxyTrust
}

func wsAllowed(ip string) bool {
	return requestLimiter.Allow("ws", ip, wsMaxPerWindow, wsWindow)
}

func apiAllowed(ip string) bool {
	return requestLimiter.Allow("api", ip, apiMaxPerWindow(), apiWindow)
}

// uploadAllowed bounds image uploads per IP; guests get a stricter bucket.
func uploadAllowed(ip string, limit int) bool {
	return requestLimiter.Allow("upload", ip, limit, time.Minute)
}

func backchannelAllowed(ip string) bool {
	return requestLimiter.Allow("oidc-backchannel", ip, backchannelMaxPerWindow, backchannelWindow)
}

func authAllowed(ip string) bool {
	return requestLimiter.Allow("auth", ip, authMaxPerWindow, authWindow)
}

func oidcAllowed(ip string) bool {
	return requestLimiter.Allow("oidc", ip, oidcMaxPerWindow, oidcWindow)
}

func apiMaxPerWindow() int {
	raw := strings.TrimSpace(os.Getenv("CHAT_API_RATE_LIMIT_PER_MINUTE"))
	if raw == "" {
		return apiMaxPerWindowDefault
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return apiMaxPerWindowDefault
	}
	return value
}

// RateLimitMiddleware limits REST API requests per minute per IP.
// Place after LoggingMiddleware so blocked requests are still logged.
func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		ip := requestIP(r)
		allowed := apiAllowed(ip)
		if r.URL.Path == "/api/oidc/backchannel-logout" {
			// Account delivers server-to-server logouts in batches. A generic
			// browser API budget would delay legitimate account-revocation
			// propagation, so this signed endpoint has its own bounded bucket.
			allowed = backchannelAllowed(ip)
		}
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded, try again later","code":"RATE_LIMITED"}`))
			log.Printf("rate limit: REST API blocked for %s path=%s", ip, r.URL.Path)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BodyLimitMiddleware caps request bodies before handlers decode them.
func BodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxJSONBodySize)
		if r.URL.Path == "/api/emoji/upload" {
			limit = maxEmojiUploadSize
		} else if r.URL.Path == "/api/upload/image" {
			limit = maxImageUploadSize + maxImageMultipartOverhead
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// WSAllow reports whether a WebSocket upgrade is allowed for an address.
func WSAllow(ip string) bool {
	return wsAllowed(ip)
}

// AuthAllow reports whether a credential endpoint request is allowed.
func AuthAllow(ip string) bool {
	return authAllowed(ip)
}

// OIDCAllow reports whether an OIDC endpoint request is allowed.
func OIDCAllow(ip string) bool {
	return oidcAllowed(ip)
}

// ResetRateLimiter clears limiter and login-backoff state between tests.
func ResetRateLimiter() {
	requestLimiter = security.NewWindowLimiter()
	authBackoff = security.NewLoginGuard()
}
