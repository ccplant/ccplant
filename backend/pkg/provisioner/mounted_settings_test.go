package provisioner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestWaitForMountedSettingsSanitizedWaitsForProjectedSecretUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeMountedSettings(t, path, map[string]string{"AGENTAPI_KEY": "bootstrap-only"})

	done := make(chan error, 1)
	go func() {
		done <- waitForMountedSettingsSanitized(context.Background(), path, time.Second)
	}()
	time.Sleep(50 * time.Millisecond)
	writeMountedSettings(t, path, map[string]string{"KEEP": "value"})

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWaitForMountedSettingsSanitizedRejectsStaleKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeMountedSettings(t, path, map[string]string{"AGENTAPI_KEY": "bootstrap-only"})

	if err := waitForMountedSettingsSanitized(context.Background(), path, 50*time.Millisecond); err == nil {
		t.Fatal("expected stale mounted API key to time out")
	}
}

func writeMountedSettings(t *testing.T, path string, env map[string]string) {
	t.Helper()
	data, err := sessionsettings.MarshalYAML(&sessionsettings.SessionSettings{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
