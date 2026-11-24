package sessionmanagerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClientRequestsManagerOwnedSuspend(t *testing.T) {
	manager := newFakeManager()
	client, _ := newTestClient(t, manager)
	if err := client.SuspendSession(context.Background(), "session-1"); err != nil {
		t.Fatal(err)
	}
	if manager.suspendedID != "session-1" {
		t.Fatalf("suspended session = %q", manager.suspendedID)
	}
}

func TestClientManagesContextSnapshotInExecutionPlane(t *testing.T) {
	manager := newFakeManager()
	store := &memorySessionStateStore{data: map[string][]byte{"session-1": []byte("checkpoint")}}
	handler, err := NewHandler(manager, testBearerToken, WithSessionStateStore(store))
	require.NoError(t, err)
	e := echo.New()
	handler.RegisterRoutes(e)
	server := httptest.NewServer(e)
	defer server.Close()
	client, err := NewClient(server.URL, testBearerToken)
	require.NoError(t, err)

	require.NoError(t, client.CreateSessionContextSnapshot(context.Background(), "session-1", "tpl-1"))
	require.Equal(t, []byte("checkpoint"), store.data["tpl-1"])
	require.NoError(t, client.DeleteSessionContextSnapshot(context.Background(), "tpl-1"))
	require.NotContains(t, store.data, "tpl-1")
}

func TestClientSendsAPIResolvedProvisionSettings(t *testing.T) {
	manager := newFakeManager()
	client, _ := newTestClient(t, manager)
	settings := &sessionsettings.SessionSettings{Env: map[string]string{"ANTHROPIC_API_KEY": "plain-api-key"}}
	client.SetProvisionSettingsBuilder(fakeProvisionSettingsBuilder{settings: settings})
	original := &entities.RunServerRequest{UserID: "user-1"}

	if _, err := client.CreateSession(context.Background(), "resolved-settings", original, nil); err != nil {
		t.Fatal(err)
	}
	if original.ProvisionSettings != nil {
		t.Fatal("CreateSession mutated the caller's request")
	}
	if manager.createdRequest == nil || manager.createdRequest.ProvisionSettings == nil {
		t.Fatal("session manager did not receive provision settings")
	}
	if got := manager.createdRequest.ProvisionSettings.Env["ANTHROPIC_API_KEY"]; got != "plain-api-key" {
		t.Fatalf("ANTHROPIC_API_KEY = %q", got)
	}
}

func TestClientSendsBaseMCPFromAPIPersistence(t *testing.T) {
	for _, adapter := range []bool{false, true} {
		for _, agentType := range []string{"claude-acp", "codex-acp"} {
			name := agentType + "/kubernetes"
			if adapter {
				name = agentType + "/kv-adapter"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				const namespace = "api-settings"
				const baseName = "custom-base-settings"
				secret := func(name, data string) *corev1.Secret {
					return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Data: map[string][]byte{"settings.json": []byte(data)}}
				}
				storage := fake.NewSimpleClientset(
					secret(baseName, `{"mcp_servers":{"base-only":{"type":"http","url":"https://base.example/mcp"},"shared":{"type":"http","url":"https://base.example/shared"},"deleted":{"type":"http","url":"https://base.example/deleted"}}}`),
					secret("agentapi-settings-org-team", `{"mcp_servers":{"team-only":{"type":"http","url":"https://team.example/mcp"},"shared":{"type":"http","url":"https://team.example/shared"}}}`),
					secret("agentapi-settings-user-1", `{"mcp_servers":{"shared":{"type":"http","url":"https://user.example/shared"},"deleted":null}}`),
				)
				var persistence kubernetes.Interface = storage
				if adapter {
					persistence = kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), kvstore.NewKubernetesStore(storage))
				}
				pvc := false
				cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
					Namespace: "workloads", SettingsBaseSecret: baseName,
					Image: "test-image", BasePort: 9000, PVCEnabled: &pvc,
				}}
				// Match API composition: the workload client is empty, and settings
				// live in a separate persistence client and namespace.
				builder, err := services.NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
				require.NoError(t, err)
				builder.SetSettingsSecretClient(persistence, namespace)
				manager := newFakeManager()
				client, _ := newTestClient(t, manager)
				client.SetProvisionSettingsBuilder(builder)
				req := &entities.RunServerRequest{UserID: "user-1", Scope: entities.ScopeUser, Teams: []string{"org/team"}, AgentType: agentType}
				_, err = client.CreateSession(ctx, "base-mcp-session", req, nil)
				require.NoError(t, err)
				require.NotNil(t, manager.createdRequest)
				settings := manager.createdRequest.ProvisionSettings
				require.NotNil(t, settings)
				servers := settings.Claude.MCPServers
				if agentType == "codex-acp" {
					servers = settings.Codex.MCPServers
				}
				require.Len(t, servers, 3)
				require.Equal(t, "https://base.example/mcp", servers["base-only"].(map[string]interface{})["url"])
				require.Equal(t, "https://team.example/mcp", servers["team-only"].(map[string]interface{})["url"])
				require.Equal(t, "https://user.example/shared", servers["shared"].(map[string]interface{})["url"])
				require.NotContains(t, servers, "deleted")
				require.Nil(t, req.ProvisionSettings)
			})
		}
	}
}

func TestClientRoundTripsStockCleanupAndAllocationQueue(t *testing.T) {
	manager := newFakeManager()
	manager.stockCount = 3
	manager.nextLocal = &coreallocation.AllocationRequest{SessionID: "local-allocation", Status: coreallocation.StatusPending}
	manager.nextExternal = &coreallocation.AllocationRequest{SessionID: "external-allocation", ManagerID: "manager-1", Status: coreallocation.StatusPending}
	client, _ := newTestClient(t, manager)
	ctx := context.Background()

	count, err := client.CountStockSessions(ctx, true)
	if err != nil || count != 3 {
		t.Fatalf("CountStockSessions() = (%d, %v)", count, err)
	}
	if err := client.CreateStockSession(ctx, true); err != nil || !manager.stockCreatedDind {
		t.Fatalf("CreateStockSession() err=%v dind=%t", err, manager.stockCreatedDind)
	}
	if err := client.PurgeStaleStockSessions(ctx); err != nil || !manager.stockPurged {
		t.Fatalf("PurgeStaleStockSessions() err=%v purged=%t", err, manager.stockPurged)
	}

	deleted, err := client.DeletePendingSessionAllocation(ctx, "pending-1")
	if err != nil || !deleted || manager.pendingDeleted != "pending-1" {
		t.Fatalf("DeletePendingSessionAllocation() = (%t, %v), id=%q", deleted, err, manager.pendingDeleted)
	}
	if err := client.DeleteProvisionRequest(ctx, "provision-1"); err != nil || manager.provisionDeleted != "provision-1" {
		t.Fatalf("DeleteProvisionRequest() err=%v id=%q", err, manager.provisionDeleted)
	}

	settings := &sessionsettings.SessionSettings{InitialMessage: "hello"}
	runRequest := &entities.RunServerRequest{UserID: "user-1"}
	runtime := &coreallocation.RuntimeBootstrap{Token: "runtime-token", Generation: 2}
	if err := client.SubmitExternalSessionAllocation(ctx, "manager-1", "external-submit", settings, runRequest, runtime); err != nil {
		t.Fatal(err)
	}
	if manager.submittedManagerID != "manager-1" || manager.submittedSessionID != "external-submit" || manager.submittedRuntime.Generation != 2 {
		t.Fatalf("submitted allocation was not preserved: manager=%q session=%q runtime=%#v", manager.submittedManagerID, manager.submittedSessionID, manager.submittedRuntime)
	}

	local, found, err := client.NextSessionAllocation(ctx, 5*time.Second)
	if err != nil || !found || local.SessionID != "local-allocation" {
		t.Fatalf("NextSessionAllocation() = (%#v, %t, %v)", local, found, err)
	}
	if empty, found, err := client.NextSessionAllocation(ctx, 0); err != nil || found || empty != nil {
		t.Fatalf("empty NextSessionAllocation() = (%#v, %t, %v)", empty, found, err)
	}
	completed, err := client.CompleteSessionAllocation(ctx, local.SessionID, coreallocation.AllocationResult{Status: coreallocation.StatusAssigned, AllocatedSessionID: "runtime-1"})
	if err != nil || completed.SessionID != local.SessionID || manager.completedLocal.AllocatedSessionID != "runtime-1" {
		t.Fatalf("CompleteSessionAllocation() = (%#v, %v), result=%#v", completed, err, manager.completedLocal)
	}

	external, found, err := client.NextExternalSessionAllocation(ctx, "manager-1", 3*time.Second)
	if err != nil || !found || external.SessionID != "external-allocation" || manager.nextExternalID != "manager-1" {
		t.Fatalf("NextExternalSessionAllocation() = (%#v, %t, %v), manager=%q", external, found, err, manager.nextExternalID)
	}
	completedExternal, err := client.CompleteExternalSessionAllocation(ctx, external.SessionID, coreallocation.AllocationResult{Status: coreallocation.StatusError, Message: "failed"})
	if err != nil || completedExternal.SessionID != external.SessionID || manager.completedExternal.Message != "failed" {
		t.Fatalf("CompleteExternalSessionAllocation() = (%#v, %v), result=%#v", completedExternal, err, manager.completedExternal)
	}
}

func TestClientReturnsTypedAuthenticationError(t *testing.T) {
	manager := newFakeManager()
	_, server := newTestClient(t, manager)
	client, err := NewClient(server.URL, "wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	err = client.Health(context.Background())
	httpErr, ok := err.(*HTTPError)
	if !ok || httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Health() error = %#v", err)
	}
	var response errorResponse
	if err := json.Unmarshal([]byte(`{"error":"unauthorized"}`), &response); err != nil || response.Error == "" {
		t.Fatal("error response contract is not JSON-decodable")
	}
}
