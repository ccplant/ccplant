package repositories

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestKubernetesSettingsRepository_FindByName(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Save settings
	settings := entities.NewSettings("find-test")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetSecretAccessKey("test-secret")
	settings.SetBedrock(bedrock)
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Find settings
	loaded, err := repo.FindByName(ctx, "find-test")
	if err != nil {
		t.Fatalf("Failed to find settings: %v", err)
	}

	if loaded.Name() != "find-test" {
		t.Errorf("Expected name 'find-test', got '%s'", loaded.Name())
	}
	if loaded.Bedrock() == nil {
		t.Fatal("Expected Bedrock settings to be set")
	}
	if loaded.Bedrock().SecretAccessKey() != "test-secret" {
		t.Errorf("Expected secret access key to be preserved")
	}
}

func TestKubernetesSettingsRepository_FindByName_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	_, err := repo.FindByName(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent settings")
	}
}

func TestKubernetesSettingsRepository_Delete(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Save settings
	settings := entities.NewSettings("delete-test")
	settings.SetBedrock(entities.NewBedrockSettings(true))
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Verify exists
	exists, err := repo.Exists(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Failed to check existence: %v", err)
	}
	if !exists {
		t.Error("Expected settings to exist")
	}

	// Delete
	err = repo.Delete(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Failed to delete settings: %v", err)
	}

	// Verify deleted
	exists, err = repo.Exists(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Failed to check existence after delete: %v", err)
	}
	if exists {
		t.Error("Expected settings to be deleted")
	}
}

func TestKubernetesSettingsRepository_Delete_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error when deleting nonexistent settings")
	}
}

func TestKubernetesSettingsRepository_List(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Save multiple settings
	for _, name := range []string{"user1", "user2", "team-a"} {
		settings := entities.NewSettings(name)
		settings.SetBedrock(entities.NewBedrockSettings(true))
		err := repo.Save(ctx, settings)
		if err != nil {
			t.Fatalf("Failed to save settings for %s: %v", name, err)
		}
	}

	// List all
	settingsList, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list settings: %v", err)
	}

	if len(settingsList) != 3 {
		t.Errorf("Expected 3 settings, got %d", len(settingsList))
	}
}

func TestKubernetesSettingsRepository_List_SkipsInvalid(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create a valid settings secret
	settings := entities.NewSettings("valid")
	settings.SetBedrock(entities.NewBedrockSettings(true))
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Create an invalid secret with the settings label but invalid data
	invalidSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentapi-settings-invalid",
			Namespace: "default",
			Labels: map[string]string{
				LabelSettings: "true",
			},
		},
		Data: map[string][]byte{
			SecretKeySettings: []byte("invalid json"),
		},
	}
	_, err = client.CoreV1().Secrets("default").Create(ctx, invalidSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create invalid secret: %v", err)
	}

	// List should only return valid settings
	settingsList, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list settings: %v", err)
	}

	if len(settingsList) != 1 {
		t.Errorf("Expected 1 valid settings, got %d", len(settingsList))
	}
}

func TestSanitizeSecretName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"UPPERCASE", "uppercase"},
		{"with spaces", "with-spaces"},
		{"org/team-slug", "org-team-slug"},
		{"user@example.com", "user-example-com"},
		{"--leading-trailing--", "leading-trailing"},
		{"multiple---dashes", "multiple-dashes"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeSecretName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeSecretName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestKubernetesSettingsRepositoryAutoSuspendRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewKubernetesSettingsRepository(fake.NewSimpleClientset(), "default")
	settings := entities.NewSettings("user-1")
	settings.SetAutoSuspend(&entities.AutoSuspendSettings{Enabled: true, IdleTimeoutMinutes: 240})
	if err := repo.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.FindByName(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AutoSuspend() == nil || !loaded.AutoSuspend().Enabled || loaded.AutoSuspend().IdleTimeoutMinutes != 240 {
		t.Fatalf("auto suspend policy was not preserved: %#v", loaded.AutoSuspend())
	}
}

func TestKubernetesSettingsRepositoryEnabledTeamProfilesRoundTrip(t *testing.T) {
	repo := NewKubernetesSettingsRepository(fake.NewSimpleClientset(), "default")
	settings := entities.NewSettings("alice")
	settings.SetEnabledTeamSessionProfileIDs([]string{"profile-a", "profile-b"})

	require.NoError(t, repo.Save(context.Background(), settings))
	loaded, err := repo.FindByName(context.Background(), "alice")
	require.NoError(t, err)
	require.Equal(t, []string{"profile-a", "profile-b"}, loaded.EnabledTeamSessionProfileIDs())
}

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with spaces", "with-spaces"},
		{"org/team-slug", "org-team-slug"},
		{"--leading-trailing--", "leading-trailing"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeLabelValue(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
