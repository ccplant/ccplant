package repositories

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation"
)

func TestCodexAuthAttemptNameIsDNS1123Subdomain(t *testing.T) {
	name := codexAuthAttemptName("cda-0123456789abcdef")

	require.Equal(t, "agentapi-codex-auth-cda-0123456789abcdef", name)
	require.Empty(t, validation.IsDNS1123Subdomain(name))
}
