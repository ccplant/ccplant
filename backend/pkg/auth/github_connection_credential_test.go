package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitHubConnectionCredentialRoundTrip(t *testing.T) {
	credential, err := SealGitHubConnectionCredential("ghec", "github-token.with:punctuation", "client-secret")
	require.NoError(t, err)
	require.True(t, IsGitHubConnectionCredential(credential))
	require.NotContains(t, credential, "github-token")

	connectionID, err := GitHubConnectionIDFromCredential(credential)
	require.NoError(t, err)
	require.Equal(t, "ghec", connectionID)

	connectionID, token, err := OpenGitHubConnectionCredential(credential, "client-secret")
	require.NoError(t, err)
	require.Equal(t, "ghec", connectionID)
	require.Equal(t, "github-token.with:punctuation", token)
}

func TestGitHubConnectionCredentialRejectsTampering(t *testing.T) {
	credential, err := SealGitHubConnectionCredential("ghec", "github-token", "client-secret")
	require.NoError(t, err)

	_, _, err = OpenGitHubConnectionCredential(credential, "different-secret")
	require.Error(t, err)

	tampered := strings.Replace(credential, "Z2hlYw", "Z2hlcw", 1)
	_, _, err = OpenGitHubConnectionCredential(tampered, "client-secret")
	require.Error(t, err)
}
