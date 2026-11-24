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

// mockSettingsRepository is a mock implementation of SettingsRepository
type mockSettingsRepository struct {
	settings map[string]*entities.Settings
}

func newMockSettingsRepository() *mockSettingsRepository {
	return &mockSettingsRepository{
		settings: make(map[string]*entities.Settings),
	}
}

func (m *mockSettingsRepository) Save(ctx context.Context, settings *entities.Settings) error {
	m.settings[settings.Name()] = settings
	return nil
}

func (m *mockSettingsRepository) FindByName(ctx context.Context, name string) (*entities.Settings, error) {
	if s, ok := m.settings[name]; ok {
		return s, nil
	}
	return nil, &notFoundError{name: name}
}

func (m *mockSettingsRepository) Delete(ctx context.Context, name string) error {
	delete(m.settings, name)
	return nil
}

func (m *mockSettingsRepository) Exists(ctx context.Context, name string) (bool, error) {
	_, ok := m.settings[name]
	return ok, nil
}

func (m *mockSettingsRepository) List(ctx context.Context) ([]*entities.Settings, error) {
	result := make([]*entities.Settings, 0, len(m.settings))
	for _, s := range m.settings {
		result = append(result, s)
	}
	return result, nil
}

type notFoundError struct {
	name string
}

func (e *notFoundError) Error() string {
	return "settings not found: " + e.name
}

func createTestUser(userID string, isAdmin bool) *entities.User {
	user := entities.NewUser(
		entities.UserID(userID),
		entities.UserTypeAPIKey,
		userID,
	)
	if isAdmin {
		_ = user.SetRoles([]entities.Role{entities.RoleAdmin})
	} else {
		_ = user.SetRoles([]entities.Role{entities.RoleUser})
	}
	return user
}

func TestGetAvailableManagersReturnsExplicitlySelectableManagers(t *testing.T) {
	repo := newMockSettingsRepository()
	settings := entities.NewSettings("test-user")
	settings.SetExternalSessionManagers([]entities.ExternalSessionManagerEntry{
		{ID: "pooled", Name: "Cluster pool manager", Pool: "k8s", HMACSecret: "connected", AutomaticAssignmentEnabled: true},
		{ID: "disabled", Name: "Disabled manager"},
		{ID: "pending", Name: "pending registration", Pool: "pending", EnrollmentTokenHash: "unused"},
	})
	require.NoError(t, repo.Save(context.Background(), settings))

	controller := NewSettingsController(repo, nil)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/settings/managers", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("internal_user", createTestUser("test-user", false))

	require.NoError(t, controller.GetAvailableManagers(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	var response AvailableManagersResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, []AvailableManagerEntry{
		{ID: "pooled", Name: "Cluster pool manager", Pool: "k8s", PoolEnabled: true, AutomaticAssignmentEnabled: true, Source: "user", SourceName: "test-user"},
	}, response.Managers)
}

func TestUpdateSettings_PreserveExistingCredentials(t *testing.T) {
	tests := []struct {
		name                    string
		existingSettings        *entities.Settings
		requestBody             UpdateSettingsRequest
		expectedAccessKeyID     string
		expectedSecretAccessKey string
		expectedRoleARN         string
		expectedProfile         string
	}{
		{
			name: "preserve all credentials when empty request",
			existingSettings: func() *entities.Settings {
				s := entities.NewSettings("test-user")
				b := entities.NewBedrockSettings(true)
				b.SetAccessKeyID("existing-key-id")
				b.SetSecretAccessKey("existing-secret-key")
				b.SetRoleARN("existing-role-arn")
				b.SetProfile("existing-profile")
				s.SetBedrock(b)
				return s
			}(),
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled: true,
				},
			},
			expectedAccessKeyID:     "existing-key-id",
			expectedSecretAccessKey: "existing-secret-key",
			expectedRoleARN:         "existing-role-arn",
			expectedProfile:         "existing-profile",
		},
		{
			name: "update only access_key_id, preserve others",
			existingSettings: func() *entities.Settings {
				s := entities.NewSettings("test-user")
				b := entities.NewBedrockSettings(true)
				b.SetAccessKeyID("existing-key-id")
				b.SetSecretAccessKey("existing-secret-key")
				b.SetRoleARN("existing-role-arn")
				b.SetProfile("existing-profile")
				s.SetBedrock(b)
				return s
			}(),
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled:     true,
					AccessKeyID: "new-key-id",
				},
			},
			expectedAccessKeyID:     "new-key-id",
			expectedSecretAccessKey: "existing-secret-key",
			expectedRoleARN:         "existing-role-arn",
			expectedProfile:         "existing-profile",
		},
		{
			name: "update only secret_access_key, preserve others",
			existingSettings: func() *entities.Settings {
				s := entities.NewSettings("test-user")
				b := entities.NewBedrockSettings(true)
				b.SetAccessKeyID("existing-key-id")
				b.SetSecretAccessKey("existing-secret-key")
				b.SetRoleARN("existing-role-arn")
				b.SetProfile("existing-profile")
				s.SetBedrock(b)
				return s
			}(),
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled:         true,
					SecretAccessKey: "new-secret-key",
				},
			},
			expectedAccessKeyID:     "existing-key-id",
			expectedSecretAccessKey: "new-secret-key",
			expectedRoleARN:         "existing-role-arn",
			expectedProfile:         "existing-profile",
		},
		{
			name: "update all credentials",
			existingSettings: func() *entities.Settings {
				s := entities.NewSettings("test-user")
				b := entities.NewBedrockSettings(true)
				b.SetAccessKeyID("existing-key-id")
				b.SetSecretAccessKey("existing-secret-key")
				b.SetRoleARN("existing-role-arn")
				b.SetProfile("existing-profile")
				s.SetBedrock(b)
				return s
			}(),
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled:         true,
					AccessKeyID:     "new-key-id",
					SecretAccessKey: "new-secret-key",
					RoleARN:         "new-role-arn",
					Profile:         "new-profile",
				},
			},
			expectedAccessKeyID:     "new-key-id",
			expectedSecretAccessKey: "new-secret-key",
			expectedRoleARN:         "new-role-arn",
			expectedProfile:         "new-profile",
		},
		{
			name:             "new settings with credentials",
			existingSettings: nil,
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled:         true,
					AccessKeyID:     "new-key-id",
					SecretAccessKey: "new-secret-key",
				},
			},
			expectedAccessKeyID:     "new-key-id",
			expectedSecretAccessKey: "new-secret-key",
			expectedRoleARN:         "",
			expectedProfile:         "",
		},
		{
			name:             "new settings with empty credentials",
			existingSettings: nil,
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled: true,
				},
			},
			expectedAccessKeyID:     "",
			expectedSecretAccessKey: "",
			expectedRoleARN:         "",
			expectedProfile:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockSettingsRepository()
			if tt.existingSettings != nil {
				err := repo.Save(context.Background(), tt.existingSettings)
				require.NoError(t, err)
			}

			h := NewSettingsController(repo, nil)

			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			e := echo.New()
			req := httptest.NewRequest(http.MethodPut, "/settings/test-user", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("name")
			c.SetParamValues("test-user")

			user := createTestUser("test-user", true)
			c.Set("internal_user", user)

			err = h.UpdateSettings(c)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, rec.Code)

			savedSettings, err := repo.FindByName(context.Background(), "test-user")
			require.NoError(t, err)

			bedrock := savedSettings.Bedrock()
			require.NotNil(t, bedrock)
			assert.Equal(t, tt.expectedAccessKeyID, bedrock.AccessKeyID())
			assert.Equal(t, tt.expectedSecretAccessKey, bedrock.SecretAccessKey())
			assert.Equal(t, tt.expectedRoleARN, bedrock.RoleARN())
			assert.Equal(t, tt.expectedProfile, bedrock.Profile())
		})
	}
}

func TestUpdateSettings_DefaultSessionProfileID(t *testing.T) {
	repo := newMockSettingsRepository()
	h := NewSettingsController(repo, nil)

	defaultProfileID := "profile-1"
	body, err := json.Marshal(UpdateSettingsRequest{
		DefaultSessionProfileID: &defaultProfileID,
	})
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
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	savedSettings, err := repo.FindByName(context.Background(), "test-user")
	require.NoError(t, err)
	assert.Equal(t, "profile-1", savedSettings.DefaultSessionProfileID())

	var resp SettingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "profile-1", resp.DefaultSessionProfileID)
}

func TestUpdateSettings_EnabledTeamSessionProfileIDsArePersonal(t *testing.T) {
	repo := newMockSettingsRepository()
	h := NewSettingsController(repo, nil)
	ids := []string{"team-profile-1", "team-profile-2"}
	body, err := json.Marshal(UpdateSettingsRequest{EnabledTeamProfileIDs: &ids})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/settings/test-user", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("test-user")
	c.Set("internal_user", createTestUser("test-user", false))

	require.NoError(t, h.UpdateSettings(c))
	saved, err := repo.FindByName(context.Background(), "test-user")
	require.NoError(t, err)
	require.Equal(t, ids, saved.EnabledTeamSessionProfileIDs())

	teamReq := httptest.NewRequest(http.MethodPut, "/settings/org/team", bytes.NewReader(body))
	teamReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	teamRec := httptest.NewRecorder()
	teamCtx := e.NewContext(teamReq, teamRec)
	teamCtx.SetParamNames("name")
	teamCtx.SetParamValues("org/team")
	teamCtx.Set("internal_user", createTestUser("test-user", true))
	require.Error(t, h.UpdateSettings(teamCtx))
}

func TestUpdateSettingsAutoSuspend(t *testing.T) {
	repo := newMockSettingsRepository()
	h := NewSettingsController(repo, nil)
	policy := &entities.AutoSuspendSettings{Enabled: true, IdleTimeoutMinutes: 120}
	body, err := json.Marshal(UpdateSettingsRequest{AutoSuspend: policy})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/settings/test-user", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("test-user")
	c.Set("internal_user", createTestUser("test-user", true))

	require.NoError(t, h.UpdateSettings(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	saved, err := repo.FindByName(context.Background(), "test-user")
	require.NoError(t, err)
	require.NotNil(t, saved.AutoSuspend())
	assert.Equal(t, 120, saved.AutoSuspend().IdleTimeoutMinutes)

	var response SettingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, policy, response.AutoSuspend)
}
