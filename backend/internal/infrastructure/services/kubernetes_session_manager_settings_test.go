package services

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type fakeSettingsRepository struct {
	settings map[string]*entities.Settings
}

type fakeUserFileRepository struct {
	files map[string][]*entities.UserFile
}

func TestResolveProjectedSecretsFiltersProfileReferences(t *testing.T) {
	settings := entities.NewSettings("user-1")
	settings.SetSecretSettings([]entities.SecretSetting{
		{ID: "selected", Values: map[string]string{"token": "selected-value"}, Projections: []entities.SecretProjection{{Key: "token", Type: "env", EnvName: "SELECTED"}}},
		{ID: "other", Values: map[string]string{"token": "other-value"}, Projections: []entities.SecretProjection{{Key: "token", Type: "env", EnvName: "OTHER"}}},
	})
	manager := &KubernetesSessionManager{settingsRepo: &fakeSettingsRepository{settings: map[string]*entities.Settings{"user-1": settings}}}

	env, files := manager.resolveProjectedSecrets(context.Background(), &entities.RunServerRequest{
		UserID: "user-1", Scope: entities.ScopeUser, ProfileSecretIDs: []string{"selected"},
	})
	if env["SELECTED"] != "selected-value" {
		t.Fatalf("SELECTED = %q", env["SELECTED"])
	}
	if _, ok := env["OTHER"]; ok {
		t.Fatal("unreferenced secret was projected")
	}
	if len(files) != 0 {
		t.Fatalf("files = %#v", files)
	}
}

func (r *fakeUserFileRepository) Save(_ context.Context, userID string, file *entities.UserFile) error {
	r.files[userID] = append(r.files[userID], file)
	return nil
}

func (r *fakeUserFileRepository) FindByID(_ context.Context, userID, fileID string) (*entities.UserFile, error) {
	for _, file := range r.files[userID] {
		if file.ID() == fileID {
			return file, nil
		}
	}
	return nil, fmt.Errorf("user file not found: %s", fileID)
}

func (r *fakeUserFileRepository) List(_ context.Context, userID string) ([]*entities.UserFile, error) {
	return r.files[userID], nil
}

func (r *fakeUserFileRepository) Delete(_ context.Context, userID, fileID string) error {
	files := r.files[userID]
	for i, file := range files {
		if file.ID() == fileID {
			r.files[userID] = append(files[:i], files[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("user file not found: %s", fileID)
}

func TestBuildSessionSettings_UserFilesComeFromInjectedRepository(t *testing.T) {
	sessionClient := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-user-files-test-user", Namespace: "test-ns"},
			Data: map[string][]byte{
				"0.path":    []byte("/stale/file"),
				"0.content": []byte("stale"),
			},
		},
	)
	wantFile := entities.NewUserFile("file-1", "GCP key", "/opt/gcp/cred.json", "kv-content", "0600")
	userFileRepo := &fakeUserFileRepository{files: map[string][]*entities.UserFile{"test-user": {wantFile}}}

	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), sessionClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.SetUserFileRepository(userFileRepo)
	req := &entities.RunServerRequest{UserID: "test-user", Scope: entities.ScopeUser}
	session := NewKubernetesSession("test-session", req,
		"test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if len(settings.Files) != 1 {
		t.Fatalf("managed files count = %d, want 1", len(settings.Files))
	}
	if got := settings.Files[0]; got.Path != "/opt/gcp/cred.json" || got.Content != "kv-content" || got.Permissions != "0600" {
		t.Fatalf("managed file = %#v, want injected repository file", got)
	}
}

func TestBuildSessionSettings_TeamScopeUsesSessionUserCredentialsWhenSelected(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-agent-files-test-user", Namespace: "test-ns"},
			Data: sessionsettings.FilesToSecretData([]sessionsettings.ManagedFile{{
				Path:    sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth],
				Content: `{"tokens":{"access_token":"user-token"}}`,
			}}),
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-agent-files-org-team-a", Namespace: "test-ns"},
			Data: sessionsettings.FilesToSecretData([]sessionsettings.ManagedFile{{
				Path:    sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth],
				Content: `{"tokens":{"access_token":"team-token"}}`,
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
	session := NewKubernetesSession("test-session", &entities.RunServerRequest{UserID: "test-user"},
		"test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)

	settings := manager.buildSessionSettings(context.Background(), session, &entities.RunServerRequest{
		UserID:           "test-user",
		Scope:            entities.ScopeTeam,
		TeamID:           "org/team-a",
		CredentialSource: "session_user",
	}, nil)

	if len(settings.Files) != 1 {
		t.Fatalf("managed files count = %d, want 1", len(settings.Files))
	}
	if got := settings.Files[0].Content; got != `{"tokens":{"access_token":"user-token"}}` {
		t.Fatalf("credential content = %q, want session user's credentials", got)
	}
}

func TestBuildSessionSettings_EmbedsProfileFilesForTeamScope(t *testing.T) {
	sessionClient := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
	)
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), sessionClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"
	req := &entities.RunServerRequest{
		UserID: "test-user", Scope: entities.ScopeTeam, TeamID: "org/team-a",
		ProfileFiles: []sessionsettings.ManagedFile{{
			Path:        "/home/agentapi/.ssh/id_ed25519",
			Content:     "private-key",
			Permissions: "0600",
		}},
	}
	session := NewKubernetesSession("test-session", req,
		"test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if len(settings.Files) != 1 {
		t.Fatalf("managed files count = %d, want 1", len(settings.Files))
	}
	if got := settings.Files[0]; got.Path != "/home/agentapi/.ssh/id_ed25519" || got.Content != "private-key" || got.Permissions != "0600" {
		t.Fatalf("profile file = %#v", got)
	}
}

func TestBuildSessionSettings_GitHubSenderCredentialsFallBackToTeam(t *testing.T) {
	tests := []struct {
		name               string
		triggeredUserID    string
		includeUserSecret  bool
		wantCredentialBody string
	}{
		{
			name:               "uses triggered user credentials when present",
			triggeredUserID:    "github-user",
			includeUserSecret:  true,
			wantCredentialBody: `{"tokens":{"access_token":"triggered-token"}}`,
		},
		{
			name:               "falls back when triggered user credentials are missing",
			triggeredUserID:    "github-user",
			wantCredentialBody: `{"tokens":{"access_token":"team-token"}}`,
		},
		{
			name:               "falls back when triggered user is unresolved",
			wantCredentialBody: `{"tokens":{"access_token":"team-token"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "agentapi-agent-files-org-team-a", Namespace: "test-ns"},
					Data: sessionsettings.FilesToSecretData([]sessionsettings.ManagedFile{{
						Path: sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth], Content: `{"tokens":{"access_token":"team-token"}}`,
					}}),
				},
			}
			if tt.includeUserSecret {
				objects = append(objects, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "agentapi-agent-files-github-user", Namespace: "test-ns"},
					Data: sessionsettings.FilesToSecretData([]sessionsettings.ManagedFile{{
						Path: sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth], Content: tt.wantCredentialBody,
					}}),
				})
			}

			k8sClient := fake.NewSimpleClientset(objects...)
			cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
				Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000, PVCEnabled: boolPtrForTest(false),
			}}
			manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
			if err != nil {
				t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
			}
			manager.namespace = "test-ns"
			req := &entities.RunServerRequest{
				UserID: "webhook-owner", TriggeredUserID: tt.triggeredUserID,
				Scope: entities.ScopeTeam, TeamID: "org/team-a", CredentialSource: "github_sender",
			}
			session := NewKubernetesSession("test-session", req, "test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)
			settings := manager.buildSessionSettings(context.Background(), session, req, nil)
			if len(settings.Files) != 1 {
				t.Fatalf("managed files count = %d, want 1", len(settings.Files))
			}
			if got := settings.Files[0].Content; got != tt.wantCredentialBody {
				t.Fatalf("credential content = %q, want %q", got, tt.wantCredentialBody)
			}
		})
	}
}

func TestBuildSessionSettings_GitHubSenderAuthAndEnvironmentOverrideTeam(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"

	teamSettings := entities.NewSettings("org/team-a")
	teamSettings.SetEnvVars(map[string]string{"SHARED": "team", "TEAM_ONLY": "team-value"})
	teamSettings.SetAuthMode(entities.AuthModeBedrock)
	senderSettings := entities.NewSettings("github-user")
	senderSettings.SetEnvVars(map[string]string{"SHARED": "sender", "SENDER_ONLY": "sender-value"})
	senderSettings.SetAuthMode(entities.AuthModeOAuth)
	senderSettings.SetClaudeCodeOAuthToken("sender-oauth-token")
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{
		"org/team-a":  teamSettings,
		"github-user": senderSettings,
	}})

	req := &entities.RunServerRequest{
		UserID: "webhook-owner", TriggeredUserID: "github-user",
		Scope: entities.ScopeTeam, TeamID: "org/team-a", CredentialSource: "github_sender",
	}
	session := NewKubernetesSession("test-session", req,
		"test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)
	settings := manager.buildSessionSettings(context.Background(), session, req, nil)

	for key, want := range map[string]string{
		"SHARED": "sender", "TEAM_ONLY": "team-value", "SENDER_ONLY": "sender-value",
		"CLAUDE_CODE_USE_BEDROCK": "0", "CLAUDE_CODE_OAUTH_TOKEN": "sender-oauth-token",
	} {
		if got := settings.Env[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestBuildSessionSettings_ProfileEnvironmentOverridesTeamBedrockAndRequestOverridesProfile(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"

	teamSettings := entities.NewSettings("org/team-a")
	teamSettings.SetEnvVars(map[string]string{"SHARED": "team", "TEAM_ONLY": "team-value"})
	teamSettings.SetAuthMode(entities.AuthModeBedrock)
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("team-model")
	teamSettings.SetBedrock(bedrock)
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{
		"org/team-a": teamSettings,
	}})

	req := &entities.RunServerRequest{
		UserID: "test-user", Scope: entities.ScopeTeam, TeamID: "org/team-a",
		ProfileEnvironment: map[string]string{
			"CLAUDE_CODE_USE_BEDROCK": "0", "ANTHROPIC_MODEL": "profile-model",
			"SHARED": "profile", "PROFILE_ONLY": "profile-value",
		},
		Environment: map[string]string{"SHARED": "request"},
	}
	session := NewKubernetesSession("test-session", req,
		"test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)
	settings := manager.buildSessionSettings(context.Background(), session, req, nil)

	for key, want := range map[string]string{
		"CLAUDE_CODE_USE_BEDROCK": "0", "ANTHROPIC_MODEL": "profile-model",
		"TEAM_ONLY": "team-value", "PROFILE_ONLY": "profile-value", "SHARED": "request",
	} {
		if got := settings.Env[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestBuildSessionSettings_WritesBedrockStaticCredentialsAsProfile(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"

	userSettings := entities.NewSettings("test-user")
	userSettings.SetAuthMode(entities.AuthModeBedrock)
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("arn:aws:bedrock:ap-northeast-1:123456789012:inference-profile/example")
	bedrock.SetAccessKeyID("AKIATEST")
	bedrock.SetSecretAccessKey("secret/key")
	userSettings.SetBedrock(bedrock)
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{
		"test-user": userSettings,
	}})

	req := &entities.RunServerRequest{UserID: "test-user", AgentType: "claude-acp"}
	session := NewKubernetesSession("test-session", req,
		"test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)
	settings := manager.buildSessionSettings(context.Background(), session, req, nil)

	if settings.Env["AWS_PROFILE"] != bedrockAWSProfile || settings.Env["AWS_SHARED_CREDENTIALS_FILE"] != bedrockCredentialsFile || settings.Env["AWS_CONFIG_FILE"] != bedrockConfigFile {
		t.Fatalf("Bedrock profile environment was not configured: %#v", settings.Env)
	}
	if _, exists := settings.Env["AWS_REGION"]; exists {
		t.Fatal("AWS_REGION must not remain in the session environment")
	}
	if _, exists := settings.Env["AWS_ACCESS_KEY_ID"]; exists {
		t.Fatal("AWS_ACCESS_KEY_ID must not remain in the session environment")
	}
	if len(settings.Files) != 2 {
		t.Fatalf("managed files count = %d, want 2", len(settings.Files))
	}
	if got := settings.Files[0]; got.Path != bedrockCredentialsFile || got.Permissions != "0600" || got.Content != "[bedrock]\naws_access_key_id = AKIATEST\naws_secret_access_key = secret/key\n" {
		t.Fatalf("Bedrock credentials file = %#v", got)
	}
	if got := settings.Files[1]; got.Path != bedrockConfigFile || got.Permissions != "0600" || got.Content != "[profile bedrock]\nregion = ap-northeast-1\n" {
		t.Fatalf("Bedrock config file = %#v", got)
	}
}
