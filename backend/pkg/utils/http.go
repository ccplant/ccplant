package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// HTTPClientConfig holds configuration for HTTP client creation
type HTTPClientConfig struct {
	Timeout time.Duration
}

// DefaultHTTPClientConfig returns default HTTP client configuration
func DefaultHTTPClientConfig() HTTPClientConfig {
	return HTTPClientConfig{
		Timeout: 30 * time.Second,
	}
}

// NewHTTPClient creates a new HTTP client with the given configuration
func NewHTTPClient(config HTTPClientConfig) *http.Client {
	return &http.Client{
		Timeout:   config.Timeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
}

// NewDefaultHTTPClient creates a new HTTP client with default configuration
func NewDefaultHTTPClient() *http.Client {
	return NewHTTPClient(DefaultHTTPClientConfig())
}

// NewPublicHTTPClient creates a client that refuses connections to private,
// loopback, link-local, multicast, and unspecified addresses. Validation is
// performed after DNS resolution and again for every redirect.
func NewPublicHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid outbound address: %w", err)
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve outbound host: %w", err)
		}
		for _, ip := range ips {
			if !isPublicAddress(ip) {
				return nil, fmt.Errorf("outbound host resolves to a non-public address")
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("outbound host has no addresses")
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: otelhttp.NewTransport(transport)}
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if req.URL.Scheme != "https" || isInternalHostname(req.URL.Hostname()) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return client
}

// IsPublicHTTPSURL performs the non-network portion of outbound URL
// validation. Dial-time address checks remain authoritative against DNS
// rebinding.
func IsPublicHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || isInternalHostname(u.Hostname()) {
		return false
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		return isPublicAddress(ip)
	}
	return true
}

func isPublicAddress(ip netip.Addr) bool {
	return ip.IsValid() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}

func isInternalHostname(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".svc") || strings.HasSuffix(host, ".cluster.local")
}

// HTTPError represents an HTTP error with status code and message
type HTTPError struct {
	StatusCode int
	Message    string
	URL        string
}

func (e HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s (URL: %s)", e.StatusCode, e.Message, e.URL)
}

// CheckHTTPResponse checks if HTTP response indicates an error
func CheckHTTPResponse(resp *http.Response, url string) error {
	if resp.StatusCode >= 400 {
		return HTTPError{
			StatusCode: resp.StatusCode,
			Message:    resp.Status,
			URL:        url,
		}
	}
	return nil
}

// SafeCloseResponse safely closes HTTP response body with error logging
func SafeCloseResponse(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		if err := resp.Body.Close(); err != nil {
			// Note: Using fmt.Printf since we're creating a utility package
			// In a real application, this should use a structured logger
			fmt.Printf("Warning: failed to close HTTP response body: %v\n", err)
		}
	}
}
