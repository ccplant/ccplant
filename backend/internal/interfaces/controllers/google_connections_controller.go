package controllers

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	googleConnectionLabel = "agentapi.ccplant.io/google-connection"
	googleIdentityLabel   = "agentapi.ccplant.io/google-identity"
	googleOAuthStateLabel = "agentapi.ccplant.io/google-oauth-state"
	googleOAuthStateTTL   = 10 * time.Minute
)

var googleSecretEnvPattern = regexp.MustCompile(`^GOOGLE_OAUTH_[A-Z0-9_]+_CLIENT_SECRET$`)

type googleConnection struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	OAuthClientID     string    `json:"oauth_client_id"`
	SecretSource      string    `json:"secret_source"`
	SecretEnvironment string    `json:"secret_environment,omitempty"`
	Enabled           bool      `json:"enabled"`
	AllowLogin        bool      `json:"allow_login"`
	ShowOnLogin       bool      `json:"show_on_login"`
	AllowUserCreation bool      `json:"allow_user_creation"`
	HostedDomains     []string  `json:"hosted_domains,omitempty"`
	EmailDomains      []string  `json:"email_domains,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type googleConnectionResponse struct {
	googleConnection
	SecretConfigured bool   `json:"secret_configured"`
	CallbackURL      string `json:"callback_url"`
	LinkedIdentities int    `json:"linked_identities"`
}

type googleConnectionRequest struct {
	Name              string   `json:"name"`
	OAuthClientID     string   `json:"oauth_client_id"`
	Enabled           *bool    `json:"enabled,omitempty"`
	AllowLogin        *bool    `json:"allow_login,omitempty"`
	ShowOnLogin       *bool    `json:"show_on_login,omitempty"`
	AllowUserCreation *bool    `json:"allow_user_creation,omitempty"`
	HostedDomains     []string `json:"hosted_domains,omitempty"`
	EmailDomains      []string `json:"email_domains,omitempty"`
	Secret            struct {
		Source      string `json:"source"`
		Value       string `json:"value,omitempty"`
		Environment string `json:"environment,omitempty"`
	} `json:"oauth_client_secret"`
}

type googleSecretUpdate struct {
	Source      string `json:"source"`
	Value       string `json:"value,omitempty"`
	Environment string `json:"environment,omitempty"`
}

type googleIdentity struct {
	ID            string    `json:"id"`
	PrincipalID   string    `json:"principal_id"`
	ConnectionID  string    `json:"connection_id"`
	Subject       string    `json:"subject"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	Name          string    `json:"name,omitempty"`
	AvatarURL     string    `json:"avatar_url,omitempty"`
	HostedDomain  string    `json:"hosted_domain,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type googleIdentityResponse struct {
	googleIdentity
	ConnectionName string `json:"connection_name"`
	Current        bool   `json:"current"`
}

type googleOAuthState struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connection_id"`
	PrincipalID  string    `json:"principal_id,omitempty"`
	ReturnTo     string    `json:"return_to,omitempty"`
	CallbackURL  string    `json:"callback_url"`
	Mode         string    `json:"mode"`
	Nonce        string    `json:"nonce"`
	PKCEVerifier string    `json:"pkce_verifier"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type GoogleConnectionLoginResult struct {
	PrincipalID  string
	UserID       string
	Email        string
	Name         string
	AvatarURL    string
	ConnectionID string
}

type googleClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	HostedDomain  string
	Nonce         string
}

// GoogleConnectionsController manages administrator-defined Google OIDC
// clients and Google identities attached to the shared ccplant principal.
type GoogleConnectionsController struct {
	client           kubernetes.Interface
	namespace        string
	httpClient       *http.Client
	callbackURL      string
	encryptedStorage bool
	principals       *GitHubConnectionsController
	validateIDToken  func(context.Context, string, string) (*googleClaims, error)
}

func NewGoogleConnectionsController(client kubernetes.Interface, namespace, publicBaseURL string, encryptedStorage bool, principals *GitHubConnectionsController) *GoogleConnectionsController {
	c := &GoogleConnectionsController{client: client, namespace: namespace, httpClient: utils.NewDefaultHTTPClient(), encryptedStorage: encryptedStorage, principals: principals}
	if publicBaseURL != "" {
		c.callbackURL = strings.TrimSuffix(publicBaseURL, "/") + "/auth/google-connections/callback"
	}
	c.validateIDToken = c.validateGoogleIDToken
	return c
}

func (c *GoogleConnectionsController) Create(ctx echo.Context) error {
	var req googleConnectionRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.OAuthClientID) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and oauth_client_id are required")
	}
	if err := validateGoogleSecret(req.Secret.Source, req.Secret.Value, req.Secret.Environment); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Secret.Source == "encrypted" && !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusBadRequest, "encrypted secret storage requires the libsql-encrypted or kubernetes KV backend")
	}
	now := time.Now().UTC()
	connection := googleConnection{ID: uuid.NewString(), Name: strings.TrimSpace(req.Name), OAuthClientID: strings.TrimSpace(req.OAuthClientID), SecretSource: req.Secret.Source, SecretEnvironment: strings.TrimSpace(req.Secret.Environment), Enabled: boolDefault(req.Enabled, true), AllowLogin: boolDefault(req.AllowLogin, true), ShowOnLogin: boolDefault(req.ShowOnLogin, true), AllowUserCreation: boolDefault(req.AllowUserCreation, false), HostedDomains: normalizeDomains(req.HostedDomains), EmailDomains: normalizeDomains(req.EmailDomains), CreatedAt: now, UpdatedAt: now}
	if connection.ShowOnLogin && (!connection.Enabled || !connection.AllowLogin) {
		return echo.NewHTTPError(http.StatusBadRequest, "show_on_login requires enabled and allow_login")
	}
	if err := c.saveConnection(ctx.Request().Context(), connection, req.Secret.Value, ""); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create Google connection").SetInternal(err)
	}
	return ctx.JSON(http.StatusCreated, c.connectionResponse(ctx.Request().Context(), connection, req.Secret.Value != ""))
}

func (c *GoogleConnectionsController) List(ctx echo.Context) error {
	connections, err := c.listConnections(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list Google connections").SetInternal(err)
	}
	result := make([]googleConnectionResponse, 0, len(connections))
	for _, connection := range connections {
		result = append(result, c.connectionResponse(ctx.Request().Context(), connection, c.secretConfigured(ctx.Request().Context(), connection)))
	}
	return ctx.JSON(http.StatusOK, map[string]any{"connections": result})
}

func (c *GoogleConnectionsController) Get(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load Google connection").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), record.connection, record.clientSecret != "" || c.secretConfigured(ctx.Request().Context(), record.connection)))
}

func (c *GoogleConnectionsController) Update(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load Google connection").SetInternal(err)
	}
	connection, secret, rv := record.connection, record.clientSecret, record.resourceVersion
	var req googleConnectionRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Name != "" {
		connection.Name = strings.TrimSpace(req.Name)
	}
	if req.OAuthClientID != "" {
		connection.OAuthClientID = strings.TrimSpace(req.OAuthClientID)
	}
	if req.Enabled != nil {
		connection.Enabled = *req.Enabled
	}
	if req.AllowLogin != nil {
		connection.AllowLogin = *req.AllowLogin
	}
	if req.ShowOnLogin != nil {
		connection.ShowOnLogin = *req.ShowOnLogin
	}
	if req.AllowUserCreation != nil {
		connection.AllowUserCreation = *req.AllowUserCreation
	}
	if req.HostedDomains != nil {
		connection.HostedDomains = normalizeDomains(req.HostedDomains)
	}
	if req.EmailDomains != nil {
		connection.EmailDomains = normalizeDomains(req.EmailDomains)
	}
	if connection.ShowOnLogin && (!connection.Enabled || !connection.AllowLogin) {
		return echo.NewHTTPError(http.StatusBadRequest, "show_on_login requires enabled and allow_login")
	}
	connection.UpdatedAt = time.Now().UTC()
	if err := c.saveConnection(ctx.Request().Context(), connection, secret, rv); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update Google connection").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), connection, secret != "" || c.secretConfigured(ctx.Request().Context(), connection)))
}

func (c *GoogleConnectionsController) UpdateSecret(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load Google connection").SetInternal(err)
	}
	connection, rv := record.connection, record.resourceVersion
	var req googleSecretUpdate
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if err := validateGoogleSecret(req.Source, req.Value, req.Environment); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Source == "encrypted" && !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusBadRequest, "encrypted secret storage requires the libsql-encrypted or kubernetes KV backend")
	}
	connection.SecretSource, connection.SecretEnvironment, connection.UpdatedAt = req.Source, strings.TrimSpace(req.Environment), time.Now().UTC()
	if err := c.saveConnection(ctx.Request().Context(), connection, req.Value, rv); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update Google connection secret").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), connection, true))
}

func (c *GoogleConnectionsController) DeleteSecret(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load Google connection").SetInternal(err)
	}
	connection, rv := record.connection, record.resourceVersion
	connection.SecretSource, connection.SecretEnvironment, connection.UpdatedAt = "", "", time.Now().UTC()
	if err := c.saveConnection(ctx.Request().Context(), connection, "", rv); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete Google connection secret").SetInternal(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *GoogleConnectionsController) Delete(ctx echo.Context) error {
	id := ctx.Param("id")
	if c.identityCount(ctx.Request().Context(), id) > 0 {
		return echo.NewHTTPError(http.StatusConflict, "Google connection has linked identities")
	}
	if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx.Request().Context(), googleConnectionSecretName(id), metav1.DeleteOptions{}); apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete Google connection").SetInternal(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *GoogleConnectionsController) Test(ctx echo.Context) error {
	record, err := c.loadConnectionRecord(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	connection := record.connection
	_, secretErr := c.resolveClientSecret(ctx.Request().Context(), connection)
	req, requestErr := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet, "https://accounts.google.com/.well-known/openid-configuration", nil)
	if requestErr != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to build Google discovery request").SetInternal(requestErr)
	}
	resp, discoveryErr := c.httpClient.Do(req)
	if resp != nil {
		utils.SafeCloseResponseBody(resp.Body)
	}
	reachable := discoveryErr == nil && resp != nil && resp.StatusCode == http.StatusOK
	return ctx.JSON(http.StatusOK, map[string]bool{"discovery_reachable": reachable, "secret_resolvable": secretErr == nil})
}

func (c *GoogleConnectionsController) ListLoginOptions(ctx echo.Context) error {
	connections, err := c.listConnections(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list Google connections").SetInternal(err)
	}
	result := make([]map[string]string, 0)
	for _, connection := range connections {
		if connection.Enabled && connection.AllowLogin && connection.ShowOnLogin && c.secretConfigured(ctx.Request().Context(), connection) {
			result = append(result, map[string]string{"id": connection.ID, "name": connection.Name})
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"connections": result})
}

func (c *GoogleConnectionsController) ListAvailable(ctx echo.Context) error {
	connections, err := c.listConnections(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list Google connections").SetInternal(err)
	}
	result := make([]map[string]any, 0)
	for _, connection := range connections {
		if connection.Enabled {
			result = append(result, map[string]any{"id": connection.ID, "name": connection.Name})
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"connections": result})
}

func (c *GoogleConnectionsController) StartLogin(ctx echo.Context) error {
	var req struct {
		ConnectionID string `json:"connection_id"`
		CallbackURL  string `json:"callback_url"`
	}
	if err := ctx.Bind(&req); err != nil || req.ConnectionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "connection_id is required")
	}
	record, err := c.loadConnectionRecord(ctx.Request().Context(), req.ConnectionID)
	if err != nil || !record.connection.Enabled || !record.connection.AllowLogin || !c.secretConfigured(ctx.Request().Context(), record.connection) {
		return echo.NewHTTPError(http.StatusBadRequest, "Google connection is unavailable")
	}
	connection := record.connection
	callbackURL, err := c.resolveCallbackURL(ctx, req.CallbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid callback_url")
	}
	state, err := c.createState(ctx.Request().Context(), googleStateRequest{connectionID: connection.ID, mode: "login", callbackURL: callbackURL})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create OAuth state").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, map[string]string{"auth_url": googleAuthorizationURL(connection, state), "state": state.ID})
}

func (c *GoogleConnectionsController) OAuthStateMode(ctx context.Context, stateID string) (string, error) {
	state, secret, err := c.loadState(ctx, stateID)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", errors.New("Google OAuth state returned no secret")
	}
	return state.Mode, nil
}
