package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *GitHubConnectionsController) mintGitHubAppToken(ctx context.Context, connection githubConnection, repository string) (string, time.Time, error) {
	repository = strings.TrimSpace(strings.TrimSuffix(repository, ".git"))
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", time.Time{}, errors.New("repository must use owner/name format")
	}
	if connection.GitHubApp == nil || connection.GitHubApp.AppID <= 0 {
		return "", time.Time{}, errors.New("GitHub App is not configured for the selected connection")
	}
	cacheKey := connection.ID + "\x00" + strings.ToLower(repository)
	c.brokerMu.Lock()
	if cached, ok := c.tokenCache[cacheKey]; ok && time.Until(cached.ExpiresAt) > 5*time.Minute {
		c.brokerMu.Unlock()
		return cached.Token, cached.ExpiresAt, nil
	}
	c.brokerMu.Unlock()

	privateKey, err := c.loadGitHubAppPrivateKey(ctx, connection.ID)
	if err != nil {
		return "", time.Time{}, err
	}
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, connection.GitHubApp.AppID, privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create GitHub App transport: %w", err)
	}
	transport.BaseURL = connection.APIURL
	client := github.NewClient(&http.Client{Transport: transport})
	if connection.APIURL != "https://api.github.com" {
		apiURL := strings.TrimSuffix(connection.APIURL, "/") + "/"
		client, err = client.WithEnterpriseURLs(apiURL, apiURL)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("configure GitHub API URL: %w", err)
		}
	}
	installation, findResponse, err := client.Apps.FindRepositoryInstallation(ctx, parts[0], parts[1])
	if err != nil {
		return "", time.Time{}, fmt.Errorf("find GitHub App installation for %s: %w", repository, err)
	}
	if findResponse == nil {
		return "", time.Time{}, errors.New("GitHub returned no installation response")
	}
	result, tokenResponse, err := client.Apps.CreateInstallationToken(ctx, installation.GetID(), &github.InstallationTokenOptions{Repositories: []string{parts[1]}})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create GitHub App installation token for %s: %w", repository, err)
	}
	if tokenResponse == nil {
		return "", time.Time{}, errors.New("GitHub returned no token response")
	}
	if result.GetToken() == "" || result.ExpiresAt == nil {
		return "", time.Time{}, errors.New("GitHub returned an incomplete installation token")
	}
	expiresAt := result.ExpiresAt.Time
	c.brokerMu.Lock()
	c.tokenCache[cacheKey] = githubCachedToken{Token: result.GetToken(), ExpiresAt: expiresAt}
	c.brokerMu.Unlock()
	return result.GetToken(), expiresAt, nil
}

// IssueBrokerLeaseForOrganization selects the existing organization-mapped
// connection and grants one session renewable access to that repository token.
func (c *GitHubConnectionsController) IssueBrokerLeaseForOrganization(ctx context.Context, sessionID, organization, repository string) (string, string, bool, error) {
	return c.IssueBrokerLeaseForRepositories(ctx, sessionID, "", organization, repository, nil, nil)
}

// IssueBrokerLeaseForRepositories grants one session renewable access to the
// working repository and explicitly configured additional repositories.
func (c *GitHubConnectionsController) IssueBrokerLeaseForRepositories(ctx context.Context, sessionID, requestedConnectionID, organization, repository string, additional []string, user *entities.User) (string, string, bool, error) {
	patterns, err := normalizeRepositoryPatterns(additional)
	if err != nil {
		return "", "", true, err
	}
	if strings.TrimSpace(repository) == "" {
		if len(patterns) == 0 {
			return "", "", false, nil
		}
		if requestedConnectionID != "" {
			record, loadErr := c.loadConnectionRecord(ctx, requestedConnectionID)
			connection := record.connection
			if loadErr != nil || !connection.Enabled || connection.GitHubApp == nil || connection.GitHubApp.AppID <= 0 || !c.githubAppPrivateKeyConfigured(ctx, connection.ID) {
				return "", requestedConnectionID, false, nil
			}
		}
		leaseToken := uuid.NewString() + uuid.NewString()
		lease := githubBrokerLease{SessionID: sessionID, ConnectionID: requestedConnectionID, Repositories: prefixedRepositoryPatterns(patterns), ExpiresAt: time.Now().UTC().Add(24 * time.Hour), TokenHash: brokerLeaseTokenHash(leaseToken)}
		if err := c.createObject(ctx, brokerLeaseSecretName(leaseToken), githubBrokerLeaseLabel, lease, map[string]string{"agentapi.ccplant.io/session-id-hash": shortHash(sessionID)}); err != nil {
			return "", "", true, fmt.Errorf("store GitHub broker lease: %w", err)
		}
		return leaseToken, requestedConnectionID, true, nil
	}
	var selected *githubConnection
	if requestedConnectionID != "" {
		record, loadErr := c.loadConnectionRecord(ctx, requestedConnectionID)
		connection := record.connection
		if loadErr != nil || !connection.Enabled {
			return "", requestedConnectionID, false, nil
		}
		selected = &connection
	} else {
		connections, listErr := c.listConnections(ctx)
		if listErr != nil {
			return "", "", false, listErr
		}
		for i := range connections {
			if connections[i].Enabled && containsOrganization(connections[i].Organizations, organization) {
				if selected != nil {
					return "", "", false, errors.New("multiple GitHub connections match the repository organization")
				}
				copy := connections[i]
				selected = &copy
			}
		}
	}
	if selected == nil {
		return "", "", false, nil
	}
	principalID := ""
	if user != nil && requestedConnectionID != "" {
		principal, principalErr := c.loadPrincipalForUser(ctx, user)
		if principalErr != nil {
			return "", selected.ID, true, errors.New("GitHub principal not found")
		}
		if _, tokenErr := c.resolveAccessTokenForPrincipal(ctx, principal.ID, selected.ID); tokenErr != nil {
			return "", selected.ID, true, tokenErr
		}
		principalID = principal.ID
	}
	if principalID == "" && (selected.GitHubApp == nil || selected.GitHubApp.AppID <= 0 || !c.githubAppPrivateKeyConfigured(ctx, selected.ID)) {
		return "", selected.ID, false, nil
	}
	primaryRepositories, err := normalizeRepositoryList([]string{repository})
	if err != nil {
		return "", "", true, err
	}
	primaryRepository := primaryRepositories[0]
	if !containsOrganization(selected.Organizations, repositoryOwner(primaryRepository)) {
		return "", "", true, fmt.Errorf("repository %s does not use the selected GitHub connection", primaryRepository)
	}
	if principalID == "" {
		token, expiresAt, err := c.mintGitHubAppToken(ctx, *selected, primaryRepository)
		if err != nil {
			return "", "", true, err
		}
		if token == "" || expiresAt.IsZero() {
			return "", "", true, errors.New("GitHub returned an incomplete installation token")
		}
	}
	repositories := append([]string{primaryRepository}, prefixedRepositoryPatterns(patterns)...)
	leaseToken := uuid.NewString() + uuid.NewString()
	lease := githubBrokerLease{SessionID: sessionID, ConnectionID: selected.ID, PrincipalID: principalID, Repository: repository, Repositories: repositories, ExpiresAt: time.Now().UTC().Add(24 * time.Hour), TokenHash: brokerLeaseTokenHash(leaseToken)}
	if err := c.createObject(ctx, brokerLeaseSecretName(leaseToken), githubBrokerLeaseLabel, lease, map[string]string{"agentapi.ccplant.io/session-id-hash": shortHash(sessionID)}); err != nil {
		return "", "", true, fmt.Errorf("store GitHub broker lease: %w", err)
	}
	return leaseToken, selected.ID, true, nil
}

// BrokerCredentials returns a current token for the repository fixed in the
// session lease. This route performs its own bearer authentication.
func (c *GitHubConnectionsController) BrokerCredentials(ctx echo.Context) error {
	ctx.Response().Header().Set("Cache-Control", "no-store")
	ctx.Response().Header().Set("Pragma", "no-cache")
	authorization := ctx.Request().Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return echo.NewHTTPError(http.StatusUnauthorized, "broker credential is required")
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	if token == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "broker credential is required")
	}
	var lease githubBrokerLease
	leaseSecret, err := c.loadObject(ctx.Request().Context(), brokerLeaseSecretName(token), &lease)
	if err != nil || lease.TokenHash != brokerLeaseTokenHash(token) || !lease.ExpiresAt.After(time.Now().UTC()) || lease.SessionID != ctx.Param("sessionId") {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired broker credential")
	}
	if time.Until(lease.ExpiresAt) < 12*time.Hour {
		lease.ExpiresAt = time.Now().UTC().Add(24 * time.Hour)
		record, marshalErr := json.Marshal(lease)
		if marshalErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to refresh broker lease").SetInternal(marshalErr)
		}
		leaseSecret.Data["record.json"] = record
		updatedSecret, updateErr := c.client.CoreV1().Secrets(c.namespace).Update(ctx.Request().Context(), leaseSecret, metav1.UpdateOptions{})
		if updateErr != nil && !apierrors.IsConflict(updateErr) {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to refresh broker lease").SetInternal(updateErr)
		}
		if updateErr == nil && updatedSecret == nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to refresh broker lease")
		}
	}
	repository := strings.TrimSpace(ctx.QueryParam("repository"))
	if repository == "" {
		repository = lease.Repository
	}
	normalized, normalizeErr := normalizeRepositoryList([]string{repository})
	if normalizeErr != nil || len(normalized) != 1 || !repositoryAllowedByLease(normalized[0], lease) {
		return echo.NewHTTPError(http.StatusForbidden, "repository is not allowed by this broker credential")
	}
	connection, err := c.connectionForBrokerRepository(ctx.Request().Context(), lease.ConnectionID, normalized[0])
	if err != nil {
		return err
	}
	if !containsOrganization(connection.Organizations, repositoryOwner(normalized[0])) {
		return echo.NewHTTPError(http.StatusForbidden, "repository does not use the broker credential's GitHub connection")
	}
	if lease.PrincipalID != "" {
		personalToken, tokenErr := c.resolveAccessTokenForPrincipal(ctx.Request().Context(), lease.PrincipalID, connection.ID)
		if tokenErr != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to refresh GitHub token").SetInternal(tokenErr)
		}
		return ctx.JSON(http.StatusOK, map[string]any{"username": "x-access-token", "token": personalToken})
	}
	installationToken, expiresAt, err := c.mintGitHubAppToken(ctx.Request().Context(), connection, normalized[0])
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to refresh GitHub token").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"username": "x-access-token", "token": installationToken, "expires_at": expiresAt})
}

func (c *GitHubConnectionsController) connectionForBrokerRepository(ctx context.Context, connectionID, repository string) (githubConnection, error) {
	if connectionID != "" {
		record, err := c.loadConnectionRecord(ctx, connectionID)
		connection := record.connection
		if err != nil || !connection.Enabled {
			return githubConnection{}, echo.NewHTTPError(http.StatusServiceUnavailable, "GitHub connection is unavailable")
		}
		return connection, nil
	}
	connections, err := c.listConnections(ctx)
	if err != nil {
		return githubConnection{}, echo.NewHTTPError(http.StatusServiceUnavailable, "GitHub connections are unavailable").SetInternal(err)
	}
	owner := repositoryOwner(repository)
	var selected *githubConnection
	for i := range connections {
		if !connections[i].Enabled || !containsOrganization(connections[i].Organizations, owner) {
			continue
		}
		if selected != nil {
			return githubConnection{}, echo.NewHTTPError(http.StatusServiceUnavailable, "multiple GitHub connections match the repository organization")
		}
		candidate := connections[i]
		selected = &candidate
	}
	if selected == nil || selected.GitHubApp == nil || selected.GitHubApp.AppID <= 0 || !c.githubAppPrivateKeyConfigured(ctx, selected.ID) {
		return githubConnection{}, echo.NewHTTPError(http.StatusServiceUnavailable, "GitHub connection is unavailable")
	}
	return *selected, nil
}

func repositoryAllowedByLease(repository string, lease githubBrokerLease) bool {
	allowed := lease.Repositories
	if len(allowed) == 0 {
		allowed = []string{lease.Repository}
	}
	for _, candidate := range allowed {
		if isRepositoryRegex(candidate) {
			pattern := strings.TrimPrefix(candidate, repositoryRegexPrefix)
			compiled, err := regexp.Compile("(?i:^(?:" + pattern + ")$)")
			if err == nil && compiled.MatchString(repository) {
				return true
			}
			continue
		}
		if strings.EqualFold(candidate, repository) {
			return true
		}
	}
	return false
}
