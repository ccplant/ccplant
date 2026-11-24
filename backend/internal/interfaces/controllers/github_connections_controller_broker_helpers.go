package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func normalizeRepositoryList(repositories []string) ([]string, error) {
	const maxRepositories = 100
	seen := make(map[string]struct{}, len(repositories))
	result := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		repository = strings.TrimSpace(strings.TrimSuffix(repository, ".git"))
		parts := strings.Split(repository, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("repository %q must use owner/name format", repository)
		}
		key := strings.ToLower(repository)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, repository)
		if len(result) > maxRepositories {
			return nil, fmt.Errorf("at most %d GitHub repositories may be configured", maxRepositories)
		}
	}
	return result, nil
}

func normalizeRepositoryPatterns(patterns []string) ([]string, error) {
	const maxPatterns = 100
	const maxRegexLength = 512
	seen := make(map[string]struct{}, len(patterns))
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if len(pattern) > maxRegexLength {
			return nil, fmt.Errorf("repository regex must be at most %d bytes", maxRegexLength)
		}
		if _, err := regexp.Compile("(?i:^(?:" + pattern + ")$)"); err != nil {
			return nil, fmt.Errorf("invalid repository regex %q: %w", pattern, err)
		}
		key := strings.ToLower(pattern)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, pattern)
		if len(result) > maxPatterns {
			return nil, fmt.Errorf("at most %d GitHub repository patterns may be configured", maxPatterns)
		}
	}
	return result, nil
}

func prefixedRepositoryPatterns(patterns []string) []string {
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		result = append(result, repositoryRegexPrefix+pattern)
	}
	return result
}

const repositoryRegexPrefix = "regex:"

func isRepositoryRegex(repository string) bool {
	return strings.HasPrefix(strings.TrimSpace(repository), repositoryRegexPrefix)
}

func brokerLeaseTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func brokerLeaseSecretName(token string) string {
	return "github-broker-" + brokerLeaseTokenHash(token)[:32]
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func (c *GitHubConnectionsController) RevokeBrokerLeases(ctx context.Context, sessionID string) error {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: "agentapi.ccplant.io/session-id-hash=" + shortHash(sessionID)})
	if err != nil {
		return err
	}
	for i := range secrets.Items {
		var lease githubBrokerLease
		if json.Unmarshal(secrets.Items[i].Data["record.json"], &lease) != nil || lease.SessionID != sessionID {
			continue
		}
		if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx, secrets.Items[i].Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
