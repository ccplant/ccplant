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
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (c *GitHubConnectionsController) exchangeCode(ctx context.Context, connection githubConnection, clientSecret, code, callbackURL string) (string, *time.Time, error) {
	values := url.Values{"client_id": {connection.OAuthClientID}, "client_secret": {clientSecret}, "code": {code}, "redirect_uri": {callbackURL}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.BaseURL+"/login/oauth/access_token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer utils.SafeCloseResponse(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("GitHub token endpoint returned %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", nil, err
	}
	if payload.AccessToken == "" {
		return "", nil, fmt.Errorf("GitHub token exchange failed: %s", payload.Error)
	}
	if payload.ExpiresIn > 0 {
		expiresAt := time.Now().UTC().Add(time.Duration(payload.ExpiresIn) * time.Second)
		return payload.AccessToken, &expiresAt, nil
	}
	return payload.AccessToken, nil, nil
}

func (c *GitHubConnectionsController) resolveCallbackURL(ctx echo.Context, requested string) (string, error) {
	if c.callbackURL != "" {
		return c.callbackURL, nil
	}
	if requested != "" {
		callback, err := url.Parse(requested)
		origin, originErr := url.Parse(ctx.Request().Header.Get("Origin"))
		allowedPath := callback != nil && (callback.Path == "/auth/github-connections/callback" || callback.Path == "/api/v1/auth/github-connections/callback" || callback.Path == "/api/proxy/auth/github-connections/callback")
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

func (c *GitHubConnectionsController) resolveLoginCallbackURL(ctx echo.Context, requested string) (string, error) {
	callback, err := url.Parse(requested)
	allowedPath := callback != nil && (callback.Path == "/api/v1/auth/github-connections/callback" || callback.Path == "/api/proxy/auth/github-connections/callback")
	if err != nil || callback.Scheme != "https" || callback.Host == "" || !allowedPath || callback.RawQuery != "" || callback.Fragment != "" || !allowedOAuthCallbackOrigin(callback) {
		return "", errors.New("callback URL must use an allowed GitHub login callback origin and path")
	}
	return callback.String(), nil
}

func allowedOAuthCallbackOrigin(callback *url.URL) bool {
	for _, raw := range strings.Split(os.Getenv("OAUTH_ALLOWED_REDIRECT_URIS"), ",") {
		allowed, err := url.Parse(strings.TrimSpace(raw))
		if err == nil && allowed.Scheme == callback.Scheme && allowed.Host == callback.Host {
			return true
		}
	}
	return false
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
	if !utils.IsPublicHTTPSURL(parsed.String()) {
		return "", errors.New("URL must use a public HTTPS destination")
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
