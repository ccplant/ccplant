package remotekv_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/remotekv"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

func TestStoreRoundTripThroughApplicationKVAPI(t *testing.T) {
	backend, err := kvstore.NewLibSQLStore(context.Background(), "file:"+filepath.Join(t.TempDir(), "application.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()

	e := echo.New()
	controller := controllers.NewApplicationKVController(backend, "worker-token")
	e.Any("/internal/application-kv", controller.Handle)
	server := httptest.NewServer(e)
	defer server.Close()

	store := remotekv.New(server.URL, "worker-token")
	record := kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "application", Key: "slack-cache", Labels: map[string]string{"app": "slack"}, Value: []byte(`{"channel":"general"}`)}
	created, err := store.Create(context.Background(), record)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := store.Get(context.Background(), record.Kind, record.Namespace, record.Key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got.Value) != string(record.Value) {
		t.Fatalf("Get().Value = %q, want %q", got.Value, record.Value)
	}
	records, err := store.List(context.Background(), kvstore.Query{Kind: record.Kind, Namespace: record.Namespace, LabelSelector: "app=slack"})
	if err != nil || len(records) != 1 {
		t.Fatalf("List() = %#v, %v", records, err)
	}
	if err := store.Delete(context.Background(), record.Kind, record.Namespace, record.Key, created.Version); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(context.Background(), record.Kind, record.Namespace, record.Key); err != kvstore.ErrNotFound {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}
}

func TestStoreRejectsInvalidToken(t *testing.T) {
	backend, err := kvstore.NewLibSQLStore(context.Background(), "file:"+filepath.Join(t.TempDir(), "application.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	e := echo.New()
	e.Any("/internal/application-kv", controllers.NewApplicationKVController(backend, "valid-token").Handle)
	server := httptest.NewServer(e)
	defer server.Close()

	_, err = remotekv.New(server.URL, "invalid-token").Get(context.Background(), kvstore.KindSecret, "application", "credential")
	if err == nil {
		t.Fatal("Get() accepted an invalid token")
	}
}
