package repositories

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

func TestKubernetesSettingsRepository_Save(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")

	settings := entities.NewSettings("test-user")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	settings.SetBedrock(bedrock)

	ctx := context.Background()
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Verify Secret was created
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-test-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	if secret.Labels[LabelSettings] != "true" {
		t.Errorf("Expected label %s to be 'true'", LabelSettings)
	}
	if _, ok := secret.Data[SecretKeySettings]; !ok {
		t.Error("Expected settings.json key in secret data")
	}
}

func TestKubernetesSettingsRepository_Save_VerifySecretContent(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")

	settings := entities.NewSettings("verify-content")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	bedrock.SetRoleARN("arn:aws:iam::123456789012:role/ExampleRole")
	bedrock.SetProfile("production")
	settings.SetBedrock(bedrock)

	ctx := context.Background()
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Verify Secret was created in Kubernetes
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-verify-content", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret from Kubernetes: %v", err)
	}

	// Verify Secret labels
	if secret.Labels[LabelSettings] != "true" {
		t.Errorf("Expected label %s to be 'true', got '%s'", LabelSettings, secret.Labels[LabelSettings])
	}
	if secret.Labels[LabelSettingsName] != "verify-content" {
		t.Errorf("Expected label %s to be 'verify-content', got '%s'", LabelSettingsName, secret.Labels[LabelSettingsName])
	}

	// Verify Secret type
	if secret.Type != corev1.SecretTypeOpaque {
		t.Errorf("Expected Secret type to be Opaque, got '%s'", secret.Type)
	}

	// Verify Secret data contains settings.json
	data, ok := secret.Data[SecretKeySettings]
	if !ok {
		t.Fatal("Expected settings.json key in secret data")
	}

	// Parse and verify JSON content
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse secret data as JSON: %v", err)
	}

	// Verify name
	if parsed["name"] != "verify-content" {
		t.Errorf("Expected name 'verify-content' in JSON, got '%v'", parsed["name"])
	}

	// Verify bedrock section exists
	bedrockData, ok := parsed["bedrock"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected bedrock section in JSON")
	}

	// Verify all bedrock fields
	if bedrockData["enabled"] != true {
		t.Errorf("Expected enabled=true, got %v", bedrockData["enabled"])
	}
	if bedrockData["model"] != "anthropic.claude-sonnet-4-20250514-v1:0" {
		t.Errorf("Expected model='anthropic.claude-sonnet-4-20250514-v1:0', got '%v'", bedrockData["model"])
	}
	// Verify sensitive fields are stored as plaintext
	if bedrockData["access_key_id"] != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("Expected access_key_id='AKIAIOSFODNN7EXAMPLE', got '%v'", bedrockData["access_key_id"])
	}
	if bedrockData["secret_access_key"] != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("Expected secret_access_key='wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY', got '%v'", bedrockData["secret_access_key"])
	}
	if bedrockData["role_arn"] != "arn:aws:iam::123456789012:role/ExampleRole" {
		t.Errorf("Expected role_arn to be preserved, got '%v'", bedrockData["role_arn"])
	}
	if bedrockData["profile"] != "production" {
		t.Errorf("Expected profile='production', got '%v'", bedrockData["profile"])
	}

	// Verify timestamps exist
	if parsed["created_at"] == nil {
		t.Error("Expected created_at in JSON")
	}
	if parsed["updated_at"] == nil {
		t.Error("Expected updated_at in JSON")
	}
}

func TestKubernetesSettingsRepository_SaveUpdate(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create initial settings
	settings := entities.NewSettings("test-user")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("model-v1")
	settings.SetBedrock(bedrock)
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save initial settings: %v", err)
	}

	// Update settings
	updatedBedrock := entities.NewBedrockSettings(true)
	updatedBedrock.SetModel("model-v2")
	settings.SetBedrock(updatedBedrock)
	err = repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to update settings: %v", err)
	}

	// Verify updated
	loaded, err := repo.FindByName(ctx, "test-user")
	if err != nil {
		t.Fatalf("Failed to load settings: %v", err)
	}
	if loaded.Bedrock().Model() != "model-v2" {
		t.Errorf("Expected model 'model-v2', got '%s'", loaded.Bedrock().Model())
	}
}

func TestKubernetesSettingsRepository_SaveUpdateWithLibSQL(t *testing.T) {
	ctx := context.Background()
	store, err := kvstore.NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "settings.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	client := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	repo := NewKubernetesSettingsRepository(client, "default")
	settings := entities.NewSettings("libsql-user")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("model-v1")
	settings.SetBedrock(bedrock)
	if err := repo.Save(ctx, settings); err != nil {
		t.Fatalf("save initial settings: %v", err)
	}

	bedrock.SetModel("model-v2")
	if err := repo.Save(ctx, settings); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	loaded, err := repo.FindByName(ctx, "libsql-user")
	if err != nil {
		t.Fatalf("load updated settings: %v", err)
	}
	if got := loaded.Bedrock().Model(); got != "model-v2" {
		t.Fatalf("model = %q, want model-v2", got)
	}
}

func TestKubernetesSettingsRepository_SaveUpdate_AllFields(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create initial settings with all fields
	settings := entities.NewSettings("update-all-fields")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7INITIAL")
	bedrock.SetSecretAccessKey("initial-secret-key")
	bedrock.SetRoleARN("arn:aws:iam::111111111111:role/InitialRole")
	bedrock.SetProfile("initial-profile")
	settings.SetBedrock(bedrock)

	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save initial settings: %v", err)
	}

	// Verify initial save in Kubernetes Secret
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-update-all-fields", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get initial secret: %v", err)
	}
	if secret.Labels[LabelSettings] != "true" {
		t.Error("Expected settings label on initial secret")
	}

	// Update all fields
	updatedBedrock := entities.NewBedrockSettings(true)
	updatedBedrock.SetModel("anthropic.claude-opus-4-20250514-v1:0")
	updatedBedrock.SetAccessKeyID("AKIAIOSFODNN7UPDATED")
	updatedBedrock.SetSecretAccessKey("updated-secret-key")
	updatedBedrock.SetRoleARN("arn:aws:iam::222222222222:role/UpdatedRole")
	updatedBedrock.SetProfile("updated-profile")
	settings.SetBedrock(updatedBedrock)
	settings.SetGitHubAppInstallationID("4242")
	settings.SetGitHubRepositories([]string{"acme/payments", "acme/shared"})

	err = repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to update settings: %v", err)
	}

	// Verify Secret was updated in Kubernetes (not recreated)
	updatedSecret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-update-all-fields", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}

	// Parse and verify updated content
	data, ok := updatedSecret.Data[SecretKeySettings]
	if !ok {
		t.Fatal("Expected settings.json key in updated secret")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse updated secret data: %v", err)
	}

	bedrockData, ok := parsed["bedrock"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected bedrock section in updated JSON")
	}

	// Verify all updated values
	if bedrockData["model"] != "anthropic.claude-opus-4-20250514-v1:0" {
		t.Errorf("Expected updated model 'anthropic.claude-opus-4-20250514-v1:0', got '%v'", bedrockData["model"])
	}
	// Verify sensitive fields are stored as plaintext
	if bedrockData["access_key_id"] != "AKIAIOSFODNN7UPDATED" {
		t.Errorf("Expected access_key_id='AKIAIOSFODNN7UPDATED', got '%v'", bedrockData["access_key_id"])
	}
	if bedrockData["secret_access_key"] != "updated-secret-key" {
		t.Errorf("Expected secret_access_key='updated-secret-key', got '%v'", bedrockData["secret_access_key"])
	}
	if bedrockData["role_arn"] != "arn:aws:iam::222222222222:role/UpdatedRole" {
		t.Errorf("Expected updated role_arn, got '%v'", bedrockData["role_arn"])
	}
	if bedrockData["profile"] != "updated-profile" {
		t.Errorf("Expected updated profile 'updated-profile', got '%v'", bedrockData["profile"])
	}
	if parsed["github_app_installation_id"] != "4242" {
		t.Errorf("Expected github_app_installation_id='4242', got '%v'", parsed["github_app_installation_id"])
	}
	if repositories, ok := parsed["github_repositories"].([]interface{}); !ok || len(repositories) != 2 {
		t.Errorf("Expected two github_repositories, got '%v'", parsed["github_repositories"])
	}

	// Verify through FindByName as well
	loaded, err := repo.FindByName(ctx, "update-all-fields")
	if err != nil {
		t.Fatalf("Failed to load settings after update: %v", err)
	}

	if loaded.Bedrock().Model() != "anthropic.claude-opus-4-20250514-v1:0" {
		t.Errorf("FindByName: Expected model 'anthropic.claude-opus-4-20250514-v1:0', got '%s'", loaded.Bedrock().Model())
	}
	if loaded.Bedrock().AccessKeyID() != "AKIAIOSFODNN7UPDATED" {
		t.Errorf("FindByName: Expected access_key_id 'AKIAIOSFODNN7UPDATED', got '%s'", loaded.Bedrock().AccessKeyID())
	}
	if loaded.Bedrock().SecretAccessKey() != "updated-secret-key" {
		t.Errorf("FindByName: Expected secret_access_key 'updated-secret-key', got '%s'", loaded.Bedrock().SecretAccessKey())
	}
	if loaded.Bedrock().RoleARN() != "arn:aws:iam::222222222222:role/UpdatedRole" {
		t.Errorf("FindByName: Expected role_arn to be updated, got '%s'", loaded.Bedrock().RoleARN())
	}
	if loaded.Bedrock().Profile() != "updated-profile" {
		t.Errorf("FindByName: Expected profile 'updated-profile', got '%s'", loaded.Bedrock().Profile())
	}
	if loaded.GitHubAppInstallationID() != "4242" {
		t.Errorf("FindByName: Expected github_app_installation_id '4242', got '%s'", loaded.GitHubAppInstallationID())
	}
	if got := loaded.GitHubRepositories(); len(got) != 2 || got[0] != "acme/payments" || got[1] != "acme/shared" {
		t.Errorf("FindByName: unexpected github_repositories: %v", got)
	}
}

func TestKubernetesSettingsRepository_SaveUpdate_VerifySecretOverwritten(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create initial settings
	settings := entities.NewSettings("overwrite-test")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("model-v1")
	settings.SetBedrock(bedrock)
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save initial settings: %v", err)
	}

	// Get initial Secret version
	initialSecret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-overwrite-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get initial secret: %v", err)
	}
	initialData := string(initialSecret.Data[SecretKeySettings])

	// Update settings
	updatedBedrock := entities.NewBedrockSettings(true)
	updatedBedrock.SetModel("model-v2")
	settings.SetBedrock(updatedBedrock)
	err = repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to update settings: %v", err)
	}

	// Verify Secret was updated (not recreated with different name)
	updatedSecret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-overwrite-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}
	updatedData := string(updatedSecret.Data[SecretKeySettings])

	// Data should be different
	if initialData == updatedData {
		t.Error("Expected Secret data to be different after update")
	}

	// Parse updated data and verify model
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(updatedData), &parsed); err != nil {
		t.Fatalf("Failed to parse updated data: %v", err)
	}
	bedrockData := parsed["bedrock"].(map[string]interface{})
	if bedrockData["model"] != "model-v2" {
		t.Errorf("Expected updated model 'model-v2', got '%v'", bedrockData["model"])
	}

	// Verify only one Secret exists with this prefix
	secrets, err := client.CoreV1().Secrets("default").List(ctx, metav1.ListOptions{
		LabelSelector: LabelSettings + "=true",
	})
	if err != nil {
		t.Fatalf("Failed to list secrets: %v", err)
	}

	overwriteSecretCount := 0
	for _, s := range secrets.Items {
		if s.Name == "agentapi-settings-overwrite-test" {
			overwriteSecretCount++
		}
	}
	if overwriteSecretCount != 1 {
		t.Errorf("Expected exactly 1 Secret for overwrite-test, got %d", overwriteSecretCount)
	}
}
