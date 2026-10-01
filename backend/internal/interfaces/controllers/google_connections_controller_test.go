package controllers

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateGoogleClaims(t *testing.T) {
	connection := googleConnection{HostedDomains: []string{"example.com"}, EmailDomains: []string{"example.com"}}
	require.NoError(t, validateGoogleClaims(connection, &googleClaims{Subject: "subject-1", Email: "user@example.com", EmailVerified: true, HostedDomain: "example.com"}))
	require.Error(t, validateGoogleClaims(connection, &googleClaims{Subject: "subject-1", Email: "user@other.test", EmailVerified: true, HostedDomain: "other.test"}))
	require.Error(t, validateGoogleClaims(connection, &googleClaims{Subject: "subject-1", Email: "user@example.com", HostedDomain: "example.com"}))
}

func TestGoogleAuthorizationURLUsesOIDCSecurityParameters(t *testing.T) {
	connection := googleConnection{OAuthClientID: "client-id", HostedDomains: []string{"example.com"}}
	state := googleOAuthState{ID: "state-id", CallbackURL: "https://ccplant.example/api/v1/auth/google-connections/callback", Nonce: "nonce", PKCEVerifier: "verifier"}
	authURL, err := url.Parse(googleAuthorizationURL(connection, state))
	require.NoError(t, err)
	require.Equal(t, "https://accounts.google.com/o/oauth2/v2/auth", authURL.Scheme+"://"+authURL.Host+authURL.Path)
	require.Equal(t, "openid email profile", authURL.Query().Get("scope"))
	require.Equal(t, "nonce", authURL.Query().Get("nonce"))
	require.Equal(t, "S256", authURL.Query().Get("code_challenge_method"))
	require.NotEmpty(t, authURL.Query().Get("code_challenge"))
	require.Equal(t, "example.com", authURL.Query().Get("hd"))
}

func TestGoogleIdentitySecretNameDoesNotExposeSubject(t *testing.T) {
	name := googleIdentitySecretName("connection", "sensitive-subject")
	require.NotContains(t, name, "sensitive-subject")
	require.Equal(t, name, googleIdentitySecretName("connection", "sensitive-subject"))
}
