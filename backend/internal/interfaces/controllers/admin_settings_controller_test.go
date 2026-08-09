package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"k8s.io/client-go/kubernetes/fake"
)

func newAdminSettingsTestController() (*AdminSettingsController, kvstore.Store) {
	store := kvstore.NewKubernetesStore(fake.NewSimpleClientset())
	return NewAdminSettingsController(store, "default"), store
}

func adminSettingsRequest(t *testing.T, e *echo.Echo, method, target, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	return e.NewContext(req, recorder), recorder
}

func TestAdminSettingsControllerVersionsAndMasksSecrets(t *testing.T) {
	controller, store := newAdminSettingsTestController()
	e := echo.New()

	ctx, recorder := adminSettingsRequest(t, e, http.MethodPut, "/admin/system-settings", `{"base_version":0,"sections":{"github":{"oauth":{"client_id":"client","client_secret":"secret"}},"slack":{"session_ttl":"72h"}}}`)
	require.NoError(t, controller.Put(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	var first adminSettingsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &first))
	require.Equal(t, int64(1), first.Version)
	require.True(t, first.SecretConfigured["github.oauth.client_secret"])
	_, exposed := getNestedString(first.Sections, "github.oauth.client_secret")
	require.False(t, exposed)

	ctx, recorder = adminSettingsRequest(t, e, http.MethodPut, "/admin/system-settings", `{"base_version":1,"sections":{"github":{"oauth":{"client_id":"changed"}},"slack":{"session_ttl":"48h"}}}`)
	require.NoError(t, controller.Put(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)

	oldDoc, _, err := controller.loadVersion(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "client", oldDoc.Sections["github"].(map[string]interface{})["oauth"].(map[string]interface{})["client_id"])
	newDoc, _, err := controller.loadVersion(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, "changed", newDoc.Sections["github"].(map[string]interface{})["oauth"].(map[string]interface{})["client_id"])
	secret, ok := getNestedString(newDoc.Sections, "github.oauth.client_secret")
	require.True(t, ok)
	require.Equal(t, "secret", secret)

	records, err := store.List(context.Background(), kvstore.Query{Kind: kvstore.KindSecret, Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, records, 3) // head plus two immutable settings.json snapshots
}

func TestAdminSettingsControllerRejectsStaleVersion(t *testing.T) {
	controller, _ := newAdminSettingsTestController()
	e := echo.New()
	ctx, _ := adminSettingsRequest(t, e, http.MethodPut, "/", `{"base_version":0,"sections":{"github":{}}}`)
	require.NoError(t, controller.Put(ctx))

	ctx, _ = adminSettingsRequest(t, e, http.MethodPut, "/", `{"base_version":0,"sections":{"github":{}}}`)
	err := controller.Put(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusConflict, httpErr.Code)
}

func TestAdminSettingsControllerListsVersionsNewestFirst(t *testing.T) {
	controller, _ := newAdminSettingsTestController()
	e := echo.New()
	for version := 0; version < 2; version++ {
		ctx, _ := adminSettingsRequest(t, e, http.MethodPut, "/", `{"base_version":`+strconv.Itoa(version)+`,"sections":{"workers":{}}}`)
		require.NoError(t, controller.Put(ctx))
	}
	ctx, recorder := adminSettingsRequest(t, e, http.MethodGet, "/admin/system-settings/versions", "")
	require.NoError(t, controller.ListVersions(ctx))
	var response struct {
		Versions []adminSettingsVersion `json:"versions"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Versions, 2)
	require.Equal(t, int64(2), response.Versions[0].Version)
}
