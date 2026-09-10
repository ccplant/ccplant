package repositories

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestPersonalAPIKeySecretName(t *testing.T) {
	repo := &KubernetesPersonalAPIKeyRepository{}

	tests := []struct {
		name   string
		userID string
	}{
		{name: "legacy compatible", userID: "example-user"},
		{name: "underscore", userID: "example_user"},
		{name: "uppercase", userID: "Takutaka"},
		{name: "symbols", userID: "team/user@example.com"},
		{name: "long", userID: strings.Repeat("a", 300)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repo.secretName(tt.userID)
			if errs := validation.IsDNS1123Subdomain(got); len(errs) != 0 {
				t.Fatalf("secretName(%q) = %q, not a valid DNS-1123 subdomain: %v", tt.userID, got, errs)
			}
		})
	}

	if got, want := repo.secretName("example-user"), PersonalAPIKeySecretPrefix+"example-user"; got != want {
		t.Fatalf("secretName() changed a legacy-compatible name: got %q, want %q", got, want)
	}

	if got, want := repo.secretName("example_user"), PersonalAPIKeySecretPrefix+"example-user"; got != want {
		t.Fatalf("secretName() did not sanitize an underscore: got %q, want %q", got, want)
	}
}
