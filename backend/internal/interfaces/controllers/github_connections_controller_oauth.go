package controllers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type githubConnectionRecord struct {
	connection      githubConnection
	clientSecret    string
	resourceVersion string
}

func (c *GitHubConnectionsController) loadConnectionRecord(ctx context.Context, id string) (githubConnectionRecord, error) {
	connection, clientSecret, resourceVersion, err := c.loadConnection(ctx, id)
	return githubConnectionRecord{connection: connection, clientSecret: clientSecret, resourceVersion: resourceVersion}, err
}

func (c *GitHubConnectionsController) StartLogin(ctx echo.Context) error {
	var request struct {
		ConnectionID string `json:"connection_id"`
		CallbackURL  string `json:"callback_url"`
	}
	if err := ctx.Bind(&request); err != nil || request.ConnectionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "connection_id is required")
	}
	record, err := c.loadConnectionRecord(ctx.Request().Context(), request.ConnectionID)
	if err != nil || !record.connection.Enabled || !c.secretConfigured(ctx.Request().Context(), record.connection) {
		return echo.NewHTTPError(http.StatusBadRequest, "GitHub connection is unavailable")
	}
	connection := record.connection
	callbackURL, err := c.resolveLoginCallbackURL(ctx, request.CallbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid callback_url")
	}
	state := githubOAuthState{ID: uuid.NewString(), ConnectionID: connection.ID, CallbackURL: callbackURL, Mode: "login", ExpiresAt: time.Now().UTC().Add(githubOAuthStateTTL)}
	if err := c.createObject(ctx.Request().Context(), stateSecretName(state.ID), githubOAuthStateLabel, state, nil); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create OAuth state").SetInternal(err)
	}
	params := url.Values{"client_id": {connection.OAuthClientID}, "redirect_uri": {callbackURL}, "state": {state.ID}, "scope": {normalizeOAuthScope(connection.OAuthScope)}}
	return ctx.JSON(http.StatusOK, map[string]string{"auth_url": connection.BaseURL + "/login/oauth/authorize?" + params.Encode(), "state": state.ID})
}

func (c *GitHubConnectionsController) CompleteLogin(ctx context.Context, stateID, code string) (*GitHubConnectionLoginResult, error) {
	if stateID == "" || code == "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "code and state are required")
	}
	var state githubOAuthState
	secret, err := c.loadObject(ctx, stateSecretName(stateID), &state)
	if err != nil || state.ID != stateID || state.Mode != "login" || time.Now().UTC().After(state.ExpiresAt) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "OAuth state is invalid or expired")
	}
	if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
		return nil, echo.NewHTTPError(http.StatusConflict, "OAuth state has already been used")
	}
	record, err := c.loadConnectionRecord(ctx, state.ConnectionID)
	if err != nil || !record.connection.Enabled {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub connection is unavailable")
	}
	connection := record.connection
	clientSecret, err := c.resolveClientSecret(ctx, connection)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub connection secret is unavailable")
	}
	token, expiresAt, err := c.exchangeCode(ctx, connection, clientSecret, code, state.CallbackURL)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "OAuth authentication failed").SetInternal(err)
	}
	githubUser, err := c.fetchGitHubUser(ctx, connection, token)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub user lookup failed").SetInternal(err)
	}
	principal, created, err := c.resolveLoginPrincipalWithCreated(ctx, connection, githubUser, token, expiresAt)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub identity could not be resolved").SetInternal(err)
	}
	if created && c.membershipSyncer != nil {
		if syncErr := c.membershipSyncer.AutoSyncForPrincipal(ctx, principal.ID, "identity_created"); syncErr != nil {
			log.Printf("[GITHUB_MEMBERSHIP] Initial membership sync failed for principal %q: %v", principal.ID, syncErr)
		}
	}
	return &GitHubConnectionLoginResult{AccessToken: token, APIURL: connection.APIURL, UserID: principal.ID, ConnectionID: connection.ID}, nil
}

// ResolvePrincipalIDForGitHubUser maps the legacy GitHub OAuth flow onto the
// same stable principal IDs used by connection-based login.
func (c *GitHubConnectionsController) ResolvePrincipalIDForGitHubUser(ctx context.Context, githubUserID int64) (string, error) {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return "", err
	}
	for _, identity := range identities {
		if identity.GitHubUserID != githubUserID {
			continue
		}
		record, err := c.loadConnectionRecord(ctx, identity.ConnectionID)
		if err == nil && c.isBuiltInConnection(record.connection) {
			return identity.PrincipalID, nil
		}
	}
	principal, err := c.getOrCreatePrincipal(ctx, fmt.Sprintf("github:%d", githubUserID))
	if err != nil {
		return "", err
	}
	return principal.ID, nil
}

// ResolveApplicationUserIDForGitHubUser returns the user ID used to own
// personal-scoped resources for a GitHub identity. Connection-backed
// identities keep their stable principal (or an explicitly linked local user),
// while the legacy built-in GitHub flow is bound to the GitHub login so
// resources created before stable principals were introduced remain visible.
func (c *GitHubConnectionsController) ResolveApplicationUserIDForGitHubUser(ctx context.Context, githubUserID int64, login string) (string, error) {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return "", err
	}
	principals, err := c.listPrincipals(ctx)
	if err != nil {
		return "", err
	}
	for _, identity := range identities {
		if identity.GitHubUserID != githubUserID {
			continue
		}
		record, loadErr := c.loadConnectionRecord(ctx, identity.ConnectionID)
		if loadErr != nil || !c.isBuiltInConnection(record.connection) {
			continue
		}
		principal, found := findGitHubPrincipal(principals, identity.PrincipalID)
		if found {
			return applicationUserID(principal), nil
		}
		return "", errors.New("principal for GitHub identity not found")
	}

	principal, err := c.getOrCreatePrincipal(ctx, fmt.Sprintf("github:%d", githubUserID))
	if err != nil {
		return "", err
	}
	if principal.ApplicationUserID == "" && login != "" {
		principal, err = c.bindPrincipalToApplicationUser(ctx, principal.InternalUserID, principal, login)
		if err != nil {
			return "", err
		}
	}
	return applicationUserID(principal), nil
}

func findGitHubPrincipal(principals []githubPrincipal, principalID string) (githubPrincipal, bool) {
	for _, principal := range principals {
		if principal.ID == principalID {
			return principal, true
		}
	}
	return githubPrincipal{}, false
}

func (c *GitHubConnectionsController) OAuthStateMode(ctx context.Context, stateID string) (string, error) {
	var state githubOAuthState
	_, err := c.loadObject(ctx, stateSecretName(stateID), &state)
	if err != nil || state.ID != stateID || time.Now().UTC().After(state.ExpiresAt) {
		return "", errors.New("OAuth state is invalid or expired")
	}
	return state.Mode, nil
}

func (c *GitHubConnectionsController) ListIdentities(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principal, err := c.loadPrincipalForUser(ctx.Request().Context(), user)
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
	credential, credentialFound := auth.GetCredentialContext(ctx)
	if !credentialFound {
		credential = nil
	}
	for _, identity := range identities {
		if identity.PrincipalID != principal.ID {
			continue
		}
		record, loadErr := c.loadConnectionRecord(ctx.Request().Context(), identity.ConnectionID)
		if loadErr == nil {
			result = append(result, githubIdentityResponse{githubIdentity: identity, ConnectionName: record.connection.Name, BaseURL: record.connection.BaseURL, Current: credential != nil && credential.Kind == auth.CredentialKindGitHub && credential.ConnectionID == identity.ConnectionID})
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"principal_id": principal.ID, "identities": result})
}

func (c *GitHubConnectionsController) StartLink(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	if !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "account linking requires the libsql-encrypted or kubernetes KV backend")
	}
	var request struct {
		ConnectionID string `json:"connection_id"`
		ReturnTo     string `json:"return_to"`
		CallbackURL  string `json:"callback_url"`
	}
	if err := ctx.Bind(&request); err != nil || request.ConnectionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "connection_id is required")
	}
	record, err := c.loadConnectionRecord(ctx.Request().Context(), request.ConnectionID)
	if apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusNotFound, "GitHub connection not found")
	}
	if err != nil || !record.connection.Enabled {
		return echo.NewHTTPError(http.StatusBadRequest, "GitHub connection is unavailable")
	}
	connection := record.connection
	principal, err := c.getOrCreatePrincipalForUser(ctx.Request().Context(), user)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve principal").SetInternal(err)
	}
	returnTo := sanitizeReturnTo(request.ReturnTo)
	callbackURL, err := c.resolveCallbackURL(ctx, request.CallbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid callback_url")
	}
	state := githubOAuthState{ID: uuid.NewString(), ConnectionID: connection.ID, PrincipalID: principal.ID, InternalUserID: principal.InternalUserID, ReturnTo: returnTo, CallbackURL: callbackURL, ExpiresAt: time.Now().UTC().Add(githubOAuthStateTTL)}
	if err := c.createObject(ctx.Request().Context(), stateSecretName(state.ID), githubOAuthStateLabel, state, nil); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create OAuth state").SetInternal(err)
	}
	params := url.Values{"client_id": {connection.OAuthClientID}, "redirect_uri": {callbackURL}, "state": {state.ID}, "scope": {normalizeOAuthScope(connection.OAuthScope)}}
	return ctx.JSON(http.StatusOK, map[string]string{"authorization_url": connection.BaseURL + "/login/oauth/authorize?" + params.Encode()})
}

func normalizeOAuthScope(scope string) string {
	if scope = strings.TrimSpace(scope); scope != "" {
		return strings.Join(strings.Fields(scope), " ")
	}
	return "read:user read:org project"
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
	record, err := c.loadConnectionRecord(ctx.Request().Context(), state.ConnectionID)
	if err != nil || !record.connection.Enabled {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "connection_unavailable")
	}
	connection := record.connection
	clientSecret, err := c.resolveClientSecret(ctx.Request().Context(), connection)
	if err != nil {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "secret_unavailable")
	}
	token, expiresAt, err := c.exchangeCode(ctx.Request().Context(), connection, clientSecret, code, state.CallbackURL)
	if err != nil {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "token_exchange_failed")
	}
	githubUser, err := c.fetchGitHubUser(ctx.Request().Context(), connection, token)
	if err != nil {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "user_lookup_failed")
	}
	identity := githubIdentity{ID: uuid.NewString(), PrincipalID: state.PrincipalID, ConnectionID: connection.ID, GitHubUserID: githubUser.ID, Login: githubUser.Login, AvatarURL: githubUser.AvatarURL, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	created, err := c.linkIdentity(ctx.Request().Context(), identity, token, expiresAt)
	if errors.Is(err, errIdentityConflict) {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "identity_linked_to_another_principal")
	}
	if err != nil {
		return c.redirectOAuthResult(ctx, state.ReturnTo, "error", "identity_link_failed")
	}
	return c.finishIdentityLink(ctx, state.ReturnTo, identity, created)
}

func (c *GitHubConnectionsController) finishIdentityLink(ctx echo.Context, returnTo string, identity githubIdentity, created bool) error {
	if !created {
		return c.redirectOAuthResult(ctx, returnTo, "success", "already_linked")
	}
	if c.membershipSyncer != nil {
		if err := c.membershipSyncer.AutoSyncForPrincipal(ctx.Request().Context(), identity.PrincipalID, "identity_linked"); err != nil {
			log.Printf("[GITHUB_MEMBERSHIP] Linked identity sync failed for principal %q: %v", identity.PrincipalID, err)
		}
	}
	return c.redirectOAuthResult(ctx, returnTo, "success", "linked")
}

func (c *GitHubConnectionsController) Unlink(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principal, err := c.loadPrincipalForUser(ctx.Request().Context(), user)
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
		if credential, ok := auth.GetCredentialContext(ctx); ok && credential.Kind == auth.CredentialKindGitHub && credential.ConnectionID == identity.ConnectionID {
			return echo.NewHTTPError(http.StatusConflict, "the connection used by the current login cannot be unlinked")
		}
		if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx.Request().Context(), identitySecretName(identity.ConnectionID, identity.GitHubUserID), metav1.DeleteOptions{}); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to unlink identity").SetInternal(err)
		}
		return ctx.NoContent(http.StatusNoContent)
	}
	return echo.NewHTTPError(http.StatusNotFound, "GitHub identity not found")
}

// DeletePrincipal removes an accidentally created GitHub principal and all of
// its linked identities. Team membership snapshots are derived from external
// GitHub state and may temporarily retain the deleted principal until the next
// synchronization.
func (c *GitHubConnectionsController) DeletePrincipal(ctx echo.Context) error {
	principalID := strings.TrimSpace(ctx.Param("principalId"))
	if principalID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "principal_id is required")
	}

	principals, err := c.listPrincipals(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list principals").SetInternal(err)
	}
	var target *githubPrincipal
	for i := range principals {
		if principals[i].ID == principalID {
			target = &principals[i]
			break
		}
	}
	if target == nil {
		return echo.NewHTTPError(http.StatusNotFound, "principal not found")
	}

	identities, err := c.listIdentities(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list identities").SetInternal(err)
	}
	if err := c.deletePrincipalIdentities(ctx.Request().Context(), principalID, identities); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete principal identity").SetInternal(err)
	}
	if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx.Request().Context(), principalSecretName(target.InternalUserID), metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return echo.NewHTTPError(http.StatusNotFound, "principal not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete principal").SetInternal(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *GitHubConnectionsController) deletePrincipalIdentities(ctx context.Context, principalID string, identities []githubIdentity) error {
	for _, identity := range identities {
		if identity.PrincipalID == principalID {
			err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx, identitySecretName(identity.ConnectionID, identity.GitHubUserID), metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// RepairPrincipal repairs authentication links without changing resource ownership.
// It is intentionally admin-only (enforced by the router) because an identity
// reassignment changes which external account can authenticate as a principal.
