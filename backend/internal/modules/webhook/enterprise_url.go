package webhook

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// validateEnterpriseURL rejects GitHub Enterprise Server URLs that would let a
// webhook drive server-side requests to internal infrastructure (SSRF,
// ccplant-deploy#66 F3). Rules:
//   - empty is allowed (github.com default)
//   - only https is accepted
//   - the host must not be an IP literal in a private, loopback, link-local,
//     or unspecified range, nor "localhost"
//
// Hostnames that resolve to internal addresses cannot be caught without
// DNS resolution at request time; blocking IP literals and localhost covers
// the direct vector.
func validateEnterpriseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("enterprise_url is not a valid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("enterprise_url must use https (got %q)", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("enterprise_url is missing a host")
	}
	if parsed.User != nil {
		return fmt.Errorf("enterprise_url must not contain credentials")
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return fmt.Errorf("enterprise_url must not point at localhost")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("enterprise_url must not point at a private or link-local address")
		}
	}
	return nil
}