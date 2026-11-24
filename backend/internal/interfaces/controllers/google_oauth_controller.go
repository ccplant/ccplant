package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"k8s.io/client-go/kubernetes"
)

// GoogleOAuthController exposes non-secret scia Google OAuth integration status.
type GoogleOAuthController struct {
	scia       config.SciaConfig
	client     kubernetes.Interface
	namespace  string
	httpClient *http.Client
	apiKeyRepo portrepos.PersonalAPIKeyRepository
}

// NewGoogleOAuthController creates a GoogleOAuthController.
func NewGoogleOAuthController(scia config.SciaConfig, client kubernetes.Interface, namespace string) *GoogleOAuthController {
	return &GoogleOAuthController{
		scia:      scia,
		client:    client,
		namespace: namespace,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// WithPersonalAPIKeyRepository wires the user's personal API key into scia
// dynamic user authorization.
func (c *GoogleOAuthController) WithPersonalAPIKeyRepository(repo portrepos.PersonalAPIKeyRepository) *GoogleOAuthController {
	c.apiKeyRepo = repo
	return c
}

// GetName returns the controller name.
func (c *GoogleOAuthController) GetName() string {
	return "GoogleOAuthController"
}

// GoogleOAuthStatusResponse is returned by GET /integrations/google-oauth/status.
type GoogleOAuthStatusResponse struct {
	Enabled          bool   `json:"enabled"`
	HealthOK         bool   `json:"health_ok"`
	HealthStatus     string `json:"health_status,omitempty"`
	ClientConfigured bool   `json:"client_configured"`
	Connected        bool   `json:"connected"`
	Credential       string `json:"credential,omitempty"`
	UserNamespace    string `json:"user_namespace,omitempty"`
	OAuthStartURL    string `json:"oauth_start_url,omitempty"`
	AuthorizationURL string `json:"authorization_url_endpoint,omitempty"`
	ProxyConfigured  bool   `json:"proxy_configured"`
}

// IntegrationsResponse is returned by GET /integrations.
type IntegrationsResponse struct {
	Enabled      bool                  `json:"enabled"`
	HealthOK     bool                  `json:"health_ok"`
	HealthStatus string                `json:"health_status,omitempty"`
	Integrations []FrontendIntegration `json:"integrations"`
}

// IntegrationAuthorizationURLRequest asks scia to build a provider authorization URL.
type IntegrationAuthorizationURLRequest struct {
	RedirectURI string   `json:"redirect_uri"`
	ScopeIDs    []string `json:"scope_ids"`
	Scope       string   `json:"scope,omitempty"`
	State       string   `json:"state,omitempty"`
}

// IntegrationAuthorizationURLResponse is returned by scia's authorization-url endpoint.
type IntegrationAuthorizationURLResponse struct {
	CredentialID     string `json:"credential_id"`
	AuthorizationURL string `json:"authorization_url"`
	AuthURL          string `json:"auth_url,omitempty"`
	RedirectURI      string `json:"redirect_uri,omitempty"`
	Scope            string `json:"scope,omitempty"`
}

// IntegrationRevokeResponse is returned after disconnecting a scia integration.
type IntegrationRevokeResponse struct {
	Revoked      bool   `json:"revoked"`
	CredentialID string `json:"credential_id,omitempty"`
}

type sciaHTTPError struct {
	StatusCode int
	Message    string
}

func (e sciaHTTPError) Error() string {
	return e.Message
}

// FrontendIntegration mirrors scia's non-secret frontend metadata with proxy status.
type FrontendIntegration struct {
	ID                       string                     `json:"id"`
	Provider                 string                     `json:"provider"`
	Namespace                string                     `json:"namespace,omitempty"`
	CredentialID             string                     `json:"credential_id"`
	Name                     string                     `json:"name"`
	IconURL                  string                     `json:"icon_url,omitempty"`
	Description              string                     `json:"description,omitempty"`
	Released                 bool                       `json:"released"`
	Source                   string                     `json:"source,omitempty"`
	StartURL                 string                     `json:"start_url"`
	AuthorizationURLEndpoint string                     `json:"authorization_url_endpoint,omitempty"`
	Setup                    map[string]string          `json:"setup,omitempty"`
	Scopes                   []FrontendIntegrationScope `json:"scopes"`
	Connected                bool                       `json:"connected"`
}

// FrontendIntegrationScope is one selectable OAuth scope in scia metadata.
type FrontendIntegrationScope struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Desc      string `json:"desc,omitempty"`
	Group     string `json:"group,omitempty"`
	GroupName string `json:"group_name,omitempty"`
	GroupDesc string `json:"group_desc,omitempty"`
	Enabled   bool   `json:"enabled"`
}

// GetStatus returns the user's scia Google OAuth integration status.
func (c *GoogleOAuthController) GetStatus(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := string(user.ID())
	userNamespace := c.userNamespace(userID)
	credential := c.credential(userNamespace)
	resp := GoogleOAuthStatusResponse{
		Enabled:          c.scia.Enabled,
		ClientConfigured: c.scia.Enabled && credential != "",
		Credential:       credential,
		UserNamespace:    userNamespace,
		OAuthStartURL:    c.oauthStartURL(userNamespace, credential),
		AuthorizationURL: c.authorizationURLEndpoint(userNamespace),
		ProxyConfigured:  c.scia.Enabled && c.scia.ProxyURL != "",
	}

	if c.scia.Enabled {
		resp.HealthOK, resp.HealthStatus = c.checkHealth(ctx.Request().Context())
		resp.Connected = c.oauthTokenSecretExists(ctx.Request().Context(), userNamespace, c.credential(userNamespace), "google")
	}

	return ctx.JSON(http.StatusOK, resp)
}

// GetIntegrations returns scia frontend integration metadata for the current user.
func (c *GoogleOAuthController) GetIntegrations(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	resp := IntegrationsResponse{Enabled: c.scia.Enabled}
	if !c.scia.Enabled {
		return ctx.JSON(http.StatusOK, resp)
	}

	resp.HealthOK, resp.HealthStatus = c.checkHealth(ctx.Request().Context())
	integrations, err := c.fetchSciaIntegrations(ctx.Request().Context())
	if err != nil {
		resp.HealthStatus = err.Error()
		return ctx.JSON(http.StatusOK, resp)
	}

	userID := string(user.ID())
	defaultNamespace := c.userNamespace(userID)
	for i := range integrations {
		namespace := integrations[i].Namespace
		if namespace == "" {
			namespace = defaultNamespace
			integrations[i].Namespace = namespace
		}
		integrations[i].StartURL = c.publicURL(c.withDynamicUserQuery(integrations[i].StartURL, namespace))
		integrations[i].AuthorizationURLEndpoint = c.publicURL(integrations[i].AuthorizationURLEndpoint)
		if integrations[i].Setup != nil {
			for key, value := range integrations[i].Setup {
				if strings.HasSuffix(key, "_url") {
					integrations[i].Setup[key] = c.publicURL(value)
				}
			}
		}
		integrations[i].Connected = c.oauthTokenSecretExists(ctx.Request().Context(), namespace, integrations[i].CredentialID, integrations[i].Provider)
	}
	resp.Integrations = integrations

	return ctx.JSON(http.StatusOK, resp)
}

// CreateAuthorizationURL returns a scia-built OAuth authorization URL for one integration.
func (c *GoogleOAuthController) CreateAuthorizationURL(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	if !c.scia.Enabled {
		return echo.NewHTTPError(http.StatusNotFound, "scia integration is disabled")
	}

	var input IntegrationAuthorizationURLRequest
	if err := json.NewDecoder(ctx.Request().Body).Decode(&input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid json request")
	}

	integrationID := ctx.Param("id")
	integration, ok, err := c.integrationForUser(ctx.Request().Context(), string(user.ID()), integrationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, err.Error())
	}
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "integration not found")
	}

	resp, err := c.createSciaAuthorizationURL(ctx.Request().Context(), integration, input, c.userToken(ctx.Request().Context(), string(user.ID())))
	if err != nil {
		var upstream sciaHTTPError
		if errors.As(err, &upstream) {
			return echo.NewHTTPError(upstream.StatusCode, upstream.Message)
		}
		return echo.NewHTTPError(http.StatusBadGateway, err.Error())
	}
	resp.Scope = ""
	return ctx.JSON(http.StatusOK, resp)
}

// RevokeIntegration removes the stored OAuth token for one scia integration.
func (c *GoogleOAuthController) RevokeIntegration(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	if !c.scia.Enabled {
		return echo.NewHTTPError(http.StatusNotFound, "scia integration is disabled")
	}

	integrationID := ctx.Param("id")
	integration, ok, err := c.integrationForUser(ctx.Request().Context(), string(user.ID()), integrationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, err.Error())
	}
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "integration not found")
	}

	resp, err := c.revokeSciaIntegration(ctx.Request().Context(), integration, c.userToken(ctx.Request().Context(), string(user.ID())))
	if err != nil {
		var upstream sciaHTTPError
		if errors.As(err, &upstream) {
			return echo.NewHTTPError(upstream.StatusCode, upstream.Message)
		}
		return echo.NewHTTPError(http.StatusBadGateway, err.Error())
	}
	return ctx.JSON(http.StatusOK, resp)
}

func (c *GoogleOAuthController) integrationForUser(ctx context.Context, userID, integrationID string) (FrontendIntegration, bool, error) {
	integrations, err := c.fetchSciaIntegrations(ctx)
	if err != nil {
		return FrontendIntegration{}, false, err
	}
	defaultNamespace := c.userNamespace(userID)
	for _, integration := range integrations {
		if integration.ID != integrationID {
			continue
		}
		if integration.Namespace == "" {
			integration.Namespace = defaultNamespace
		}
		return integration, true, nil
	}
	return FrontendIntegration{}, false, nil
}

func (c *GoogleOAuthController) createSciaAuthorizationURL(ctx context.Context, integration FrontendIntegration, input IntegrationAuthorizationURLRequest, userToken string) (IntegrationAuthorizationURLResponse, error) {
	base := strings.TrimRight(c.scia.OAuthInternalURL, "/")
	if base == "" {
		base = strings.TrimRight(c.scia.PublicBaseURL, "/")
	}
	if base == "" {
		return IntegrationAuthorizationURLResponse{}, fmt.Errorf("scia_oauth_url_not_configured")
	}
	if integration.Namespace == "" || integration.Provider == "" {
		return IntegrationAuthorizationURLResponse{}, fmt.Errorf("integration is missing namespace or provider")
	}
	if integration.AuthorizationURLEndpoint == "" {
		return c.createSciaStartAuthorizationURL(ctx, integration, input, userToken)
	}

	path := fmt.Sprintf("/oauth/%s/%s/authorization-url", url.PathEscape(integration.Namespace), url.PathEscape(integration.Provider))
	payload, err := json.Marshal(input.sciaRequest())
	if err != nil {
		return IntegrationAuthorizationURLResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return IntegrationAuthorizationURLResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if userToken != "" {
		req.Header.Set("X-S"+"cia-User-Token", userToken)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return IntegrationAuthorizationURLResponse{}, err
	}
	defer func() {
		closeutils.SafeCloseResponseBody(res.Body)
	}()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return IntegrationAuthorizationURLResponse{}, sciaHTTPError{
			StatusCode: res.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}
	var output IntegrationAuthorizationURLResponse
	if err := json.NewDecoder(res.Body).Decode(&output); err != nil {
		return IntegrationAuthorizationURLResponse{}, fmt.Errorf("failed to decode scia authorization-url: %w", err)
	}
	return output, nil
}

func (c *GoogleOAuthController) createSciaStartAuthorizationURL(ctx context.Context, integration FrontendIntegration, input IntegrationAuthorizationURLRequest, userToken string) (IntegrationAuthorizationURLResponse, error) {
	base := strings.TrimRight(c.scia.OAuthInternalURL, "/")
	if base == "" {
		base = strings.TrimRight(c.scia.PublicBaseURL, "/")
	}
	if base == "" {
		return IntegrationAuthorizationURLResponse{}, fmt.Errorf("scia_oauth_url_not_configured")
	}
	values := url.Values{}
	values.Set("credential", integration.CredentialID)
	values.Set("user", integration.Namespace)
	if input.RedirectURI != "" {
		values.Set("redirect_uri", input.RedirectURI)
	}
	if input.State != "" {
		values.Set("state", input.State)
	}
	if scope := input.requestedScope(); scope != "" {
		values.Set("scope", scope)
	}
	endpoint := fmt.Sprintf("%s/oauth/%s/start?%s", base, url.PathEscape(integration.Provider), values.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return IntegrationAuthorizationURLResponse{}, err
	}
	if userToken != "" {
		req.Header.Set("X-S"+"cia-User-Token", userToken)
	}
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	res, err := client.Do(req)
	if err != nil {
		return IntegrationAuthorizationURLResponse{}, err
	}
	defer func() {
		closeutils.SafeCloseResponseBody(res.Body)
	}()
	if res.StatusCode != http.StatusFound && res.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return IntegrationAuthorizationURLResponse{}, sciaHTTPError{
			StatusCode: res.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}
	location := res.Header.Get("Location")
	if location == "" {
		return IntegrationAuthorizationURLResponse{}, fmt.Errorf("scia start response did not include Location")
	}
	return IntegrationAuthorizationURLResponse{
		CredentialID:     integration.CredentialID,
		AuthorizationURL: location,
		RedirectURI:      input.RedirectURI,
	}, nil
}
