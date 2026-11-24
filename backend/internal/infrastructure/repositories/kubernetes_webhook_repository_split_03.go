package repositories

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

func generateWebhookSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func normalizeEnterpriseURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	url = strings.ToLower(url)
	// Remove URL scheme (https:// or http://) to match GitHub Enterprise Host header
	// GitHub sends only the hostname in X-GitHub-Enterprise-Host header
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	return url
}

func matchWebhookRepository(pattern, repository string) bool {
	if pattern == repository {
		return true
	}

	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		parts := strings.SplitN(repository, "/", 2)
		if len(parts) == 2 && parts[0] == prefix {
			return true
		}
	}

	return false
}
