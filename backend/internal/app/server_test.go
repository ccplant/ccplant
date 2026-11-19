package app

import (
	"testing"
)

func TestExtractRepoFullNameFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL",
			url:      "https://github.com/owner/repo",
			expected: "owner/repo",
		},
		{
			name:     "HTTPS URL with .git",
			url:      "https://github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "SSH URL",
			url:      "git@github.com:owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "Invalid URL format",
			url:      "invalid-url",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := extractRepoFullNameFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestResolveApplicationNamespace(t *testing.T) {
	t.Run("runtime override wins over configured value", func(t *testing.T) {
		t.Setenv("AGENTAPI_KV_STORE_NAMESPACE", "agentapi-ui")
		t.Setenv("POD_NAMESPACE", "pod-namespace")

		if got := resolveApplicationNamespace("default"); got != "agentapi-ui" {
			t.Fatalf("namespace = %q, want agentapi-ui", got)
		}
	})

	t.Run("configured value wins over pod fallback", func(t *testing.T) {
		t.Setenv("AGENTAPI_KV_STORE_NAMESPACE", "")
		t.Setenv("POD_NAMESPACE", "pod-namespace")

		if got := resolveApplicationNamespace(" configured "); got != "configured" {
			t.Fatalf("namespace = %q, want configured", got)
		}
	})

	t.Run("falls back to pod namespace and default", func(t *testing.T) {
		t.Setenv("AGENTAPI_KV_STORE_NAMESPACE", "")
		t.Setenv("POD_NAMESPACE", " pod-namespace ")
		if got := resolveApplicationNamespace(""); got != "pod-namespace" {
			t.Fatalf("namespace = %q, want pod-namespace", got)
		}

		t.Setenv("POD_NAMESPACE", "")
		if got := resolveApplicationNamespace(""); got != "default" {
			t.Fatalf("namespace = %q, want default", got)
		}
	})
}
