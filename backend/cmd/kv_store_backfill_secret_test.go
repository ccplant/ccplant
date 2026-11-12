package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollectNamedSecretRecordsSelectsOnlyExplicitNames(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-worker-slack-default", Namespace: "application"}, Data: map[string][]byte{"bot-token": []byte("secret")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unselected", Namespace: "application"}},
	)
	records, err := collectNamedSecretRecords(context.Background(), kvstore.NewKubernetesStore(client), "application", []string{"agentapi-worker-slack-default", "agentapi-worker-slack-default"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Key != "agentapi-worker-slack-default" || records[0].Kind != kvstore.KindSecret {
		t.Fatalf("records = %#v", records)
	}
}

func TestBackfillSecretFlagDefaultsUsePrimaryStoreEnvironment(t *testing.T) {
	values := map[string]string{
		"AGENTAPI_KV_STORE_PRIMARY_DATABASE_URL":             "libsql://primary.example",
		"AGENTAPI_KV_STORE_PRIMARY_AUTH_TOKEN":               "primary-auth",
		"AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_ACTIVE_KEY_ID": "primary-key",
		"AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_KEYS":          `{"primary-key":"key-ref"}`,
		"AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_PROVIDER":      "cloud-kms-branch-scoped",
		"AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_KMS_REGION":    "ap-northeast-1",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	command := newKVStoreBackfillSecretCommand()
	want := map[string]string{
		"database-url":             "libsql://primary.example",
		"auth-token":               "primary-auth",
		"encryption-active-key-id": "primary-key",
		"encryption-keys-json":     `{"primary-key":"key-ref"}`,
		"encryption-provider":      "cloud-kms-branch-scoped",
		"encryption-kms-region":    "ap-northeast-1",
	}
	for name, expected := range want {
		actual, err := command.Flags().GetString(name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if actual != expected {
			t.Errorf("%s = %q, want %q", name, actual, expected)
		}
	}
}

func TestBackfillSecretFlagDefaultsPreferSecondaryOverPrimary(t *testing.T) {
	t.Setenv("AGENTAPI_KV_STORE_SECONDARY_DATABASE_URL", "libsql://secondary.example")
	t.Setenv("AGENTAPI_KV_STORE_PRIMARY_DATABASE_URL", "libsql://primary.example")
	command := newKVStoreBackfillSecretCommand()
	actual, err := command.Flags().GetString("database-url")
	if err != nil {
		t.Fatal(err)
	}
	if actual != "libsql://secondary.example" {
		t.Fatalf("database-url = %q", actual)
	}
}

func TestCollectNamedSecretRecordsRejectsMissingSecret(t *testing.T) {
	_, err := collectNamedSecretRecords(context.Background(), kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "application", []string{"missing"})
	if err == nil || !strings.Contains(err.Error(), "source Secret application/missing not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestBackfillSecretEncryptsOperationalSecretInLibSQLFallback(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-worker-slack-default", Namespace: "application"},
		Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
	})
	source := kvstore.NewKubernetesStore(client)
	databasePath := filepath.Join(t.TempDir(), "backfill.db")
	backend, err := kvstore.NewLibSQLStore(ctx, "file://"+databasePath, "")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := encryptedMigrationDestination(ctx, backend, true, "local", "active", "", `{"active":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`)
	if err != nil {
		t.Fatal(err)
	}
	records, err := collectNamedSecretRecords(ctx, source, "application", []string{"agentapi-worker-slack-default"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := migrateSelectedKVStoreRecords(ctx, source, destination, records, kvStoreMigrateOptions{namespace: "application"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 {
		t.Fatalf("result = %#v", result)
	}
	got, err := destination.Get(ctx, kvstore.KindSecret, "application", "agentapi-worker-slack-default")
	if err != nil {
		t.Fatal(err)
	}
	var secret corev1.Secret
	if err := json.Unmarshal(got.Value, &secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["bot-token"]) != "xoxb-test" {
		t.Fatalf("decrypted bot token mismatch")
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT value FROM agentapi_kv WHERE kind = 'secret' AND namespace = ? AND key = ?`, "application", "agentapi-worker-slack-default").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "xoxb-test") {
		t.Fatal("libSQL stored plaintext token")
	}
	var envelope struct {
		Format string `json:"format"`
		KeyID  string `json:"key_id"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Format != "agentapi-kv-envelope/v1" || envelope.KeyID != "active" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestBackfillSecretRequiresEncryptionConfiguration(t *testing.T) {
	store := newMemoryKVStore()
	_, err := encryptedMigrationDestination(context.Background(), store, true, "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "libsql-encrypted destination requires") {
		t.Fatalf("error = %v", err)
	}
}
