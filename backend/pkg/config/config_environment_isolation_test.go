package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClearAGENTAPIEnvVarsRestoresPresentEmptyValues(t *testing.T) {
	const key = "AGENTAPI_TEST_PRESENT_EMPTY_VALUE"
	t.Setenv(key, "")
	t.Run("isolated environment", func(t *testing.T) {
		clearAGENTAPIEnvVars(t)
		_, present := os.LookupEnv(key)
		assert.False(t, present)
	})
	value, present := os.LookupEnv(key)
	assert.True(t, present, "an originally present empty variable must not become unset")
	assert.Empty(t, value)
}
