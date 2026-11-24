package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func TestBuildSessionSettings_MergesSettingsAndProfileSkills(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}})
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000, PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"
	userSettings := entities.NewSettings("test-user")
	userSettings.SetSkills([]string{"org/settings-skills", "org/shared"})
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{"test-user": userSettings}})

	req := &entities.RunServerRequest{
		UserID: "test-user", Scope: entities.ScopeUser,
		ProfileSkills: []string{"org/shared", "org/profile-skills"},
	}
	session := NewKubernetesSession("test-session", req, "test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)
	settings := manager.buildSessionSettings(context.Background(), session, req, nil)

	got, ok := settings.Claude.SettingsJSON["skills"].([]string)
	if !ok {
		t.Fatalf("skills type = %T", settings.Claude.SettingsJSON["skills"])
	}
	want := []string{"org/profile-skills", "org/settings-skills", "org/shared"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skills = %#v, want %#v", got, want)
	}
}

func (r *fakeSettingsRepository) Save(ctx context.Context, settings *entities.Settings) error {
	r.settings[settings.Name()] = settings
	return nil
}

func (r *fakeSettingsRepository) FindByName(ctx context.Context, name string) (*entities.Settings, error) {
	settings, ok := r.settings[name]
	if !ok {
		return nil, fmt.Errorf("settings not found: %s", name)
	}
	return settings, nil
}

func (r *fakeSettingsRepository) Delete(ctx context.Context, name string) error {
	delete(r.settings, name)
	return nil
}

func (r *fakeSettingsRepository) Exists(ctx context.Context, name string) (bool, error) {
	_, ok := r.settings[name]
	return ok, nil
}

func (r *fakeSettingsRepository) List(ctx context.Context) ([]*entities.Settings, error) {
	result := make([]*entities.Settings, 0, len(r.settings))
	for _, settings := range r.settings {
		result = append(result, settings)
	}
	return result, nil
}

func TestBuildSessionSettings_TeamSettingsUsesRepositoryEnvVars(t *testing.T) {
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

	teamSettings := entities.NewSettings("org/team-a")
	teamSettings.SetEnvVars(map[string]string{"SECRET_TOKEN": "decrypted-secret"})
	manager.SetSettingsRepository(&fakeSettingsRepository{
		settings: map[string]*entities.Settings{
			"org/team-a": teamSettings,
		},
	})

	// This is the shape written by KubernetesSettingsRepository when env vars are
	// encrypted. The session manager must not rely on this raw JSON for team/user
	// settings because SettingsPatch intentionally has no decryptor.
	_, err = k8sClient.CoreV1().Secrets("test-ns").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-org-team-a", Namespace: "test-ns"},
		Data: map[string][]byte{
			"settings.json": []byte(`{
				"name": "org/team-a",
				"encrypted_env_vars": {
					"SECRET_TOKEN": {
						"v": "ciphertext",
						"alg": "aes-256-gcm",
						"kid": "sha256:test",
						"at": "2026-06-10T00:00:00Z",
						"ver": "v1"
					}
				}
			}`),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create settings secret: %v", err)
	}

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
		UserID: "test-user",
		Scope:  entities.ScopeTeam,
		TeamID: "org/team-a",
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if got := settings.Env["SECRET_TOKEN"]; got != "decrypted-secret" {
		t.Fatalf("SECRET_TOKEN = %q, want decrypted-secret", got)
	}
}

func TestBuildSessionSettings_TeamGitHubInstallationIDCreatesInitialToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v3/app/installations/4242/access_tokens" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"team-installation-token"}`))
	}))
	defer api.Close()
	t.Setenv("GITHUB_API", api.URL+"/")

	k8sClient := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "github-auth", Namespace: "test-ns"},
			Data: map[string][]byte{
				"GITHUB_APP_ID":  []byte("1234"),
				"GITHUB_APP_PEM": pemData,
				"GITHUB_TOKEN":   []byte("shared-token"),
			},
		},
	)
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace:        "test-ns",
		Image:            "test-image:latest",
		BasePort:         9000,
		PVCEnabled:       boolPtrForTest(false),
		GitHubSecretName: "github-auth",
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatal(err)
	}
	manager.namespace = "test-ns"
	teamSettings := entities.NewSettings("org/team-a")
	teamSettings.SetGitHubAppInstallationID("4242")
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{
		"org/team-a": teamSettings,
	}})

	session := NewKubernetesSession("test-session", &entities.RunServerRequest{UserID: "test-user"},
		"test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)
	settings := manager.buildSessionSettings(context.Background(), session, &entities.RunServerRequest{
		UserID: "test-user",
		Scope:  entities.ScopeTeam,
		TeamID: "org/team-a",
	}, nil)

	if got := settings.Env["GITHUB_TOKEN"]; got != "team-installation-token" {
		t.Fatalf("GITHUB_TOKEN = %q, want team installation token", got)
	}
	if got := settings.Env["GITHUB_INSTALLATION_ID"]; got != "4242" {
		t.Fatalf("GITHUB_INSTALLATION_ID = %q, want 4242", got)
	}
}

func TestBuildSessionSettings_InheritsGitHubAppEnvironment(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_INSTALLATION_ID", "67890")
	t.Setenv("GITHUB_APP_PEM", "test-pem")
	t.Setenv("GITHUB_API", "https://ghe.example.com/api/v3")
	t.Setenv("GITHUB_URL", "https://ghe.example.com")

	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}})
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace:  "test-ns",
		Image:      "test-image:latest",
		BasePort:   9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatal(err)
	}
	manager.namespace = "test-ns"
	req := &entities.RunServerRequest{UserID: "test-user"}
	session := NewKubernetesSession("test-session", req, "test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)

	want := map[string]string{
		"GITHUB_APP_ID":          "12345",
		"GITHUB_INSTALLATION_ID": "67890",
		"GITHUB_APP_PEM":         "test-pem",
		"GITHUB_API":             "https://ghe.example.com/api/v3",
		"GITHUB_URL":             "https://ghe.example.com",
	}
	for key, expected := range want {
		if got := settings.Env[key]; got != expected {
			t.Errorf("settings.Env[%q] = %q, want %q", key, got, expected)
		}
	}
}

func TestBuildSessionSettings_GitHubTokenExcludesInheritedAppCredentials(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PEM", "test-pem")

	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}})
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace:  "test-ns",
		Image:      "test-image:latest",
		BasePort:   9000,
		PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatal(err)
	}
	manager.namespace = "test-ns"
	req := &entities.RunServerRequest{UserID: "test-user", GithubToken: "request-token"}
	session := NewKubernetesSession("test-session", req, "test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)

	if got := settings.Env["GITHUB_TOKEN"]; got != "request-token" {
		t.Fatalf("GITHUB_TOKEN = %q, want request-token", got)
	}
	if _, ok := settings.Env["GITHUB_APP_ID"]; ok {
		t.Error("GITHUB_APP_ID should not be inherited when a request token is provided")
	}
	if _, ok := settings.Env["GITHUB_APP_PEM"]; ok {
		t.Error("GITHUB_APP_PEM should not be inherited when a request token is provided")
	}
}

func TestBuildSessionSettings_LoadsGitHubConfigSecretWithoutAuthentication(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "github-config", Namespace: "test-ns"},
			Data: map[string][]byte{
				"GITHUB_API": []byte("https://ghe.example.com/api/v3"),
				"GITHUB_URL": []byte("https://ghe.example.com"),
			},
		},
	)
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace:              "test-ns",
		Image:                  "test-image:latest",
		BasePort:               9000,
		PVCEnabled:             boolPtrForTest(false),
		GitHubConfigSecretName: "github-config",
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatal(err)
	}
	req := &entities.RunServerRequest{UserID: "test-user"}
	session := NewKubernetesSession("test-session", req, "test-deploy", "test-service", "test-pvc", "test-ns", 9000, nil, nil)

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)

	if got := settings.Env["GITHUB_API"]; got != "https://ghe.example.com/api/v3" {
		t.Errorf("GITHUB_API = %q", got)
	}
	if got := settings.Env["GITHUB_URL"]; got != "https://ghe.example.com" {
		t.Errorf("GITHUB_URL = %q", got)
	}
	if settings.Github == nil || settings.Github.SecretName != "" || settings.Github.ConfigSecretName != "github-config" {
		t.Errorf("Github = %+v, want config-only github-config", settings.Github)
	}
}

func TestBuildSessionSettings_CodexACPDisablesNestedSandbox(t *testing.T) {
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

	req := &entities.RunServerRequest{
		UserID:    "test-user",
		AgentType: "codex-acp",
	}
	session := NewKubernetesSession(
		"test-session",
		req,
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if got := settings.Env["INITIAL_AGENT_MODE"]; got != "agent-full-access" {
		t.Fatalf("INITIAL_AGENT_MODE = %q, want agent-full-access", got)
	}
	if !strings.Contains(settings.Codex.ConfigTOML, `sandbox_mode = "danger-full-access"`) {
		t.Fatalf("Codex config does not disable the nested sandbox: %q", settings.Codex.ConfigTOML)
	}
	if !strings.Contains(settings.Codex.ConfigTOML, `check_for_update_on_startup = false`) {
		t.Fatalf("Codex config does not disable startup update checks: %q", settings.Codex.ConfigTOML)
	}
}
