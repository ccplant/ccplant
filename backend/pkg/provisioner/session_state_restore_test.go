package provisioner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionstate"
)

func TestRestoreSessionStateFromWorkdirVolume(t *testing.T) {
	sourceHome := t.TempDir()
	sourceCWD := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceHome, ".session"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceHome, ".session", "acp-history.jsonl"), []byte("history\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceCWD, ".acp-session-id"), []byte("thread-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session-state.tar.zst")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionstate.Pack(archive, "codex-acp", "thread-1", sourceHome, sourceCWD); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	oldRuntimeHome := runtimeHome
	runtimeHome = t.TempDir()
	t.Cleanup(func() { runtimeHome = oldRuntimeHome })
	restoreCWD := t.TempDir()
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", archivePath)
	t.Setenv("PROVISIONER_PROXY_URL", "http://must-not-be-used.invalid")

	found, err := (&Server{}).restoreSessionState(context.Background(), "session-1", restoreCWD, false)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("volume snapshot was not restored")
	}
	got, err := os.ReadFile(filepath.Join(runtimeHome, ".session", "acp-history.jsonl"))
	if err != nil || string(got) != "history\n" {
		t.Fatalf("restored history = %q, err=%v", got, err)
	}
}

func TestRestoreSessionStateKeepsWorkspaceOnRestoredVolume(t *testing.T) {
	sourceHome := t.TempDir()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".acp-session-id"), []byte("thread-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceHome, ".session"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceHome, ".session", "acp-history.jsonl"), []byte("history\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session-state.tar.zst")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionstate.Pack(archive, "codex-acp", "thread-1", sourceHome, cwd); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("v1\nv2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldRuntimeHome := runtimeHome
	runtimeHome = t.TempDir()
	t.Cleanup(func() { runtimeHome = oldRuntimeHome })
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", archivePath)
	t.Setenv("PROVISIONER_PROXY_URL", "http://must-not-be-used.invalid")

	found, err := (&Server{}).restoreSessionState(context.Background(), "session-1", cwd, true)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got, _ := os.ReadFile(filepath.Join(cwd, "notes.txt")); string(got) != "v1\nv2\n" {
		t.Fatalf("workspace = %q, want newer content", got)
	}
	if got, _ := os.ReadFile(filepath.Join(runtimeHome, ".session", "acp-history.jsonl")); string(got) != "history\n" {
		t.Fatalf("restored history = %q", got)
	}
}

func TestWorkspaceAlreadyRestored(t *testing.T) {
	oldRepoPath := workdirRepoPath
	workdirRepoPath = filepath.Join(t.TempDir(), "repo")
	t.Cleanup(func() { workdirRepoPath = oldRepoPath })
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", "")

	if workspaceAlreadyRestored("session-1") {
		t.Fatal("fresh volume was treated as restored")
	}
	if err := recordWorkspaceRestored("session-1"); err != nil {
		t.Fatal(err)
	}
	if !workspaceAlreadyRestored("session-1") {
		t.Fatal("recorded volume was not treated as restored")
	}
	if workspaceAlreadyRestored("tpl_other") {
		t.Fatal("volume restored from another source was treated as restored")
	}

	archivePath := filepath.Join(t.TempDir(), "session-state.tar.zst")
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", archivePath)
	if workspaceAlreadyRestored("tpl_other") {
		t.Fatal("missing volume archive was treated as restored")
	}
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !workspaceAlreadyRestored("tpl_other") {
		t.Fatal("volume holding its own archive was not treated as restored")
	}
}

func TestRestoreSessionStateNotFoundIsAnEmptyInitialSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/session-state/session-1/download-url" {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		if r.URL.Path != "/internal/session-state/session-1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer provisioner-token" {
			t.Fatalf("authorization header was not propagated")
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	t.Setenv("SESSION_STATE_PROXY_URL", "")
	t.Setenv("PROVISIONER_PROXY_URL", server.URL)
	t.Setenv("PROVISIONER_TOKEN", "provisioner-token")

	found, err := (&Server{httpClient: server.Client()}).restoreSessionState(context.Background(), "session-1", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("missing initial snapshot was reported as restored")
	}
}

func TestVolumeBackedTemplateFallsBackToPortableSnapshot(t *testing.T) {
	requested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		if r.URL.Path == "/internal/session-state/tpl_one/download-url" {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		if r.URL.Path != "/internal/session-state/tpl_one" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", filepath.Join(t.TempDir(), "missing.tar.zst"))
	t.Setenv("SESSION_STATE_PROXY_URL", server.URL)
	t.Setenv("PROVISIONER_TOKEN", "provisioner-token")

	found, err := (&Server{httpClient: server.Client()}).restoreSessionState(context.Background(), "tpl_one", t.TempDir(), false)
	if err != nil || found || !requested {
		t.Fatalf("found=%v requested=%v err=%v", found, requested, err)
	}
}

func TestRestoreSessionStateUnavailableCanBeSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("SESSION_STATE_PROXY_URL", "")
	t.Setenv("PROVISIONER_PROXY_URL", server.URL)
	t.Setenv("PROVISIONER_TOKEN", "provisioner-token")

	found, err := (&Server{httpClient: server.Client()}).restoreSessionState(context.Background(), "session-1", t.TempDir(), false)
	if found {
		t.Fatal("unavailable backend was reported as restored")
	}
	if !errors.Is(err, errSessionStateBackendUnavailable) {
		t.Fatalf("error = %v, want errSessionStateBackendUnavailable", err)
	}
}

func TestRestoreSessionStatePrefersDedicatedStateProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/session-state/session-1/download-url" {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	t.Setenv("SESSION_STATE_PROXY_URL", server.URL)
	t.Setenv("PROVISIONER_PROXY_URL", "http://parent-api.invalid")
	t.Setenv("PROVISIONER_TOKEN", "provisioner-token")

	found, err := (&Server{httpClient: server.Client()}).restoreSessionState(context.Background(), "session-1", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("missing snapshot was reported as restored")
	}
}

func TestNativeSessionDoesNotImplicitlyRestoreNewSession(t *testing.T) {
	t.Setenv("AGENTAPI_NATIVE_SESSION_ROOT", t.TempDir())
	settings := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{PersistenceEnabled: true}}
	if shouldImplicitlyRestoreSessionState(settings) {
		t.Fatal("native session unexpectedly enabled implicit restore")
	}
}

func TestKubernetesSessionImplicitlyRestoresPersistentSession(t *testing.T) {
	t.Setenv("AGENTAPI_NATIVE_SESSION_ROOT", "")
	settings := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{PersistenceEnabled: true}}
	if !shouldImplicitlyRestoreSessionState(settings) {
		t.Fatal("persistent Kubernetes session unexpectedly disabled implicit restore")
	}
}

func TestTemplateRestoreRequiresConversationResume(t *testing.T) {
	tests := []struct {
		name     string
		settings *sessionsettings.SessionSettings
		want     bool
	}{
		{name: "new session", settings: &sessionsettings.SessionSettings{}, want: false},
		{name: "restart", settings: &sessionsettings.SessionSettings{Restart: true}, want: true},
		{name: "template", settings: &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{ResumeFrom: "tpl_context"}}, want: true},
		{name: "nil settings", settings: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRequireConversationResume(tt.settings); got != tt.want {
				t.Fatalf("shouldRequireConversationResume() = %v, want %v", got, tt.want)
			}
		})
	}
}
