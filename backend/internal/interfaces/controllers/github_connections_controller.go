package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	githubConnectionLabel = "agentapi.ccplant.io/github-connection"
	githubIdentityLabel   = "agentapi.ccplant.io/github-identity"
	githubPrincipalLabel  = "agentapi.ccplant.io/github-principal"
	githubOAuthStateLabel = "agentapi.ccplant.io/github-oauth-state"
	githubOAuthStateTTL   = 10 * time.Minute
)

var githubSecretEnvPattern = regexp.MustCompile(`^GITHUB_OAUTH_[A-Z0-9_]+_CLIENT_SECRET$`)

type githubConnection struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	BaseURL           string    `json:"base_url"`
	APIURL            string    `json:"api_url"`
	OAuthClientID     string    `json:"oauth_client_id"`
	SecretSource      string    `json:"secret_source"`
	SecretEnvironment string    `json:"secret_environment,omitempty"`
	Enabled           bool      `json:"enabled"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type githubConnectionResponse struct {
	githubConnection
	SecretConfigured bool   `json:"secret_configured"`
	CallbackURL      string `json:"callback_url"`
	LinkedIdentities int    `json:"linked_identities"`
}

type githubConnectionRequest struct {
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	APIURL        string `json:"api_url"`
	OAuthClientID string `json:"oauth_client_id"`
	Enabled       *bool  `json:"enabled,omitempty"`
	Secret        struct {
		Source      string `json:"source"`
		Value       string `json:"value,omitempty"`
		Environment string `json:"environment,omitempty"`
	} `json:"oauth_client_secret"`
}

type githubSecretUpdate struct {
	Source      string `json:"source"`
	Value       string `json:"value,omitempty"`
	Environment string `json:"environment,omitempty"`
}

type githubPrincipal struct {
	ID             string    `json:"id"`
	InternalUserID string    `json:"internal_user_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type githubIdentity struct {
	ID           string    `json:"id"`
	PrincipalID  string    `json:"principal_id"`
	ConnectionID string    `json:"connection_id"`
	GitHubUserID int64     `json:"github_user_id"`
	Login        string    `json:"login"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type githubIdentityResponse struct {
	githubIdentity
	ConnectionName string `json:"connection_name"`
	BaseURL        string `json:"base_url"`
}

type githubOAuthState struct {
	ID             string    `json:"id"`
	ConnectionID   string    `json:"connection_id"`
	PrincipalID    string    `json:"principal_id"`
	InternalUserID string    `json:"internal_user_id"`
	ReturnTo       string    `json:"return_to"`
	CallbackURL    string    `json:"callback_url"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type githubOAuthUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// GitHubConnectionsController manages administrator-defined GitHub OAuth
// connections and user-owned external identities.
type GitHubConnectionsController struct {
	client           kubernetes.Interface
	namespace        string
	httpClient       *http.Client
	callbackURL      string
	encryptedStorage bool
}

func NewGitHubConnectionsController(client kubernetes.Interface, namespace, publicBaseURL string, encryptedStorage ...bool) *GitHubConnectionsController {
	controller := &GitHubConnectionsController{
		client:     client,
		namespace:  namespace,
		httpClient: utils.NewDefaultHTTPClient(),
	}
	if publicBaseURL != "" {
		controller.callbackURL = strings.TrimSuffix(publicBaseURL, "/") + "/auth/github-connections/callback"
	}
	if len(encryptedStorage) > 0 {
		controller.encryptedStorage = encryptedStorage[0]
	}
	return controller
}

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
		return echo.NewHTTPError(http.StatusBadRequest, "encrypted secret storage requires the libsql-encrypted KV backend")
	}

	now := time.Now().UTC()
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	connection := githubConnection{
		ID: uuid.NewString(), Name: strings.TrimSpace(request.Name), BaseURL: baseURL, APIURL: apiURL,
		OAuthClientID: strings.TrimSpace(request.OAuthClientID), SecretSource: request.Secret.Source,
		SecretEnvironment: request.Secret.Environment, Enabled: enabled, CreatedAt: now, UpdatedAt: now,
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
	connection, secret, _, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), connection, secret != "" || c.secretConfigured(ctx.Request().Context(), connection)))
}

func (c *GitHubConnectionsController) Update(ctx echo.Context) error {
	connection, secret, resourceVersion, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
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
	if request.Enabled != nil {
		connection.Enabled = *request.Enabled
	}
	connection.UpdatedAt = time.Now().UTC()
	if err := c.saveConnection(ctx.Request().Context(), connection, secret, resourceVersion); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update GitHub connection").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), connection, c.secretConfigured(ctx.Request().Context(), connection)))
}

func (c *GitHubConnectionsController) UpdateSecret(ctx echo.Context) error {
	connection, _, resourceVersion, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	var request githubSecretUpdate
	if err := ctx.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if err := validateGitHubSecret(request.Source, request.Value, request.Environment); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if request.Source == "encrypted" && !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusBadRequest, "encrypted secret storage requires the libsql-encrypted KV backend")
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
	connection, _, resourceVersion, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	connection.SecretEnvironment = ""
	connection.UpdatedAt = time.Now().UTC()
	if err := c.saveConnection(ctx.Request().Context(), connection, "", resourceVersion); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete GitHub client secret").SetInternal(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *GitHubConnectionsController) Test(ctx echo.Context) error {
	connection, _, _, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
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
	connection, _, resourceVersion, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
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

func (c *GitHubConnectionsController) ListIdentities(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principal, err := c.loadPrincipal(ctx.Request().Context(), principalSubject(user))
	if apierrors.IsNotFound(err) {
		return ctx.JSON(http.StatusOK, map[string]any{"principal_id": nil, "identities": []any{}})
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load principal").SetInternal(err)
	}
	identities, err := c.listIdentities(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list GitHub identities").SetInternal(err)
	}
	result := make([]githubIdentityResponse, 0)
	for _, identity := range identities {
		if identity.PrincipalID != principal.ID {
			continue
		}
		connection, _, _, loadErr := c.loadConnection(ctx.Request().Context(), identity.ConnectionID)
		if loadErr == nil {
			result = append(result, githubIdentityResponse{githubIdentity: identity, ConnectionName: connection.Name, BaseURL: connection.BaseURL})
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"principal_id": principal.ID, "identities": result})
}

func (c *GitHubConnectionsController) StartLink(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	var request struct {
		ConnectionID string `json:"connection_id"`
		ReturnTo     string `json:"return_to"`
		CallbackURL  string `json:"callback_url"`
	}
	if err := ctx.Bind(&request); err != nil || request.ConnectionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "connection_id is required")
	}
	connection, _, _, err := c.loadConnection(ctx.Request().Context(), request.ConnectionID)
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil || !connection.Enabled {
		return echo.NewHTTPError(http.StatusBadRequest, "GitHub connection is unavailable")
	}
	internalSubject := principalSubject(user)
	principal, err := c.getOrCreatePrincipal(ctx.Request().Context(), internalSubject)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve principal").SetInternal(err)
	}
	returnTo := sanitizeReturnTo(request.ReturnTo)
	callbackURL, err := c.resolveCallbackURL(ctx, request.CallbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid callback_url")
	}
	state := githubOAuthState{ID: uuid.NewString(), ConnectionID: connection.ID, PrincipalID: principal.ID, InternalUserID: internalSubject, ReturnTo: returnTo, CallbackURL: callbackURL, ExpiresAt: time.Now().UTC().Add(githubOAuthStateTTL)}
	if err := c.createObject(ctx.Request().Context(), stateSecretName(state.ID), githubOAuthStateLabel, state, nil); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create OAuth state").SetInternal(err)
	}
	params := url.Values{"client_id": {connection.OAuthClientID}, "redirect_uri": {callbackURL}, "state": {state.ID}, "scope": {"read:user"}}
	return ctx.JSON(http.StatusOK, map[string]string{"authorization_url": connection.BaseURL + "/login/oauth/authorize?" + params.Encode()})
}

func (c *GitHubConnectionsController) Callback(ctx echo.Context) error {
	stateID, code := ctx.QueryParam("state"), ctx.QueryParam("code")
	if stateID == "" || code == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "code and state are required")
	}
	var state githubOAuthState
	secret, err := c.loadObject(ctx.Request().Context(), stateSecretName(stateID), &state)
	if apierrors.IsNotFound(err) || state.ID != stateID || time.Now().UTC().After(state.ExpiresAt) {
		return echo.NewHTTPError(http.StatusBadRequest, "OAuth state is invalid or expired")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load OAuth state").SetInternal(err)
	}
	if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx.Request().Context(), secret.Name, metav1.DeleteOptions{}); err != nil {
		return echo.NewHTTPError(http.StatusConflict, "OAuth state has already been used")
	}
	connection, _, _, err := c.loadConnection(ctx.Request().Context(), state.ConnectionID)
	if err != nil || !connection.Enabled {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "connection_unavailable")
	}
	clientSecret, err := c.resolveClientSecret(ctx.Request().Context(), connection)
	if err != nil {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "secret_unavailable")
	}
	token, err := c.exchangeCode(ctx.Request().Context(), connection, clientSecret, code, state.CallbackURL)
	if err != nil {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "token_exchange_failed")
	}
	githubUser, err := c.fetchGitHubUser(ctx.Request().Context(), connection, token)
	if err != nil {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "user_lookup_failed")
	}
	identity := githubIdentity{ID: uuid.NewString(), PrincipalID: state.PrincipalID, ConnectionID: connection.ID, GitHubUserID: githubUser.ID, Login: githubUser.Login, AvatarURL: githubUser.AvatarURL, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	created, err := c.linkIdentity(ctx.Request().Context(), identity)
	if errors.Is(err, errIdentityConflict) {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "identity_linked_to_another_principal")
	}
	if err != nil {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "identity_link_failed")
	}
	if !created {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "success", "already_linked")
	}
	return c.redirectOAuthResult(ctx, state.ReturnTo, "success", "linked")
}

func (c *GitHubConnectionsController) Unlink(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principal, err := c.loadPrincipal(ctx.Request().Context(), principalSubject(user))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "principal not found")
	}
	identities, err := c.listIdentities(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list identities")
	}
	for _, identity := range identities {
		if identity.ID != ctx.Param("identity_id") || identity.PrincipalID != principal.ID {
			continue
		}
		if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx.Request().Context(), identitySecretName(identity.ConnectionID, identity.GitHubUserID), metav1.DeleteOptions{}); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to unlink identity").SetInternal(err)
		}
		return ctx.NoContent(http.StatusNoContent)
	}
	return echo.NewHTTPError(http.StatusNotFound, "GitHub identity not found")
}

var errIdentityConflict = errors.New("GitHub identity belongs to another principal")

func (c *GitHubConnectionsController) linkIdentity(ctx context.Context, identity githubIdentity) (bool, error) {
	name := identitySecretName(identity.ConnectionID, identity.GitHubUserID)
	var existing githubIdentity
	_, err := c.loadObject(ctx, name, &existing)
	if err == nil {
		if existing.PrincipalID == identity.PrincipalID {
			return false, nil
		}
		return false, errIdentityConflict
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	err = c.createObject(ctx, name, githubIdentityLabel, identity, map[string]string{"agentapi.ccplant.io/connection-id": identity.ConnectionID})
	if apierrors.IsAlreadyExists(err) {
		return c.linkIdentity(ctx, identity)
	}
	return err == nil, err
}

func (c *GitHubConnectionsController) getOrCreatePrincipal(ctx context.Context, internalUserID string) (githubPrincipal, error) {
	principal, err := c.loadPrincipal(ctx, internalUserID)
	if err == nil {
		return principal, nil
	}
	if !apierrors.IsNotFound(err) {
		return githubPrincipal{}, err
	}
	principal = githubPrincipal{ID: uuid.NewString(), InternalUserID: internalUserID, CreatedAt: time.Now().UTC()}
	err = c.createObject(ctx, principalSecretName(internalUserID), githubPrincipalLabel, principal, nil)
	if apierrors.IsAlreadyExists(err) {
		return c.loadPrincipal(ctx, internalUserID)
	}
	return principal, err
}

func (c *GitHubConnectionsController) loadPrincipal(ctx context.Context, internalUserID string) (githubPrincipal, error) {
	var principal githubPrincipal
	_, err := c.loadObject(ctx, principalSecretName(internalUserID), &principal)
	return principal, err
}

func (c *GitHubConnectionsController) listConnections(ctx context.Context) ([]githubConnection, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: githubConnectionLabel + "=true"})
	if err != nil {
		return nil, err
	}
	connections := make([]githubConnection, 0, len(secrets.Items))
	for i := range secrets.Items {
		var connection githubConnection
		if err := json.Unmarshal(secrets.Items[i].Data["record.json"], &connection); err == nil {
			connections = append(connections, connection)
		}
	}
	sort.Slice(connections, func(i, j int) bool { return connections[i].Name < connections[j].Name })
	return connections, nil
}

func (c *GitHubConnectionsController) listIdentities(ctx context.Context) ([]githubIdentity, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: githubIdentityLabel + "=true"})
	if err != nil {
		return nil, err
	}
	identities := make([]githubIdentity, 0, len(secrets.Items))
	for i := range secrets.Items {
		var identity githubIdentity
		if err := json.Unmarshal(secrets.Items[i].Data["record.json"], &identity); err == nil {
			identities = append(identities, identity)
		}
	}
	return identities, nil
}

func (c *GitHubConnectionsController) identityCount(ctx context.Context, connectionID string) int {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return 0
	}
	count := 0
	for _, identity := range identities {
		if identity.ConnectionID == connectionID {
			count++
		}
	}
	return count
}

func (c *GitHubConnectionsController) saveConnection(ctx context.Context, connection githubConnection, clientSecret, resourceVersion string) error {
	record, err := json.Marshal(connection)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: connectionSecretName(connection.ID), Namespace: c.namespace, ResourceVersion: resourceVersion, Labels: map[string]string{githubConnectionLabel: "true"}}, Data: map[string][]byte{"record.json": record}}
	if connection.SecretSource == "encrypted" && clientSecret != "" {
		secret.Data["client_secret"] = []byte(clientSecret)
	}
	if resourceVersion == "" {
		_, err = c.client.CoreV1().Secrets(c.namespace).Create(ctx, secret, metav1.CreateOptions{})
	} else {
		_, err = c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	}
	return err
}

func (c *GitHubConnectionsController) loadConnection(ctx context.Context, id string) (githubConnection, string, string, error) {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, connectionSecretName(id), metav1.GetOptions{})
	if err != nil {
		return githubConnection{}, "", "", err
	}
	var connection githubConnection
	if err := json.Unmarshal(secret.Data["record.json"], &connection); err != nil {
		return githubConnection{}, "", "", err
	}
	return connection, string(secret.Data["client_secret"]), secret.ResourceVersion, nil
}

func (c *GitHubConnectionsController) createObject(ctx context.Context, name, label string, value any, labels map[string]string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if labels == nil {
		labels = map[string]string{}
	}
	labels[label] = "true"
	_, err = c.client.CoreV1().Secrets(c.namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: labels}, Data: map[string][]byte{"record.json": data}}, metav1.CreateOptions{})
	return err
}

func (c *GitHubConnectionsController) loadObject(ctx context.Context, name string, target any) (*corev1.Secret, error) {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(secret.Data["record.json"], target); err != nil {
		return nil, err
	}
	return secret, nil
}

func (c *GitHubConnectionsController) resolveClientSecret(ctx context.Context, connection githubConnection) (string, error) {
	if connection.SecretSource == "environment" {
		if !githubSecretEnvPattern.MatchString(connection.SecretEnvironment) {
			return "", errors.New("invalid client secret environment variable")
		}
		value, ok := os.LookupEnv(connection.SecretEnvironment)
		if !ok || value == "" {
			return "", errors.New("client secret environment variable is not configured")
		}
		return value, nil
	}
	_, secret, _, err := c.loadConnection(ctx, connection.ID)
	if err != nil || secret == "" {
		return "", errors.New("encrypted client secret is not configured")
	}
	return secret, nil
}

func (c *GitHubConnectionsController) secretConfigured(ctx context.Context, connection githubConnection) bool {
	_, err := c.resolveClientSecret(ctx, connection)
	return err == nil
}

func (c *GitHubConnectionsController) connectionResponse(ctx context.Context, connection githubConnection, configured bool) githubConnectionResponse {
	callbackURL := c.callbackURL
	if callbackURL == "" {
		callbackURL = "/auth/github-connections/callback"
	}
	return githubConnectionResponse{githubConnection: connection, SecretConfigured: configured, CallbackURL: callbackURL, LinkedIdentities: c.identityCount(ctx, connection.ID)}
}

func (c *GitHubConnectionsController) exchangeCode(ctx context.Context, connection githubConnection, clientSecret, code, callbackURL string) (string, error) {
	values := url.Values{"client_id": {connection.OAuthClientID}, "client_secret": {clientSecret}, "code": {code}, "redirect_uri": {callbackURL}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.BaseURL+"/login/oauth/access_token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer utils.SafeCloseResponse(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub token endpoint returned %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("GitHub token exchange failed: %s", payload.Error)
	}
	return payload.AccessToken, nil
}

func (c *GitHubConnectionsController) resolveCallbackURL(ctx echo.Context, requested string) (string, error) {
	if c.callbackURL != "" {
		return c.callbackURL, nil
	}
	if requested != "" {
		callback, err := url.Parse(requested)
		origin, originErr := url.Parse(ctx.Request().Header.Get("Origin"))
		allowedPath := callback != nil && (callback.Path == "/auth/github-connections/callback" || callback.Path == "/api/proxy/auth/github-connections/callback")
		if err != nil || originErr != nil || callback.Scheme != origin.Scheme || callback.Host != origin.Host || !allowedPath || callback.RawQuery != "" || callback.Fragment != "" {
			return "", errors.New("callback URL must use the request origin and the GitHub connection callback path")
		}
		return callback.String(), nil
	}
	scheme := ctx.Request().Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = ctx.Scheme()
	}
	host := ctx.Request().Header.Get("X-Forwarded-Host")
	if host == "" {
		host = ctx.Request().Host
	}
	return scheme + "://" + host + "/auth/github-connections/callback", nil
}

func (c *GitHubConnectionsController) fetchGitHubUser(ctx context.Context, connection githubConnection, token string) (githubOAuthUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.APIURL+"/user", nil)
	if err != nil {
		return githubOAuthUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return githubOAuthUser{}, err
	}
	defer utils.SafeCloseResponse(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return githubOAuthUser{}, fmt.Errorf("GitHub user endpoint returned %d", resp.StatusCode)
	}
	var user githubOAuthUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return githubOAuthUser{}, err
	}
	if user.ID == 0 || user.Login == "" {
		return githubOAuthUser{}, errors.New("GitHub user response is incomplete")
	}
	return user, nil
}

func (c *GitHubConnectionsController) redirectOAuthResult(ctx echo.Context, returnTo, status, result string) error {
	target, _ := url.Parse(sanitizeReturnTo(returnTo))
	query := target.Query()
	query.Set("github_link", status)
	query.Set("github_link_result", result)
	target.RawQuery = query.Encode()
	return ctx.Redirect(http.StatusFound, target.String())
}

func normalizeGitHubURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("URL must be an HTTPS origin or path without credentials, query, or fragment")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func validateGitHubSecret(source, value, environment string) error {
	switch source {
	case "encrypted":
		if value == "" {
			return errors.New("oauth_client_secret.value is required for encrypted source")
		}
	case "environment":
		if !githubSecretEnvPattern.MatchString(environment) {
			return errors.New("oauth_client_secret.environment must match GITHUB_OAUTH_*_CLIENT_SECRET")
		}
	default:
		return errors.New("oauth_client_secret.source must be encrypted or environment")
	}
	return nil
}

func sanitizeReturnTo(value string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "/settings/personal/account-connections"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host != "" || parsed.Scheme != "" {
		return "/settings/personal/account-connections"
	}
	return parsed.String()
}

func connectionSecretName(id string) string { return "github-connection-" + id }
func stateSecretName(id string) string      { return "github-oauth-state-" + id }
func identitySecretName(connectionID string, userID int64) string {
	return fmt.Sprintf("github-identity-%s-%d", connectionID, userID)
}
func principalSecretName(internalUserID string) string {
	digest := sha256.Sum256([]byte(internalUserID))
	return "github-principal-" + hex.EncodeToString(digest[:16])
}

func principalSubject(user *entities.User) string {
	if info := user.GitHubInfo(); info != nil && info.ID() != 0 {
		return fmt.Sprintf("github:%d", info.ID())
	}
	return "internal:" + string(user.ID())
}
