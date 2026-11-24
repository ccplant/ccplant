package config

import (
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strings"
)

// LoadConfigLegacy loads configuration from a JSON file (legacy method)
func LoadConfigLegacy(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close config file: %v", err)
		}
	}()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}

	applyConfigDefaults(&config)

	// Apply post-processing
	if err := postProcessConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// expandEnvVars expands environment variables in the form ${VAR_NAME}
func expandEnvVars(s string) string {
	if s == "" {
		return s
	}

	// Match ${VAR_NAME} pattern
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name (remove ${})
		varName := strings.TrimPrefix(strings.TrimSuffix(match, "}"), "${")

		// Get environment variable value
		if value := os.Getenv(varName); value != "" {
			return value
		}

		// Return original string if environment variable is not set
		log.Printf("[CONFIG] Warning: Environment variable %s is not set", varName)
		return match
	})
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Auth: AuthConfig{
			BootstrapAdmin: &BootstrapAdminAuthConfig{
				Enabled:  false,
				UserID:   "bootstrap-admin",
				Username: "admin",
			},
		},
		StockInventoryWorker: StockInventoryWorkerConfig{
			Enabled:       false,
			CheckInterval: "30s",
			TargetCount:   2,
			DockerEnabled: false,
			LeaseDuration: "9m",
			RenewDeadline: "6m",
			RetryPeriod:   "1m",
		},
	}
}
