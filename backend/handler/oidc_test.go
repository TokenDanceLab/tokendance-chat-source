package handler

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"tokendancechat/backend/hub"
	"tokendancechat/backend/store"

	"github.com/golang-jwt/jwt/v5"
)

// lastTokenForm records the most recent form received by the mock OIDC
// provider's /token endpoint, so tests can assert client_secret / PKCE
// behaviour without spinning up a separate provider.
var lastTokenForm url.Values
var lastTokenBasicUser string
var lastTokenBasicPasswordOK bool
var mockIDTokenNonce string
var mockUserInfoSubject string

// generateTestRSAKey creates a 2048-bit RSA key pair for test token signing.
func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return key
}

func TestOIDCHTTPClientHasBoundedTimeout(t *testing.T) {
	if oidcHTTPClient == nil {
		t.Fatal("oidcHTTPClient is nil")
	}
	if oidcHTTPClient.Timeout != oidcHTTPTimeout {
		t.Fatalf("oidcHTTPClient timeout = %s, want %s", oidcHTTPClient.Timeout, oidcHTTPTimeout)
	}
	if oidcHTTPClient.Timeout <= 0 || oidcHTTPClient.Timeout > 5*time.Second {
		t.Fatalf("oidcHTTPClient timeout is not bounded tightly enough: %s", oidcHTTPClient.Timeout)
	}
}

func TestReadOIDCResponseBodyRejectsOversizedResponse(t *testing.T) {
	body, err := readOIDCResponseBody(bytes.NewReader([]byte("ok")), 2)
	if err != nil {
		t.Fatalf("expected small response to be accepted: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", string(body))
	}

	_, err = readOIDCResponseBody(bytes.NewReader(bytes.Repeat([]byte("x"), 4)), 3)
	if err == nil {
		t.Fatal("expected oversized response to be rejected")
	}
}

func TestOIDCStateStoreCloseStopsCleanupLoop(t *testing.T) {
	store := newOIDCStateStore()
	done := make(chan struct{})

	go func() {
		store.close()
		store.close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("state store close did not stop cleanup loop")
	}
}

func TestOIDCStateStoreRejectsNewEntriesAtCapacity(t *testing.T) {
	store := newOIDCStateStore()
	t.Cleanup(store.close)
	now := time.Now()
	for i := 0; i < oidcStateStoreMaxEntries; i++ {
		if ok := store.put(fmt.Sprintf("state-%03d", i), &oidcStateEntry{
			CodeVerifier: fmt.Sprintf("verifier-%03d", i),
			CreatedAt:    now.Add(time.Duration(i) * time.Second),
		}); !ok {
			t.Fatalf("state insert %d was rejected before capacity", i)
		}
	}

	if ok := store.put("state-overflow", &oidcStateEntry{
		CodeVerifier: "verifier-overflow",
		CreatedAt:    now.Add(time.Hour),
	}); ok {
		t.Fatal("expected state store to reject new entry at capacity")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if got := len(store.store); got != oidcStateStoreMaxEntries {
		t.Fatalf("state store len = %d, want %d", got, oidcStateStoreMaxEntries)
	}
	if _, ok := store.store["state-000"]; !ok {
		t.Fatal("expected oldest state entry to be retained")
	}
	if _, ok := store.store["state-overflow"]; ok {
		t.Fatal("expected overflow state entry to be absent")
	}
}

func TestSetupOIDCFailureDoesNotInstallTransientStores(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	h := &Handler{store: &mockStore{}}
	err := h.SetupOIDC(true, "test-client", "", srv.URL, "http://localhost:8080/api/oidc/callback", "http://localhost:8080/api/logout/account/callback")
	if err == nil {
		t.Fatal("expected SetupOIDC to fail")
	}
	if h.oidcStates != nil {
		t.Fatal("expected failed SetupOIDC to leave transient stores uninstalled")
	}
}

func TestSetupOIDCFailureDoesNotInstallTransientStoresConfidential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	h := &Handler{store: &mockStore{}}
	err := h.SetupOIDC(true, "test-client", "test-secret", srv.URL, "http://localhost:8080/api/oidc/callback", "http://localhost:8080/api/logout/account/callback")
	if err == nil {
		t.Fatal("expected SetupOIDC to fail")
	}
	if h.oidcStates != nil {
		t.Fatal("expected failed SetupOIDC to leave transient stores uninstalled")
	}
	if h.oidcClientSecret != "" {
		t.Fatalf("expected failed SetupOIDC to not retain client secret, got %q", h.oidcClientSecret)
	}
}

func TestSetupOIDCReconfigureClosesPreviousTransientStores(t *testing.T) {
	key := generateTestRSAKey(t)
	first := startMockOIDCProvider(t, key, "test-kid-reconfigure-1")
	defer first.Close()
	second := startMockOIDCProvider(t, key, "test-kid-reconfigure-2")
	defer second.Close()

	h := &Handler{store: &mockStore{}}
	if err := h.SetupOIDC(true, "test-client", "test-secret", first.URL, "http://localhost:8080/api/oidc/callback", "http://localhost:8080/api/logout/account/callback"); err != nil {
		t.Fatalf("first SetupOIDC failed: %v", err)
	}
	oldStates := h.oidcStates
	if err := h.SetupOIDC(true, "test-client", "test-secret", second.URL, "http://localhost:8080/api/oidc/callback", "http://localhost:8080/api/logout/account/callback"); err != nil {
		t.Fatalf("second SetupOIDC failed: %v", err)
	}
	t.Cleanup(h.closeOIDCStores)

	select {
	case <-oldStates.stopped:
	default:
		t.Fatal("expected previous state store cleanup loop to be stopped after reconfigure")
	}
	if h.oidcStates == oldStates {
		t.Fatal("expected reconfigure to install fresh transient stores")
	}
}

// signTestIDToken creates a valid RS256-signed JWT with the given claims.
func signTestIDToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

// startMockOIDCProvider starts a test HTTP server that serves OIDC discovery, JWKS, and token endpoints.
func startMockOIDCProvider(t *testing.T, key *rsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()
	lastTokenForm = nil
	lastTokenBasicUser = ""
	lastTokenBasicPasswordOK = false
	mockIDTokenNonce = ""
	mockUserInfoSubject = ""

	mux := http.NewServeMux()

	// All OIDC endpoints at the root of the test server.
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base := scheme + "://" + r.Host
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                               base,
			"authorization_endpoint":               base + "/authorize",
			"token_endpoint":                       base + "/token",
			"userinfo_endpoint":                    base + "/userinfo",
			"jwks_uri":                             base + "/jwks",
			"end_session_endpoint":                 base + "/end-session",
			"backchannel_logout_supported":         true,
			"backchannel_logout_session_supported": false,
		})
	})

	// JWKS endpoint.
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString([]byte{0, 0, 0, 0})
		// Compute exponent properly.
		eBytes := make([]byte, 0)
		exp := uint32(key.E)
		for exp > 0 {
			eBytes = append([]byte{byte(exp & 0xff)}, eBytes...)
			exp >>= 8
		}
		if len(eBytes) == 0 {
			eBytes = []byte{1, 0, 1}
		}
		e = base64.RawURLEncoding.EncodeToString(eBytes)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]string{
				{
					"kid": kid,
					"kty": "RSA",
					"alg": "RS256",
					"use": "sig",
					"n":   n,
					"e":   e,
				},
			},
		})
	})

	// Token endpoint.
	// Records the received form values on the test server's request so tests
	// can assert that a confidential client sends client_secret (and that a
	// PKCE public client does not).
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		lastTokenForm = r.PostForm
		user, password, ok := r.BasicAuth()
		lastTokenBasicUser = user
		lastTokenBasicPasswordOK = ok && password == "test-secret"
		grantType := r.FormValue("grant_type")

		switch grantType {
		case "authorization_code":
			if r.FormValue("code") == "" {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			mockIssuer := "http://" + r.Host
			now := time.Now()
			idToken := signTestIDToken(t, key, kid, jwt.MapClaims{
				"iss":   mockIssuer,
				"sub":   "test-oidc-sub-123",
				"aud":   []string{"test-client"},
				"exp":   now.Add(15 * time.Minute).Unix(),
				"iat":   now.Unix(),
				"nonce": mockIDTokenNonce,
			})
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "test-access-token-value",
				"token_type":   "Bearer",
				"expires_in":   900,
				"id_token":     idToken,
			})
		default:
			http.Error(w, `{"error":"unsupported_grant_type"}`, 400)
		}
	})

	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		subject := "test-oidc-sub-123"
		if mockUserInfoSubject != "" {
			subject = mockUserInfoSubject
		}
		switch r.Header.Get("Authorization") {
		case "Bearer test-access-token-value":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"sub":                subject,
				"email":              "testuser@example.com",
				"preferred_username": "TestOIDCUser",
				"name":               "Test User",
				"picture":            "https://img.tokendancelab.com/avatars/test/256.webp",
			})
		case "Bearer other-sub-token":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"sub":                "other-oidc-sub-456",
				"email":              "other@example.com",
				"preferred_username": "OtherUser",
				"name":               "Other User",
			})
		default:
			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
		}
	})

	return httptest.NewServer(mux)
}

// ─── PKCE Utilities ─────────────────────────────────────────────────

func TestGenerateCodeVerifier(t *testing.T) {
	v, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("generateCodeVerifier failed: %v", err)
	}
	if len(v) < 43 {
		t.Errorf("code verifier too short: got %d chars, want >=43", len(v))
	}
	// Should be base64url-safe (no +, /, or =).
	for _, ch := range v {
		if ch == '+' || ch == '/' || ch == '=' {
			t.Errorf("code verifier contains invalid char: %c", ch)
		}
	}
}

func TestComputeCodeChallenge(t *testing.T) {
	// Test vector from RFC 7636 Appendix B.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	challenge := computeCodeChallenge(verifier)
	if challenge != expected {
		t.Errorf("code challenge mismatch: got %s, want %s", challenge, expected)
	}
}

// ─── resolveOIDCUsername ──────────────────────────────────────────

func TestResolveOIDCUsername(t *testing.T) {
	tests := []struct {
		name string
		info oidcUserInfo
		want string
	}{
		{
			name: "uses preferred_username when valid",
			info: oidcUserInfo{
				PreferredUsername: "Alice",
				Email:             "alice@example.com",
			},
			want: "Alice",
		},
		{
			name: "falls back to email local part",
			info: oidcUserInfo{
				Email: "bob_test@example.com",
			},
			want: "bob_test",
		},
		{
			name: "uses name when available",
			info: oidcUserInfo{
				Name: "Charlie",
			},
			want: "Charlie",
		},
		{
			name: "sanitizes email with special chars",
			info: oidcUserInfo{
				Email: "user.name+tag@example.com",
			},
			want: "user_name_tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveOIDCUsername(tt.info)
			if got != tt.want {
				t.Errorf("resolveOIDCUsername() = %q, want %q", got, tt.want)
			}
			// Must be a valid chat username.
			if got != "" && !hub.ValidateUsername(got) {
				t.Errorf("resolved username %q is not valid", got)
			}
		})
	}

	fallback := resolveOIDCUsername(oidcUserInfo{Subject: "abcdef1234567890"})
	if !hub.ValidateUsername(fallback) || !strings.HasPrefix(fallback, "td_") {
		t.Fatalf("fallback username = %q", fallback)
	}
}

// ─── OIDC Handler Integration ─────────────────────────────────────

func setupOIDCForTest(t *testing.T, h *Handler, issuer string) {
	t.Helper()
	if err := h.SetupOIDC(true, "test-client", "test-secret", issuer, "http://localhost:8080/api/oidc/callback", "http://localhost:8080/api/logout/account/callback"); err != nil {
		t.Fatalf("SetupOIDC failed: %v", err)
	}
	t.Cleanup(h.closeOIDCStores)
}

// setupOIDCForTestConfidential enables OIDC with a client secret (confidential client).
func setupOIDCForTestConfidential(t *testing.T, h *Handler, issuer string) {
	t.Helper()
	if err := h.SetupOIDC(true, "test-client", "test-secret", issuer, "http://localhost:8080/api/oidc/callback", "http://localhost:8080/api/logout/account/callback"); err != nil {
		t.Fatalf("SetupOIDC failed: %v", err)
	}
	t.Cleanup(h.closeOIDCStores)
}

func TestOIDCConfigHandler(t *testing.T) {
	key := generateTestRSAKey(t)
	kid := "test-kid-001"
	srv := startMockOIDCProvider(t, key, kid)
	defer srv.Close()

	// Extract the actual host from the test server to construct the issuer URL.
	addr := srv.Listener.Addr().String()
	issuer := "http://" + addr

	h := &Handler{
		store: &mockStore{},
	}
	setupOIDCForTest(t, h, issuer)

	req := httptest.NewRequest("GET", "/api/oidc/config", nil)
	rec := httptest.NewRecorder()

	h.OIDCConfigHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp oidcConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected enabled=true")
	}
	if resp.ClientID != "test-client" {
		t.Errorf("expected client_id=test-client, got %s", resp.ClientID)
	}
	if resp.AuthURL == "" {
		t.Error("expected non-empty auth_url")
	}
}

func TestOIDCConfigHandlerNotEnabled(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest("GET", "/api/oidc/config", nil)
	rec := httptest.NewRecorder()
	h.OIDCConfigHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when not enabled, got %d", rec.Code)
	}
}

func TestOIDCLoginRedirect(t *testing.T) {
	key := generateTestRSAKey(t)
	kid := "test-kid-002"
	srv := startMockOIDCProvider(t, key, kid)
	defer srv.Close()

	addr := srv.Listener.Addr().String()
	issuer := "http://" + addr

	h := &Handler{
		store: &mockStore{},
	}
	setupOIDCForTest(t, h, issuer)

	req := httptest.NewRequest("GET", "/api/oidc/login", nil)
	rec := httptest.NewRecorder()

	h.OIDCLogin(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse redirect URL: %v", err)
	}

	params := parsed.Query()
	if params.Get("response_type") != "code" {
		t.Error("missing response_type=code")
	}
	if params.Get("client_id") != "test-client" {
		t.Error("wrong client_id")
	}
	if params.Get("code_challenge_method") != "S256" {
		t.Error("wrong code_challenge_method")
	}
	if params.Get("code_challenge") == "" {
		t.Error("missing code_challenge")
	}
	if params.Get("state") == "" {
		t.Error("missing state")
	}
	if params.Get("nonce") == "" {
		t.Error("missing nonce")
	}

	// Verify state was stored.
	h.oidcStates.mu.Lock()
	entries := len(h.oidcStates.store)
	h.oidcStates.mu.Unlock()
	if entries != 1 {
		t.Errorf("expected 1 stored state, got %d", entries)
	}
}

func TestOIDCLoginRateLimitedByIP(t *testing.T) {
	ResetRateLimiter()
	key := generateTestRSAKey(t)
	kid := "test-kid-rate-limit"
	srv := startMockOIDCProvider(t, key, kid)
	defer srv.Close()

	addr := srv.Listener.Addr().String()
	issuer := "http://" + addr

	h := &Handler{store: &mockStore{}}
	setupOIDCForTest(t, h, issuer)

	ip := "192.0.2.222:1234"
	for i := 0; i < oidcMaxPerWindow; i++ {
		req := httptest.NewRequest("GET", "/api/oidc/login", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		h.OIDCLogin(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("request %d: expected 302 before limit, got %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest("GET", "/api/oidc/login", nil)
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	h.OIDCLogin(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected OIDC login to be rate limited, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOIDCCallbackSuccess(t *testing.T) {
	key := generateTestRSAKey(t)
	kid := "test-kid-003"
	srv := startMockOIDCProvider(t, key, kid)
	defer srv.Close()

	addr := srv.Listener.Addr().String()
	issuer := "http://" + addr

	st := newMockStoreForOIDC()

	h := &Handler{
		store:        st,
		cookieSecure: true,
	}
	setupOIDCForTest(t, h, issuer)

	// First, simulate a login to create a valid state entry.
	reqLogin := httptest.NewRequest("GET", "/api/oidc/login", nil)
	recLogin := httptest.NewRecorder()
	h.OIDCLogin(recLogin, reqLogin)

	// Extract the state from the redirect URL.
	parsed, _ := url.Parse(recLogin.Header().Get("Location"))
	state := parsed.Query().Get("state")
	mockIDTokenNonce = parsed.Query().Get("nonce")

	callbackURL := fmt.Sprintf(
		"/api/oidc/callback?state=%s&code=fake-auth-code&iss=%s",
		url.QueryEscape(state),
		url.QueryEscape(issuer),
	)
	req := requestWithLoginCookies(callbackURL, recLogin)
	rec := httptest.NewRecorder()

	h.OIDCCallback(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d: %s", rec.Code, rec.Body.String())
		return
	}

	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	if location != "/?oidc_success=1" {
		t.Errorf("unexpected callback redirect: %s", location)
	}
	if lastTokenBasicUser != "test-client" || !lastTokenBasicPasswordOK {
		t.Errorf("token exchange did not use HTTP Basic client authentication")
	}
	if lastTokenForm.Get("client_secret") != "" {
		t.Error("client secret must not be sent in the token form body")
	}
	var sessionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || !sessionCookie.Secure ||
		sessionCookie.SameSite != http.SameSiteLaxMode || sessionCookie.Path != "/" {
		t.Fatal("expected secure HttpOnly Chat session cookie")
	}
	if strings.Contains(rec.Body.String(), "test-access-token-value") ||
		strings.Contains(location, "test-access-token-value") {
		t.Fatal("provider access token leaked to the browser")
	}
	user, err := st.GetOIDCUserBySub("test-oidc-sub-123")
	if err != nil {
		t.Fatalf("expected OIDC user mapping: %v", err)
	}
	if user.ChatUsername != "TestOIDCUser" || user.Name != "Test User" ||
		user.Picture == "" || user.Email != "testuser@example.com" {
		t.Fatalf("unexpected OIDC profile: %#v", user)
	}

	// Verify state was cleaned up (one-time use).
	h.oidcStates.mu.Lock()
	remaining := len(h.oidcStates.store)
	h.oidcStates.mu.Unlock()
	if remaining != 0 {
		t.Errorf("expected 0 remaining states, got %d", remaining)
	}
}

func TestOIDCCallbackInvalidState(t *testing.T) {
	key := generateTestRSAKey(t)
	kid := "test-kid-004"
	srv := startMockOIDCProvider(t, key, kid)
	defer srv.Close()

	addr := srv.Listener.Addr().String()
	issuer := "http://" + addr

	h := &Handler{
		store: &mockStore{},
	}
	setupOIDCForTest(t, h, issuer)

	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/api/oidc/callback?state=bad-state&code=fake-code&iss=%s", url.QueryEscape(issuer)),
		nil,
	)
	rec := httptest.NewRecorder()

	h.OIDCCallback(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	if location == "" || !containsQueryParam(location, "oidc_error", "invalid_state") {
		t.Errorf("expected oidc_error=invalid_state in redirect: %s", location)
	}
}

func TestOIDCCallbackRejectsInvalidIssuer(t *testing.T) {
	key := generateTestRSAKey(t)
	srv := startMockOIDCProvider(t, key, "test-kid-invalid-issuer")
	defer srv.Close()

	issuer := "http://" + srv.Listener.Addr().String()
	st := newMockStoreForOIDC()
	h := &Handler{store: st}
	setupOIDCForTest(t, h, issuer)

	login := httptest.NewRecorder()
	h.OIDCLogin(login, httptest.NewRequest(http.MethodGet, "/api/oidc/login", nil))
	parsed, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}

	callbackURL := fmt.Sprintf(
		"/api/oidc/callback?state=%s&code=test-code&iss=%s",
		url.QueryEscape(parsed.Query().Get("state")),
		url.QueryEscape("https://wrong.example"),
	)
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, requestWithLoginCookies(callbackURL, login))

	if !containsQueryParam(rec.Header().Get("Location"), "oidc_error", "invalid_issuer") {
		t.Fatalf("unexpected redirect: %s", rec.Header().Get("Location"))
	}
	if _, err := st.GetOIDCUserBySub("test-oidc-sub-123"); err == nil {
		t.Fatal("invalid issuer callback created a user")
	}
}

func TestOIDCCallbackRejectsNonceMismatch(t *testing.T) {
	key := generateTestRSAKey(t)
	srv := startMockOIDCProvider(t, key, "test-kid-nonce-mismatch")
	defer srv.Close()

	issuer := "http://" + srv.Listener.Addr().String()
	h := &Handler{store: newMockStoreForOIDC()}
	setupOIDCForTest(t, h, issuer)

	login := httptest.NewRecorder()
	h.OIDCLogin(login, httptest.NewRequest(http.MethodGet, "/api/oidc/login", nil))
	parsed, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}
	mockIDTokenNonce = "different-nonce"

	callbackURL := fmt.Sprintf(
		"/api/oidc/callback?state=%s&code=test-code&iss=%s",
		url.QueryEscape(parsed.Query().Get("state")),
		url.QueryEscape(issuer),
	)
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, requestWithLoginCookies(callbackURL, login))

	if !containsQueryParam(rec.Header().Get("Location"), "oidc_error", "invalid_token") {
		t.Fatalf("unexpected redirect: %s", rec.Header().Get("Location"))
	}
	if hasAuthenticatedSessionCookie(rec) {
		t.Fatal("nonce mismatch issued a session cookie")
	}
}

func TestOIDCCallbackRejectsMissingNonce(t *testing.T) {
	key := generateTestRSAKey(t)
	srv := startMockOIDCProvider(t, key, "test-kid-nonce-missing")
	defer srv.Close()

	issuer := "http://" + srv.Listener.Addr().String()
	h := &Handler{store: newMockStoreForOIDC()}
	setupOIDCForTest(t, h, issuer)

	login := httptest.NewRecorder()
	h.OIDCLogin(login, httptest.NewRequest(http.MethodGet, "/api/oidc/login", nil))
	parsed, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}
	mockIDTokenNonce = ""

	callbackURL := fmt.Sprintf(
		"/api/oidc/callback?state=%s&code=test-code&iss=%s",
		url.QueryEscape(parsed.Query().Get("state")),
		url.QueryEscape(issuer),
	)
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, requestWithLoginCookies(callbackURL, login))

	if !containsQueryParam(rec.Header().Get("Location"), "oidc_error", "invalid_token") {
		t.Fatalf("unexpected redirect: %s", rec.Header().Get("Location"))
	}
}

func TestOIDCCallbackRejectsUserInfoSubjectMismatch(t *testing.T) {
	key := generateTestRSAKey(t)
	srv := startMockOIDCProvider(t, key, "test-kid-userinfo-sub")
	defer srv.Close()

	issuer := "http://" + srv.Listener.Addr().String()
	h := &Handler{store: newMockStoreForOIDC()}
	setupOIDCForTest(t, h, issuer)

	login := httptest.NewRecorder()
	h.OIDCLogin(login, httptest.NewRequest(http.MethodGet, "/api/oidc/login", nil))
	parsed, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}
	mockIDTokenNonce = parsed.Query().Get("nonce")
	mockUserInfoSubject = "different-userinfo-sub"

	callbackURL := fmt.Sprintf(
		"/api/oidc/callback?state=%s&code=test-code&iss=%s",
		url.QueryEscape(parsed.Query().Get("state")),
		url.QueryEscape(issuer),
	)
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, requestWithLoginCookies(callbackURL, login))

	if !containsQueryParam(rec.Header().Get("Location"), "oidc_error", "invalid_userinfo") {
		t.Fatalf("unexpected redirect: %s", rec.Header().Get("Location"))
	}
	if hasAuthenticatedSessionCookie(rec) {
		t.Fatal("userinfo subject mismatch issued a session cookie")
	}
}

func TestOIDCJWKSCacheRefreshesUnknownKid(t *testing.T) {
	firstKey := generateTestRSAKey(t)
	secondKey := generateTestRSAKey(t)
	currentKey := firstKey
	currentKid := "initial-kid"
	fetches := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		eBytes := make([]byte, 0)
		exp := uint32(currentKey.E)
		for exp > 0 {
			eBytes = append([]byte{byte(exp & 0xff)}, eBytes...)
			exp >>= 8
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]string{{
				"kid": currentKid,
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(currentKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			}},
		})
	}))
	defer srv.Close()

	cache := newOIDCJWKSCache()
	keyFunc := cache.keyFunc(srv.URL)
	parse := func(key *rsa.PrivateKey, kid string) error {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "test"})
		token.Header["kid"] = kid
		signed, err := token.SignedString(key)
		if err != nil {
			return err
		}
		_, err = jwt.Parse(signed, keyFunc, jwt.WithValidMethods([]string{"RS256"}))
		return err
	}

	if err := parse(firstKey, "initial-kid"); err != nil {
		t.Fatalf("initial key validation failed: %v", err)
	}
	currentKey = secondKey
	currentKid = "rotated-kid"
	if err := parse(secondKey, "rotated-kid"); err != nil {
		t.Fatalf("rotated key validation failed: %v", err)
	}
	if fetches != 2 {
		t.Fatalf("JWKS fetches = %d, want 2", fetches)
	}
}

func TestOIDCJWKSCacheCoolsDownUnknownKidRefreshes(t *testing.T) {
	key := generateTestRSAKey(t)
	fetches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		eBytes := []byte{byte(key.E >> 16), byte(key.E >> 8), byte(key.E)}
		for len(eBytes) > 1 && eBytes[0] == 0 {
			eBytes = eBytes[1:]
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]string{{
				"kid": "known-kid",
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			}},
		})
	}))
	defer srv.Close()

	cache := newOIDCJWKSCache()
	keyFunc := cache.keyFunc(srv.URL)
	parse := func(kid string) error {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "test"})
		token.Header["kid"] = kid
		signed, err := token.SignedString(key)
		if err != nil {
			return err
		}
		_, err = jwt.Parse(signed, keyFunc, jwt.WithValidMethods([]string{"RS256"}))
		return err
	}

	if err := parse("known-kid"); err != nil {
		t.Fatalf("initial key validation failed: %v", err)
	}
	if err := parse("first-unknown-kid"); err == nil {
		t.Fatal("unknown kid was accepted")
	}
	if fetches != 2 {
		t.Fatalf("first unknown kid fetch count = %d, want 2", fetches)
	}
	for i := 0; i < 5; i++ {
		if err := parse("storm-unknown-kid"); err == nil {
			t.Fatal("storm kid was accepted")
		}
	}
	if fetches != 2 {
		t.Fatalf("unknown kid cooldown allowed extra fetches: got %d, want 2", fetches)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────

func containsQueryParam(rawURL, key, value string) bool {
	parts := stringsSplitN(rawURL, "?", 2)
	if len(parts) < 2 {
		return false
	}
	params, _ := url.ParseQuery(parts[1])
	return params.Get(key) == value
}

// mockStoreForOIDC is the standard mockStore from handler_test.go with
// OIDC methods overridden to actually store OIDC users.
type mockStoreForOIDC struct {
	mockStore
	oidcUsers map[string]*store.OIDCUser
}

func newMockStoreForOIDC() *mockStoreForOIDC {
	return &mockStoreForOIDC{
		oidcUsers: make(map[string]*store.OIDCUser),
	}
}

func (m *mockStoreForOIDC) UpsertOIDCUser(profile store.OIDCUserProfile) (string, error) {
	m.oidcUsers[profile.DesiredUsername] = &store.OIDCUser{
		Sub:               profile.Sub,
		ChatUsername:      profile.DesiredUsername,
		Email:             profile.Email,
		PreferredUsername: profile.PreferredUsername,
		Name:              profile.Name,
		Picture:           profile.Picture,
	}
	return profile.DesiredUsername, nil
}

func (m *mockStoreForOIDC) GetOIDCUserBySub(sub string) (*store.OIDCUser, error) {
	for _, u := range m.oidcUsers {
		if u.Sub == sub {
			return u, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockStoreForOIDC) GetOIDCUserByUsername(username string) (*store.OIDCUser, error) {
	if u, ok := m.oidcUsers[username]; ok {
		return u, nil
	}
	return nil, fmt.Errorf("not found")
}

// Avoid importing strings for simple helpers.
func stringsNewReader(s string) *stringsReaderImpl { return &stringsReaderImpl{s: s} }

type stringsReaderImpl struct {
	s string
	i int
}

func (r *stringsReaderImpl) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

func stringsSplitN(s, sep string, n int) []string {
	result := make([]string, 0, n)
	remaining := s
	for i := 0; i < n-1 && remaining != ""; i++ {
		idx := stringsIndex(remaining, sep)
		if idx < 0 {
			break
		}
		result = append(result, remaining[:idx])
		remaining = remaining[idx+len(sep):]
	}
	result = append(result, remaining)
	return result
}

func stringsIndex(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
