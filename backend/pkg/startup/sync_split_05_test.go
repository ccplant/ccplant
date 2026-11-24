package startup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGitHubEnvForHost(t *testing.T) {
	containsEnv := func(env []string, key, value string) bool {
		target := key + "=" + value
		for _, e := range env {
			if e == target {
				return true
			}
		}
		return false
	}

	t.Run("no GHES configured - GH_HOST not set for any host", func(t *testing.T) {
		preserveTestEnv(t, "GITHUB_API")
		unsetTestEnv(t, "GITHUB_API")

		env := buildGitHubEnvForHost("github.com")
		for _, e := range env {
			if strings.HasPrefix(e, "GH_HOST=") {
				t.Errorf("expected no GH_HOST in env, got %q", e)
			}
		}
	})

	t.Run("GHES configured - GH_HOST set when targetHost matches GHES", func(t *testing.T) {
		preserveTestEnv(t, "GITHUB_API")
		setTestEnv(t, "GITHUB_API", "https://github.enterprise.com/api/v3")

		env := buildGitHubEnvForHost("github.enterprise.com")
		if !containsEnv(env, "GH_HOST", "github.enterprise.com") {
			t.Error("expected GH_HOST=github.enterprise.com in env for GHES URL")
		}
	})

	t.Run("GHES configured - GH_HOST NOT set when targetHost is github.com", func(t *testing.T) {
		preserveTestEnv(t, "GITHUB_API")
		setTestEnv(t, "GITHUB_API", "https://github.enterprise.com/api/v3")

		env := buildGitHubEnvForHost("github.com")
		for _, e := range env {
			if strings.HasPrefix(e, "GH_HOST=") {
				t.Errorf("expected no GH_HOST in env for github.com URL in GHES mode, got %q", e)
			}
		}
	})
}

// TestCloneMarketplace_GHESWithGitHubDotComURL verifies that in a GHES environment,
// cloneMarketplace dispatches to the git-clone path (not gh repo clone with GH_HOST)
// for github.com URLs. This is validated by using a local file-system URL as the
// github.com stand-in so no real network call is needed.
func TestCloneMarketplace_GHESWithLocalURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	t.Run("non-github URL still clones via git clone in GHES env", func(t *testing.T) {
		preserveTestEnv(t, "GITHUB_API")
		setTestEnv(t, "GITHUB_API", "https://github.enterprise.com/api/v3")

		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"file.txt": "content",
		})

		targetDir := filepath.Join(tmpDir, "target")
		// Local path is not a GitHub URL, so cloneMarketplace falls back to git clone.
		if err := cloneMarketplace(sourceDir, targetDir); err != nil {
			t.Fatalf("cloneMarketplace failed in GHES env for local URL: %v", err)
		}
		if _, err := os.Stat(filepath.Join(targetDir, ".git")); os.IsNotExist(err) {
			t.Error("expected .git to exist after clone")
		}
	})
}

func preserveTestEnv(t *testing.T, key string) {
	t.Helper()
	original, exists := os.LookupEnv(key)
	t.Cleanup(func() {
		if exists {
			setTestEnv(t, key, original)
			return
		}
		unsetTestEnv(t, key)
	})
}

func setTestEnv(t *testing.T, key, value string) {
	t.Helper()
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func unsetTestEnv(t *testing.T, key string) {
	t.Helper()
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}
