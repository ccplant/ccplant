package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	sessionrunnerinfra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

func esmTestContext(e *echo.Echo, method, path string, body interface{}, userID string) (echo.Context, *httptest.ResponseRecorder) {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetPath(path)
	ctx.Set("internal_user", createTestUser(userID, false))
	return ctx, rec
}

func TestExternalSessionManagerEnrollmentAndHeartbeatUsesToken(t *testing.T) {
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer probe.Close()

	repo := newMockSettingsRepository()
	controller := NewSettingsController(repo, nil)
	e := echo.New()
	ctx, rec := esmTestContext(e, http.MethodPost, "/external-session-managers/registration-tokens", ESMEnrollmentTokenRequest{}, "user1")
	require.NoError(t, controller.IssueExternalSessionManagerEnrollmentToken(ctx))
	var issued esmEnrollmentTokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &issued))
	body := ESMEnrollmentRequest{RegistrationToken: issued.RegistrationToken, InstanceID: "machine-1", Name: "native-1", PublicURL: probe.URL,
		Labels: map[string]string{"os": "linux", "arch": "amd64"}}
	ctx, rec = esmTestContext(e, http.MethodPost, "/external-session-managers/enroll", body, "")
	require.NoError(t, controller.EnrollExternalSessionManager(ctx))
	var created esmRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.True(t, created.Created)
	require.NotEmpty(t, created.ConnectionToken)
	require.Len(t, repo.settings["user1"].ExternalSessionManagers(), 1)

	heartbeat := ESMHeartbeatRequest{PublicURL: probe.URL, Version: "test-version", ActiveSessions: 2}
	ctx, rec = esmTestContext(e, http.MethodPost, "/external-session-managers/:id/heartbeat", heartbeat, "")
	ctx.SetParamNames("id")
	ctx.SetParamValues(created.ID)
	ctx.Request().Header.Set("Authorization", "Bearer "+created.ConnectionToken)
	require.NoError(t, controller.HeartbeatExternalSessionManager(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	manager := repo.settings["user1"].ExternalSessionManagers()[0]
	require.False(t, manager.LastHeartbeatAt.IsZero())
	require.Equal(t, "test-version", manager.Version)
	require.Equal(t, 2, manager.ActiveSessions)
}

func TestExternalSessionManagerPoolMembershipIsManagedBySuppliers(t *testing.T) {
	repo := newMockSettingsRepository()
	store := sessionrunnerinfra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSettingsController(repo, nil)
	controller.SetSessionRunnerStore(store)
	e := echo.New()
	ctx, rec := esmTestContext(e, http.MethodPost, "/external-session-managers/registration-tokens", ESMEnrollmentTokenRequest{}, "user1")
	require.NoError(t, controller.IssueExternalSessionManagerEnrollmentToken(ctx))
	var issued esmEnrollmentTokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &issued))
	ctx, rec = esmTestContext(e, http.MethodPost, "/external-session-managers/enroll", ESMEnrollmentRequest{
		RegistrationToken: issued.RegistrationToken, InstanceID: "machine-pool", Name: "native-pool",
	}, "")
	require.NoError(t, controller.EnrollExternalSessionManager(ctx))
	var created esmRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	manager, err := store.GetManager(context.Background(), created.ID)
	require.NoError(t, err)
	require.Contains(t, manager.Capabilities, core.CapabilityRunnerClaimV1)
	require.True(t, manager.Enabled)
	pools, err := store.ListLogicalPools(context.Background())
	require.NoError(t, err)
	require.Empty(t, pools, "enrollment must not create a manager-owned pool")

	require.NoError(t, store.CreateLogicalPool(context.Background(), &core.LogicalPool{Name: "shared", Enabled: true}))
	require.NoError(t, store.CreatePoolSupplier(context.Background(), &core.PoolSupplier{Pool: "shared", ManagerID: created.ID, Enabled: true}))
	require.NoError(t, store.CreateBinding(context.Background(), &core.Binding{Pool: "shared", SubjectType: core.SubjectUser, SubjectID: "user1", Role: core.BindingRoleManage, Enabled: true}))

	ctx, rec = esmTestContext(e, http.MethodPatch, "/external-session-managers/:id", ESMUpdateRequest{Name: "renamed-manager"}, "user1")
	ctx.SetParamNames("id")
	ctx.SetParamValues(created.ID)
	require.NoError(t, controller.PatchExternalSessionManager(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	manager, err = store.GetManager(context.Background(), created.ID)
	require.NoError(t, err)
	require.True(t, manager.Enabled)
	require.Equal(t, "renamed-manager", manager.Name)
	_, err = store.GetPoolSupplier(context.Background(), created.ID, "shared")
	require.NoError(t, err, "manager metadata updates must not change pool membership")
	ctx, _ = esmTestContext(e, http.MethodPost, "/external-session-managers/:id/heartbeat", ESMHeartbeatRequest{}, "")
	ctx.SetParamNames("id")
	ctx.SetParamValues(created.ID)
	ctx.Request().Header.Set("Authorization", "Bearer "+created.ConnectionToken)
	require.NoError(t, controller.HeartbeatExternalSessionManager(ctx))
	manager, err = store.GetManager(context.Background(), created.ID)
	require.NoError(t, err)
	require.False(t, manager.LastHeartbeatAt.IsZero())

	ctx, rec = esmTestContext(e, http.MethodPut, "/settings/:name", map[string]any{"external_session_managers": []any{}}, "user1")
	ctx.SetParamNames("name")
	ctx.SetParamValues("user1")
	require.NoError(t, controller.UpdateSettings(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	_, err = store.GetManager(context.Background(), created.ID)
	require.ErrorIs(t, err, core.ErrNotFound)
	_, err = store.GetPoolSupplier(context.Background(), created.ID, "shared")
	require.ErrorIs(t, err, core.ErrNotFound)
	_, err = store.GetLogicalPool(context.Background(), "shared")
	require.NoError(t, err, "deleting a manager must not delete an independently owned pool")
}

func TestExternalSessionManagerHeartbeatRejectsUnreachablePublicURL(t *testing.T) {
	repo := newMockSettingsRepository()
	controller := NewSettingsController(repo, nil)
	e := echo.New()
	ctx, rec := esmTestContext(e, http.MethodPost, "/external-session-managers/registration-tokens", ESMEnrollmentTokenRequest{}, "user1")
	require.NoError(t, controller.IssueExternalSessionManagerEnrollmentToken(ctx))
	var issued esmEnrollmentTokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &issued))
	body := ESMEnrollmentRequest{RegistrationToken: issued.RegistrationToken, InstanceID: "machine-2", Name: "native-2"}
	ctx, rec = esmTestContext(e, http.MethodPost, "/external-session-managers/enroll", body, "")
	require.NoError(t, controller.EnrollExternalSessionManager(ctx))
	var created esmRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	heartbeat := ESMHeartbeatRequest{PublicURL: "http://127.0.0.1:1"}
	ctx, _ = esmTestContext(e, http.MethodPost, "/external-session-managers/:id/heartbeat", heartbeat, "")
	ctx.SetParamNames("id")
	ctx.SetParamValues(created.ID)
	ctx.Request().Header.Set("Authorization", "Bearer "+created.ConnectionToken)
	err := controller.HeartbeatExternalSessionManager(ctx)
	require.Error(t, err)
}

func TestExternalSessionManagerEnrollmentTokenIsOneTime(t *testing.T) {
	repo := newMockSettingsRepository()
	controller := NewSettingsController(repo, nil)
	e := echo.New()

	ctx, rec := esmTestContext(e, http.MethodPost, "/external-session-managers/registration-tokens", ESMEnrollmentTokenRequest{}, "user1")
	require.NoError(t, controller.IssueExternalSessionManagerEnrollmentToken(ctx))
	require.Equal(t, http.StatusCreated, rec.Code)
	var issued esmEnrollmentTokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &issued))
	require.NotEmpty(t, issued.RegistrationToken)
	require.NotEmpty(t, issued.ManagerID)

	enrollment := ESMEnrollmentRequest{RegistrationToken: issued.RegistrationToken, InstanceID: "machine-enrolled", Name: "native-enrolled"}
	ctx, rec = esmTestContext(e, http.MethodPost, "/external-session-managers/enroll", enrollment, "")
	require.NoError(t, controller.EnrollExternalSessionManager(ctx))
	var registered esmRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &registered))
	require.Equal(t, issued.ManagerID, registered.ID)
	require.NotEmpty(t, registered.ConnectionToken)

	ctx, _ = esmTestContext(e, http.MethodPost, "/external-session-managers/enroll", enrollment, "")
	err := controller.EnrollExternalSessionManager(ctx)
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, err.(*echo.HTTPError).Code)
}

func TestServiceAccountEnrollmentTokenDefaultsToTeamScope(t *testing.T) {
	repo := newMockSettingsRepository()
	controller := NewSettingsController(repo, nil)
	e := echo.New()
	ctx, rec := esmTestContext(e, http.MethodPost, "/external-session-managers/registration-tokens", ESMEnrollmentTokenRequest{}, "")
	ctx.Set("internal_user", entities.NewServiceAccountUser("service-account", "org/builders", nil))

	require.NoError(t, controller.IssueExternalSessionManagerEnrollmentToken(ctx))
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Contains(t, repo.settings, "org/builders")
	require.NotContains(t, repo.settings, "service-account")
}

func TestServiceAccountEnrollmentTokenHonorsExplicitUserScope(t *testing.T) {
	repo := newMockSettingsRepository()
	controller := NewSettingsController(repo, nil)
	e := echo.New()
	ctx, rec := esmTestContext(e, http.MethodPost, "/external-session-managers/registration-tokens", ESMEnrollmentTokenRequest{Scope: "user"}, "")
	ctx.Set("internal_user", entities.NewServiceAccountUser("service-account", "org/builders", nil))

	require.NoError(t, controller.IssueExternalSessionManagerEnrollmentToken(ctx))
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Contains(t, repo.settings, "service-account")
}
