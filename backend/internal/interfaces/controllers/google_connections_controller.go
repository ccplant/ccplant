package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
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
	connection, secret, _, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load Google connection").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, c.connectionResponse(ctx.Request().Context(), connection, secret != "" || c.secretConfigured(ctx.Request().Context(), connection)))
}

func (c *GoogleConnectionsController) Update(ctx echo.Context) error {
	connection, secret, rv, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load Google connection").SetInternal(err)
	}
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
	connection, _, rv, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load Google connection").SetInternal(err)
	}
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
	connection, _, rv, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load Google connection").SetInternal(err)
	}
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
	connection, _, _, err := c.loadConnection(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Google connection not found")
	}
	_, secretErr := c.resolveClientSecret(ctx.Request().Context(), connection)
	req, _ := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet, "https://accounts.google.com/.well-known/openid-configuration", nil)
	resp, discoveryErr := c.httpClient.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
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
	connection, _, _, err := c.loadConnection(ctx.Request().Context(), req.ConnectionID)
	if err != nil || !connection.Enabled || !connection.AllowLogin || !c.secretConfigured(ctx.Request().Context(), connection) {
		return echo.NewHTTPError(http.StatusBadRequest, "Google connection is unavailable")
	}
	callbackURL, err := c.resolveCallbackURL(ctx, req.CallbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid callback_url")
	}
	state, err := c.createState(ctx.Request().Context(), connection.ID, "login", "", "", callbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create OAuth state").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, map[string]string{"auth_url": googleAuthorizationURL(connection, state), "state": state.ID})
}

func (c *GoogleConnectionsController) OAuthStateMode(ctx context.Context, stateID string) (string, error) {
	state, _, err := c.loadState(ctx, stateID)
	if err != nil {
		return "", err
	}
	return state.Mode, nil
}

func (c *GoogleConnectionsController) CompleteLogin(ctx context.Context, stateID, code string) (*GoogleConnectionLoginResult, error) {
	state, connection, claims, err := c.completeAuthorization(ctx, stateID, code, "login")
	if err != nil {
		return nil, err
	}
	if !connection.AllowLogin {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Google login is disabled")
	}
	principal, err := c.resolveLoginPrincipal(ctx, connection, claims)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Google identity could not be resolved").SetInternal(err)
	}
	return &GoogleConnectionLoginResult{PrincipalID: principal.ID, UserID: applicationUserID(principal), Email: claims.Email, Name: claims.Name, AvatarURL: claims.Picture, ConnectionID: state.ConnectionID}, nil
}

func (c *GoogleConnectionsController) ListIdentities(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principal, err := c.principals.loadPrincipalForUser(ctx.Request().Context(), user)
	if apierrors.IsNotFound(err) {
		return ctx.JSON(http.StatusOK, map[string]any{"principal_id": nil, "identities": []any{}})
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load principal").SetInternal(err)
	}
	identities, err := c.listIdentities(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list Google identities").SetInternal(err)
	}
	result := make([]googleIdentityResponse, 0)
	for _, identity := range identities {
		if identity.PrincipalID == principal.ID {
			if connection, _, _, loadErr := c.loadConnection(ctx.Request().Context(), identity.ConnectionID); loadErr == nil {
				result = append(result, googleIdentityResponse{googleIdentity: identity, ConnectionName: connection.Name})
			}
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"principal_id": principal.ID, "identities": result})
}

func (c *GoogleConnectionsController) StartLink(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	if !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "account linking requires encrypted storage")
	}
	var req struct {
		ConnectionID string `json:"connection_id"`
		ReturnTo     string `json:"return_to"`
		CallbackURL  string `json:"callback_url"`
	}
	if err := ctx.Bind(&req); err != nil || req.ConnectionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "connection_id is required")
	}
	connection, _, _, err := c.loadConnection(ctx.Request().Context(), req.ConnectionID)
	if err != nil || !connection.Enabled {
		return echo.NewHTTPError(http.StatusBadRequest, "Google connection is unavailable")
	}
	principal, err := c.principals.getOrCreatePrincipalForUser(ctx.Request().Context(), user)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve principal").SetInternal(err)
	}
	callbackURL, err := c.resolveCallbackURL(ctx, req.CallbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid callback_url")
	}
	state, err := c.createState(ctx.Request().Context(), connection.ID, "link", principal.ID, sanitizeReturnTo(req.ReturnTo), callbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create OAuth state").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, map[string]string{"authorization_url": googleAuthorizationURL(connection, state)})
}

func (c *GoogleConnectionsController) Callback(ctx echo.Context) error {
	stateID, code := ctx.QueryParam("state"), ctx.QueryParam("code")
	state, _, claims, err := c.completeAuthorization(ctx.Request().Context(), stateID, code, "link")
	if err != nil {
		return c.redirectResult(ctx, state.ReturnTo, "error", "authentication_failed")
	}
	identity := googleIdentity{ID: uuid.NewString(), PrincipalID: state.PrincipalID, ConnectionID: state.ConnectionID, Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: claims.Name, AvatarURL: claims.Picture, HostedDomain: claims.HostedDomain, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	created, err := c.linkIdentity(ctx.Request().Context(), identity)
	if errors.Is(err, errGoogleIdentityConflict) {
		return c.redirectResult(ctx, state.ReturnTo, "error", "identity_linked_to_another_principal")
	}
	if err != nil {
		return c.redirectResult(ctx, state.ReturnTo, "error", "identity_link_failed")
	}
	if !created {
		return c.redirectResult(ctx, state.ReturnTo, "success", "already_linked")
	}
	return c.redirectResult(ctx, state.ReturnTo, "success", "linked")
}

func (c *GoogleConnectionsController) Unlink(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principal, err := c.principals.loadPrincipalForUser(ctx.Request().Context(), user)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "principal not found")
	}
	identities, err := c.listIdentities(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list identities")
	}
	for _, identity := range identities {
		if identity.ID == ctx.Param("identity_id") && identity.PrincipalID == principal.ID {
			if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx.Request().Context(), googleIdentitySecretName(identity.ConnectionID, identity.Subject), metav1.DeleteOptions{}); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to unlink identity").SetInternal(err)
			}
			return ctx.NoContent(http.StatusNoContent)
		}
	}
	return echo.NewHTTPError(http.StatusNotFound, "Google identity not found")
}

func (c *GoogleConnectionsController) completeAuthorization(ctx context.Context, stateID, code, mode string) (googleOAuthState, googleConnection, *googleClaims, error) {
	if stateID == "" || code == "" {
		return googleOAuthState{}, googleConnection{}, nil, echo.NewHTTPError(http.StatusBadRequest, "code and state are required")
	}
	state, secret, err := c.loadState(ctx, stateID)
	if err != nil || state.Mode != mode {
		return state, googleConnection{}, nil, echo.NewHTTPError(http.StatusBadRequest, "OAuth state is invalid or expired")
	}
	if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
		return state, googleConnection{}, nil, echo.NewHTTPError(http.StatusConflict, "OAuth state has already been used")
	}
	connection, _, _, err := c.loadConnection(ctx, state.ConnectionID)
	if err != nil || !connection.Enabled {
		return state, googleConnection{}, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google connection is unavailable")
	}
	clientSecret, err := c.resolveClientSecret(ctx, connection)
	if err != nil {
		return state, connection, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google connection secret is unavailable")
	}
	config := oauth2.Config{ClientID: connection.OAuthClientID, ClientSecret: clientSecret, RedirectURL: state.CallbackURL, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token"}, Scopes: []string{"openid", "email", "profile"}}
	exchangeCtx := context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	token, err := config.Exchange(exchangeCtx, code, oauth2.SetAuthURLParam("code_verifier", state.PKCEVerifier))
	if err != nil {
		return state, connection, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google OAuth code exchange failed").SetInternal(err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return state, connection, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google did not return an ID token")
	}
	claims, err := c.validateIDToken(ctx, rawIDToken, connection.OAuthClientID)
	if err != nil || claims.Nonce != state.Nonce {
		return state, connection, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google ID token validation failed").SetInternal(err)
	}
	if err := validateGoogleClaims(connection, claims); err != nil {
		return state, connection, nil, echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	return state, connection, claims, nil
}

func (c *GoogleConnectionsController) validateGoogleIDToken(ctx context.Context, raw, audience string) (*googleClaims, error) {
	validator, err := idtoken.NewValidator(ctx, option.WithHTTPClient(c.httpClient))
	if err != nil {
		return nil, err
	}
	payload, err := validator.Validate(ctx, raw, audience)
	if err != nil {
		return nil, err
	}
	return &googleClaims{Subject: payload.Subject, Email: claimString(payload.Claims, "email"), EmailVerified: claimBool(payload.Claims, "email_verified"), Name: claimString(payload.Claims, "name"), Picture: claimString(payload.Claims, "picture"), HostedDomain: strings.ToLower(claimString(payload.Claims, "hd")), Nonce: claimString(payload.Claims, "nonce")}, nil
}

func (c *GoogleConnectionsController) resolveLoginPrincipal(ctx context.Context, connection googleConnection, claims *googleClaims) (githubPrincipal, error) {
	var identity googleIdentity
	_, err := c.principals.loadObject(ctx, googleIdentitySecretName(connection.ID, claims.Subject), &identity)
	if apierrors.IsNotFound(err) {
		if !connection.AllowUserCreation {
			return githubPrincipal{}, errors.New("user creation is disabled for this Google connection")
		}
		principal, createErr := c.principals.getOrCreatePrincipal(ctx, "google-connection:"+connection.ID+":"+claims.Subject)
		if createErr != nil {
			return githubPrincipal{}, createErr
		}
		identity = googleIdentity{ID: uuid.NewString(), PrincipalID: principal.ID, ConnectionID: connection.ID, Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: claims.Name, AvatarURL: claims.Picture, HostedDomain: claims.HostedDomain, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		_, linkErr := c.linkIdentity(ctx, identity)
		return principal, linkErr
	}
	if err != nil {
		return githubPrincipal{}, err
	}
	identity.Email, identity.EmailVerified, identity.Name, identity.AvatarURL, identity.HostedDomain, identity.UpdatedAt = claims.Email, claims.EmailVerified, claims.Name, claims.Picture, claims.HostedDomain, time.Now().UTC()
	if _, err := c.linkIdentity(ctx, identity); err != nil {
		return githubPrincipal{}, err
	}
	principals, err := c.principals.listPrincipals(ctx)
	if err != nil {
		return githubPrincipal{}, err
	}
	for _, principal := range principals {
		if principal.ID == identity.PrincipalID {
			return principal, nil
		}
	}
	return githubPrincipal{}, errors.New("principal for Google identity not found")
}

var errGoogleIdentityConflict = errors.New("google identity belongs to another principal")

func (c *GoogleConnectionsController) linkIdentity(ctx context.Context, identity googleIdentity) (bool, error) {
	name := googleIdentitySecretName(identity.ConnectionID, identity.Subject)
	var existing googleIdentity
	secret, err := c.principals.loadObject(ctx, name, &existing)
	if err == nil {
		if existing.PrincipalID != identity.PrincipalID {
			return false, errGoogleIdentityConflict
		}
		identity.ID, identity.CreatedAt = existing.ID, existing.CreatedAt
		record, marshalErr := json.Marshal(identity)
		if marshalErr != nil {
			return false, marshalErr
		}
		secret.Data["record.json"] = record
		_, err = c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		return false, err
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	record, err := json.Marshal(identity)
	if err != nil {
		return false, err
	}
	_, err = c.client.CoreV1().Secrets(c.namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: map[string]string{googleIdentityLabel: "true", "agentapi.ccplant.io/connection-id": identity.ConnectionID}}, Data: map[string][]byte{"record.json": record}}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return c.linkIdentity(ctx, identity)
	}
	return err == nil, err
}

func (c *GoogleConnectionsController) createState(ctx context.Context, connectionID, mode, principalID, returnTo, callbackURL string) (googleOAuthState, error) {
	state := googleOAuthState{ID: uuid.NewString(), ConnectionID: connectionID, PrincipalID: principalID, ReturnTo: returnTo, CallbackURL: callbackURL, Mode: mode, Nonce: randomURLToken(32), PKCEVerifier: randomURLToken(48), ExpiresAt: time.Now().UTC().Add(googleOAuthStateTTL)}
	err := c.principals.createObject(ctx, googleStateSecretName(state.ID), googleOAuthStateLabel, state, nil)
	return state, err
}

func (c *GoogleConnectionsController) loadState(ctx context.Context, id string) (googleOAuthState, *corev1.Secret, error) {
	var state googleOAuthState
	secret, err := c.principals.loadObject(ctx, googleStateSecretName(id), &state)
	if err != nil || state.ID != id || time.Now().UTC().After(state.ExpiresAt) {
		return state, secret, errors.New("OAuth state is invalid or expired")
	}
	return state, secret, nil
}

func googleAuthorizationURL(connection googleConnection, state googleOAuthState) string {
	hash := sha256.Sum256([]byte(state.PKCEVerifier))
	params := url.Values{"client_id": {connection.OAuthClientID}, "redirect_uri": {state.CallbackURL}, "response_type": {"code"}, "scope": {"openid email profile"}, "state": {state.ID}, "nonce": {state.Nonce}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}}
	if len(connection.HostedDomains) == 1 {
		params.Set("hd", connection.HostedDomains[0])
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()
}

func (c *GoogleConnectionsController) resolveCallbackURL(ctx echo.Context, requested string) (string, error) {
	if c.callbackURL != "" {
		return c.callbackURL, nil
	}
	parsed, err := url.Parse(requested)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "/api/v1/auth/google-connections/callback" && parsed.Path != "/api/proxy/auth/google-connections/callback") {
		return "", errors.New("invalid callback URL")
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || (parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1")) {
		return "", errors.New("callback URL must use HTTPS")
	}
	requestOrigin := ctx.Request().Header.Get("Origin")
	if requestOrigin != "" {
		origin, originErr := url.Parse(requestOrigin)
		if originErr != nil || !strings.EqualFold(origin.Scheme, parsed.Scheme) || !strings.EqualFold(origin.Host, parsed.Host) {
			return "", errors.New("callback URL origin mismatch")
		}
	}
	return parsed.String(), nil
}

func (c *GoogleConnectionsController) redirectResult(ctx echo.Context, returnTo, status, result string) error {
	target, _ := url.Parse(sanitizeReturnTo(returnTo))
	query := target.Query()
	query.Set("google_link", status)
	query.Set("google_result", result)
	target.RawQuery = query.Encode()
	return ctx.Redirect(http.StatusFound, target.String())
}

func (c *GoogleConnectionsController) listConnections(ctx context.Context) ([]googleConnection, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: googleConnectionLabel + "=true"})
	if err != nil {
		return nil, err
	}
	result := make([]googleConnection, 0, len(secrets.Items))
	for i := range secrets.Items {
		var item googleConnection
		if json.Unmarshal(secrets.Items[i].Data["record.json"], &item) == nil {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (c *GoogleConnectionsController) listIdentities(ctx context.Context) ([]googleIdentity, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: googleIdentityLabel + "=true"})
	if err != nil {
		return nil, err
	}
	result := make([]googleIdentity, 0, len(secrets.Items))
	for i := range secrets.Items {
		var item googleIdentity
		if json.Unmarshal(secrets.Items[i].Data["record.json"], &item) == nil {
			result = append(result, item)
		}
	}
	return result, nil
}

func (c *GoogleConnectionsController) saveConnection(ctx context.Context, connection googleConnection, clientSecret, rv string) error {
	record, err := json.Marshal(connection)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: googleConnectionSecretName(connection.ID), Namespace: c.namespace, ResourceVersion: rv, Labels: map[string]string{googleConnectionLabel: "true"}}, Data: map[string][]byte{"record.json": record}}
	if connection.SecretSource == "encrypted" && clientSecret != "" {
		secret.Data["client_secret"] = []byte(clientSecret)
	}
	if rv == "" {
		_, err = c.client.CoreV1().Secrets(c.namespace).Create(ctx, secret, metav1.CreateOptions{})
	} else {
		_, err = c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	}
	return err
}

func (c *GoogleConnectionsController) loadConnection(ctx context.Context, id string) (googleConnection, string, string, error) {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, googleConnectionSecretName(id), metav1.GetOptions{})
	if err != nil {
		return googleConnection{}, "", "", err
	}
	var connection googleConnection
	if err := json.Unmarshal(secret.Data["record.json"], &connection); err != nil {
		return googleConnection{}, "", "", err
	}
	return connection, string(secret.Data["client_secret"]), secret.ResourceVersion, nil
}

func (c *GoogleConnectionsController) resolveClientSecret(ctx context.Context, connection googleConnection) (string, error) {
	switch connection.SecretSource {
	case "encrypted":
		_, secret, _, err := c.loadConnection(ctx, connection.ID)
		if err != nil || secret == "" {
			return "", errors.New("encrypted client secret is not configured")
		}
		return secret, nil
	case "environment":
		value := os.Getenv(connection.SecretEnvironment)
		if value == "" {
			return "", errors.New("client secret environment variable is empty")
		}
		return value, nil
	default:
		return "", errors.New("client secret is not configured")
	}
}

func (c *GoogleConnectionsController) secretConfigured(ctx context.Context, connection googleConnection) bool {
	_, err := c.resolveClientSecret(ctx, connection)
	return err == nil
}
func (c *GoogleConnectionsController) identityCount(ctx context.Context, id string) int {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return 0
	}
	count := 0
	for _, identity := range identities {
		if identity.ConnectionID == id {
			count++
		}
	}
	return count
}
func (c *GoogleConnectionsController) connectionResponse(ctx context.Context, connection googleConnection, configured bool) googleConnectionResponse {
	return googleConnectionResponse{googleConnection: connection, SecretConfigured: configured, CallbackURL: c.callbackURL, LinkedIdentities: c.identityCount(ctx, connection.ID)}
}

func validateGoogleSecret(source, value, environment string) error {
	switch source {
	case "encrypted":
		if strings.TrimSpace(value) == "" {
			return errors.New("encrypted client secret value is required")
		}
	case "environment":
		if !googleSecretEnvPattern.MatchString(strings.TrimSpace(environment)) {
			return errors.New("invalid Google client secret environment variable")
		}
	default:
		return errors.New("oauth_client_secret.source must be encrypted or environment")
	}
	return nil
}
func validateGoogleClaims(connection googleConnection, claims *googleClaims) error {
	if claims.Subject == "" || claims.Email == "" || !claims.EmailVerified {
		return errors.New("google account must have a verified email")
	}
	if len(connection.HostedDomains) > 0 && !containsStringFold(connection.HostedDomains, claims.HostedDomain) {
		return errors.New("google hosted domain is not allowed")
	}
	parts := strings.Split(strings.ToLower(claims.Email), "@")
	if len(connection.EmailDomains) > 0 && (len(parts) != 2 || !containsStringFold(connection.EmailDomains, parts[1])) {
		return errors.New("google email domain is not allowed")
	}
	return nil
}
func normalizeDomains(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	sort.Strings(result)
	return result
}
func containsStringFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
func claimString(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return value
}
func claimBool(claims map[string]any, key string) bool { value, _ := claims[key].(bool); return value }
func randomURLToken(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return uuid.NewString()
	}
	return base64.RawURLEncoding.EncodeToString(value)
}
func googleConnectionSecretName(id string) string { return "agentapi-google-connection-" + id }
func googleStateSecretName(id string) string      { return "agentapi-google-oauth-state-" + id }
func googleIdentitySecretName(connectionID, subject string) string {
	sum := sha256.Sum256([]byte(connectionID + "\x00" + subject))
	return "agentapi-google-identity-" + hex.EncodeToString(sum[:])
}
