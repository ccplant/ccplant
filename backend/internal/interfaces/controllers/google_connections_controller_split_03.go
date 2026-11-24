package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type googleConnectionRecord struct {
	connection      googleConnection
	clientSecret    string
	resourceVersion string
}

func (c *GoogleConnectionsController) loadConnectionRecord(ctx context.Context, id string) (googleConnectionRecord, error) {
	connection, clientSecret, resourceVersion, err := c.loadConnection(ctx, id)
	return googleConnectionRecord{connection: connection, clientSecret: clientSecret, resourceVersion: resourceVersion}, err
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
