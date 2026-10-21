// Package security holds transport-independent abuse controls: client IP
// resolution behind trusted proxies, request rate limiting, login backoff, and
// Cloudflare Turnstile verification. None of it depends on the HTTP handler, so
// the policies can be tested and reused on their own.
package security

import (
	"net"
	"net/http"
	"strings"
)

// ProxyTrust decides which direct peers may supply forwarded client headers.
// Headers from any other peer are ignored, so a client cannot choose its own
// rate-limit bucket.
type ProxyTrust struct {
	nets []*net.IPNet
}

// NewProxyTrust parses a comma-separated list of CIDRs or plain IPs.
func NewProxyTrust(cidrs string) *ProxyTrust {
	trust := &ProxyTrust{}
	for _, part := range strings.Split(cidrs, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(part); err == nil {
			trust.nets = append(trust.nets, network)
			continue
		}
		if ip := net.ParseIP(part); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			trust.nets = append(trust.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return trust
}

func (p *ProxyTrust) trusted(ip net.IP) bool {
	if p == nil || ip == nil {
		return false
	}
	for _, network := range p.nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// IsTrustedProxy reports whether the direct peer may supply forwarded
// transport metadata. Member headers must use the same trust boundary as IP
// resolution.
func (p *ProxyTrust) IsTrustedProxy(remoteAddr string) bool {
	return p.trusted(net.ParseIP(hostFromAddr(remoteAddr)))
}

// ClientIP returns the best known address for the request's author. It only
// reads forwarded headers when the direct peer is a trusted proxy.
// Cloudflare's CF-Connecting-IP wins because the edge overwrites it; nginx
// forwards the visitor address in X-Real-IP.
func (p *ProxyTrust) ClientIP(remoteAddr string, header http.Header) string {
	peer := hostFromAddr(remoteAddr)
	if peer == "" {
		return ""
	}
	if !p.trusted(net.ParseIP(peer)) {
		return peer
	}
	for _, name := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if ip := parseIP(header.Get(name)); ip != "" {
			return ip
		}
	}
	parts := strings.Split(header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := parseIP(parts[i])
		if ip == "" || p.trusted(net.ParseIP(ip)) {
			continue
		}
		return ip
	}
	return peer
}

func hostFromAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return parseIP(host)
}

func parseIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}
