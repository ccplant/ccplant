package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestUpdateSettingsRejectsInvalidAutoSuspend(t *testing.T) {
	repo := newMockSettingsRepository()
	h := NewSettingsController(repo, nil)
	body, err := json.Marshal(UpdateSettingsRequest{AutoSuspend: &entities.AutoSuspendSettings{Enabled: true, IdleTimeoutMinutes: 0}})
	require.NoError(t, err)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/settings/test-user", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("test-user")
	c.Set("internal_user", createTestUser("test-user", true))

	err = h.UpdateSettings(c)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, err.(*echo.HTTPError).Code)
}

func TestUpdateSettingsRejectsDirectExternalSessionManagerRegistration(t *testing.T) {
	repo := newMockSettingsRepository()
	h := NewSettingsController(repo, nil)
	managers := []ExternalSessionManagerRequest{{Name: "legacy-manager"}}
	body, err := json.Marshal(UpdateSettingsRequest{ExternalSessionManagers: &managers})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/settings/test-user", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("test-user")
	c.Set("internal_user", createTestUser("test-user", true))

	err = h.UpdateSettings(c)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, err.(*echo.HTTPError).Code)
}

func TestUpdateSettings_GitHubAppInstallationID(t *testing.T) {
	repo := newMockSettingsRepository()
	h := NewSettingsController(repo, nil)
	installationID := "4242"
	body, err := json.Marshal(UpdateSettingsRequest{GitHubAppInstallationID: &installationID})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/settings/org%2Fteam-a", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("org/team-a")
	c.Set("internal_user", createTestUser("test-user", true))

	require.NoError(t, h.UpdateSettings(c))
	saved, err := repo.FindByName(context.Background(), "org/team-a")
	require.NoError(t, err)
	assert.Equal(t, "4242", saved.GitHubAppInstallationID())

	var resp SettingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "4242", resp.GitHubAppInstallationID)
}

func TestUpdateSettings_RejectsInvalidGitHubAppInstallationID(t *testing.T) {
	repo := newMockSettingsRepository()
	h := NewSettingsController(repo, nil)
	installationID := "not-a-number"
	body, err := json.Marshal(UpdateSettingsRequest{GitHubAppInstallationID: &installationID})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/settings/test-user", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("test-user")
	c.Set("internal_user", createTestUser("test-user", true))

	err = h.UpdateSettings(c)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestMergeSecrets(t *testing.T) {
	ctrl := &SettingsController{}

	t.Run("new is nil returns existing", func(t *testing.T) {
		existing := map[string]string{"A": "1", "B": "2"}
		result := ctrl.mergeSecrets(existing, nil)
		assert.Equal(t, existing, result)
	})

	t.Run("existing is nil returns new without empty values", func(t *testing.T) {
		newMap := map[string]string{"A": "1", "B": ""}
		result := ctrl.mergeSecrets(nil, newMap)
		assert.Equal(t, map[string]string{"A": "1"}, result)
	})

	t.Run("keys not in new are preserved from existing", func(t *testing.T) {
		existing := map[string]string{"A": "1", "B": "2", "C": "3"}
		newMap := map[string]string{"A": "updated"}
		result := ctrl.mergeSecrets(existing, newMap)
		assert.Equal(t, map[string]string{"A": "updated", "B": "2", "C": "3"}, result)
	})

	t.Run("empty string in new deletes the existing key", func(t *testing.T) {
		existing := map[string]string{"A": "1", "B": "2"}
		newMap := map[string]string{"A": ""}
		result := ctrl.mergeSecrets(existing, newMap)
		assert.Equal(t, map[string]string{"B": "2"}, result)
	})

	t.Run("add new key while preserving existing keys", func(t *testing.T) {
		existing := map[string]string{"A": "1", "B": "2"}
		newMap := map[string]string{"C": "3"}
		result := ctrl.mergeSecrets(existing, newMap)
		assert.Equal(t, map[string]string{"A": "1", "B": "2", "C": "3"}, result)
	})

	t.Run("update key while empty string for another deletes it", func(t *testing.T) {
		existing := map[string]string{"A": "1", "B": "2", "C": "3"}
		newMap := map[string]string{"A": "updated", "B": ""}
		result := ctrl.mergeSecrets(existing, newMap)
		assert.Equal(t, map[string]string{"A": "updated", "C": "3"}, result)
	})

	t.Run("delete all keys by sending all as empty strings", func(t *testing.T) {
		existing := map[string]string{"A": "1", "B": "2"}
		newMap := map[string]string{"A": "", "B": ""}
		result := ctrl.mergeSecrets(existing, newMap)
		assert.Equal(t, map[string]string{}, result)
	})

	t.Run("both nil returns nil", func(t *testing.T) {
		result := ctrl.mergeSecrets(nil, nil)
		assert.Nil(t, result)
	})
}
