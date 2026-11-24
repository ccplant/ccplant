package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestConfigurationEnvironmentBindings(t *testing.T) {
	for _, binding := range configurationEnvBindings {
		t.Run(binding[0], func(t *testing.T) {
			v := viper.New()
			v.SetEnvPrefix("AGENTAPI")
			v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
			if err := bindEnvVars(v); err != nil {
				t.Fatal(err)
			}
			envName := "AGENTAPI_" + strings.ToUpper(strings.ReplaceAll(binding[0], ".", "_"))
			if len(binding) > 1 {
				envName = binding[1]
			}
			t.Setenv(envName, "test-binding-value")
			if got := v.GetString(binding[0]); got != "test-binding-value" {
				t.Fatalf("%s resolves to %q", envName, got)
			}
		})
	}
}

func TestConfigurationEnvironmentAliasPriority(t *testing.T) {
	v := viper.New()
	if err := bindEnvVars(v); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL", "fallback")
	if got := v.GetString("worker.control_api_url"); got != "fallback" {
		t.Fatalf("legacy alias = %q, want fallback", got)
	}
	t.Setenv("AGENTAPI_WORKER_CONTROL_API_URL", "preferred")
	if got := v.GetString("worker.control_api_url"); got != "preferred" {
		t.Fatalf("preferred alias = %q, want preferred", got)
	}
}
