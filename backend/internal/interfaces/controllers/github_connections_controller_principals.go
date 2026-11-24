package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
		return c.getOrCreatePrincipal(ctx, internalUserID)
	}
	return principal, err
}

func (c *GitHubConnectionsController) bindPrincipalToApplicationUser(ctx context.Context, internalSubject string, principal githubPrincipal, userID string) (githubPrincipal, error) {
	if userID == "" || principal.ApplicationUserID == userID {
		return principal, nil
	}
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, principalSecretName(internalSubject), metav1.GetOptions{})
	if err != nil {
		return githubPrincipal{}, err
	}
	principal.ApplicationUserID = userID
	record, err := json.Marshal(principal)
	if err != nil {
		return githubPrincipal{}, err
	}
	secret.Data["record.json"] = record
	if _, err := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return githubPrincipal{}, err
	}
	return principal, nil
}

func (c *GitHubConnectionsController) getOrCreatePrincipalForUser(ctx context.Context, user *entities.User) (githubPrincipal, error) {
	principal, err := c.loadPrincipalForUser(ctx, user)
	if apierrors.IsNotFound(err) {
		principal, err = c.getOrCreatePrincipal(ctx, principalSubject(user))
	}
	if err != nil {
		return githubPrincipal{}, err
	}
	return c.bindPrincipalToApplicationUser(ctx, principal.InternalUserID, principal, string(user.ID()))
}

func applicationUserID(principal githubPrincipal) string {
	if principal.ApplicationUserID != "" {
		return principal.ApplicationUserID
	}
	if strings.HasPrefix(principal.InternalUserID, "internal:") {
		return strings.TrimPrefix(principal.InternalUserID, "internal:")
	}
	return principal.ID
}

func (c *GitHubConnectionsController) listPrincipals(ctx context.Context) ([]githubPrincipal, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: githubPrincipalLabel + "=true"})
	if err != nil {
		return nil, err
	}
	principals := make([]githubPrincipal, 0, len(secrets.Items))
	for i := range secrets.Items {
		var principal githubPrincipal
		if json.Unmarshal(secrets.Items[i].Data["record.json"], &principal) == nil {
			principals = append(principals, principal)
		}
	}
	return principals, nil
}

func (c *GitHubConnectionsController) loadPrincipalForUser(ctx context.Context, user *entities.User) (githubPrincipal, error) {
	internalSubject := principalSubject(user)
	principal, err := c.loadPrincipal(ctx, internalSubject)
	if err == nil {
		// Principals created before application_user_id was introduced are
		// repaired the next time the user authenticates with their original
		// login method. This restores access without requiring an unlink/relink.
		return c.bindPrincipalToApplicationUser(ctx, internalSubject, principal, string(user.ID()))
	}
	if !apierrors.IsNotFound(err) {
		return githubPrincipal{}, err
	}
	principals, err := c.listPrincipals(ctx)
	if err != nil {
		return githubPrincipal{}, err
	}
	for _, candidate := range principals {
		if candidate.ID == string(user.ID()) || candidate.ApplicationUserID == string(user.ID()) {
			return candidate, nil
		}
	}
	return githubPrincipal{}, apierrors.NewNotFound(corev1.Resource("secrets"), principalSecretName(principalSubject(user)))
}

func (c *GitHubConnectionsController) resolveLoginPrincipal(ctx context.Context, connection githubConnection, user githubOAuthUser, token string, expiresAt *time.Time) (githubPrincipal, error) {
	principal, _, err := c.resolveLoginPrincipalWithCreated(ctx, connection, user, token, expiresAt)
	return principal, err
}

func (c *GitHubConnectionsController) resolveLoginPrincipalWithCreated(ctx context.Context, connection githubConnection, user githubOAuthUser, token string, expiresAt *time.Time) (githubPrincipal, bool, error) {
	var identity githubIdentity
	_, err := c.loadObject(ctx, identitySecretName(connection.ID, user.ID), &identity)
	if apierrors.IsNotFound(err) {
		if c.isBuiltInConnection(connection) {
			principal, principalErr := c.loadPrincipal(ctx, fmt.Sprintf("github:%d", user.ID))
			if principalErr == nil {
				identity = githubIdentity{ID: uuid.NewString(), PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: user.ID, Login: user.Login, AvatarURL: user.AvatarURL, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
				_, linkErr := c.linkIdentity(ctx, identity, token, expiresAt)
				return principal, linkErr == nil, linkErr
			}
			if !apierrors.IsNotFound(principalErr) {
				return githubPrincipal{}, false, principalErr
			}
		}
		if !connection.AllowUserCreation {
			return githubPrincipal{}, false, errors.New("user creation is disabled for this GitHub connection")
		}
		principal, createErr := c.getOrCreatePrincipal(ctx, fmt.Sprintf("github-connection:%s:%d", connection.ID, user.ID))
		if createErr != nil {
			return githubPrincipal{}, false, createErr
		}
		identity = githubIdentity{ID: uuid.NewString(), PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: user.ID, Login: user.Login, AvatarURL: user.AvatarURL, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		_, linkErr := c.linkIdentity(ctx, identity, token, expiresAt)
		return principal, linkErr == nil, linkErr
	}
	if err != nil {
		return githubPrincipal{}, false, err
	}
	if _, err := c.linkIdentity(ctx, identity, token, expiresAt); err != nil {
		return githubPrincipal{}, false, err
	}
	principals, err := c.listPrincipals(ctx)
	if err != nil {
		return githubPrincipal{}, false, err
	}
	for _, principal := range principals {
		if principal.ID != identity.PrincipalID {
			continue
		}
		return principal, false, nil
	}
	return githubPrincipal{}, false, errors.New("principal for GitHub identity not found")
}

func (c *GitHubConnectionsController) loadPrincipal(ctx context.Context, internalUserID string) (githubPrincipal, error) {
	var principal githubPrincipal
	_, err := c.loadObject(ctx, principalSecretName(internalUserID), &principal)
	return principal, err
}
