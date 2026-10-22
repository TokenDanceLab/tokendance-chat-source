package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tokendancechat/backend/handler"
	"tokendancechat/backend/hub"
	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
	"tokendancechat/backend/websearch"
)

type authRuntimeConfig struct {
	OIDCEnabled               bool
	OIDCIssuer                string
	OIDCClientID              string
	OIDCClientSecret          string
	OIDCRedirectURI           string
	OIDCPostLogoutRedirectURI string
}

// Server creates and returns the configured HTTP server, store, and hub.
func Server(dbPath, frontendDist, addr string) (*http.Server, *store.Store, *hub.Hub, error) {
	authCfg, err := loadAuthRuntimeConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := handler.ValidateTurnstileConfig(); err != nil {
		return nil, nil, nil, err
	}

	// Read bot config from environment.
	botName := os.Getenv("CHAT_BOT_NAME")
	if botName == "" {
		botName = "TokenBot"
	}

	llmCfg, err := loadLLMConfig()
	if err != nil {
		return nil, nil, nil, err
	}

	st, err := store.New(dbPath)
	if err != nil {
		return nil, nil, nil, err
	}

	h := hub.New(st, llmCfg, botName)
	h.LoadPersistedState()

	// Server restart announcement: if there are existing messages, broadcast restart.
	existingCount := len(st.GetMessages(1, 0))
	if existingCount > 0 {
		st.InsertMessage("system", "服务器已重启 Server restarted", "", "", "", "", "")
		log.Printf("server restart announced (existing messages: %d)", existingCount)
	}

	go h.Run()

	dataDir := filepath.Dir(dbPath)
	uploadsDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return nil, nil, nil, err
	}
	emojiDir := filepath.Join(uploadsDir, "emojis")
	if err := os.MkdirAll(emojiDir, 0755); err != nil {
		return nil, nil, nil, err
	}

	hdlr := handler.New(h, st, uploadsDir)

	// OIDC setup (TokenDance Account integration).
	if err := hdlr.SetupOIDC(
		authCfg.OIDCEnabled,
		authCfg.OIDCClientID,
		authCfg.OIDCClientSecret,
		authCfg.OIDCIssuer,
		authCfg.OIDCRedirectURI,
		authCfg.OIDCPostLogoutRedirectURI,
	); err != nil {
		return nil, nil, nil, fmt.Errorf("OIDC setup failed: %w", err)
	}
	if authCfg.OIDCEnabled {
		log.Printf("oidc: enabled — issuer=%s client_id=%s confidential=%v", authCfg.OIDCIssuer, authCfg.OIDCClientID, authCfg.OIDCClientSecret != "")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", hdlr.HealthCheck)
	mux.HandleFunc("/api/config", hdlr.ConfigHandler)
	mux.HandleFunc("/api/session", hdlr.SessionHandler)
	mux.HandleFunc("/api/logout", hdlr.Logout)
	mux.HandleFunc("/api/logout/account", hdlr.AccountLogoutURL)
	mux.HandleFunc("/api/logout/account/callback", hdlr.AccountLogoutCallback)
	mux.HandleFunc("/api/messages", hdlr.GetMessages)
	mux.HandleFunc("/api/users/online", hdlr.GetOnlineUsers)
	mux.HandleFunc("/api/stats", hdlr.Stats)
	mux.HandleFunc("/api/link-preview", hdlr.LinkPreview)
	mux.HandleFunc("/api/emoji/upload", hdlr.UploadEmoji)
	mux.HandleFunc("/api/upload/image", hdlr.UploadImage)
	mux.HandleFunc("/api/search", hdlr.Search)
	mux.HandleFunc("/api/export", hdlr.ExportMessages)
	mux.HandleFunc("/api/register", hdlr.Register)
	mux.HandleFunc("/api/login", hdlr.Login)
	mux.HandleFunc("/api/invite/generate", hdlr.InviteGenerate)
	mux.HandleFunc("/api/invite/list", hdlr.InviteList)
	mux.HandleFunc("/uploads/emojis/", hdlr.ServeEmoji)
	mux.HandleFunc("/uploads/images/", hdlr.ServeImage)
	mux.HandleFunc("/ws", hdlr.HandleWebSocket)

	// OIDC routes (only registered when enabled).
	if authCfg.OIDCEnabled {
		mux.HandleFunc("/api/oidc/config", hdlr.OIDCConfigHandler)
		mux.HandleFunc("/api/oidc/login", hdlr.OIDCLogin)
		mux.HandleFunc("/api/oidc/callback", hdlr.OIDCCallback)
		mux.HandleFunc("/api/oidc/backchannel-logout", hdlr.OIDCBackchannelLogout)
	}

	fs := http.FileServer(http.Dir(frontendDist))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(r.URL.Path)

		// Retired routes must 404, not fall through to the SPA fallback (which
		// would serve index.html and make route removal look green). Check the
		// raw URL path: filepath.Clean is platform-dependent (backslashes on
		// Windows) and strips trailing slashes ("/uploads/" -> "/uploads").
		rawPath := r.URL.Path
		if rawPath == "/api" || strings.HasPrefix(rawPath, "/api/") ||
			rawPath == "/uploads" || strings.HasPrefix(rawPath, "/uploads/") {
			http.NotFound(w, r)
			return
		}

		// SPA fallback: root or empty path serves index.html.
		if cleanPath == "/" || cleanPath == "." {
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, filepath.Join(frontendDist, "index.html"))
			return
		}

		resolved := filepath.Join(frontendDist, cleanPath)

		// Path traversal guard: ensure resolved path is within frontendDist.
		absBase, err := filepath.Abs(frontendDist)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		absResolved, err := filepath.Abs(resolved)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(absResolved, absBase+string(filepath.Separator)) && absResolved != absBase {
			http.NotFound(w, r)
			return
		}

		// Serve file directly, fall back to index.html for SPA routes.
		if _, err := os.Stat(resolved); os.IsNotExist(err) {
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, filepath.Join(frontendDist, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	}))

	var srv http.Handler = mux
	srv = handler.BodyLimitMiddleware(srv)
	srv = handler.LoggingMiddleware(srv)
	srv = handler.RateLimitMiddleware(srv)
	srv = handler.SecurityHeadersMiddleware(srv)
	srv = handler.CORSMiddleware(srv)

	server := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	return server, st, h, nil
}

func loadAuthRuntimeConfig() (authRuntimeConfig, error) {
	oidcEnabled, err := envBool("CHAT_OIDC_ENABLED", false)
	if err != nil {
		return authRuntimeConfig{}, err
	}
	cookieSecure, err := envBool("CHAT_COOKIE_SECURE", true)
	if err != nil {
		return authRuntimeConfig{}, err
	}

	cfg := authRuntimeConfig{
		OIDCEnabled:               oidcEnabled,
		OIDCIssuer:                strings.TrimSpace(os.Getenv("CHAT_OIDC_ISSUER")),
		OIDCClientID:              strings.TrimSpace(os.Getenv("CHAT_OIDC_CLIENT_ID")),
		OIDCClientSecret:          strings.TrimSpace(os.Getenv("CHAT_OIDC_CLIENT_SECRET")),
		OIDCRedirectURI:           strings.TrimSpace(os.Getenv("CHAT_OIDC_REDIRECT_URI")),
		OIDCPostLogoutRedirectURI: strings.TrimSpace(os.Getenv("CHAT_OIDC_POST_LOGOUT_REDIRECT_URI")),
	}
	if !oidcEnabled {
		return cfg, nil
	}
	if strings.TrimSpace(os.Getenv("CHAT_SESSION_SECRET")) == "" {
		return authRuntimeConfig{}, fmt.Errorf("CHAT_SESSION_SECRET is required when OIDC is enabled")
	}
	if cfg.OIDCClientID == "" || cfg.OIDCClientSecret == "" || cfg.OIDCIssuer == "" || cfg.OIDCRedirectURI == "" {
		return authRuntimeConfig{}, fmt.Errorf("CHAT_OIDC_CLIENT_ID, CHAT_OIDC_CLIENT_SECRET, CHAT_OIDC_ISSUER, and CHAT_OIDC_REDIRECT_URI are required when OIDC is enabled")
	}
	if cfg.OIDCPostLogoutRedirectURI == "" {
		derived, err := derivePostLogoutRedirectURI(cfg.OIDCRedirectURI)
		if err != nil {
			return authRuntimeConfig{}, fmt.Errorf("derive CHAT_OIDC_POST_LOGOUT_REDIRECT_URI: %w", err)
		}
		cfg.OIDCPostLogoutRedirectURI = derived
	}
	if !cookieSecure && (!isLoopbackURL(cfg.OIDCIssuer) || !isLoopbackURL(cfg.OIDCRedirectURI)) {
		return authRuntimeConfig{}, fmt.Errorf("CHAT_COOKIE_SECURE=false is only allowed when CHAT_OIDC_ISSUER and CHAT_OIDC_REDIRECT_URI are loopback URLs")
	}
	return cfg, nil
}

func derivePostLogoutRedirectURI(redirectURI string) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("redirect URI is invalid")
	}
	u.Path = "/api/logout/account/callback"
	u.RawPath = ""
	u.RawQuery = ""
	return u.String(), nil
}

func envBool(name string, fallback bool) (bool, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return fallback, nil
	}
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false, got %q", name, os.Getenv(name))
	}
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// parseEnvBool is retained for non-security feature parsing. Security-sensitive
// flags must use envBool so a typo cannot silently disable the feature.
func parseEnvBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// loadLLMConfig reads the OpenAI-compatible endpoint configuration. Any
// provider that exposes /chat/completions (OpenAI, DeepSeek, a gateway) works;
// the provider value is a label, not a protocol switch.
func loadLLMConfig() (*llm.Config, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("CHAT_LLM_PROVIDER")))
	if provider == "" {
		return nil, nil
	}
	model := strings.TrimSpace(os.Getenv("CHAT_LLM_MODEL"))
	apiKey := strings.TrimSpace(os.Getenv("CHAT_LLM_API_KEY"))
	if model == "" || apiKey == "" {
		return nil, fmt.Errorf("CHAT_LLM_PROVIDER=%s requires CHAT_LLM_MODEL and CHAT_LLM_API_KEY", provider)
	}

	cfg := &llm.Config{
		Provider: provider,
		APIKey:   apiKey,
		Model:    model,
		BaseURL:  strings.TrimSpace(os.Getenv("CHAT_LLM_BASE_URL")),
	}

	var err error
	if cfg.MaxTokens, err = envInt("CHAT_LLM_MAX_TOKENS"); err != nil {
		return nil, err
	}
	if cfg.ContextMessages, err = envInt("CHAT_LLM_MEMORY_SIZE"); err != nil {
		return nil, err
	}
	if cfg.MaxToolRounds, err = envInt("CHAT_LLM_MAX_TOOL_ROUNDS"); err != nil {
		return nil, err
	}
	if cfg.Temperature, err = envFloat("CHAT_LLM_TEMPERATURE"); err != nil {
		return nil, err
	}
	if cfg.TopP, err = envFloat("CHAT_LLM_TOP_P"); err != nil {
		return nil, err
	}
	if cfg.Timeout, err = envDuration("CHAT_LLM_TIMEOUT"); err != nil {
		return nil, err
	}
	cfg.ReasoningEffort = strings.ToLower(strings.TrimSpace(os.Getenv("CHAT_LLM_REASONING_EFFORT")))
	switch cfg.ReasoningEffort {
	case "", "low", "medium", "high":
	default:
		return nil, fmt.Errorf("CHAT_LLM_REASONING_EFFORT must be low, medium, or high, got %q", cfg.ReasoningEffort)
	}
	if cfg.WebSearch, err = loadWebSearchConfig(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadWebSearchConfig() (*websearch.Config, error) {
	raw := strings.TrimSpace(os.Getenv("CHAT_WEB_SEARCH_ENABLED"))
	if raw == "" || raw == "false" {
		return nil, nil
	}
	if raw != "true" {
		return nil, fmt.Errorf("CHAT_WEB_SEARCH_ENABLED must be true or false")
	}
	cfg := &websearch.Config{BaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("CHAT_WEB_SEARCH_BASE_URL")), "/"), APIKey: strings.TrimSpace(os.Getenv("CHAT_WEB_SEARCH_API_KEY")), Timeout: 15 * time.Second, DailyMicroCNY: 5000000}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.deepseek.com/v1"
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, "/v1") {
		return nil, fmt.Errorf("CHAT_WEB_SEARCH_BASE_URL must be an HTTPS API base ending in /v1")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("CHAT_WEB_SEARCH_API_KEY is required when web search is enabled")
	}
	if value := strings.TrimSpace(os.Getenv("CHAT_WEB_SEARCH_DAILY_MICRO_CNY")); value != "" {
		amount, e := strconv.ParseInt(value, 10, 64)
		if e != nil || amount < websearch.PriceMicroCNY || amount > 1000000000 {
			return nil, fmt.Errorf("CHAT_WEB_SEARCH_DAILY_MICRO_CNY must be between 5000 and 1000000000")
		}
		cfg.DailyMicroCNY = amount
	}
	return cfg, nil
}

func envInt(name string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", name, raw)
	}
	return value, nil
}

func envFloat(name string) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number, got %q", name, raw)
	}
	return value, nil
}

func envDuration(name string) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 90s or 2m, got %q", name, raw)
	}
	return value, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	loadEnvFiles()
	log.Println("starting TokenDanceChat backend...")

	dbPath := filepath.Join("..", "data", "chat.db")
	if envPath := os.Getenv("CHAT_DB_PATH"); envPath != "" {
		dbPath = envPath
	}

	frontendDist := os.Getenv("CHAT_FRONTEND_DIR")
	if frontendDist == "" {
		frontendDist = filepath.Join("..", "frontend", "dist")
	}

	addr := ":8080"
	if envAddr := os.Getenv("CHAT_ADDR"); envAddr != "" {
		addr = envAddr
	}

	server, st, h, err := Server(dbPath, frontendDist, addr)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	defer st.Close()

	go func() {
		log.Printf("listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")

	// Shut down HTTP server first to stop accepting new connections
	// and drain in-flight requests, then close hub connections.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("server exited gracefully")

	h.Shutdown()
	log.Println("hub shut down")
}
