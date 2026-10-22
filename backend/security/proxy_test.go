package security

import (
	"net/http"
	"testing"
)

func TestProxyTrustPrefersCloudflareHeader(t *testing.T) {
	trust := NewProxyTrust("192.168.0.0/16")
	header := http.Header{}
	header.Set("CF-Connecting-IP", "203.0.113.7")
	header.Set("X-Real-IP", "173.245.48.1")
	header.Set("X-Forwarded-For", "198.51.100.9")

	if got := trust.ClientIP("192.168.32.1:4000", header); got != "203.0.113.7" {
		t.Fatalf("ClientIP = %q, want CF-Connecting-IP", got)
	}
}

func TestProxyTrustFallsBackToRightmostUntrustedForwardedIP(t *testing.T) {
	trust := NewProxyTrust("127.0.0.1/32,10.0.0.0/8")
	header := http.Header{}
	header.Set("X-Forwarded-For", "198.51.100.250, 203.0.113.10, 10.0.0.5")

	if got := trust.ClientIP("127.0.0.1:5000", header); got != "203.0.113.10" {
		t.Fatalf("ClientIP = %q, want 203.0.113.10", got)
	}
}

func TestProxyTrustIgnoresHeadersFromUnknownPeer(t *testing.T) {
	trust := NewProxyTrust("127.0.0.1/32")
	header := http.Header{}
	header.Set("CF-Connecting-IP", "203.0.113.7")
	header.Set("X-Real-IP", "203.0.113.8")

	if got := trust.ClientIP("198.51.100.20:5000", header); got != "198.51.100.20" {
		t.Fatalf("ClientIP = %q, want the direct peer", got)
	}
}

func TestProxyTrustHandlesInvalidHeaders(t *testing.T) {
	trust := NewProxyTrust("127.0.0.1/32")
	header := http.Header{}
	header.Set("CF-Connecting-IP", "not-an-ip")
	header.Set("X-Forwarded-For", "also-not-an-ip")

	if got := trust.ClientIP("127.0.0.1:5000", header); got != "127.0.0.1" {
		t.Fatalf("ClientIP = %q, want the trusted peer fallback", got)
	}
}

func TestProxyTrustIdentifiesForwardedMetadataBoundary(t *testing.T) {
	trust := NewProxyTrust("127.0.0.1/32,10.0.0.0/8")

	for _, addr := range []string{"127.0.0.1:5000", "10.2.3.4:443"} {
		if !trust.IsTrustedProxy(addr) {
			t.Fatalf("IsTrustedProxy(%q) = false, want true", addr)
		}
	}
	if trust.IsTrustedProxy("198.51.100.20:5000") {
		t.Fatal("public peer was treated as a trusted proxy")
	}
}
