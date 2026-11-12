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
