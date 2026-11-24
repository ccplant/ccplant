package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestBuildSessionSettings_PiConfiguresCloudProvider(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:     "test-ns",
			Image:         "test-image:latest",
			BasePort:      9000,
			PVCEnabled:    boolPtrForTest(false),
			CPURequest:    "100m",
			CPULimit:      "1",
			MemoryRequest: "128Mi",
			MemoryLimit:   "512Mi",
		},
	}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "test-user"},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)
	req := &entities.RunServerRequest{
		UserID:       "test-user",
		AgentType:    "pi",
		Environment:  map[string]string{"OPENAI_API_KEY": "openai-key"},
		PiConnection: &modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://ollama.com/v1", Model: "qwen3-coder", Authentication: "none"},
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if _, ok := settings.Env["OLLAMA_API_KEY"]; ok {
		t.Fatalf("OLLAMA_API_KEY should not be synthesized")
	}
	if got := settings.Env["PI_ACP_PI_COMMAND"]; got != "pi" {
		t.Fatalf("PI_ACP_PI_COMMAND = %q", got)
	}
	if got := settings.Pi.SettingsJSON["defaultProvider"]; got != "ccplant" {
		t.Fatalf("defaultProvider = %v", got)
	}
	if got := settings.Pi.SettingsJSON["defaultModel"]; got != "qwen3-coder" {
		t.Fatalf("defaultModel = %v", got)
	}
	provider := settings.Pi.ModelsJSON["providers"].(map[string]interface{})["ccplant"].(map[string]interface{})
	if provider["baseUrl"] != "https://ollama.com/v1" || provider["apiKey"] != "none" {
		t.Fatalf("unexpected provider: %#v", provider)
	}
	if settings.Startup.PreScript == "" {
		t.Fatalf("expected pi startup pre-script")
	}
	if strings.Contains(settings.Startup.PreScript, "pi-cloud") {
		t.Fatalf("pi cloud extension must not be installed")
	}
	if !strings.Contains(settings.Startup.PreScript, "node_modules/pi-mcp-adapter") {
		t.Fatalf("expected pi pre-script to skip pi-mcp-adapter install when package is baked into the image")
	}
}

func TestBuildPiConfigUsesStructuredConnection(t *testing.T) {
	window := int64(999424)
	got := buildPiConfig(&modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://ollama.com/v1", Model: "glm-5.2:cloud", Authentication: "api_key", APIKey: "secret", ContextWindow: &window})
	provider := got.ModelsJSON["providers"].(map[string]interface{})["ccplant"].(map[string]interface{})
	model := provider["models"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "secret", provider["apiKey"])
	require.Equal(t, int64(999424), model["contextWindow"])
	require.Equal(t, "ccplant", got.SettingsJSON["defaultProvider"])
	require.Equal(t, "glm-5.2:cloud", got.SettingsJSON["defaultModel"])
}

// fakeCredentialsRepository is a test CredentialsRepository that returns
// canned managed files for a given name.
type fakeCredentialsRepository struct {
	filesByName map[string][]sessionsettings.ManagedFile
	errByName   map[string]error
}

func (f *fakeCredentialsRepository) Save(ctx context.Context, c *entities.Credentials) error {
	return nil
}
func (f *fakeCredentialsRepository) FindByName(ctx context.Context, name string) (*entities.Credentials, error) {
	if err, ok := f.errByName[name]; ok {
		return nil, err
	}
	files, ok := f.filesByName[name]
	if !ok {
		return nil, fmt.Errorf("credentials not found: %s", name)
	}
	creds := entities.NewCredentials(name, nil)
	creds.SetFiles(files)
	return creds, nil
}
func (f *fakeCredentialsRepository) Delete(ctx context.Context, name string) error { return nil }
func (f *fakeCredentialsRepository) Exists(ctx context.Context, name string) (bool, error) {
	_, ok := f.filesByName[name]
	return ok, nil
}
func (f *fakeCredentialsRepository) List(ctx context.Context) ([]*entities.Credentials, error) {
	return nil, nil
}

// TestBuildSessionSettings_CredentialsRepoMergesKVAndLegacySecret verifies that
// managed credential files are read from the credentials repository (KV store)
// and that legacy files only present in the agentapi-agent-files-* Kubernetes
// Secret are merged in so they are not lost during the KV transition.
func TestBuildSessionSettings_CredentialsRepoMergesKVAndLegacySecret(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-agent-files-test-user", Namespace: "test-ns"},
			Data: sessionsettings.FilesToSecretData([]sessionsettings.ManagedFile{{
				Path:    sessionsettings.ManagedFileTypes[sessionsettings.FileTypeClaudeCredentials],
				Content: `{"legacy":"claude-credentials"}`,
			}}),
		},
	)
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"
	manager.SetCredentialsRepository(&fakeCredentialsRepository{
		filesByName: map[string][]sessionsettings.ManagedFile{
			"test-user": {{
				Path:    sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth],
				Content: `{"tokens":{"access_token":"codex-token"}}`,
			}},
		},
	})

	session := NewKubernetesSession("test-session", &entities.RunServerRequest{UserID: "test-user"},
		"test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)
	settings := manager.buildSessionSettings(context.Background(), session, &entities.RunServerRequest{
		UserID: "test-user",
	}, nil)

	byPath := map[string]string{}
	for _, f := range settings.Files {
		byPath[f.Path] = f.Content
	}
	if got, ok := byPath[sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth]]; !ok || got != `{"tokens":{"access_token":"codex-token"}}` {
		t.Fatalf("codex auth.json from KV missing or wrong: %#v", byPath)
	}
	if got, ok := byPath[sessionsettings.ManagedFileTypes[sessionsettings.FileTypeClaudeCredentials]]; !ok || got != `{"legacy":"claude-credentials"}` {
		t.Fatalf("legacy claude credentials from K8s Secret missing or wrong: %#v", byPath)
	}
}

// TestResolveAutoAgentType_CredentialsRepoCodexAuth verifies that auto agent
// type resolution reads ~/.codex/auth.json from the credentials repository
// (KV store) instead of only the Kubernetes Secret.
func TestResolveAutoAgentType_CredentialsRepoCodexAuth(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
		// No agentapi-agent-files Secret exists in K8s; the credential is only in KV.
	)
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"
	manager.SetCredentialsRepository(&fakeCredentialsRepository{
		filesByName: map[string][]sessionsettings.ManagedFile{
			"test-user": {{
				Path:    sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth],
				Content: `{"tokens":{"access_token":"codex-token"}}`,
			}},
		},
	})

	got := manager.resolveAutoAgentType(context.Background(), &entities.RunServerRequest{
		UserID:    "test-user",
		AgentType: "auto",
	})
	if got != "codex-acp" {
		t.Fatalf("resolveAutoAgentType = %q, want codex-acp", got)
	}
}

func TestResolveAutoAgentType_UsesTeamDefaultWhenAgentTypeIsOmitted(t *testing.T) {
	teamSettings := entities.NewSettings("org/team")
	teamSettings.SetDefaultAgentType("codex-acp")
	manager := &KubernetesSessionManager{
		settingsRepo: &fakeSettingsRepository{settings: map[string]*entities.Settings{
			"org/team": teamSettings,
		}},
	}

	got := manager.resolveAutoAgentType(context.Background(), &entities.RunServerRequest{
		Scope:  entities.ScopeTeam,
		TeamID: "org/team",
	})
	if got != "codex-acp" {
		t.Fatalf("resolveAutoAgentType = %q, want codex-acp", got)
	}
}

func TestResolveAutoAgentType_UsesPersonalDefaultWhenAgentTypeIsOmitted(t *testing.T) {
	personalSettings := entities.NewSettings("test-user")
	personalSettings.SetDefaultAgentType("codex-acp")
	manager := &KubernetesSessionManager{
		settingsRepo: &fakeSettingsRepository{settings: map[string]*entities.Settings{
			"test-user": personalSettings,
		}},
	}

	got := manager.resolveAutoAgentType(context.Background(), &entities.RunServerRequest{
		Scope:  entities.ScopeUser,
		UserID: "test-user",
	})
	if got != "codex-acp" {
		t.Fatalf("resolveAutoAgentType = %q, want codex-acp", got)
	}
}

func TestResolveAutoAgentType_DefaultsToAutoWhenAgentTypeIsOmitted(t *testing.T) {
	manager := &KubernetesSessionManager{}

	got := manager.resolveAutoAgentType(context.Background(), &entities.RunServerRequest{})
	if got != "claude-acp" {
		t.Fatalf("resolveAutoAgentType = %q, want claude-acp", got)
	}
}

func TestBuildSessionSettings_ProfileAuthOverridesCompatible(t *testing.T) {
	k8sClient := fake.NewSimpleClientset()
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{Namespace: "test-ns", Image: "test-image", BasePort: 9000, PVCEnabled: boolPtrForTest(false)}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatal(err)
	}
	settings := entities.NewSettings("user")
	settings.SetAuthMode(entities.AuthModeAnthropicCompatible)
	settings.SetClaudeCodeOAuthToken("saved-oauth")
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{"user": settings}})
	req := &entities.RunServerRequest{UserID: "user", AgentType: "claude-acp", ClaudeAuthMode: "oauth"}
	if err := manager.prepareModelConnections(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	session := NewKubernetesSession("test-auth", req, "deploy", "service", "pvc", "test-ns", 9000, nil, nil)
	resolved := manager.buildSessionSettings(context.Background(), session, req, nil)
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "saved-oauth" || resolved.Env["CLAUDE_CODE_USE_BEDROCK"] != "0" {
		t.Fatal("profile OAuth selection did not materialize saved OAuth credentials")
	}
	if settings.AuthMode() != entities.AuthModeAnthropicCompatible {
		t.Fatal("stored auth mode changed")
	}
}

func TestBuildSessionSettings_ProfileSelectsTeamSettings(t *testing.T) {
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{Namespace: "test", Image: "test-image", BasePort: 9000, PVCEnabled: boolPtrForTest(false)}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatal(err)
	}
	team := entities.NewSettings("org/team")
	team.SetAuthMode(entities.AuthModeOAuth)
	team.SetClaudeCodeOAuthToken("team-oauth")
	team.SetEnvVars(map[string]string{"SHARED": "team"})
	personal := entities.NewSettings("user")
	personal.SetAuthMode(entities.AuthModeOAuth)
	personal.SetClaudeCodeOAuthToken("personal-oauth")
	personal.SetEnvVars(map[string]string{"SHARED": "personal", "PERSONAL_ONLY": "private"})
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{"org/team": team, "user": personal}})
	profile := entities.NewSessionProfile("profile", "Profile", "user")
	pc := entities.NewSessionProfileConfig()
	pc.SetSettingsTeamID("org/team")
	profile.SetConfig(pc)
	manager.SetSessionProfileRepository(profileConnectionRepository{profile: profile})
	req := &entities.RunServerRequest{UserID: "user", Teams: []string{"org/team"}, ResolvedSessionProfileID: "profile", AgentType: "claude-acp"}
	if err := manager.prepareModelConnections(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	session := NewKubernetesSession("session", req, "deploy", "service", "pvc", "test", 9000, nil, nil)
	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if settings.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "team-oauth" || settings.Env["SHARED"] != "team" || settings.Env["PERSONAL_ONLY"] != "" {
		t.Fatal("personal settings overrode selected team")
	}
}
