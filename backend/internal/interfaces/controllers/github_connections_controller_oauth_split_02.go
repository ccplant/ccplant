package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *GitHubConnectionsController) RepairPrincipal(ctx echo.Context) error {
	principalID, request, err := bindPrincipalRepairRequest(ctx)
	if err != nil {
		return err
	}
	principal, err := c.findRepairPrincipal(ctx.Request().Context(), principalID)
	if err != nil {
		return err
	}
	if err := c.validateRepairIdentities(ctx.Request().Context(), principalID, request.Identities); err != nil {
		return err
	}
	if request.ClearApplicationUserID && principal.ApplicationUserID != "" {
		if err := c.clearPrincipalApplicationUserID(ctx.Request().Context(), principal); err != nil {
			return err
		}
	}
	for _, identity := range request.Identities {
		if err := c.upsertRepairIdentity(ctx.Request().Context(), principalID, identity); err != nil {
			return err
		}
	}
	linked, err := c.linkedIdentities(ctx.Request().Context(), principalID)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, principalRepairResponse{Principal: *principal, Identities: linked})
}

func bindPrincipalRepairRequest(ctx echo.Context) (string, principalRepairRequest, error) {
	principalID := strings.TrimSpace(ctx.Param("principalId"))
	if principalID == "" {
		return "", principalRepairRequest{}, echo.NewHTTPError(http.StatusBadRequest, "principal_id is required")
	}
	var request principalRepairRequest
	if err := ctx.Bind(&request); err != nil {
		return "", request, echo.NewHTTPError(http.StatusBadRequest, "invalid request").SetInternal(err)
	}
	if !request.ClearApplicationUserID && len(request.Identities) == 0 {
		return "", request, echo.NewHTTPError(http.StatusBadRequest, "at least one repair operation is required")
	}
	return principalID, request, nil
}

func (c *GitHubConnectionsController) findRepairPrincipal(ctx context.Context, principalID string) (*githubPrincipal, error) {
	principals, err := c.listPrincipals(ctx)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to list principals").SetInternal(err)
	}
	for i := range principals {
		if principals[i].ID == principalID {
			principal := principals[i]
			return &principal, nil
		}
	}
	return nil, echo.NewHTTPError(http.StatusNotFound, "principal not found")
}

func (c *GitHubConnectionsController) validateRepairIdentities(ctx context.Context, principalID string, identities []principalRepairIdentityRequest) error {
	seen := map[string]struct{}{}
	for _, identity := range identities {
		identity.ConnectionID = strings.TrimSpace(identity.ConnectionID)
		if identity.ConnectionID == "" || identity.GitHubUserID <= 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "connection_id and a positive github_user_id are required")
		}
		key := fmt.Sprintf("%s:%d", identity.ConnectionID, identity.GitHubUserID)
		if _, exists := seen[key]; exists {
			return echo.NewHTTPError(http.StatusBadRequest, "duplicate identity in request")
		}
		seen[key] = struct{}{}
		if err := c.validateRepairIdentity(ctx, principalID, identity); err != nil {
			return err
		}
	}
	return nil
}

func (c *GitHubConnectionsController) validateRepairIdentity(ctx context.Context, principalID string, identity principalRepairIdentityRequest) error {
	connection, connectionSecret, connectionRecord, err := c.loadConnection(ctx, identity.ConnectionID)
	_, _, _ = connection, connectionSecret, connectionRecord
	if err != nil {
		if apierrors.IsNotFound(err) {
			return echo.NewHTTPError(http.StatusBadRequest, "GitHub connection not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub connection").SetInternal(err)
	}
	var existing githubIdentity
	secret, err := c.loadObject(ctx, identitySecretName(identity.ConnectionID, identity.GitHubUserID), &existing)
	_ = secret
	if err == nil && existing.PrincipalID != principalID && !identity.ReassignExisting {
		return echo.NewHTTPError(http.StatusConflict, "GitHub identity belongs to another principal; set reassign_existing to move it")
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to inspect GitHub identity").SetInternal(err)
	}
	return nil
}

func (c *GitHubConnectionsController) clearPrincipalApplicationUserID(ctx context.Context, principal *githubPrincipal) error {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, principalSecretName(principal.InternalUserID), metav1.GetOptions{})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load principal").SetInternal(err)
	}
	principal.ApplicationUserID = ""
	record, err := json.Marshal(principal)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode principal").SetInternal(err)
	}
	secret.Data["record.json"] = record
	if _, err := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update principal").SetInternal(err)
	}
	return nil
}

func (c *GitHubConnectionsController) upsertRepairIdentity(ctx context.Context, principalID string, requested principalRepairIdentityRequest) error {
	requested.ConnectionID = strings.TrimSpace(requested.ConnectionID)
	name := identitySecretName(requested.ConnectionID, requested.GitHubUserID)
	var identity githubIdentity
	secret, err := c.loadObject(ctx, name, &identity)
	now := time.Now().UTC()
	if apierrors.IsNotFound(err) {
		identity = githubIdentity{ID: uuid.NewString(), PrincipalID: principalID, ConnectionID: requested.ConnectionID, GitHubUserID: requested.GitHubUserID, Login: strings.TrimSpace(requested.Login), AvatarURL: strings.TrimSpace(requested.AvatarURL), CreatedAt: now, UpdatedAt: now}
		if err := c.createObject(ctx, name, githubIdentityLabel, identity, nil); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to create GitHub identity").SetInternal(err)
		}
		return nil
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load GitHub identity").SetInternal(err)
	}
	identity.PrincipalID = principalID
	if login := strings.TrimSpace(requested.Login); login != "" {
		identity.Login = login
	}
	if avatarURL := strings.TrimSpace(requested.AvatarURL); avatarURL != "" {
		identity.AvatarURL = avatarURL
	}
	identity.UpdatedAt = now
	record, err := json.Marshal(identity)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode GitHub identity").SetInternal(err)
	}
	secret.Data["record.json"] = record
	if _, err := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update GitHub identity").SetInternal(err)
	}
	return nil
}

func (c *GitHubConnectionsController) linkedIdentities(ctx context.Context, principalID string) ([]githubIdentity, error) {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to list identities").SetInternal(err)
	}
	linked := make([]githubIdentity, 0)
	for _, identity := range identities {
		if identity.PrincipalID == principalID {
			linked = append(linked, identity)
		}
	}
	return linked, nil
}
