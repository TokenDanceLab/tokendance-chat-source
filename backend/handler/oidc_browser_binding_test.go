package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func requestWithLoginCookies(target string, login *httptest.ResponseRecorder) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	for _, cookie := range login.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			r.AddCookie(cookie)
		}
	}
	return r
}

func hasAuthenticatedSessionCookie(rec *httptest.ResponseRecorder) bool {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" && cookie.MaxAge > 0 {
			return true
		}
	}
	return false
}

func TestOIDCCallbackRequiresInitiatingBrowser(t *testing.T) {
	for _, mode := range []string{"missing", "wrong"} {
		t.Run(mode, func(t *testing.T) {
			ResetRateLimiter()
			key := generateTestRSAKey(t)
			provider := startMockOIDCProvider(t, key, "browser-binding")
			defer provider.Close()
			h := &Handler{store: newMockStoreForOIDC(), cookieSecure: true}
			setupOIDCForTest(t, h, provider.URL)
			login := httptest.NewRecorder()
			h.OIDCLogin(login, httptest.NewRequest(http.MethodGet, "/api/oidc/login", nil))
			auth, err := url.Parse(login.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			mockIDTokenNonce = auth.Query().Get("nonce")
			target := "/api/oidc/callback?" + url.Values{
				"iss": {provider.URL}, "code": {"test-code"}, "state": {auth.Query().Get("state")},
			}.Encode()
			request := httptest.NewRequest(http.MethodGet, target, nil)
			if mode == "wrong" {
				for _, cookie := range login.Result().Cookies() {
					cookie.Value = "another-browser"
					request.AddCookie(cookie)
				}
			}
			rejected := httptest.NewRecorder()
			h.OIDCCallback(rejected, request)
			if !strings.Contains(rejected.Header().Get("Location"), "oidc_error=invalid_state") || hasAuthenticatedSessionCookie(rejected) {
				t.Fatal("callback from a different browser was not rejected")
			}
			if lastTokenForm != nil {
				t.Fatal("unbound callback reached token endpoint")
			}
			valid := httptest.NewRecorder()
			h.OIDCCallback(valid, requestWithLoginCookies(target, login))
			if !hasAuthenticatedSessionCookie(valid) {
				t.Fatal("foreign callback consumed the initiating browser's login")
			}
			replay := httptest.NewRecorder()
			h.OIDCCallback(replay, requestWithLoginCookies(target, login))
			if hasAuthenticatedSessionCookie(replay) {
				t.Fatal("callback replay issued an authenticated session")
			}
		})
	}
}

func TestOIDCCancelConsumesOnlyBoundState(t *testing.T) {
	ResetRateLimiter()
	provider := startMockOIDCProvider(t, generateTestRSAKey(t), "cancel-binding")
	defer provider.Close()
	h := &Handler{store: newMockStoreForOIDC(), cookieSecure: true}
	setupOIDCForTest(t, h, provider.URL)
	login := httptest.NewRecorder()
	h.OIDCLogin(login, httptest.NewRequest(http.MethodGet, "/api/oidc/login", nil))
	auth, _ := url.Parse(login.Header().Get("Location"))
	target := "/api/oidc/callback?" + url.Values{
		"iss": {provider.URL}, "error": {"access_denied"}, "state": {auth.Query().Get("state")},
	}.Encode()
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, requestWithLoginCookies(target, login))
	if !strings.Contains(rec.Header().Get("Location"), "oidc_error=access_denied") {
		t.Fatal("bound cancellation was not returned")
	}
	if len(h.oidcStates.store) != 0 {
		t.Fatal("cancelled authorization left a live state")
	}
	replay := httptest.NewRecorder()
	h.OIDCCallback(replay, requestWithLoginCookies(target, login))
	if !strings.Contains(replay.Header().Get("Location"), "oidc_error=invalid_state") {
		t.Fatal("cancelled state could be replayed")
	}
}
