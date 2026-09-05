package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

func connectionPatch(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()
	var p map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(body), &p))
	return p
}
func TestConnectionKeyLifecycle(t *testing.T) {
	original := &modelprovider.Connection{Mode: "anthropic_compatible", BaseURL: "https://gateway.example", Model: "default", Authentication: "bearer_token", APIKey: "secret", ModelAliases: map[string]string{"haiku": "old"}}
	kept, err := mergeModelConnection(original, connectionPatch(t, `{"mode":"anthropic_compatible","model":"new","model_aliases":{}}`), "claude")
	require.NoError(t, err)
	require.Equal(t, "secret", kept.APIKey)
	require.Empty(t, kept.ModelAliases)
	require.Equal(t, "default", original.Model)
	_, err = mergeModelConnection(kept, connectionPatch(t, `{"mode":"anthropic_compatible","clear_api_key":true}`), "claude")
	require.Error(t, err)
	deleted, err := mergeModelConnection(kept, connectionPatch(t, `{"mode":"oauth","clear_api_key":true}`), "claude")
	require.NoError(t, err)
	require.Empty(t, deleted.APIKey)
	_, err = mergeModelConnection(kept, connectionPatch(t, `{"mode":null}`), "claude")
	require.Error(t, err)
}
func TestSettingsConnectionAPI(t *testing.T) {
	repo := newMockSettingsRepository()
	h := NewSettingsController(repo, nil)
	e := echo.New()
	request := httptest.NewRequest(http.MethodPut, "/settings/test-user", strings.NewReader(`{"claude_connection":{"mode":"anthropic_compatible","base_url":"https://gateway.example","model":"default","authentication":"api_key","api_key":"secret-value"}}`))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(request, rec)
	ctx.SetParamNames("name")
	ctx.SetParamValues("test-user")
	ctx.Set("internal_user", createTestUser("test-user", false))
	require.NoError(t, h.UpdateSettings(ctx))
	require.Equal(t, 200, rec.Code)
	require.NotContains(t, rec.Body.String(), "secret-value")
	require.Contains(t, rec.Body.String(), `"has_api_key":true`)
	require.Equal(t, entities.AuthModeAnthropicCompatible, repo.settings["test-user"].AuthMode())
}
func TestConnectionUpdateAtomicAndModeConflict(t *testing.T) {
	s := entities.NewSettings("user")
	s.SetCodexConnection(&modelprovider.Connection{Mode: "auth_json"})
	request := &UpdateSettingsRequest{CodexConnection: connectionPatch(t, `{"mode":"openai_compatible","base_url":"https://gateway.example/v1","model":"default","authentication":"none"}`), ClaudeConnection: connectionPatch(t, `{"mode":"anthropic_compatible"}`)}
	require.Error(t, updateModelConnections(s, request))
	require.Equal(t, "auth_json", s.CodexConnection().Mode)
	mode := "bedrock"
	request = &UpdateSettingsRequest{AuthMode: &mode, ClaudeConnection: connectionPatch(t, `{"mode":"oauth"}`)}
	require.Error(t, updateModelConnections(s, request))
}

func TestRejectedModelMetadataDoesNotMutateStoredConnection(t *testing.T) {
	window := int64(1000)
	limit := int64(500)
	original := &modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://gateway.example/v1", Model: "default", Authentication: "none", ContextWindow: &window, AutoCompactTokenLimit: &limit}
	_, err := mergeModelConnection(original, connectionPatch(t, `{"mode":"openai_compatible","context_window":100}`), "codex")
	require.Error(t, err)
	require.Equal(t, int64(1000), *original.ContextWindow)
}
