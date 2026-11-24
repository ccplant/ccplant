package startup

import (
	"os"
	"testing"
)

func unsetStartupTestEnv(t *testing.T, key string) {
	t.Helper()
	// Register restoration before removing the variable, including empty values.
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset test environment %s: %v", key, err)
	}
}

func TestUnsetStartupTestEnvRestoresEmptyValue(t *testing.T) {
	const key = "CCPLANT_STARTUP_TEST_EMPTY"
	t.Setenv(key, "")
	t.Run("unset", func(t *testing.T) {
		unsetStartupTestEnv(t, key)
		if _, present := os.LookupEnv(key); present {
			t.Fatal("variable must be absent")
		}
	})
	if value, present := os.LookupEnv(key); !present || value != "" {
		t.Fatal("original empty variable was not restored")
	}
}
