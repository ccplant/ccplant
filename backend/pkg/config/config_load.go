package config

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
)

// LoadConfig loads configuration using viper with support for JSON, YAML, and environment variables
func LoadConfig(filename string) (*Config, error) {
	v := viper.New()

	// Set up configuration file
	if filename != "" {
		v.SetConfigFile(filename)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.agentapi")
		v.AddConfigPath("/etc/agentapi/")
	}

	// Enable environment variable support
	v.AutomaticEnv()
	v.SetEnvPrefix("AGENTAPI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Explicitly bind environment variables for nested configuration
	if err := bindEnvVars(v); err != nil {
		return nil, err
	}

	// Set defaults
	setDefaults(v)

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		// If no config file is found, use defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		log.Printf("[CONFIG] No config file found, using defaults and environment variables")
	} else {
		log.Printf("[CONFIG] Using config file: %s", v.ConfigFileUsed())
	}

	var config Config
	decodeHook := mapstructure.ComposeDecodeHookFunc(stockInventoryPoolsDecodeHook(), teamDiscoveryDecodeHook(), stringMapJSONDecodeHook())
	if err := v.Unmarshal(&config, viper.DecodeHook(decodeHook)); err != nil {
		return nil, err
	}
	for name, value := range map[string]string{"schedule_session_ttl": config.ScheduleSessionTTL, "webhook_session_ttl": config.WebhookSessionTTL, "interactive_session_ttl": config.InteractiveSessionTTL} {
		if value == "" {
			continue
		}
		if ttl, err := time.ParseDuration(value); err != nil || ttl <= 0 {
			return nil, fmt.Errorf("invalid %s %q: must be a positive duration", name, value)
		}
	}

	// Apply defaults for any fields that weren't set in config file
	applyConfigDefaults(&config)

	// Initialize config structs from environment variables if they don't exist
	initializeConfigStructsFromEnv(&config, v)

	// Load external auth configuration if specified
	if config.AuthConfigFile != "" {
		if err := loadAuthConfigFromFile(&config, config.AuthConfigFile); err != nil {
			log.Printf("[CONFIG] Warning: Failed to load auth config from %s: %v", config.AuthConfigFile, err)
		} else {
			log.Printf("[CONFIG] Loaded auth config from: %s", config.AuthConfigFile)
		}
	}

	// Apply post-processing
	if err := postProcessConfig(&config); err != nil {
		return nil, err
	}

	// Debug: Log configuration summary
	log.Printf("[CONFIG] Admin key authentication configured: %v", config.Auth.AdminKey != "")
	log.Printf("[CONFIG] GitHub auth enabled: %v", config.Auth.GitHub != nil && config.Auth.GitHub.Enabled)
	if config.Auth.GitHub != nil {
		log.Printf("[CONFIG] GitHub OAuth configured: %v", config.Auth.GitHub.OAuth != nil)
	}
	log.Printf("[CONFIG] Role-based env files enabled: %v", config.RoleEnvFiles.Enabled)

	return &config, nil
}

func stringMapJSONDecodeHook() mapstructure.DecodeHookFunc {
	stringMapType := reflect.TypeOf(map[string]string{})
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		if from.Kind() != reflect.String || to != stringMapType {
			return data, nil
		}
		if data == "" {
			return map[string]string{}, nil
		}
		var values map[string]string
		if err := json.Unmarshal([]byte(data.(string)), &values); err != nil {
			return nil, err
		}
		return values, nil
	}
}

func stockInventoryPoolsDecodeHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		if from.Kind() != reflect.String || to != reflect.TypeOf([]StockInventoryPoolConfig{}) {
			return data, nil
		}
		if data == "" {
			return []StockInventoryPoolConfig{}, nil
		}
		pools, err := parseStockInventoryPoolsJSON(data.(string))
		if err != nil {
			return nil, err
		}
		return pools, nil
	}
}

func teamDiscoveryDecodeHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		if from.Kind() != reflect.String || to != reflect.TypeOf([]TeamDiscoveryRule{}) {
			return data, nil
		}
		if data == "" {
			return []TeamDiscoveryRule{}, nil
		}
		var rules []TeamDiscoveryRule
		if err := json.Unmarshal([]byte(data.(string)), &rules); err != nil {
			return nil, fmt.Errorf("parse team discovery JSON: %w", err)
		}
		return rules, nil
	}
}

func parseStockInventoryPoolsJSON(poolsJSON string) ([]StockInventoryPoolConfig, error) {
	var rawPools []map[string]interface{}
	if err := json.Unmarshal([]byte(poolsJSON), &rawPools); err != nil {
		return nil, err
	}

	pools := make([]StockInventoryPoolConfig, 0, len(rawPools))
	for _, rawPool := range rawPools {
		name, _ := rawPool["name"].(string)
		targetCount, err := jsonInt(rawPool, "target_count", "targetCount")
		if err != nil {
			return nil, err
		}
		dockerEnabled, err := jsonBool(rawPool, "docker_enabled", "dockerEnabled")
		if err != nil {
			return nil, err
		}

		pools = append(pools, StockInventoryPoolConfig{
			Name:          name,
			TargetCount:   targetCount,
			DockerEnabled: dockerEnabled,
		})
	}
	return pools, nil
}

func jsonInt(values map[string]interface{}, keys ...string) (int, error) {
	value, ok := jsonValue(values, keys...)
	if !ok {
		return 0, nil
	}
	switch typedValue := value.(type) {
	case float64:
		return int(typedValue), nil
	case string:
		return strconv.Atoi(typedValue)
	default:
		return 0, fmt.Errorf("expected integer for %s, got %T", keys[0], value)
	}
}

func jsonBool(values map[string]interface{}, keys ...string) (bool, error) {
	value, ok := jsonValue(values, keys...)
	if !ok {
		return false, nil
	}
	switch typedValue := value.(type) {
	case bool:
		return typedValue, nil
	case string:
		return strconv.ParseBool(typedValue)
	default:
		return false, fmt.Errorf("expected boolean for %s, got %T", keys[0], value)
	}
}

func jsonValue(values map[string]interface{}, keys ...string) (interface{}, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func commaSeparatedList(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// initializeConfigStructsFromEnv initializes config structs from environment variables
