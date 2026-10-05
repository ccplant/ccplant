package config

import "testing"

func TestLoadUsageConfigFromEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_USAGE_ENABLED", "true")
	t.Setenv("AGENTAPI_USAGE_DATABASE_URL", "libsql://usage.example")
	t.Setenv("AGENTAPI_USAGE_AUTH_TOKEN", "usage-token")
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Usage.Enabled || cfg.Usage.DatabaseURL != "libsql://usage.example" || cfg.Usage.AuthToken != "usage-token" {
		t.Fatalf("usage config = %#v", cfg.Usage)
	}
}

func TestLoadSessionCountConfigFromEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_SESSION_COUNT_ENABLED", "true")
	t.Setenv("AGENTAPI_SESSION_COUNT_BACKEND", "libsql")
	t.Setenv("AGENTAPI_SESSION_COUNT_DATABASE_URL", "libsql://counts.example")
	t.Setenv("AGENTAPI_SESSION_COUNT_AUTH_TOKEN", "count-token")
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SessionCount.Enabled || cfg.SessionCount.Backend != "libsql" || cfg.SessionCount.DatabaseURL != "libsql://counts.example" || cfg.SessionCount.AuthToken != "count-token" {
		t.Fatalf("session count config = %#v", cfg.SessionCount)
	}
}
