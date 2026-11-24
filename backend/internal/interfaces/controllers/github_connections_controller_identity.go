package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var errIdentityConflict = errors.New("GitHub identity belongs to another principal")

func (c *GitHubConnectionsController) linkIdentity(ctx context.Context, identity githubIdentity, accessToken string, expiresAt *time.Time) (bool, error) {
	name := identitySecretName(identity.ConnectionID, identity.GitHubUserID)
	var existing githubIdentity
	secret, err := c.loadObject(ctx, name, &existing)
	if err == nil {
		if existing.PrincipalID == identity.PrincipalID {
			secret.Data[githubAccessTokenKey] = []byte(accessToken)
			if expiresAt != nil {
				secret.Data[githubExpiresAtKey] = []byte(expiresAt.UTC().Format(time.RFC3339))
			} else {
				delete(secret.Data, githubExpiresAtKey)
			}
			updated, updateErr := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
			return false, validateSavedGitHubSecret(updated, updateErr)
		}
		return false, errIdentityConflict
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	record, err := json.Marshal(identity)
	if err != nil {
		return false, err
	}
	data := map[string][]byte{"record.json": record, githubAccessTokenKey: []byte(accessToken)}
	if expiresAt != nil {
		data[githubExpiresAtKey] = []byte(expiresAt.UTC().Format(time.RFC3339))
	}
	created, err := c.client.CoreV1().Secrets(c.namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: map[string]string{githubIdentityLabel: "true", "agentapi.ccplant.io/connection-id": identity.ConnectionID}}, Data: data}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return c.linkIdentity(ctx, identity, accessToken, expiresAt)
	}
	if err := validateSavedGitHubSecret(created, err); err != nil {
		return false, err
	}
	return true, nil
}

// ResolveAccessToken returns the selected linked credential for the authenticated user.
func (c *GitHubConnectionsController) ResolveAccessToken(ctx context.Context, user *entities.User, connectionID string) (string, error) {
	principal, err := c.loadPrincipalForUser(ctx, user)
	if err != nil {
		return "", errors.New("GitHub principal not found")
	}
	return c.resolveAccessTokenForPrincipal(ctx, principal.ID, connectionID)
}

func (c *GitHubConnectionsController) resolveAccessTokenForPrincipal(ctx context.Context, principalID, connectionID string) (string, error) {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return "", err
	}
	for _, identity := range identities {
		if identity.PrincipalID != principalID || identity.ConnectionID != connectionID {
			continue
		}
		secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, identitySecretName(identity.ConnectionID, identity.GitHubUserID), metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if raw := string(secret.Data[githubExpiresAtKey]); raw != "" {
			expiresAt, parseErr := time.Parse(time.RFC3339, raw)
			if parseErr != nil || !expiresAt.After(time.Now().UTC()) {
				return "", errors.New("GitHub access token has expired")
			}
		}
		token := string(secret.Data[githubAccessTokenKey])
		if token == "" {
			return "", errors.New("GitHub access token is not stored; reconnect the account")
		}
		return token, nil
	}
	return "", errors.New("GitHub connection is not linked to this user")
}

// IssueGitHubConnectionCredential returns a signed credential which binds the
// OAuth token to its connection. Callers can safely carry this credential as a
// bearer token without allowing its routing identifier to be changed.
func (c *GitHubConnectionsController) IssueGitHubConnectionCredential(ctx context.Context, connectionID, accessToken string) (string, error) {
	record, err := c.loadConnectionRecord(ctx, connectionID)
	connection := record.connection
	if err != nil || !connection.Enabled {
		return "", errors.New("GitHub connection is unavailable")
	}
	clientSecret, err := c.resolveClientSecret(ctx, connection)
	if err != nil {
		return "", errors.New("GitHub connection secret is unavailable")
	}
	return auth.SealGitHubConnectionCredential(connection.ID, accessToken, clientSecret)
}

// ResolveGitHubConnectionCredential verifies a signed connection credential
// before returning the endpoint and raw token. No GitHub request is made until
// this method succeeds.
func (c *GitHubConnectionsController) ResolveGitHubConnectionCredential(ctx context.Context, credential string) (*auth.GitHubConnectionCredential, error) {
	connectionID, err := auth.GitHubConnectionIDFromCredential(credential)
	if err != nil {
		return nil, err
	}
	record, err := c.loadConnectionRecord(ctx, connectionID)
	connection := record.connection
	if err != nil || !connection.Enabled {
		return nil, errors.New("GitHub connection is unavailable")
	}
	clientSecret, err := c.resolveClientSecret(ctx, connection)
	if err != nil {
		return nil, errors.New("GitHub connection secret is unavailable")
	}
	verifiedConnectionID, accessToken, err := auth.OpenGitHubConnectionCredential(credential, clientSecret)
	if err != nil || verifiedConnectionID != connection.ID {
		return nil, errors.New("invalid GitHub connection credential")
	}
	return &auth.GitHubConnectionCredential{ConnectionID: connection.ID, APIURL: connection.APIURL, AccessToken: accessToken}, nil
}

// ResolveApplicationUserForExternalIdentity maps a connection-scoped GitHub
// user to the stable application user and principal used for authorization.
func (c *GitHubConnectionsController) ResolveApplicationUserForExternalIdentity(ctx context.Context, connectionID string, githubUserID int64) (string, string, error) {
	principalID, found, err := c.PrincipalForExternalIdentity(ctx, connectionID, githubUserID)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", errors.New("GitHub connection identity is not linked")
	}
	principals, err := c.listPrincipals(ctx)
	if err != nil {
		return "", "", err
	}
	for _, principal := range principals {
		if principal.ID == principalID {
			return applicationUserID(principal), principal.ID, nil
		}
	}
	return "", "", errors.New("principal for GitHub connection identity not found")
}

// PrincipalIDForUser resolves the stable principal used by linked identities.
func (c *GitHubConnectionsController) PrincipalIDForUser(ctx context.Context, user *entities.User) (string, error) {
	principal, err := c.loadPrincipalForUser(ctx, user)
	if err != nil {
		return "", err
	}
	return principal.ID, nil
}

// PrincipalForExternalIdentity resolves a GitHub identity into its linked user principal.
func (c *GitHubConnectionsController) PrincipalForExternalIdentity(ctx context.Context, connectionID string, githubUserID int64) (string, bool, error) {
	if connectionID == "" {
		identities, err := c.listIdentities(ctx)
		if err != nil {
			return "", false, err
		}
		for _, identity := range identities {
			if identity.GitHubUserID == githubUserID {
				return identity.PrincipalID, true, nil
			}
		}
		// Built-in GitHub OAuth principals predate connection identities. Link
		// only an already-existing github:<id> principal; team synchronization
		// must never create application users for arbitrary GitHub members.
		principal, err := c.loadPrincipal(ctx, fmt.Sprintf("github:%d", githubUserID))
		if err == nil {
			return principal.ID, true, nil
		}
		if !apierrors.IsNotFound(err) {
			return "", false, err
		}
		return "", false, nil
	}
	var identity githubIdentity
	_, err := c.loadObject(ctx, identitySecretName(connectionID, githubUserID), &identity)
	if apierrors.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return identity.PrincipalID, true, nil
}

// FetchExternalTeamMembers loads the complete member list for one exact binding
// using a credential linked to actorPrincipalID.
