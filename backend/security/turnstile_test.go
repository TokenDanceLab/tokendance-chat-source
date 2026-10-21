package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTurnstileVerifiesActionHostnameAndIdempotency(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"action":"login","hostname":"chat.example.com"}`))
	}))
	defer srv.Close()

	verifier := NewTurnstile("secret", []string{"chat.example.com"}, "login")
	verifier.endpoint = srv.URL

	if err := verifier.Verify(context.Background(), "token", "203.0.113.5"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if gotForm.Get("secret") != "secret" || gotForm.Get("response") != "token" {
		t.Fatalf("unexpected form: %v", gotForm)
	}
	if gotForm.Get("idempotency_key") == "" {
		t.Error("expected an idempotency key")
	}
	if gotForm.Get("remoteip") != "203.0.113.5" {
		t.Errorf("remoteip = %q", gotForm.Get("remoteip"))
	}
}

func TestTurnstileRejectsActionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"action":"register","hostname":"chat.example.com"}`))
	}))
	defer srv.Close()

	verifier := NewTurnstile("secret", nil, "login")
	verifier.endpoint = srv.URL
	if err := verifier.Verify(context.Background(), "token", ""); err == nil {
		t.Fatal("expected the action mismatch to fail")
	}
}

func TestTurnstileMapsDuplicateToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["timeout-or-duplicate"]}`))
	}))
	defer srv.Close()

	verifier := NewTurnstile("secret", nil, "login")
	verifier.endpoint = srv.URL
	err := verifier.Verify(context.Background(), "token", "")
	if !errors.Is(err, ErrTurnstileExpired) {
		t.Fatalf("error = %v, want ErrTurnstileExpired", err)
	}
}

func TestTurnstileMapsServiceFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	verifier := NewTurnstile("secret", nil, "login")
	verifier.endpoint = srv.URL
	err := verifier.Verify(context.Background(), "token", "")
	if !errors.Is(err, ErrTurnstileUnavailable) {
		t.Fatalf("error = %v, want ErrTurnstileUnavailable", err)
	}
}

func TestTurnstileDisabledAndInputLimits(t *testing.T) {
	disabled := NewTurnstile("", nil, "")
	if err := disabled.Verify(context.Background(), "", ""); err != nil {
		t.Fatalf("disabled verifier must pass: %v", err)
	}

	verifier := NewTurnstile("secret", nil, "")
	if err := verifier.Verify(context.Background(), "", ""); err == nil {
		t.Fatal("expected a missing token to fail")
	}
	if err := verifier.Verify(context.Background(), strings.Repeat("x", turnstileMaxTokenLength+1), ""); err == nil {
		t.Fatal("expected an oversized token to fail")
	}
}
