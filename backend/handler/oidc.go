package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"tokendancechat/backend/hub"
	"tokendancechat/backend/store"

	"github.com/golang-jwt/jwt/v5"
)

const oidcHTTPTimeout = 5 * time.Second
const oidcMaxDiscoveryBodyBytes = 64 * 1024
const oidcMaxJWKSBodyBytes = 256 * 1024
const oidcMaxTokenBodyBytes = 64 * 1024
const oidcMaxBackchannelBodyBytes = 8 * 1024
const oidcStateTTL = 10 * time.Minute
const oidcStateStoreMaxEntries = 1024
const oidcJWKSCacheTTL = 5 * time.Minute
const oidcJWKSUnknownKidRefreshCooldown = 30 * time.Second
const oidcLogoutTokenMaxAge = 65 * time.Minute
const oidcBackchannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"
const accountLogoutCallbackPath = "/api/logout/account/callback"
const accountLogoutStateTTL = 10 * time.Minute
const accountLogoutCookieNameSecure = "__Host-tdchat_logout"
const accountLogoutCookieNameInsecure = "tdchat_logout"

var oidcHTTPClient = &http.Client{
	Timeout: oidcHTTPTimeout,
	// Discovery endpoints must be exact. In particular a token/UserInfo
	// redirect must never forward credentials to another destination.
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

func readOIDCResponseBody(r io.Reader, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("OIDC provider response exceeds %d bytes", maxBytes)
	}
	return body, nil
}

// OIDCConfig holds the parsed OIDC discovery document.
type OIDCConfig struct {
	Issuer                      string `json:"issuer"`
	AuthEndpoint                string `json:"authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	UserInfoEndpoint            string `json:"userinfo_endpoint"`
	JWKSURI                     string `json:"jwks_uri"`
	EndSessionEndpoint          string `json:"end_session_endpoint"`
	BackchannelLogoutSupported  bool   `json:"backchannel_logout_supported"`
	BackchannelSessionSupported bool   `json:"backchannel_logout_session_supported"`
}

// oidcStateEntry stores PKCE state for an in-flight OIDC authorization.
type oidcStateEntry struct {
	CodeVerifier string
	Nonce        string
	BrowserHash  [sha256.Size]byte
	CreatedAt    time.Time
}

// OIDCStateStore is an in-memory store for PKCE state → code_verifier mapping.
type OIDCStateStore struct {
	mu        sync.Mutex
	store     map[string]*oidcStateEntry
	stop      chan struct{}
	stopped   chan struct{}
	closeOnce sync.Once
}

func newOIDCStateStore() *OIDCStateStore {
	s := &OIDCStateStore{
		store:   make(map[string]*oidcStateEntry),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

func (s *OIDCStateStore) put(state string, entry *oidcStateEntry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(time.Now())
	if _, exists := s.store[state]; !exists && len(s.store) >= oidcStateStoreMaxEntries {
		return false
	}
	s.store[state] = entry
	return true
}

func (s *OIDCStateStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	defer close(s.stopped)
	for {
		select {
		case <-ticker.C:
		case <-s.stop:
			return
		}
		s.mu.Lock()
		s.pruneExpiredLocked(time.Now())
		s.mu.Unlock()
	}
}

func (s *OIDCStateStore) close() {
	s.closeOnce.Do(func() {
		close(s.stop)
	})
	<-s.stopped
}

func (s *OIDCStateStore) take(state, browserToken string) (*oidcStateEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.store[state]
	if !ok {
		return nil, false
	}
	if time.Since(entry.CreatedAt) > oidcStateTTL {
		delete(s.store, state)
		return nil, false
	}
	digest := sha256.Sum256([]byte(browserToken))
	if browserToken == "" || subtle.ConstantTimeCompare(entry.BrowserHash[:], digest[:]) != 1 {
		return nil, false
	}
	delete(s.store, state)
	return entry, true
}

func (s *OIDCStateStore) pruneExpiredLocked(now time.Time) {
	cutoff := now.Add(-oidcStateTTL)
	for k, v := range s.store {
		if v.CreatedAt.Before(cutoff) {
			delete(s.store, k)
		}
	}
}

// oidcJWKSCache caches the JWKS key set with periodic refresh.
type oidcJWKSCache struct {
	mu                    sync.RWMutex
	keys                  map[string]*rsa.PublicKey // kid → public key
	fetched               time.Time
	lastUnknownKidRefresh time.Time
	ttl                   time.Duration
}

func newOIDCJWKSCache() *oidcJWKSCache {
	return &oidcJWKSCache{ttl: oidcJWKSCacheTTL}
}

func (c *oidcJWKSCache) keyFunc(jwksURI string) jwt.Keyfunc {
	return func(token *jwt.Token) (interface{}, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("missing kid in token header")
		}

		c.mu.RLock()
		if c.keys != nil && time.Since(c.fetched) < c.ttl {
			key, exists := c.keys[kid]
			c.mu.RUnlock()
			if exists {
				return key, nil
			}
		} else {
			c.mu.RUnlock()
		}

		c.mu.Lock()
		defer c.mu.Unlock()

		// A fresh cache can still miss a newly rotated key. Allow one forced
		// refresh, then cool down so arbitrary kids cannot amplify requests.
		if c.keys == nil || time.Since(c.fetched) >= c.ttl {
			if err := c.fetchJWKS(jwksURI); err != nil {
				return nil, fmt.Errorf("jwks fetch failed: %w", err)
			}
		} else if c.keys[kid] == nil {
			if !c.lastUnknownKidRefresh.IsZero() &&
				time.Since(c.lastUnknownKidRefresh) < oidcJWKSUnknownKidRefreshCooldown {
				return nil, fmt.Errorf("unknown kid: %s", kid)
			}
			c.lastUnknownKidRefresh = time.Now()
			if err := c.fetchJWKS(jwksURI); err != nil {
				return nil, fmt.Errorf("jwks fetch failed: %w", err)
			}
		}

		if key, exists := c.keys[kid]; exists {
			return key, nil
		}
		return nil, fmt.Errorf("unknown kid: %s", kid)
	}
}

func (c *oidcJWKSCache) fetchJWKS(jwksURI string) error {
	resp, err := oidcHTTPClient.Get(jwksURI)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := readOIDCResponseBody(resp.Body, oidcMaxJWKSBodyBytes)
	if err != nil {
		return err
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return err
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, jk := range jwks.Keys {
		if jk.Kid == "" || jk.Kty != "RSA" || (jk.Alg != "" && jk.Alg != "RS256") ||
			(jk.Use != "" && jk.Use != "sig") {
			continue
		}
		if jk.N == "" || jk.E == "" {
			continue
		}

		nBytes, err := base64.RawURLEncoding.DecodeString(jk.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(jk.E)
		if err != nil {
			continue
		}

		n := new(big.Int).SetBytes(nBytes)
		e := new(big.Int).SetBytes(eBytes)

		keys[jk.Kid] = &rsa.PublicKey{N: n, E: int(e.Int64())}
	}

	if len(keys) == 0 {
		return fmt.Errorf("no usable RSA keys in JWKS")
	}

	c.keys = keys
	c.fetched = time.Now()
	return nil
}

func (h *Handler) closeOIDCStores() {
	if h.oidcStates != nil {
		h.oidcStates.close()
		h.oidcStates = nil
	}
}

// oidcClaims extends the standard JWT claims with OIDC-specific fields.
type oidcClaims struct {
	jwt.RegisteredClaims
	Nonce string `json:"nonce"`
	Azp   string `json:"azp"`
}

type oidcLogoutClaims struct {
	jwt.RegisteredClaims
	Events map[string]json.RawMessage `json:"events"`
	Sid    string                     `json:"sid"`
	Nonce  string                     `json:"nonce"`
}

// oidcTokenResponse is the JSON body from POST /oidc/token.
type oidcTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// oidcConfigResponse is returned by GET /api/oidc/config.
type oidcConfigResponse struct {
	Enabled     bool   `json:"enabled"`
	Issuer      string `json:"issuer"`
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri"`
	AuthURL     string `json:"auth_url"`
}

func validateOIDCOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("must be a canonical origin")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopbackHostname(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("must use HTTPS")
}

func validateOIDCRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("must be an absolute callback URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopbackHostname(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("must use HTTPS")
}

func validateOIDCDiscovery(cfg OIDCConfig, expectedIssuer string) error {
	if cfg.Issuer != expectedIssuer {
		return fmt.Errorf("issuer mismatch")
	}
	issuerURL, err := url.Parse(cfg.Issuer)
	if err != nil {
		return fmt.Errorf("issuer is invalid")
	}
	required := map[string]string{
		"authorization_endpoint": cfg.AuthEndpoint,
		"token_endpoint":         cfg.TokenEndpoint,
		"userinfo_endpoint":      cfg.UserInfoEndpoint,
		"jwks_uri":               cfg.JWKSURI,
	}
	for name, endpoint := range required {
		u, err := url.Parse(endpoint)
		if err != nil || u.User != nil || u.Fragment != "" || u.Host != issuerURL.Host {
			return fmt.Errorf("%s is invalid", name)
		}
		if (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHostname(u.Hostname()))) ||
			u.Host != issuerURL.Host {
			return fmt.Errorf("%s must use the issuer origin over HTTPS", name)
		}
	}
	if cfg.EndSessionEndpoint != "" {
		u, err := url.Parse(cfg.EndSessionEndpoint)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host != issuerURL.Host ||
			(u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHostname(u.Hostname()))) {
			return fmt.Errorf("end_session_endpoint must use the issuer origin over HTTPS")
		}
	}
	return nil
}

func isLoopbackHostname(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// SetupOIDC initializes the OIDC subsystem by fetching the discovery document.
func (h *Handler) SetupOIDC(
	enabled bool,
	clientID, clientSecret, issuer, redirectURI, postLogoutRedirectURI string,
) error {
	if !enabled {
		h.closeOIDCStores()
		h.oidcEnabled = false
		h.oidcConfig = nil
		h.oidcClientID = ""
		h.oidcClientSecret = ""
		h.oidcIssuer = ""
		h.oidcRedirectURI = ""
		h.oidcPostLogoutURI = ""
		h.oidcJWKS = nil
		return nil
	}
	if clientID == "" || clientSecret == "" || issuer == "" || redirectURI == "" || postLogoutRedirectURI == "" {
		return fmt.Errorf("CHAT_OIDC_CLIENT_ID, CHAT_OIDC_CLIENT_SECRET, CHAT_OIDC_ISSUER, CHAT_OIDC_REDIRECT_URI, and CHAT_OIDC_POST_LOGOUT_REDIRECT_URI are required when OIDC is enabled")
	}

	normalizedIssuer := strings.TrimRight(issuer, "/")
	if err := validateOIDCOrigin(normalizedIssuer); err != nil {
		return fmt.Errorf("invalid OIDC issuer: %w", err)
	}
	if err := validateOIDCRedirectURI(redirectURI); err != nil {
		return fmt.Errorf("invalid OIDC redirect URI: %w", err)
	}
	if err := validateOIDCRedirectURI(postLogoutRedirectURI); err != nil {
		return fmt.Errorf("invalid OIDC post-logout redirect URI: %w", err)
	}
	redirectURL, _ := url.Parse(redirectURI)
	postLogoutURL, _ := url.Parse(postLogoutRedirectURI)
	if !strings.EqualFold(redirectURL.Scheme+"://"+redirectURL.Host, postLogoutURL.Scheme+"://"+postLogoutURL.Host) {
		return fmt.Errorf("OIDC post-logout redirect URI must use the callback origin")
	}
	if postLogoutURL.Path != accountLogoutCallbackPath || postLogoutURL.RawQuery != "" {
		return fmt.Errorf("OIDC post-logout redirect URI must use %s without a query", accountLogoutCallbackPath)
	}
	wellKnown := normalizedIssuer + "/.well-known/openid-configuration"
	resp, err := oidcHTTPClient.Get(wellKnown)
	if err != nil {
		return fmt.Errorf("oidc discovery failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc discovery returned status %d", resp.StatusCode)
	}
	body, err := readOIDCResponseBody(resp.Body, oidcMaxDiscoveryBodyBytes)
	if err != nil {
		return fmt.Errorf("oidc discovery read failed: %w", err)
	}
	var cfg OIDCConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return fmt.Errorf("oidc discovery parse failed: %w", err)
	}
	if err := validateOIDCDiscovery(cfg, normalizedIssuer); err != nil {
		return fmt.Errorf("invalid OIDC discovery document: %w", err)
	}
	newStates := newOIDCStateStore()
	h.closeOIDCStores()
	h.oidcEnabled = true
	h.oidcClientID = clientID
	h.oidcClientSecret = clientSecret
	h.oidcIssuer = normalizedIssuer
	h.oidcRedirectURI = redirectURI
	h.oidcPostLogoutURI = postLogoutRedirectURI
	h.oidcStates = newStates
	h.oidcJWKS = newOIDCJWKSCache()
	h.oidcConfig = &cfg
	log.Printf(
		"oidc: discovered provider at %s (confidential client, backchannel=%t)",
		h.oidcIssuer,
		cfg.BackchannelLogoutSupported,
	)
	return nil
}

// OIDCConfigHandler returns OIDC configuration to the frontend.
// GET /api/oidc/config
func (h *Handler) OIDCConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !h.oidcEnabled {
		writeJSONError(w, http.StatusNotFound, "OIDC not enabled", "NOT_FOUND", requestIDFromContext(r.Context()))
		return
	}
	resp := oidcConfigResponse{
		Enabled:     true,
		Issuer:      h.oidcIssuer,
		ClientID:    h.oidcClientID,
		RedirectURI: h.oidcRedirectURI,
		AuthURL:     h.oidcConfig.AuthEndpoint,
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// OIDCLogin initiates the OIDC authorization code flow.
// GET /api/oidc/login
func (h *Handler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !h.oidcEnabled {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !OIDCAllow(requestIP(r)) {
		writeJSONError(w, http.StatusTooManyRequests, "too many OIDC attempts, try again later", "RATE_LIMITED", requestIDFromContext(r.Context()))
		return
	}
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	verifier, err := generateCodeVerifier()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	challenge := computeCodeChallenge(verifier)
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	browserToken, err := generateCodeVerifier()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if ok := h.oidcStates.put(state, &oidcStateEntry{
		CodeVerifier: verifier,
		Nonce:        nonce,
		BrowserHash:  sha256.Sum256([]byte(browserToken)),
		CreatedAt:    time.Now(),
	}); !ok {
		log.Printf("oidc: state store at capacity")
		writeJSONError(w, http.StatusServiceUnavailable, "OIDC login capacity exceeded, try again later", "OIDC_BUSY", requestIDFromContext(r.Context()))
		return
	}
	h.setOIDCFlowCookie(w, browserToken, int(oidcStateTTL.Seconds()))

	authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
		h.oidcConfig.AuthEndpoint,
		url.QueryEscape(h.oidcClientID),
		url.QueryEscape(h.oidcRedirectURI),
		url.QueryEscape("openid profile email"),
		url.QueryEscape(state),
		url.QueryEscape(nonce),
		url.QueryEscape(challenge),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// OIDCCallback handles the authorization code callback from the OIDC provider.
// GET /api/oidc/callback?code=...&state=...
func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !h.oidcEnabled {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !OIDCAllow(requestIP(r)) {
		http.Redirect(w, r, "/?oidc_error=rate_limited", http.StatusFound)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	errorParam := r.URL.Query().Get("error")
	issuer := r.URL.Query().Get("iss")

	if state == "" || (code == "" && errorParam == "") || (code != "" && errorParam != "") {
		http.Redirect(w, r, "/?oidc_error=invalid_request", http.StatusFound)
		return
	}
	if issuer != h.oidcIssuer {
		http.Redirect(w, r, "/?oidc_error=invalid_issuer", http.StatusFound)
		return
	}

	// Bind the authorization to the initiating browser, not just a global
	// state lookup. A forwarded callback must not log its recipient in.
	browserToken := ""
	for _, cookie := range r.Cookies() {
		if cookie.Name == h.oidcFlowCookieName() {
			if browserToken != "" {
				http.Redirect(w, r, "/?oidc_error=invalid_state", http.StatusFound)
				return
			}
			browserToken = cookie.Value
		}
	}
	entry, ok := h.oidcStates.take(state, browserToken)

	if !ok {
		http.Redirect(w, r, "/?oidc_error=invalid_state", http.StatusFound)
		return
	}
	h.setOIDCFlowCookie(w, "", -1)
	if errorParam != "" {
		// Do not log or reflect arbitrary provider text. A bound cancellation
		// still consumes the authorization transaction.
		failure := "authorization_failed"
		if errorParam == "access_denied" {
			failure = "access_denied"
		}
		http.Redirect(w, r, "/?oidc_error="+failure, http.StatusFound)
		return
	}

	tokenResp, err := h.exchangeCodeForTokens(code, entry.CodeVerifier)
	if err != nil {
		log.Printf("oidc: token exchange failed: %v", err)
		http.Redirect(w, r, "/?oidc_error=token_exchange_failed", http.StatusFound)
		return
	}

	claims, err := h.validateIDToken(tokenResp.IDToken, entry.Nonce)
	if err != nil {
		log.Printf("oidc: id_token validation failed: %v", err)
		http.Redirect(w, r, "/?oidc_error=invalid_token", http.StatusFound)
		return
	}

	profile, err := h.fetchUserInfo(tokenResp.AccessToken)
	if err != nil || profile.Subject != claims.Subject {
		log.Printf("oidc: userinfo validation failed: %v", err)
		http.Redirect(w, r, "/?oidc_error=invalid_userinfo", http.StatusFound)
		return
	}

	desiredUsername := resolveOIDCUsername(profile)
	chatUsername, err := h.store.UpsertOIDCUser(store.OIDCUserProfile{
		Sub:               claims.Subject,
		DesiredUsername:   desiredUsername,
		Email:             profile.Email,
		PreferredUsername: profile.PreferredUsername,
		Name:              profile.Name,
		Picture:           profile.Picture,
	})
	if err != nil {
		log.Printf("oidc: user upsert failed: %v", err)
		http.Redirect(w, r, "/?oidc_error=registration_failed", http.StatusFound)
		return
	}
	if err := h.setOIDCSessionCookie(w, chatUsername, claims.Subject); err != nil {
		log.Printf("oidc: session cookie issue failed: %v", err)
		http.Redirect(w, r, "/?oidc_error=session_failed", http.StatusFound)
		return
	}

	log.Printf("oidc: user %q authenticated", chatUsername)
	http.Redirect(w, r, "/?oidc_success=1", http.StatusSeeOther)
}

func (h *Handler) accountLogoutURL(state string) (string, error) {
	if !h.oidcEnabled || h.oidcConfig == nil ||
		h.oidcConfig.EndSessionEndpoint == "" || h.oidcPostLogoutURI == "" {
		return "", fmt.Errorf("Account end-session is not configured")
	}
	endpoint, err := url.Parse(h.oidcConfig.EndSessionEndpoint)
	if err != nil || endpoint.User != nil || endpoint.Fragment != "" {
		return "", fmt.Errorf("Account end-session endpoint is invalid")
	}
	if len(state) < 32 || len(state) > 128 {
		return "", fmt.Errorf("Account logout state is invalid")
	}
	query := endpoint.Query()
	query.Set("client_id", h.oidcClientID)
	query.Set("post_logout_redirect_uri", h.oidcPostLogoutURI)
	query.Set("state", state)
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

// AccountLogoutCallback verifies that the Account end-session return belongs
// to the browser that started this logout. The HttpOnly cookie is the
// authoritative state store; the query value is only a bearer to compare.
// GET /api/logout/account/callback?state=...
func (h *Handler) AccountLogoutCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !h.oidcEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Consume the browser binding on every callback, including invalid and
	// replayed requests, so a captured URL cannot be retried later.
	h.setAccountLogoutCookie(w, "", -1)
	states := r.URL.Query()["state"]
	if len(states) != 1 || len(states[0]) < 32 || len(states[0]) > 128 {
		http.Redirect(w, r, "/?oidc_logout_error=1", http.StatusFound)
		return
	}
	cookieValue := ""
	cookieCount := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == h.accountLogoutCookieName() {
			cookieCount++
			cookieValue = cookie.Value
		}
	}
	if cookieCount != 1 ||
		subtle.ConstantTimeCompare([]byte(cookieValue), []byte(states[0])) != 1 {
		http.Redirect(w, r, "/?oidc_logout_error=1", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/?oidc_logged_out=1", http.StatusFound)
}

// OIDCBackchannelLogout accepts Account's server-to-server Logout Token.
// POST /api/oidc/backchannel-logout
func (h *Handler) OIDCBackchannelLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.oidcEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "METHOD_NOT_ALLOWED", requestIDFromContext(r.Context()))
		return
	}
	if r.ContentLength > oidcMaxBackchannelBodyBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large", "PAYLOAD_TOO_LARGE", requestIDFromContext(r.Context()))
		return
	}
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || len(params) != 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid content type", "INVALID_REQUEST", requestIDFromContext(r.Context()))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oidcMaxBackchannelBodyBytes)
	if err := r.ParseForm(); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large", "PAYLOAD_TOO_LARGE", requestIDFromContext(r.Context()))
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid form body", "INVALID_REQUEST", requestIDFromContext(r.Context()))
		return
	}
	tokens := r.PostForm["logout_token"]
	if len(tokens) != 1 || tokens[0] == "" {
		writeJSONError(w, http.StatusBadRequest, "exactly one logout_token is required", "INVALID_REQUEST", requestIDFromContext(r.Context()))
		return
	}

	claims, err := h.validateLogoutToken(tokens[0])
	if err != nil {
		log.Printf("oidc: back-channel logout token rejected")
		writeJSONError(w, http.StatusBadRequest, "invalid logout token", "INVALID_REQUEST", requestIDFromContext(r.Context()))
		return
	}
	logoutStore, ok := h.store.(oidcBackchannelStore)
	if !ok {
		writeJSONError(w, http.StatusServiceUnavailable, "logout store unavailable", "OIDC_UNAVAILABLE", requestIDFromContext(r.Context()))
		return
	}
	receivedAt := time.Now()
	expiresAt := claims.ExpiresAt.Time.Add(5 * time.Minute)
	claimsHash, err := logoutClaimsHash(tokens[0])
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid logout token", "INVALID_REQUEST", requestIDFromContext(r.Context()))
		return
	}
	result, err := logoutStore.ApplyOIDCBackchannelLogout(
		claims.Subject,
		claims.ID,
		claimsHash,
		receivedAt,
		expiresAt,
	)
	if errors.Is(err, store.ErrOIDCLogoutJTIReuse) {
		log.Printf("oidc: back-channel logout JTI reuse rejected")
		writeJSONError(w, http.StatusBadRequest, "logout token JTI was reused with different claims", "INVALID_REQUEST", requestIDFromContext(r.Context()))
		return
	}
	if err != nil {
		log.Printf("oidc: back-channel logout persistence failed")
		writeJSONError(w, http.StatusServiceUnavailable, "logout persistence unavailable", "OIDC_UNAVAILABLE", requestIDFromContext(r.Context()))
		return
	}
	if result.ChatUsername != "" {
		if err := h.hub.DisconnectAccountSessions(result.ChatUsername, result.Generation); err != nil {
			log.Printf("oidc: live-session invalidation enqueue failed")
			writeJSONError(w, http.StatusServiceUnavailable, "live-session invalidation unavailable", "OIDC_UNAVAILABLE", requestIDFromContext(r.Context()))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) oidcFlowCookieName() string {
	if h.cookieSecure {
		return "__Host-tdchat_oidc"
	}
	return "tdchat_oidc"
}

func (h *Handler) setOIDCFlowCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: h.oidcFlowCookieName(), Value: value, Path: "/",
		HttpOnly: true, Secure: h.cookieSecure, SameSite: http.SameSiteLaxMode,
		MaxAge: maxAge,
	})
}

func (h *Handler) accountLogoutCookieName() string {
	if h.cookieSecure {
		return accountLogoutCookieNameSecure
	}
	return accountLogoutCookieNameInsecure
}

func (h *Handler) setAccountLogoutCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: h.accountLogoutCookieName(), Value: value, Path: "/",
		HttpOnly: true, Secure: h.cookieSecure, SameSite: http.SameSiteLaxMode,
		MaxAge: maxAge,
	})
}

// exchangeCodeForTokens calls POST /oidc/token with an authorization code.
// Chat is a confidential BFF client, so the secret is sent with HTTP Basic
// authentication and PKCE is still included.
func (h *Handler) exchangeCodeForTokens(code, codeVerifier string) (*oidcTokenResponse, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {h.oidcRedirectURI},
		"client_id":     {h.oidcClientID},
		"code_verifier": {codeVerifier},
	}
	req, err := http.NewRequest(http.MethodPost, h.oidcConfig.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(h.oidcClientID, h.oidcClientSecret)
	resp, err := oidcHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := readOIDCResponseBody(resp.Body, oidcMaxTokenBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var tr oidcTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" || tr.IDToken == "" {
		return nil, fmt.Errorf("token response is missing required credentials")
	}
	return &tr, nil
}

// validateIDToken validates an RS256-signed OIDC ID token and its nonce.
func (h *Handler) validateIDToken(idToken, expectedNonce string) (*oidcClaims, error) {
	if idToken == "" {
		return nil, fmt.Errorf("empty id_token")
	}
	if expectedNonce == "" {
		return nil, fmt.Errorf("missing expected nonce")
	}

	keyFunc := h.oidcJWKS.keyFunc(h.oidcConfig.JWKSURI)

	token, err := jwt.ParseWithClaims(idToken, &oidcClaims{}, keyFunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(h.oidcIssuer),
		jwt.WithAudience(h.oidcClientID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("id_token parse: %w", err)
	}

	claims, ok := token.Claims.(*oidcClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid claims or token not valid")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("missing sub claim")
	}
	if claims.IssuedAt == nil {
		return nil, fmt.Errorf("missing iat claim")
	}
	if (len(claims.Audience) > 1 || claims.Azp != "") && claims.Azp != h.oidcClientID {
		return nil, fmt.Errorf("invalid azp claim")
	}
	if subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(expectedNonce)) != 1 {
		return nil, fmt.Errorf("nonce mismatch")
	}
	return claims, nil
}

func (h *Handler) validateLogoutToken(raw string) (*oidcLogoutClaims, error) {
	if raw == "" || h.oidcJWKS == nil || h.oidcConfig == nil {
		return nil, fmt.Errorf("logout token is unavailable")
	}
	keyFunc := h.oidcJWKS.keyFunc(h.oidcConfig.JWKSURI)
	token, err := jwt.ParseWithClaims(raw, &oidcLogoutClaims{}, keyFunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(h.oidcIssuer),
		jwt.WithAudience(h.oidcClientID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("logout token parse failed")
	}
	claims, ok := token.Claims.(*oidcLogoutClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("logout token claims are invalid")
	}
	if claims.Subject == "" || len(claims.Subject) > 512 {
		return nil, fmt.Errorf("logout token subject is invalid")
	}
	if claims.ID == "" || len(claims.ID) > 512 {
		return nil, fmt.Errorf("logout token jti is invalid")
	}
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return nil, fmt.Errorf("logout token timestamps are required")
	}
	now := time.Now()
	if now.Sub(claims.IssuedAt.Time) > oidcLogoutTokenMaxAge ||
		claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > 2*oidcLogoutTokenMaxAge {
		return nil, fmt.Errorf("logout token is stale")
	}
	if claims.Sid != "" || claims.Nonce != "" {
		return nil, fmt.Errorf("logout token contains forbidden claims")
	}
	if len(claims.Events) != 1 {
		return nil, fmt.Errorf("logout token event is invalid")
	}
	rawEvent, ok := claims.Events[oidcBackchannelLogoutEvent]
	if !ok {
		return nil, fmt.Errorf("logout token event is missing")
	}
	var event map[string]json.RawMessage
	if err := json.Unmarshal(rawEvent, &event); err != nil || event == nil || len(event) != 0 {
		return nil, fmt.Errorf("logout token event is invalid")
	}
	return claims, nil
}

func logoutClaimsHash(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("logout token is malformed")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("logout token payload is invalid")
	}
	var claims any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("logout token claims are invalid")
	}
	canonical, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("logout token claims cannot be canonicalized")
	}
	digest := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

type oidcUserInfo struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Picture           string `json:"picture"`
}

func (h *Handler) fetchUserInfo(accessToken string) (oidcUserInfo, error) {
	if accessToken == "" || h.oidcConfig == nil || h.oidcConfig.UserInfoEndpoint == "" {
		return oidcUserInfo{}, fmt.Errorf("userinfo is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), oidcHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.oidcConfig.UserInfoEndpoint, nil)
	if err != nil {
		return oidcUserInfo{}, fmt.Errorf("userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := oidcHTTPClient.Do(req)
	if err != nil {
		return oidcUserInfo{}, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return oidcUserInfo{}, fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}
	body, err := readOIDCResponseBody(resp.Body, oidcMaxDiscoveryBodyBytes)
	if err != nil {
		return oidcUserInfo{}, fmt.Errorf("userinfo read failed: %w", err)
	}
	var info oidcUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return oidcUserInfo{}, fmt.Errorf("userinfo parse failed: %w", err)
	}
	if info.Subject == "" {
		return oidcUserInfo{}, fmt.Errorf("userinfo missing sub")
	}
	if info.Picture != "" && !validOIDCPictureURL(info.Picture) {
		return oidcUserInfo{}, fmt.Errorf("userinfo picture is not an HTTPS URL")
	}
	return info, nil
}

// resolveOIDCUsername resolves a stable requested Chat handle from UserInfo.
func resolveOIDCUsername(info oidcUserInfo) string {
	if info.PreferredUsername != "" && hub.ValidateUsername(info.PreferredUsername) &&
		!hub.IsReservedUsername(info.PreferredUsername) {
		return info.PreferredUsername
	}
	if info.Email != "" {
		local := strings.Split(info.Email, "@")[0]
		sanitized := sanitizeOIDCUsername(local)
		if sanitized != "" && hub.ValidateUsername(sanitized) && !hub.IsReservedUsername(sanitized) {
			return sanitized
		}
	}
	if info.Name != "" {
		sanitized := sanitizeOIDCUsername(info.Name)
		if hub.ValidateUsername(sanitized) && !hub.IsReservedUsername(sanitized) {
			return sanitized
		}
	}
	digest := sha256.Sum256([]byte(info.Subject))
	return fmt.Sprintf("td_%x", digest[:6])
}

func sanitizeOIDCUsername(s string) string {
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, s)
	result = strings.Trim(result, "_")
	if len(result) > 20 {
		result = strings.TrimRight(result[:20], "_")
	}
	if len(result) < 2 {
		if len(s) >= 2 {
			return s[:2]
		}
		return ""
	}
	return result
}

func validOIDCPictureURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" &&
		u.User == nil && u.Fragment == ""
}

// PKCE utilities.

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func computeCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
