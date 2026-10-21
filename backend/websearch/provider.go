// Package websearch adapts DeepSeek's independent Search/Fetch APIs. The model
// remains on Chat Completions; no local crawler or user browser is involved.
package websearch

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const (
	MaxCalls               = 3
	MaxResults             = 3
	MaxContentRunes        = 5000
	MaxToolJSONRunes       = 7500 // Below the model-loop's 8,000-rune cap.
	MaxResponseBytes       = 1 << 20
	PriceMicroCNY    int64 = 5000 // CNY 5 / 1,000 requests, 2026-10-21.
	PriceVersion           = "deepseek-search-fetch-cny-2026-10-21"
)

type Config struct {
	BaseURL, APIKey string
	Timeout         time.Duration
	DailyMicroCNY   int64
}

type Hit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
}
type Page struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Error never exposes an upstream error body (which can contain submitted data).
// Unknown charges retain the reservation, including cancellations/timeouts.
type Error struct {
	Code          string
	KnownUnbilled bool
}

func (e *Error) Error() string { return "web research: " + e.Code }

type Provider interface {
	Search(context.Context, string, int) ([]Hit, error)
	Fetch(context.Context, string) (Page, error)
}

type DeepSeek struct{ sdk openai.Client }

func New(cfg Config) *DeepSeek {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	transport.MaxResponseHeaderBytes = 32768
	transport.ResponseHeaderTimeout = cfg.Timeout
	client := &boundedClient{client: &http.Client{Transport: transport, Timeout: cfg.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	return &DeepSeek{sdk: openai.NewClient(option.WithAPIKey(cfg.APIKey), option.WithBaseURL(strings.TrimRight(cfg.BaseURL, "/")+"/"), option.WithMaxRetries(0), option.WithHTTPClient(client), option.WithRequestTimeout(cfg.Timeout))}
}

type boundedClient struct{ client *http.Client }

func (c *boundedClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &boundedBody{ReadCloser: resp.Body, left: MaxResponseBytes}
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		compressed := resp.Body
		decoded, decodeErr := gzip.NewReader(compressed)
		if decodeErr != nil {
			_ = compressed.Close()
			return nil, decodeErr
		}
		resp.Body = &boundedBody{ReadCloser: &decodedBody{Reader: decoded, body: compressed, decoded: decoded}, left: MaxResponseBytes}
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
	}
	return resp, nil
}

type decodedBody struct {
	io.Reader
	body    io.Closer
	decoded io.Closer
}

func (b *decodedBody) Close() error { _ = b.decoded.Close(); return b.body.Close() }

type boundedBody struct {
	io.ReadCloser
	left int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, &Error{Code: "response_too_large"}
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= int64(n)
	return n, err
}
func safeError(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		code := "provider_unavailable"
		if apiErr.StatusCode == 401 || apiErr.StatusCode == 403 {
			code = "provider_denied"
		}
		if apiErr.StatusCode == 402 || apiErr.StatusCode == 429 {
			code = "provider_limit"
		}
		// Gateway 5xx/408 can follow a completed upstream request. Only clear
		// client rejections are known unbilled; ambiguous failures retain cost.
		return &Error{Code: code, KnownUnbilled: apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != 408}
	}
	return &Error{Code: "provider_unavailable"}
}
func (p *DeepSeek) Search(ctx context.Context, query string, count int) ([]Hit, error) {
	var result struct {
		Results *[]Hit `json:"results"`
	}
	err := p.sdk.Post(ctx, "tools/search", map[string]any{"query": query, "count": count, "need_content": false}, &result)
	if err != nil {
		return nil, safeError(err)
	}
	if result.Results == nil {
		return nil, &Error{Code: "invalid_provider_response"}
	}
	hits := *result.Results
	if len(hits) > count {
		hits = hits[:count]
	}
	return hits, nil
}
func (p *DeepSeek) Fetch(ctx context.Context, target string) (Page, error) {
	var response struct {
		Title   string  `json:"title"`
		Content *string `json:"content"`
	}
	if err := p.sdk.Post(ctx, "tools/fetch", map[string]string{"url": target}, &response); err != nil {
		return Page{}, safeError(err)
	}
	if response.Content == nil {
		return Page{}, &Error{Code: "invalid_provider_response"}
	}
	return Page{Title: response.Title, Content: *response.Content}, nil
}

// SafeURL is a publication and provider-input check, not a local-fetch SSRF
// transport. DeepSeek owns DNS, redirects, MIME handling and page extraction.
// We never make a connection to the supplied URL or forward user cookies/IPs.
func SafeURL(raw string) bool {
	if len(raw) > 2048 || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 || strings.ContainsAny(raw, "\\\"<>") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	// Require ASCII/punycode hostnames so browser IDNA dot normalization cannot
	// turn an apparently public host into a private-name suffix after validation.
	if strings.IndexFunc(host, func(r rune) bool { return r > 127 }) >= 0 {
		return false
	}
	if host == "" || host == "localhost" || !strings.Contains(host, ".") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return false
	}
	// Reject alternative IPv4 spellings (e.g. 127.1 or octal components).
	if net.ParseIP(host) == nil {
		labels := strings.Split(host, ".")
		tail := labels[len(labels)-1]
		if strings.Trim(tail, "0123456789") == "" || strings.HasPrefix(tail, "0x") {
			return false
		}
	}
	return u.Port() == "" || u.Port() == "443" || u.Port() == "80"
}
func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return false
		}
	}
	return true
}
func Clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
