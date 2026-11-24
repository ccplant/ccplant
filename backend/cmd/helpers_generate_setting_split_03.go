package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"gopkg.in/yaml.v3"
)

func setBedrockField(env map[string]string, envKey, value, fieldName string) {
	if value != "" {
		log.Printf("[GENERATE-SETTING]       %s → %s=%q", fieldName, envKey, maskSecret(envKey, value))
		env[envKey] = value
	} else {
		log.Printf("[GENERATE-SETTING]       %s が空 → %s をスキップ (既存値を保持)", fieldName, envKey)
	}
}

// maskSecret masks sensitive values in log output.
func maskSecret(envKey, value string) string {
	sensitiveKeys := []string{
		"AWS_SECRET_ACCESS_KEY",
		"AWS_ACCESS_KEY_ID",
		"CLAUDE_CODE_OAUTH_TOKEN",
	}
	for _, k := range sensitiveKeys {
		if envKey == k && len(value) > 4 {
			return value[:4] + strings.Repeat("*", len(value)-4)
		}
	}
	return value
}

// ---------------------------------------------------------------------------
// Output helpers
// ---------------------------------------------------------------------------

// outputSessionSettings serializes the SessionSettings and writes it to stdout.
func outputSessionSettings(s *sessionsettings.SessionSettings, format string) error {
	// Marshal to YAML first (the canonical internal format)
	yamlBytes, err := sessionsettings.MarshalYAML(s)
	if err != nil {
		return fmt.Errorf("YAML へのシリアライズに失敗しました: %w", err)
	}

	switch format {
	case "yaml":
		fmt.Print(string(yamlBytes))
		return nil

	case "json":
		// Convert YAML → generic map → JSON for a clean JSON representation
		var generic interface{}
		if err := yaml.Unmarshal(yamlBytes, &generic); err != nil {
			return fmt.Errorf("YAML の解析に失敗しました: %w", err)
		}
		// yaml.Unmarshal returns map[interface{}]interface{} in some cases; normalize.
		normalized := normalizeYAMLValue(generic)
		jsonBytes, err := json.MarshalIndent(normalized, "", "  ")
		if err != nil {
			return fmt.Errorf("JSON へのシリアライズに失敗しました: %w", err)
		}
		fmt.Println(string(jsonBytes))
		return nil

	default:
		return fmt.Errorf("不明な出力形式: %q", format)
	}
}

// normalizeYAMLValue converts map[interface{}]interface{} (produced by yaml.v3 in some cases)
// to map[string]interface{} so that json.Marshal can handle it correctly.
func normalizeYAMLValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			m[fmt.Sprintf("%v", k)] = normalizeYAMLValue(v2)
		}
		return m
	case map[string]interface{}:
		for k, v2 := range val {
			val[k] = normalizeYAMLValue(v2)
		}
		return val
	case []interface{}:
		for i, v2 := range val {
			val[i] = normalizeYAMLValue(v2)
		}
		return val
	default:
		return val
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// readGenerateSettingInput reads input data from a file or stdin.
func readGenerateSettingInput(path string) ([]byte, error) {
	if path == "-" || path == "" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// sortedKeys returns the keys of a string map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
