package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const GitHubConnectionCredentialPrefix = "ccp-gh-v1."

// GitHubConnectionCredential is a verified connection-bound GitHub token.
// Resolvers must verify the credential before returning an API URL so callers
// never send the token to an attacker-selected host.
type GitHubConnectionCredential struct {
	ConnectionID string
	APIURL       string
	AccessToken  string
}

// SealGitHubConnectionCredential binds a GitHub access token to the connection
// that issued it. The connection's OAuth client secret is used only as an HMAC
// key; the access token remains opaque to callers of this package.
func SealGitHubConnectionCredential(connectionID, accessToken, clientSecret string) (string, error) {
	if connectionID == "" || accessToken == "" || clientSecret == "" {
		return "", errors.New("GitHub connection credential fields are required")
	}
	connectionPart := base64.RawURLEncoding.EncodeToString([]byte(connectionID))
	tokenPart := base64.RawURLEncoding.EncodeToString([]byte(accessToken))
	payload := connectionPart + "." + tokenPart
	signature := base64.RawURLEncoding.EncodeToString(githubConnectionCredentialMAC(payload, clientSecret))
	return GitHubConnectionCredentialPrefix + payload + "." + signature, nil
}

// GitHubConnectionIDFromCredential extracts the routing identifier without
// trusting it. Call OpenGitHubConnectionCredential with the selected
// connection's secret before making any outbound request.
func GitHubConnectionIDFromCredential(credential string) (string, error) {
	parts, err := splitGitHubConnectionCredential(credential)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(raw) == 0 {
		return "", errors.New("invalid GitHub connection credential")
	}
	return string(raw), nil
}

// OpenGitHubConnectionCredential verifies the connection binding and returns
// the underlying GitHub token. Verification must happen before using the
// connection ID to select an outbound GitHub API endpoint.
func OpenGitHubConnectionCredential(credential, clientSecret string) (string, string, error) {
	parts, err := splitGitHubConnectionCredential(credential)
	if err != nil || clientSecret == "" {
		return "", "", errors.New("invalid GitHub connection credential")
	}
	payload := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, githubConnectionCredentialMAC(payload, clientSecret)) {
		return "", "", errors.New("invalid GitHub connection credential")
	}
	connectionRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(connectionRaw) == 0 {
		return "", "", errors.New("invalid GitHub connection credential")
	}
	tokenRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(tokenRaw) == 0 {
		return "", "", errors.New("invalid GitHub connection credential")
	}
	return string(connectionRaw), string(tokenRaw), nil
}

func IsGitHubConnectionCredential(credential string) bool {
	return strings.HasPrefix(credential, GitHubConnectionCredentialPrefix)
}

func splitGitHubConnectionCredential(credential string) ([]string, error) {
	if !IsGitHubConnectionCredential(credential) {
		return nil, errors.New("not a GitHub connection credential")
	}
	parts := strings.Split(strings.TrimPrefix(credential, GitHubConnectionCredentialPrefix), ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, errors.New("invalid GitHub connection credential")
	}
	return parts, nil
}

func githubConnectionCredentialMAC(payload, clientSecret string) []byte {
	mac := hmac.New(sha256.New, []byte("agentapi-proxy/github-connection-credential/v1\x00"+clientSecret))
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}
