package handler

import (
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tokendancechat/backend/hub"
	"tokendancechat/backend/store"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

func issueAndReadOIDCCookie(t *testing.T, h *Handler, username, subject string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := h.setOIDCSessionCookie(rec, username, subject); err != nil {
		t.Fatalf("setOIDCSessionCookie failed: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName {
		t.Fatalf("session cookies = %#v", cookies)
	}
	return cookies[0]
}

func signTestLogoutToken(
	t *testing.T,
	key *rsa.PrivateKey,
	kid, issuer, subject, jti string,
	now time.Time,
) string {
	t.Helper()
	return signTestIDToken(t, key, kid, jwt.MapClaims{
		"iss": issuer,
		"sub": subject,
		"aud": "test-client",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
		"jti": jti,
		"events": map[string]any{
			oidcBackchannelLogoutEvent: map[string]any{},
		},
	})
}

func newOIDCLogoutHandler(t *testing.T) (*Handler, *store.Store, *rsa.PrivateKey, string, *httptest.Server) {
	t.Helper()
	key := generateTestRSAKey(t)
	provider := startMockOIDCProvider(t, key, "logout-test-kid")
	t.Cleanup(provider.Close)

	st, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := hub.New(st, nil, "")
	go h.Run()
	t.Cleanup(h.Shutdown)
	hdlr := New(h, st, t.TempDir())
	if err := hdlr.SetupOIDC(
		true,
		"test-client",
		"test-secret",
		provider.URL,
		"http://localhost:8080/api/oidc/callback",
		"http://localhost:8080/api/logout/account/callback",
	); err != nil {
		t.Fatalf("SetupOIDC failed: %v", err)
	}
	t.Cleanup(hdlr.closeOIDCStores)
	return hdlr, st, key, "logout-test-kid", provider
}

func TestOIDCBackchannelLogoutReplayAndSessionGeneration(t *testing.T) {
	h, st, key, kid, provider := newOIDCLogoutHandler(t)
	username, err := st.UpsertOIDCUser(store.OIDCUserProfile{
		Sub:             "account-sub",
		DesiredUsername: "alice",
		Name:            "Alice",
	})
	if err != nil {
		t.Fatalf("UpsertOIDCUser failed: %v", err)
	}
	cookie := issueAndReadOIDCCookie(t, h, username, "account-sub")
	reqWithCookie := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	reqWithCookie.AddCookie(cookie)
	if _, ok := h.sessionFromRequest(reqWithCookie); !ok {
		t.Fatal("fresh OIDC cookie was not accepted")
	}

	now := time.Now().UTC()
	token := signTestLogoutToken(t, key, kid, provider.URL, "account-sub", "logout-jti-1", now)
	postLogoutToken := func(raw string) *httptest.ResponseRecorder {
		form := url.Values{"logout_token": {raw}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/api/oidc/backchannel-logout", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.OIDCBackchannelLogout(rec, req)
		return rec
	}

	if rec := postLogoutToken(token); rec.Code != http.StatusNoContent {
		t.Fatalf("first logout status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := h.sessionFromRequest(reqWithCookie); ok {
		t.Fatal("OIDC cookie remained valid after subject generation changed")
	}
	generation, err := st.OIDCLogoutGeneration("account-sub")
	if err != nil || generation != 1 {
		t.Fatalf("generation = %d, err=%v; want 1", generation, err)
	}

	if rec := postLogoutToken(token); rec.Code != http.StatusNoContent {
		t.Fatalf("idempotent retry status = %d body=%s", rec.Code, rec.Body.String())
	}
	generation, err = st.OIDCLogoutGeneration("account-sub")
	if err != nil || generation != 1 {
		t.Fatalf("generation after retry = %d, err=%v; want 1", generation, err)
	}

	changed := signTestLogoutToken(t, key, kid, provider.URL, "different-sub", "logout-jti-1", now.Add(time.Second))
	if rec := postLogoutToken(changed); rec.Code != http.StatusBadRequest {
		t.Fatalf("changed claims status = %d body=%s", rec.Code, rec.Body.String())
	}
	generation, err = st.OIDCLogoutGeneration("account-sub")
	if err != nil || generation != 1 {
		t.Fatalf("generation changed after JTI reuse rejection: %d, err=%v", generation, err)
	}

	unknown := signTestLogoutToken(t, key, kid, provider.URL, "unknown-sub", "logout-jti-unknown", now.Add(2*time.Second))
	if rec := postLogoutToken(unknown); rec.Code != http.StatusNoContent {
		t.Fatalf("unknown subject status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOIDCBackchannelLogoutRejectsStaleCookieAtWebSocketJoin(t *testing.T) {
	h, st, key, kid, provider := newOIDCLogoutHandler(t)
	username, err := st.UpsertOIDCUser(store.OIDCUserProfile{
		Sub:             "account-ws-sub",
		DesiredUsername: "alicews",
		Name:            "Alice WS",
	})
	if err != nil {
		t.Fatalf("UpsertOIDCUser failed: %v", err)
	}
	cookie := issueAndReadOIDCCookie(t, h, username, "account-ws-sub")
	now := time.Now().UTC()
	token := signTestLogoutToken(t, key, kid, provider.URL, "account-ws-sub", "logout-jti-ws", now)
	form := url.Values{"logout_token": {token}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/api/oidc/backchannel-logout", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.OIDCBackchannelLogout(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d body=%s", rec.Code, rec.Body.String())
	}

	server := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(hub.Message{Type: "join", Username: username}); err != nil {
		t.Fatalf("write join: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var msg hub.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read join response: %v", err)
	}
	if msg.Type != "error" || msg.ErrorCode != "OIDC_REQUIRED" {
		t.Fatalf("join response = %#v; want OIDC_REQUIRED", msg)
	}
}

func TestOIDCBackchannelLogoutDisconnectsLiveWebSocket(t *testing.T) {
	h, st, key, kid, provider := newOIDCLogoutHandler(t)
	username, err := st.UpsertOIDCUser(store.OIDCUserProfile{
		Sub:             "account-live-ws-sub",
		DesiredUsername: "alivelive",
		Name:            "Alice Live",
	})
	if err != nil {
		t.Fatalf("UpsertOIDCUser failed: %v", err)
	}
	cookie := issueAndReadOIDCCookie(t, h, username, "account-live-ws-sub")

	server := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(hub.Message{Type: "join", Username: username}); err != nil {
		t.Fatalf("write join: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var joined hub.Message
	if err := conn.ReadJSON(&joined); err != nil {
		t.Fatalf("read join response: %v", err)
	}
	if joined.Type != "history" {
		t.Fatalf("join response = %#v; want history", joined)
	}

	now := time.Now().UTC()
	token := signTestLogoutToken(t, key, kid, provider.URL, "account-live-ws-sub", "logout-live-ws", now)
	form := url.Values{"logout_token": {token}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/api/oidc/backchannel-logout", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.OIDCBackchannelLogout(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d body=%s", rec.Code, rec.Body.String())
	}

	for i := 0; i < 12; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var event hub.Message
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read account logout event: %v", err)
		}
		if event.Type == "account_logout" {
			return
		}
	}
	t.Fatal("live socket did not receive account_logout")
}

func TestOIDCBackchannelLogoutInvalidatesPreJoinWebSocket(t *testing.T) {
	h, st, key, kid, provider := newOIDCLogoutHandler(t)
	username, err := st.UpsertOIDCUser(store.OIDCUserProfile{
		Sub:             "account-prejoin-sub",
		DesiredUsername: "prejoinalice",
		Name:            "Prejoin Alice",
	})
	if err != nil {
		t.Fatalf("UpsertOIDCUser failed: %v", err)
	}
	cookie := issueAndReadOIDCCookie(t, h, username, "account-prejoin-sub")

	server := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	now := time.Now().UTC()
	token := signTestLogoutToken(t, key, kid, provider.URL, "account-prejoin-sub", "logout-prejoin", now)
	form := url.Values{"logout_token": {token}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/api/oidc/backchannel-logout", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.OIDCBackchannelLogout(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d body=%s", rec.Code, rec.Body.String())
	}

	if err := conn.WriteJSON(hub.Message{Type: "join", Username: username}); err != nil {
		t.Fatalf("write join: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event hub.Message
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read join response: %v", err)
	}
	if event.Type != "error" || event.ErrorCode != "OIDC_REQUIRED" {
		t.Fatalf("pre-join response = %#v; want OIDC_REQUIRED", event)
	}
}

func TestOIDCBackchannelLogoutRequestBoundaries(t *testing.T) {
	h := newTestHandler()
	h.oidcEnabled = true

	tests := []struct {
		name        string
		method      string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "method", method: http.MethodGet, wantStatus: http.StatusMethodNotAllowed},
		{name: "content type", method: http.MethodPost, contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "content type parameter", method: http.MethodPost, contentType: "application/x-www-form-urlencoded; charset=utf-8", body: "logout_token=x", wantStatus: http.StatusBadRequest},
		{name: "missing token", method: http.MethodPost, contentType: "application/x-www-form-urlencoded", body: "", wantStatus: http.StatusBadRequest},
		{name: "duplicate token", method: http.MethodPost, contentType: "application/x-www-form-urlencoded", body: "logout_token=a&logout_token=b", wantStatus: http.StatusBadRequest},
		{name: "oversized token body", method: http.MethodPost, contentType: "application/x-www-form-urlencoded", body: "logout_token=" + strings.Repeat("x", oidcMaxBackchannelBodyBytes), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/oidc/backchannel-logout", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()
			h.OIDCBackchannelLogout(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s; want %d", rec.Code, rec.Body.String(), tc.wantStatus)
			}
			if tc.wantStatus == http.StatusMethodNotAllowed && rec.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("Allow = %q", rec.Header().Get("Allow"))
			}
		})
	}
}

func TestValidateLogoutTokenClaimBoundaries(t *testing.T) {
	h, _, key, kid, provider := newOIDCLogoutHandler(t)
	now := time.Now().UTC()
	baseClaims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": provider.URL,
			"sub": "claim-boundary-sub",
			"aud": "test-client",
			"iat": now.Unix(),
			"exp": now.Add(time.Hour).Unix(),
			"jti": "claim-boundary-jti",
			"events": map[string]any{
				oidcBackchannelLogoutEvent: map[string]any{},
			},
		}
	}

	tests := []struct {
		name    string
		mutate  func(jwt.MapClaims)
		wantErr bool
	}{
		{name: "valid"},
		{name: "wrong issuer", mutate: func(c jwt.MapClaims) { c["iss"] = "https://wrong.example.test" }, wantErr: true},
		{name: "wrong audience", mutate: func(c jwt.MapClaims) { c["aud"] = "other-client" }, wantErr: true},
		{name: "missing subject", mutate: func(c jwt.MapClaims) { delete(c, "sub") }, wantErr: true},
		{name: "missing jti", mutate: func(c jwt.MapClaims) { delete(c, "jti") }, wantErr: true},
		{name: "nonce", mutate: func(c jwt.MapClaims) { c["nonce"] = "forbidden" }, wantErr: true},
		{name: "sid", mutate: func(c jwt.MapClaims) { c["sid"] = "forbidden" }, wantErr: true},
		{
			name: "stale",
			mutate: func(c jwt.MapClaims) {
				c["iat"] = now.Add(-oidcLogoutTokenMaxAge - time.Minute).Unix()
			},
			wantErr: true,
		},
		{
			name: "excessive lifetime",
			mutate: func(c jwt.MapClaims) {
				c["exp"] = now.Add(2*oidcLogoutTokenMaxAge + time.Minute).Unix()
			},
			wantErr: true,
		},
		{name: "missing events", mutate: func(c jwt.MapClaims) { delete(c, "events") }, wantErr: true},
		{
			name: "wrong event",
			mutate: func(c jwt.MapClaims) {
				c["events"] = map[string]any{"https://example.test/other": map[string]any{}}
			},
			wantErr: true,
		},
		{
			name: "non-empty event",
			mutate: func(c jwt.MapClaims) {
				c["events"] = map[string]any{
					oidcBackchannelLogoutEvent: map[string]any{"unexpected": true},
				}
			},
			wantErr: true,
		},
		{
			name: "null event",
			mutate: func(c jwt.MapClaims) {
				c["events"] = map[string]any{oidcBackchannelLogoutEvent: nil}
			},
			wantErr: true,
		},
		{
			name: "extra event",
			mutate: func(c jwt.MapClaims) {
				c["events"] = map[string]any{
					oidcBackchannelLogoutEvent:   map[string]any{},
					"https://example.test/other": map[string]any{},
				}
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claims := baseClaims()
			if tc.mutate != nil {
				tc.mutate(claims)
			}
			raw := signTestIDToken(t, key, kid, claims)
			_, err := h.validateLogoutToken(raw)
			if tc.wantErr && err == nil {
				t.Fatal("validateLogoutToken accepted invalid claims")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateLogoutToken rejected valid claims: %v", err)
			}
		})
	}
}

func TestLogoutRequiresSameOriginAndCSRF(t *testing.T) {
	h := newTestHandler()
	token, err := h.issueSessionToken("alice", "local")
	if err != nil {
		t.Fatalf("issueSessionToken failed: %v", err)
	}
	session, ok := h.verifySessionToken(token)
	if !ok {
		t.Fatal("test session token is invalid")
	}

	tests := []struct {
		name       string
		origin     string
		csrf       string
		wantStatus int
	}{
		{name: "cross origin", origin: "https://evil.example", csrf: h.sessionCSRFToken(session), wantStatus: http.StatusForbidden},
		{name: "cross scheme", origin: "http://chat.example.test", csrf: h.sessionCSRFToken(session), wantStatus: http.StatusForbidden},
		{name: "missing csrf", origin: "https://chat.example.test", wantStatus: http.StatusForbidden},
		{name: "wrong csrf", origin: "https://chat.example.test", csrf: "wrong", wantStatus: http.StatusForbidden},
		{name: "same origin", origin: "https://chat.example.test", csrf: h.sessionCSRFToken(session), wantStatus: http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://chat.example.test/api/logout", nil)
			req.Host = "chat.example.test"
			req.Header.Set("Origin", tc.origin)
			if tc.csrf != "" {
				req.Header.Set("X-CSRF-Token", tc.csrf)
			}
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			rec := httptest.NewRecorder()
			h.Logout(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s; want %d", rec.Code, rec.Body.String(), tc.wantStatus)
			}
			if tc.wantStatus == http.StatusNoContent {
				cookies := rec.Result().Cookies()
				if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
					t.Fatalf("session cookie was not cleared: %#v", cookies)
				}
			}
		})
	}
}

func TestTrustedProxyHTTPSAllowsSameOriginLogout(t *testing.T) {
	t.Setenv("CHAT_TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	h := newTestHandler()
	token, err := h.issueSessionToken("alice", "local")
	if err != nil {
		t.Fatalf("issueSessionToken failed: %v", err)
	}
	session, ok := h.verifySessionToken(token)
	if !ok {
		t.Fatal("test session token is invalid")
	}

	req := httptest.NewRequest(http.MethodPost, "http://chat.example.test/api/logout", nil)
	req.Host = "chat.example.test"
	req.RemoteAddr = "127.0.0.1:4000"
	req.Header.Set("Origin", "https://chat.example.test")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-CSRF-Token", h.sessionCSRFToken(session))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.Logout(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s; want 204", rec.Code, rec.Body.String())
	}
}

func TestSessionHandlerReturnsDerivedCSRFWithoutSessionNonce(t *testing.T) {
	h := newTestHandler()
	token, err := h.issueSessionToken("alice", "local")
	if err != nil {
		t.Fatalf("issueSessionToken failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.SessionHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if body["authenticated"] != true || body["csrf_token"] == "" {
		t.Fatalf("session response = %#v", body)
	}
	if _, leaked := body["nonce"]; leaked {
		t.Fatal("session response exposed the nonce")
	}
}

func TestAccountLogoutReturnsDiscoveryURLWithoutTokens(t *testing.T) {
	ms := newMockStoreForOIDC()
	h := hub.New(ms, nil, "")
	go h.Run()
	t.Cleanup(h.Shutdown)
	hdlr := New(h, ms, t.TempDir())
	key := generateTestRSAKey(t)
	provider := startMockOIDCProvider(t, key, "account-logout-kid")
	defer provider.Close()
	if err := hdlr.SetupOIDC(
		true,
		"test-client",
		"test-secret",
		provider.URL,
		"http://localhost:8080/api/oidc/callback",
		"http://localhost:8080/api/logout/account/callback",
	); err != nil {
		t.Fatalf("SetupOIDC failed: %v", err)
	}
	defer hdlr.closeOIDCStores()

	cookie := issueAndReadOIDCCookie(t, hdlr, "alice", "account-logout-sub")
	reqSession := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	reqSession.AddCookie(cookie)
	session, ok := hdlr.sessionFromRequest(reqSession)
	if !ok {
		t.Fatal("OIDC session was not valid")
	}

	req := httptest.NewRequest(http.MethodPost, "http://chat.example.test/api/logout/account", nil)
	req.Host = "chat.example.test"
	req.Header.Set("Origin", "http://chat.example.test")
	req.Header.Set("X-CSRF-Token", hdlr.sessionCSRFToken(session))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	hdlr.AccountLogoutURL(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	redirectURL, err := url.Parse(body.RedirectURL)
	if err != nil {
		t.Fatalf("redirect URL: %v", err)
	}
	if redirectURL.Path != "/end-session" ||
		redirectURL.Query().Get("client_id") != "test-client" ||
		redirectURL.Query().Get("post_logout_redirect_uri") != "http://localhost:8080/api/logout/account/callback" ||
		redirectURL.Query().Get("state") == "" {
		t.Fatalf("unexpected Account logout URL: %s", body.RedirectURL)
	}
	for _, forbidden := range []string{"id_token", "access_token", "refresh_token", "client_secret"} {
		if strings.Contains(body.RedirectURL, forbidden) {
			t.Fatalf("logout URL contains %s", forbidden)
		}
	}
	var clearedSession, logoutBinding *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		switch {
		case cookie.Name == sessionCookieName && cookie.MaxAge < 0:
			clearedSession = cookie
		case cookie.Name == accountLogoutCookieNameSecure && cookie.MaxAge > 0:
			logoutBinding = cookie
		}
	}
	if clearedSession == nil || logoutBinding == nil {
		t.Fatalf("logout cookies = %#v", rec.Result().Cookies())
	}
	if logoutBinding.Value != redirectURL.Query().Get("state") ||
		!logoutBinding.HttpOnly || !logoutBinding.Secure ||
		logoutBinding.SameSite != http.SameSiteLaxMode ||
		logoutBinding.Path != "/" ||
		logoutBinding.MaxAge != int(accountLogoutStateTTL.Seconds()) {
		t.Fatalf("logout binding = %#v", logoutBinding)
	}
}

func TestAccountLogoutCallbackBindsStateToBrowserAndConsumesIt(t *testing.T) {
	hdlr, _, _, _, _ := newOIDCLogoutHandler(t)
	cookie := issueAndReadOIDCCookie(t, hdlr, "alice", "account-logout-callback-sub")
	reqSession := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	reqSession.AddCookie(cookie)
	session, ok := hdlr.sessionFromRequest(reqSession)
	if !ok {
		t.Fatal("OIDC session was not valid")
	}

	logout := httptest.NewRequest(http.MethodPost, "http://chat.example.test/api/logout/account", nil)
	logout.Host = "chat.example.test"
	logout.Header.Set("Origin", "http://chat.example.test")
	logout.Header.Set("X-CSRF-Token", hdlr.sessionCSRFToken(session))
	logout.AddCookie(cookie)
	logoutResponse := httptest.NewRecorder()
	hdlr.AccountLogoutURL(logoutResponse, logout)
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logoutResponse.Code, logoutResponse.Body.String())
	}
	var body struct {
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.NewDecoder(logoutResponse.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	logoutURL, err := url.Parse(body.RedirectURL)
	if err != nil {
		t.Fatal(err)
	}
	state := logoutURL.Query().Get("state")
	var binding *http.Cookie
	for _, candidate := range logoutResponse.Result().Cookies() {
		if candidate.Name == accountLogoutCookieNameSecure {
			binding = candidate
		}
	}
	if binding == nil || binding.Value != state {
		t.Fatalf("logout binding = %#v", binding)
	}

	callback := httptest.NewRequest(
		http.MethodGet,
		"/api/logout/account/callback?state="+url.QueryEscape(state),
		nil,
	)
	callback.AddCookie(binding)
	callbackResponse := httptest.NewRecorder()
	hdlr.AccountLogoutCallback(callbackResponse, callback)
	if callbackResponse.Code != http.StatusFound ||
		callbackResponse.Header().Get("Location") != "/?oidc_logged_out=1" {
		t.Fatalf(
			"callback status=%d location=%q body=%s",
			callbackResponse.Code,
			callbackResponse.Header().Get("Location"),
			callbackResponse.Body.String(),
		)
	}
	cleared := false
	for _, candidate := range callbackResponse.Result().Cookies() {
		if candidate.Name == accountLogoutCookieNameSecure && candidate.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("logout binding was not cleared: %#v", callbackResponse.Result().Cookies())
	}

	replay := httptest.NewRequest(
		http.MethodGet,
		"/api/logout/account/callback?state="+url.QueryEscape(state),
		nil,
	)
	replayResponse := httptest.NewRecorder()
	hdlr.AccountLogoutCallback(replayResponse, replay)
	if replayResponse.Code != http.StatusFound ||
		replayResponse.Header().Get("Location") != "/?oidc_logout_error=1" {
		t.Fatalf("replay status=%d location=%q", replayResponse.Code, replayResponse.Header().Get("Location"))
	}
}

func TestAccountLogoutCallbackRejectsMissingMismatchedAndDuplicateState(t *testing.T) {
	hdlr, _, _, _, _ := newOIDCLogoutHandler(t)
	valid := strings.Repeat("a", 43)
	tests := []struct {
		name   string
		query  string
		cookie string
	}{
		{name: "missing", query: "", cookie: valid},
		{name: "missing cookie", query: "?state=" + valid, cookie: ""},
		{name: "mismatch", query: "?state=" + strings.Repeat("b", 43), cookie: valid},
		{name: "duplicate", query: "?state=" + valid + "&state=" + valid, cookie: valid},
		{name: "short", query: "?state=short", cookie: "short"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/logout/account/callback"+test.query, nil)
			if test.cookie != "" {
				request.AddCookie(&http.Cookie{
					Name:  accountLogoutCookieNameSecure,
					Value: test.cookie,
				})
			}
			response := httptest.NewRecorder()
			hdlr.AccountLogoutCallback(response, request)
			if response.Code != http.StatusFound ||
				response.Header().Get("Location") != "/?oidc_logout_error=1" {
				t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
			}
		})
	}
}
