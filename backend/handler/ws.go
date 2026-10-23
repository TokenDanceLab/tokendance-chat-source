package handler

import (
	"log"
	"net/http"

	"tokendancechat/backend/hub"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		if _, ok := allowedOrigin(r); ok {
			return true
		}
		log.Printf("ws: rejected origin %q for host %q", r.Header.Get("Origin"), r.Host)
		return false
	},
}

// HandleWebSocket handles GET /ws for WebSocket upgrade.
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Rate limit per-IP WebSocket upgrades.
	ip := requestIP(r)
	if !WSAllow(ip) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"too many connections, try again later","code":"RATE_LIMITED"}`))
		log.Printf("ws: rate limit blocked for %s", ip)
		return
	}

	// Reject if the hub is at capacity.
	if h.hub.IsFull() {
		http.Error(w, "server is full, please try again later", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade error: %v", err)
		return
	}

	client := hub.NewClient(h.hub, conn, ip)
	guestID := h.guestFromRequest(r)
	if guestID != "" {
		client.SetGuestID(guestID)
	} else if _, ok := h.sessionFromRequest(r); !ok {
		log.Printf("ws: guest upgrade without a valid guest cookie ip=%s", ip)
	}
	if session, ok := h.sessionFromRequest(r); ok {
		client.SetAuthenticatedUsername(session.Username)
		client.SetAuthenticatedSessionExpiry(session.Exp)
		if session.Kind == "oidc" {
			client.SetAuthenticatedSubject(session.Subject)
			client.SetAuthenticatedSessionGeneration(session.Generation)
			logoutStore, storeOK := h.store.(oidcLogoutStore)
			subject, generation := session.Subject, session.Generation
			if !storeOK {
				client.SetAuthenticatedSessionValidator(func() bool { return false })
			} else {
				client.SetAuthenticatedSessionValidator(func() bool {
					current, err := logoutStore.OIDCLogoutGeneration(subject)
					return err == nil && current == generation
				})
			}
		}
	}

	go client.WritePump()
	go client.ReadPump()
}
