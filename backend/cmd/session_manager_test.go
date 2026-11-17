package cmd

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/buildinfo"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestSetSessionManagerCurrentVersionUsesBuildVersion(t *testing.T) {
	previous := buildinfo.Version
	buildinfo.Version = "v1.2.3"
	t.Cleanup(func() { buildinfo.Version = previous })

	cfg := &config.Config{}
	setSessionManagerCurrentVersion(cfg)

	if got := cfg.SessionManager.CurrentVersion; got != "v1.2.3" {
		t.Fatalf("current version = %q, want %q", got, "v1.2.3")
	}
}

func TestSetSessionManagerCurrentVersionPreservesExplicitOverride(t *testing.T) {
	previous := buildinfo.Version
	buildinfo.Version = "v1.2.3"
	t.Cleanup(func() { buildinfo.Version = previous })

	cfg := &config.Config{}
	cfg.SessionManager.CurrentVersion = "dev.ccplant.override"
	setSessionManagerCurrentVersion(cfg)

	if got := cfg.SessionManager.CurrentVersion; got != "dev.ccplant.override" {
		t.Fatalf("current version = %q, want explicit override", got)
	}
}
