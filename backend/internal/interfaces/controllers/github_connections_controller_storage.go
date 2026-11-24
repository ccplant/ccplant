package controllers

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *GitHubConnectionsController) listConnections(ctx context.Context) ([]githubConnection, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: githubConnectionLabel + "=true"})
	if err != nil {
		return nil, err
	}
	connections := make([]githubConnection, 0, len(secrets.Items))
	for i := range secrets.Items {
		var connection githubConnection
		if err := json.Unmarshal(secrets.Items[i].Data["record.json"], &connection); err == nil {
			normalizeConnectionDefaults(&connection)
			connections = append(connections, connection)
		}
	}
	sort.Slice(connections, func(i, j int) bool { return connections[i].Name < connections[j].Name })
	return connections, nil
}

func (c *GitHubConnectionsController) listIdentities(ctx context.Context) ([]githubIdentity, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: githubIdentityLabel + "=true"})
	if err != nil {
		return nil, err
	}
	identities := make([]githubIdentity, 0, len(secrets.Items))
	for i := range secrets.Items {
		var identity githubIdentity
		if err := json.Unmarshal(secrets.Items[i].Data["record.json"], &identity); err == nil {
			identities = append(identities, identity)
		}
	}
	return identities, nil
}

func (c *GitHubConnectionsController) identityCount(ctx context.Context, connectionID string) int {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return 0
	}
	count := 0
	for _, identity := range identities {
		if identity.ConnectionID == connectionID {
			count++
		}
	}
	return count
}

func (c *GitHubConnectionsController) saveConnection(ctx context.Context, connection githubConnection, clientSecret, resourceVersion string) error {
	record, err := json.Marshal(connection)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: connectionSecretName(connection.ID), Namespace: c.namespace, ResourceVersion: resourceVersion, Labels: map[string]string{githubConnectionLabel: "true"}}, Data: map[string][]byte{"record.json": record}}
	if resourceVersion != "" {
		if existing, getErr := c.client.CoreV1().Secrets(c.namespace).Get(ctx, connectionSecretName(connection.ID), metav1.GetOptions{}); getErr == nil {
			if privateKey := existing.Data[githubAppPrivateKeyKey]; len(privateKey) > 0 {
				secret.Data[githubAppPrivateKeyKey] = append([]byte(nil), privateKey...)
			}
		}
	}
	if connection.SecretSource == "encrypted" && clientSecret != "" {
		secret.Data["client_secret"] = []byte(clientSecret)
	}
	if resourceVersion == "" {
		saved, saveErr := c.client.CoreV1().Secrets(c.namespace).Create(ctx, secret, metav1.CreateOptions{})
		err = validateSavedGitHubSecret(saved, saveErr)
	} else {
		saved, saveErr := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		err = validateSavedGitHubSecret(saved, saveErr)
	}
	return err
}

func (c *GitHubConnectionsController) loadConnection(ctx context.Context, id string) (githubConnection, string, string, error) {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, connectionSecretName(id), metav1.GetOptions{})
	if err != nil {
		return githubConnection{}, "", "", err
	}
	var connection githubConnection
	if err := json.Unmarshal(secret.Data["record.json"], &connection); err != nil {
		return githubConnection{}, "", "", err
	}
	normalizeConnectionDefaults(&connection)
	return connection, string(secret.Data["client_secret"]), secret.ResourceVersion, nil
}

func normalizeConnectionDefaults(connection *githubConnection) {
	if connection.ShowOnLogin == nil {
		show := true
		connection.ShowOnLogin = &show
	}
	connection.Organizations = normalizeOrganizations(connection.Organizations)
}

func normalizeOrganizations(organizations []string) []string {
	seen := make(map[string]struct{}, len(organizations))
	result := make([]string, 0, len(organizations))
	for _, organization := range organizations {
		organization = strings.ToLower(strings.TrimSpace(organization))
		if organization == "" {
			continue
		}
		if _, ok := seen[organization]; ok {
			continue
		}
		seen[organization] = struct{}{}
		result = append(result, organization)
	}
	sort.Strings(result)
	return result
}

func containsOrganization(organizations []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, organization := range organizations {
		if strings.EqualFold(organization, target) {
			return true
		}
	}
	return false
}

func (c *GitHubConnectionsController) validateOrganizationAssignments(ctx context.Context, connectionID string, organizations []string) error {
	connections, err := c.listConnections(ctx)
	if err != nil {
		return err
	}
	for _, connection := range connections {
		if connection.ID == connectionID {
			continue
		}
		for _, organization := range organizations {
			if containsOrganization(connection.Organizations, organization) {
				return fmt.Errorf("organization %q is already assigned to connection %q", organization, connection.Name)
			}
		}
	}
	return nil
}

func showConnectionOnLogin(connection githubConnection) bool {
	return connection.ShowOnLogin == nil || *connection.ShowOnLogin
}

func (c *GitHubConnectionsController) createObject(ctx context.Context, name, label string, value any, labels map[string]string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if labels == nil {
		labels = map[string]string{}
	}
	labels[label] = "true"
	created, err := c.client.CoreV1().Secrets(c.namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: labels}, Data: map[string][]byte{"record.json": data}}, metav1.CreateOptions{})
	return validateSavedGitHubSecret(created, err)
}

func (c *GitHubConnectionsController) loadObject(ctx context.Context, name string, target any) (*corev1.Secret, error) {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(secret.Data["record.json"], target); err != nil {
		return nil, err
	}
	return secret, nil
}

func (c *GitHubConnectionsController) resolveClientSecret(ctx context.Context, connection githubConnection) (string, error) {
	if connection.SecretSource == "environment" {
		if !githubSecretEnvPattern.MatchString(connection.SecretEnvironment) {
			return "", errors.New("invalid client secret environment variable")
		}
		value, ok := os.LookupEnv(connection.SecretEnvironment)
		if !ok || value == "" {
			return "", errors.New("client secret environment variable is not configured")
		}
		return value, nil
	}
	record, err := c.loadConnectionRecord(ctx, connection.ID)
	secret := record.clientSecret
	if err != nil || secret == "" {
		return "", errors.New("encrypted client secret is not configured")
	}
	return secret, nil
}

func (c *GitHubConnectionsController) secretConfigured(ctx context.Context, connection githubConnection) bool {
	_, err := c.resolveClientSecret(ctx, connection)
	return err == nil
}

func (c *GitHubConnectionsController) connectionResponse(ctx context.Context, connection githubConnection, configured bool) githubConnectionResponse {
	callbackURL := c.callbackURL
	if callbackURL == "" {
		callbackURL = "/auth/github-connections/callback"
	}
	return githubConnectionResponse{githubConnection: connection, SecretConfigured: configured, GitHubAppPrivateKeyConfigured: c.githubAppPrivateKeyConfigured(ctx, connection.ID), CallbackURL: callbackURL, LinkedIdentities: c.identityCount(ctx, connection.ID)}
}

func validateGitHubApp(app *githubAppConfiguration) error {
	if app != nil && app.AppID <= 0 {
		return errors.New("github_app.app_id must be a positive integer")
	}
	return nil
}

func validateGitHubAppPrivateKey(value []byte) error {
	block, trailing := pem.Decode(value)
	if block == nil {
		return errors.New("github_app private key must be PEM encoded")
	}
	if len(bytes.TrimSpace(trailing)) > 0 {
		return errors.New("github_app private key contains trailing non-PEM data")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		switch key.(type) {
		case *rsa.PrivateKey:
			return nil
		}
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return nil
	}
	return errors.New("github_app private key must contain an RSA private key")
}

func (c *GitHubConnectionsController) githubAppPrivateKeyConfigured(ctx context.Context, connectionID string) bool {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, connectionSecretName(connectionID), metav1.GetOptions{})
	return err == nil && len(secret.Data[githubAppPrivateKeyKey]) > 0
}

func (c *GitHubConnectionsController) saveGitHubAppPrivateKey(ctx context.Context, connection githubConnection, value []byte, resourceVersion string) error {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, connectionSecretName(connection.ID), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if secret.ResourceVersion != resourceVersion {
		return errors.New("GitHub connection changed during private key update")
	}
	if len(value) == 0 {
		delete(secret.Data, githubAppPrivateKeyKey)
	} else {
		secret.Data[githubAppPrivateKeyKey] = append([]byte(nil), value...)
	}
	updated, err := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return validateSavedGitHubSecret(updated, err)
}

func validateSavedGitHubSecret(secret *corev1.Secret, err error) error {
	if err != nil {
		return err
	}
	if secret == nil {
		return errors.New("GitHub connection storage returned no Secret")
	}
	return nil
}

func (c *GitHubConnectionsController) loadGitHubAppPrivateKey(ctx context.Context, connectionID string) ([]byte, error) {
	secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, connectionSecretName(connectionID), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	value := secret.Data[githubAppPrivateKeyKey]
	if len(value) == 0 {
		return nil, errors.New("GitHub App private key is not configured")
	}
	return append([]byte(nil), value...), nil
}

func (c *GitHubConnectionsController) invalidateGitHubAppTokens(connectionID string) {
	c.brokerMu.Lock()
	defer c.brokerMu.Unlock()
	for key := range c.tokenCache {
		if strings.HasPrefix(key, connectionID+"\x00") {
			delete(c.tokenCache, key)
		}
	}
}
