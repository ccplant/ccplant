package webhook

import (
	"testing"
)

func TestValidateEnterpriseURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"empty allowed", "", false},
		{"public https host", "https://github.enterprise.com/api/v3", false},
		{"public https host with path", "https://ghes.example.com/", false},
		{"http rejected", "http://ghes.example.com", true},
		{"missing scheme", "ghes.example.com", true},
		{"localhost rejected", "https://localhost:8443", true},
		{"localhost subdomain rejected", "https://api.localhost", true},
		{"loopback ip rejected", "https://127.0.0.1:8443", true},
		{"private ip rejected", "https://10.0.0.5", true},
		{"rfc1918 rejected", "https://192.168.1.10", true},
		{"link-local metadata rejected", "https://169.254.169.254/latest/meta-data/", true},
		{"unspecified rejected", "https://0.0.0.0", true},
		{"credentials rejected", "https://user:pass@ghes.example.com", true},
		{"missing host rejected", "https:///path", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEnterpriseURL(tt.raw)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tt.raw, err)
			}
		})
	}
}

func TestValidateEnterpriseURL_TrimsWhitespace(t *testing.T) {
	if err := validateEnterpriseURL("  https://ghes.example.com  "); err != nil {
		t.Errorf("whitespace-only padding should be trimmed, got: %v", err)
	}
}
