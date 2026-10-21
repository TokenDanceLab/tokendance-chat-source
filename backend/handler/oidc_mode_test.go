package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOIDCModeDisablesLocalCredentialHandlers(t *testing.T) {
	h := &Handler{oidcEnabled: true}
	tests := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{name: "register", method: http.MethodPost, path: "/api/register", handler: h.Register},
		{name: "login", method: http.MethodPost, path: "/api/login", handler: h.Login},
		{name: "invite generate", method: http.MethodPost, path: "/api/invite/generate", handler: h.InviteGenerate},
		{name: "invite list", method: http.MethodGet, path: "/api/invite/list", handler: h.InviteList},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.handler(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
		})
	}
}

func TestOIDCModeRejectsLocalSessionCookie(t *testing.T) {
	h := newTestHandler()
	token, err := h.issueSessionToken("legacy-local", "local")
	if err != nil {
		t.Fatalf("issueSessionToken failed: %v", err)
	}
	h.oidcEnabled = true

	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	if _, ok := h.sessionFromRequest(req); ok {
		t.Fatal("local session was accepted while OIDC mode was enabled")
	}
}
