package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type sciaAuthorizationURLRequest struct {
	RedirectURI string `json:"redirect_uri,omitempty"`
	Scope       string `json:"scope,omitempty"`
	State       string `json:"state,omitempty"`
}

func (r IntegrationAuthorizationURLRequest) sciaRequest() sciaAuthorizationURLRequest {
	return sciaAuthorizationURLRequest{
		RedirectURI: r.RedirectURI,
		Scope:       r.requestedScope(),
		State:       r.State,
	}
}

func (r IntegrationAuthorizationURLRequest) requestedScope() string {
	if r.Scope != "" {
		return r.Scope
	}
	return strings.Join(r.ScopeIDs, " ")
}

func (c *GoogleOAuthController) revokeSciaIntegration(ctx context.Context, integration FrontendIntegration, userToken string) (IntegrationRevokeResponse, error) {
	base := strings.TrimRight(c.scia.OAuthInternalURL, "/")
	if base == "" {
		base = strings.TrimRight(c.scia.PublicBaseURL, "/")
	}
	if base == "" {
		return IntegrationRevokeResponse{}, fmt.Errorf("scia_oauth_url_not_configured")
	}
	if integration.Namespace == "" || integration.Provider == "" {
		return IntegrationRevokeResponse{}, fmt.Errorf("integration is missing namespace or provider")
	}
	if integration.AuthorizationURLEndpoint == "" {
		return c.revokeLocalOAuthSecret(ctx, integration)
	}

	path := fmt.Sprintf("/oauth/%s/%s/revoke", url.PathEscape(integration.Namespace), url.PathEscape(integration.Provider))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(`{}`))
	if err != nil {
		return IntegrationRevokeResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if userToken != "" {
		req.Header.Set("X-Scia-User-Token", userToken)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return IntegrationRevokeResponse{}, err
	}
	defer func() {
		closeutils.SafeCloseResponseBody(res.Body)
	}()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return IntegrationRevokeResponse{}, sciaHTTPError{
			StatusCode: res.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}
	var output IntegrationRevokeResponse
	if err := json.NewDecoder(res.Body).Decode(&output); err != nil {
		return IntegrationRevokeResponse{}, fmt.Errorf("failed to decode scia revoke response: %w", err)
	}
	if output.CredentialID == "" {
		output.CredentialID = integration.CredentialID
	}
	return output, nil
}

func (c *GoogleOAuthController) revokeLocalOAuthSecret(ctx context.Context, integration FrontendIntegration) (IntegrationRevokeResponse, error) {
	if c.client == nil || c.namespace == "" {
		return IntegrationRevokeResponse{}, fmt.Errorf("kubernetes client is not configured")
	}
	secretName := "scia-oauth-" + sanitizeSciaSecretSuffix(integration.Namespace)
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return IntegrationRevokeResponse{}, err
	}
	delete(secret.Data, integration.CredentialID+".refresh_token")
	delete(secret.Data, integration.CredentialID+".access_token")
	if integration.Provider == "google" {
		delete(secret.Data, "refresh_token")
	}
	if _, err := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return IntegrationRevokeResponse{}, err
	}
	return IntegrationRevokeResponse{Revoked: true, CredentialID: integration.CredentialID}, nil
}

func (c *GoogleOAuthController) fetchSciaIntegrations(ctx context.Context) ([]FrontendIntegration, error) {
	base := strings.TrimRight(c.scia.OAuthInternalURL, "/")
	if base == "" {
		base = strings.TrimRight(c.scia.PublicBaseURL, "/")
	}
	if base == "" {
		return nil, fmt.Errorf("scia_oauth_url_not_configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/integrations", nil)
	if err != nil {
		return nil, fmt.Errorf("invalid_integrations_url")
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeutils.SafeCloseResponseBody(res.Body)
	}()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("scia integrations returned %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	var body struct {
		Integrations []FrontendIntegration `json:"integrations"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to decode scia integrations: %w", err)
	}
	return body.Integrations, nil
}

func (c *GoogleOAuthController) checkHealth(ctx context.Context) (bool, string) {
	base := strings.TrimRight(c.scia.OAuthInternalURL, "/")
	if base == "" {
		base = strings.TrimRight(c.scia.PublicBaseURL, "/")
	}
	if base == "" {
		return false, "scia_oauth_url_not_configured"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/_scia/healthz", nil)
	if err != nil {
		return false, "invalid_health_url"
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer func() {
		closeutils.SafeCloseResponseBody(res.Body)
	}()

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return true, res.Status
	}
	return false, res.Status
}

func (c *GoogleOAuthController) oauthTokenSecretExists(ctx context.Context, userNamespace, credentialID, provider string) bool {
	if c.client == nil || c.namespace == "" || userNamespace == "" {
		return false
	}
	secretName := "scia-oauth-" + sanitizeSciaSecretSuffix(userNamespace)
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return false
	}
	if hasSecretData(secret.Data, credentialID+".refresh_token") || hasSecretData(secret.Data, credentialID+".access_token") {
		return true
	}
	// Older scia deployments stored the original Google refresh token under a
	// shared key. Treat it as Google-only so other providers do not appear connected.
	return provider == "google" && hasSecretData(secret.Data, "refresh_token")
}

func hasSecretData(data map[string][]byte, key string) bool {
	value, ok := data[key]
	return ok && len(value) > 0
}

func (c *GoogleOAuthController) userNamespace(userID string) string {
	if c.scia.UserNamespace != "" {
		return c.scia.UserNamespace
	}
	return sanitizeSciaDynamicUserID(userID)
}

func (c *GoogleOAuthController) credential(userNamespace string) string {
	if c.scia.Credential != "" {
		return c.scia.Credential
	}
	if userNamespace == "" {
		return ""
	}
	return userNamespace + ".google"
}

func (c *GoogleOAuthController) oauthStartURL(userNamespace, credential string) string {
	if credential == "" || userNamespace == "" {
		return ""
	}
	values := url.Values{}
	values.Set("credential", credential)
	values.Set("user", userNamespace)
	return c.withPublicBase("/oauth/google/start") + "?" + values.Encode()
}

func (c *GoogleOAuthController) authorizationURLEndpoint(userNamespace string) string {
	if userNamespace == "" {
		return ""
	}
	return fmt.Sprintf("%s/oauth/%s/google/authorization-url", c.withPublicBase(""), url.PathEscape(userNamespace))
}

func (c *GoogleOAuthController) withPublicBase(path string) string {
	return c.publicURL(path)
}

func (c *GoogleOAuthController) publicURL(path string) string {
	if path == "" {
		return ""
	}
	if parsed, err := url.Parse(path); err == nil && parsed.IsAbs() {
		return path
	}
	base := strings.TrimRight(c.scia.PublicBaseURL, "/")
	if base == "" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (c *GoogleOAuthController) withDynamicUserQuery(rawURL, userNamespace string) string {
	if rawURL == "" || userNamespace == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	values := parsed.Query()
	if values.Get("user") == "" && strings.HasPrefix(parsed.Path, "/oauth/") && strings.Count(strings.Trim(parsed.Path, "/"), "/") == 2 {
		values.Set("user", userNamespace)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

var sciaSecretInvalidChars = regexp.MustCompile(`[^a-z0-9.-]+`)
var sciaDynamicUserInvalidChars = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeSciaSecretSuffix(value string) string {
	value = strings.ToLower(value)
	value = sciaSecretInvalidChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if value == "" {
		return "default"
	}
	return value
}

func sanitizeSciaDynamicUserID(value string) string {
	value = strings.ToLower(value)
	value = sciaDynamicUserInvalidChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "default"
	}
	if len(value) > 63 {
		value = strings.Trim(value[:63], "-")
	}
	if value == "" {
		return "default"
	}
	return value
}

func (c *GoogleOAuthController) userToken(ctx context.Context, userID string) string {
	if c.apiKeyRepo == nil || userID == "" {
		return ""
	}
	apiKey, err := c.apiKeyRepo.FindByUserID(ctx, entities.UserID(userID))
	if err != nil || apiKey == nil {
		return ""
	}
	return apiKey.APIKey()
}
