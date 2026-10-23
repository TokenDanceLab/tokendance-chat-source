package websearch

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeepSeekSDKSearchFetchContract(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-tool-key" {
			t.Errorf("incorrect method/auth")
		}
		if r.Header.Get("Cookie") != "" {
			t.Error("cookie forwarded")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("bad JSON")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/tools/search":
			if body["query"] != "release" || body["count"] != float64(2) || body["need_content"] != false || len(body) != 3 {
				t.Errorf("search payload: %#v", body)
			}
			_, _ = w.Write([]byte(`{"results":[{"title":"Official","url":"https://example.com/release","summary":"Discovery summary","content":"must not be treated as fetched"}]}`))
		case "/v1/tools/fetch":
			if body["url"] != "https://example.com/release" || len(body) != 1 {
				t.Errorf("fetch payload: %#v", body)
			}
			_, _ = w.Write([]byte(`{"title":"Official","content":"# Release\nEvidence"}`))
		default:
			t.Errorf("wrong path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL + "/v1", APIKey: "test-tool-key", Timeout: time.Second})
	hits, err := p.Search(context.Background(), "release", 2)
	if err != nil || len(hits) != 1 || hits[0].Summary != "Discovery summary" {
		t.Fatalf("search %#v %v", hits, err)
	}
	page, err := p.Fetch(context.Background(), hits[0].URL)
	if err != nil || !strings.Contains(page.Content, "Evidence") {
		t.Fatalf("fetch %#v %v", page, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("%d calls", calls.Load())
	}
}

func TestProviderLimitsErrorsAndNoRetries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		serve    func(http.ResponseWriter, *http.Request)
		unbilled bool
	}{
		{"upstream_error", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"message":"private-upstream-input","type":"server_error"}}`))
		}, false},
		{"compressed_oversize", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "application/json")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(`{"content":"` + strings.Repeat("x", MaxResponseBytes*2) + `"}`))
			_ = gz.Close()
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); tc.serve(w, r) }))
			defer srv.Close()
			p := New(Config{BaseURL: srv.URL + "/v1", APIKey: "test", Timeout: time.Second})
			_, err := p.Fetch(context.Background(), "https://example.com")
			var webErr *Error
			if !errors.As(err, &webErr) || webErr.KnownUnbilled != tc.unbilled || strings.Contains(err.Error(), "private") {
				t.Fatalf("uncontrolled error: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatal("implicit retry")
			}
		})
	}
}

func TestProviderRejectsRedirectAndCancellation(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL + "/v1", Timeout: time.Second})
	if _, err := p.Fetch(context.Background(), "https://example.com"); err == nil {
		t.Fatal("redirect accepted")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("redirect followed with credentials")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Search(ctx, "query", 1)
	var webErr *Error
	if !errors.As(err, &webErr) || webErr.KnownUnbilled {
		t.Fatalf("cancel must retain unknown charge: %v", err)
	}
}

func TestAmbiguousProviderResponsesRetainReservation(t *testing.T) {
	for _, tc := range []struct {
		status   int
		body     string
		unbilled bool
	}{
		{400, `{"error":{"message":"invalid request"}}`, true},
		{408, `{"error":{"message":"timeout"}}`, false},
		{502, `{"error":{"message":"gateway lost upstream response"}}`, false},
		{200, `{}`, false},
	} {
		t.Run(http.StatusText(tc.status)+tc.body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			p := New(Config{BaseURL: srv.URL + "/v1", Timeout: time.Second})
			for _, call := range []func() error{func() error { _, err := p.Search(context.Background(), "query", 1); return err }, func() error { _, err := p.Fetch(context.Background(), "https://example.com"); return err }} {
				err := call()
				var providerErr *Error
				if !errors.As(err, &providerErr) || providerErr.KnownUnbilled != tc.unbilled {
					t.Fatalf("incorrect charge certainty: %v", err)
				}
			}
		})
	}
}

func TestSafeURL(t *testing.T) {
	for _, raw := range []string{"http://0x7f.0.0.1/", "http://127.0.0.0x1/", "http://0177.0.0.01/", "http://0x7f000001/"} {
		if SafeURL(raw) {
			t.Errorf("browser-normalized private IP allowed: %s", raw)
		}
	}
	for _, raw := range []string{"http://service.internal。/", "http://private.local．/", "http://printer.local｡/"} {
		if SafeURL(raw) {
			t.Errorf("IDNA-normalized private hostname allowed: %s", raw)
		}
	}
	for _, raw := range []string{"https://example.com/docs?q=release", "http://docs.example.com/a(b)", "https://api-docs.deepseek.com/guides/search/"} {
		if !SafeURL(raw) {
			t.Errorf("valid URL rejected: %s", raw)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://user:pass@example.com/", "http://127.0.0.1/", "http://127.1/", "http://0177.0.0.1/", "http://169.254.169.254/latest/", "http://10.1.2.3/", "http://100.64.1.2/", "http://localhost/", "http://service.internal/", "http://[::1]/", "https://example.com:8443/", "https://example.com/\nsecret", "https://example.com/<script>", "https://example.com\\@evil.test"} { // # leak-guard-allow:LG-F4C6A155 leak-guard-allow:LG-4BF15CB1 synthetic credentials and CGNAT address rejected by this test
		if SafeURL(raw) {
			t.Errorf("unsafe URL allowed: %s", raw)
		}
	}
}
