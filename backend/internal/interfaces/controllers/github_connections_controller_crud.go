package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *GitHubConnectionsController) Create(ctx echo.Context) error {
	var request githubConnectionRequest
	if err := ctx.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	baseURL, err := normalizeGitHubURL(request.BaseURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid base_url").SetInternal(err)
	}
	apiURL, err := normalizeGitHubURL(request.APIURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid api_url").SetInternal(err)
	}
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.OAuthClientID) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and oauth_client_id are required")
	}
	if err := validateGitHubSecret(request.Secret.Source, request.Secret.Value, request.Secret.Environment); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if request.Secret.Source == "encrypted" && !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusBadRequest, "encrypted secret storage requires the libsql-encrypted or kubernetes KV backend")
	}

	now := time.Now().UTC()
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	showOnLogin := true
	if request.ShowOnLogin != nil {
		showOnLogin = *request.ShowOnLogin
	}
	connection := githubConnection{
		ID: uuid.NewString(), Name: strings.TrimSpace(request.Name), BaseURL: baseURL, APIURL: apiURL,
		OAuthClientID: strings.TrimSpace(request.OAuthClientID), OAuthScope: normalizeOAuthScope(request.OAuthScope), SecretSource: request.Secret.Source,
		SecretEnvironment: request.Secret.Environment, Enabled: enabled, ShowOnLogin: &showOnLogin,
		AllowUserCreation: request.AllowUserCreation != nil && *request.AllowUserCreation,
		Organizations:     normalizeOrganizations(request.Organizations), CreatedAt: now, UpdatedAt: now,
		GitHubApp: request.GitHubApp,
	}
	if err := validateGitHubApp(connection.GitHubApp); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := c.validateOrganizationAssignments(ctx.Request().Context(), connection.ID, connection.Organizations); err != nil {
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	if err := c.saveConnection(ctx.Request().Context(), connection, request.Secret.Value, ""); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create GitHub connection").SetInternal(err)
	}
	return ctx.JSON(http.StatusCreated, c.connectionResponse(ctx.Request().Context(), connection, request.Secret.Value != ""))
}

func (c *GitHubConnectionsController) List(ctx echo.Context) error {
	connections, err := c.listConnections(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list GitHub connections").SetInternal(err)
	}
	responses := make([]githubConnectionResponse, 0, len(connections))
	for _, connection := range connections {
		responses = append(responses, c.connectionResponse(ctx.Request().Context(), connection, c.secretConfigured(ctx.Request().Context(), connection)))
	}
	return ctx.JSON(http.StatusOK, map[string]any{"connections": responses})
}

func (c *GitHubConnectionsController) Get(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), record.connection, record.clientSecret != "" || c.secretConfigured(ctx.Request().Context(), record.connection)))
}

func (c *GitHubConnectionsController) Update(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection, secret, resourceVersion := record.connection, record.clientSecret, record.resourceVersion
	previousAppID := int64(0)
	if connection.GitHubApp != nil {
		previousAppID = connection.GitHubApp.AppID
	}
	var request githubConnectionRequest
	if err := ctx.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if request.Name != "" {
		connection.Name = strings.TrimSpace(request.Name)
	}
	if request.BaseURL != "" {
		connection.BaseURL, err = normalizeGitHubURL(request.BaseURL)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid base_url")
		}
	}
	if request.APIURL != "" {
		connection.APIURL, err = normalizeGitHubURL(request.APIURL)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid api_url")
		}
	}
	if request.OAuthClientID != "" {
		connection.OAuthClientID = strings.TrimSpace(request.OAuthClientID)
	}
	if request.OAuthScope != "" {
		connection.OAuthScope = normalizeOAuthScope(request.OAuthScope)
	}
	if request.Enabled != nil {
		connection.Enabled = *request.Enabled
	}
	if request.ShowOnLogin != nil {
		connection.ShowOnLogin = request.ShowOnLogin
	}
	if request.AllowUserCreation != nil {
		connection.AllowUserCreation = *request.AllowUserCreation
	}
	if request.Organizations != nil {
		connection.Organizations = normalizeOrganizations(request.Organizations)
		if err := c.validateOrganizationAssignments(ctx.Request().Context(), connection.ID, connection.Organizations); err != nil {
			return echo.NewHTTPError(http.StatusConflict, err.Error())
		}
	}
	if request.GitHubApp != nil {
		if err := validateGitHubApp(request.GitHubApp); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		connection.GitHubApp = request.GitHubApp
	}
	connection.UpdatedAt = time.Now().UTC()
	if err := c.saveConnection(ctx.Request().Context(), connection, secret, resourceVersion); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update GitHub connection").SetInternal(err)
	}
	if connection.GitHubApp != nil && connection.GitHubApp.AppID != previousAppID {
		c.invalidateGitHubAppTokens(connection.ID)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), connection, c.secretConfigured(ctx.Request().Context(), connection)))
}

func (c *GitHubConnectionsController) UpdateSecret(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection, resourceVersion := record.connection, record.resourceVersion
	var request githubSecretUpdate
	if err := ctx.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if err := validateGitHubSecret(request.Source, request.Value, request.Environment); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if request.Source == "encrypted" && !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusBadRequest, "encrypted secret storage requires the libsql-encrypted or kubernetes KV backend")
	}
	connection.SecretSource = request.Source
	connection.SecretEnvironment = request.Environment
	connection.UpdatedAt = time.Now().UTC()
	if err := c.saveConnection(ctx.Request().Context(), connection, request.Value, resourceVersion); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update GitHub client secret").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), connection, true))
}

func (c *GitHubConnectionsController) DeleteSecret(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection, resourceVersion := record.connection, record.resourceVersion
	connection.SecretEnvironment = ""
	connection.UpdatedAt = time.Now().UTC()
	if err := c.saveConnection(ctx.Request().Context(), connection, "", resourceVersion); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete GitHub client secret").SetInternal(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *GitHubConnectionsController) UpdateGitHubAppPrivateKey(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection, resourceVersion := record.connection, record.resourceVersion
	if connection.GitHubApp == nil || connection.GitHubApp.AppID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "github_app.app_id must be configured first")
	}
	if !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusBadRequest, "encrypted private key storage requires the libsql-encrypted or kubernetes KV backend")
	}
	var request githubAppPrivateKeyUpdate
	if err := ctx.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if err := validateGitHubAppPrivateKey([]byte(request.Value)); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := c.saveGitHubAppPrivateKey(ctx.Request().Context(), connection, []byte(request.Value), resourceVersion); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update GitHub App private key").SetInternal(err)
	}
	c.invalidateGitHubAppTokens(connection.ID)
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), connection, c.secretConfigured(ctx.Request().Context(), connection)))
}

func (c *GitHubConnectionsController) DeleteGitHubAppPrivateKey(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection, resourceVersion := record.connection, record.resourceVersion
	if err := c.saveGitHubAppPrivateKey(ctx.Request().Context(), connection, nil, resourceVersion); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete GitHub App private key").SetInternal(err)
	}
	c.invalidateGitHubAppTokens(connection.ID)
	return ctx.NoContent(http.StatusNoContent)
}

func (c *GitHubConnectionsController) TestGitHubApp(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection := record.connection
	var request githubAppTestRequest
	if err := ctx.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	token, expiresAt, err := c.mintGitHubAppToken(ctx.Request().Context(), connection, request.Repository)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return ctx.JSON(http.StatusOK, map[string]any{"valid": token != "", "expires_at": expiresAt})
}

func (c *GitHubConnectionsController) Test(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection := record.connection
	_, secretErr := c.resolveClientSecret(ctx.Request().Context(), connection)
	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet, connection.APIURL, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid API URL")
	}
	resp, requestErr := c.httpClient.Do(req)
	apiReachable := requestErr == nil && resp.StatusCode < http.StatusInternalServerError
	if resp != nil {
		utils.SafeCloseResponse(resp)
	}
	return ctx.JSON(http.StatusOK, map[string]any{
		"api_reachable":     apiReachable,
		"secret_resolvable": secretErr == nil,
	})
}

func (c *GitHubConnectionsController) Delete(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection, resourceVersion := record.connection, record.resourceVersion
	count := c.identityCount(ctx.Request().Context(), connection.ID)
	if count > 0 {
		return ctx.JSON(http.StatusConflict, map[string]any{"error": "connection_in_use", "linked_identity_count": count})
	}
	err = c.client.CoreV1().Secrets(c.namespace).Delete(ctx.Request().Context(), connectionSecretName(connection.ID), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{ResourceVersion: &resourceVersion}})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete GitHub connection").SetInternal(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *GitHubConnectionsController) ListAvailable(ctx echo.Context) error {
	connections, err := c.listConnections(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list GitHub connections").SetInternal(err)
	}
	result := make([]map[string]any, 0, len(connections))
	for _, connection := range connections {
		if connection.Enabled {
			result = append(result, map[string]any{"id": connection.ID, "name": connection.Name, "base_url": connection.BaseURL})
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"connections": result})
}

// ListLoginOptions returns only the non-sensitive fields required to render
// the unauthenticated login screen.
func (c *GitHubConnectionsController) ListLoginOptions(ctx echo.Context) error {
	connections, err := c.listConnections(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list GitHub connections").SetInternal(err)
	}
	result := make([]map[string]any, 0, len(connections))
	for _, connection := range connections {
		if connection.Enabled && showConnectionOnLogin(connection) && c.secretConfigured(ctx.Request().Context(), connection) {
			result = append(result, map[string]any{"id": connection.ID, "name": connection.Name, "base_url": connection.BaseURL})
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"connections": result})
}

// StartLogin initiates an unauthenticated OAuth login using an administrator
// configured connection. State is kept server-side so the callback does not
// need to expose or separately carry the connection ID.
