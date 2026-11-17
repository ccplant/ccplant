package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/client"
	"gopkg.in/yaml.v3"
)

const cliTokenHelperTimeout = 30 * time.Second

type cliConfig struct {
	APIEndpoint    string         `yaml:"api_endpoint"`
	APIToken       string         `yaml:"api_token"`
	APITokenHelper cliTokenHelper `yaml:"api_token_helper"`
}

type cliTokenHelper struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

func loadCLIConfig() (*cliConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory for ccplant config: %w", err)
	}
	path := filepath.Join(home, ".ccplant", "config.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &cliConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read ccplant config %q: %w", path, err)
	}
	var config cliConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse ccplant config %q: %w", path, err)
	}
	return &config, nil
}

func resolveCLIEndpoint(flagValue string, config *cliConfig) (string, error) {
	if value := strings.TrimSpace(flagValue); value != "" {
		return value, nil
	}
	if endpointEnvironmentConfigured() {
		return client.EndpointFromEnv()
	}
	if value := strings.TrimSpace(config.APIEndpoint); value != "" {
		return value, nil
	}
	_, err := client.EndpointFromEnv()
	return "", err
}

func endpointEnvironmentConfigured() bool {
	return os.Getenv("AGENTAPI_PROXY_ENDPOINT") != "" ||
		os.Getenv("AGENTAPI_PROXY_SERVICE_HOST") != "" ||
		os.Getenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP") != ""
}

func resolveCLIToken(ctx context.Context, flagValue string, config *cliConfig) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if value := os.Getenv("AGENTAPI_KEY"); value != "" {
		return value, nil
	}
	if config.APIToken != "" {
		return config.APIToken, nil
	}
	helper := config.APITokenHelper
	if helper.Command == "" {
		if len(helper.Args) != 0 {
			return "", errors.New("api_token_helper.command is required when args are configured")
		}
		return "", nil
	}
	helperCtx, cancel := context.WithTimeout(ctx, cliTokenHelperTimeout)
	defer cancel()
	output, err := exec.CommandContext(helperCtx, helper.Command, helper.Args...).Output()
	if err != nil {
		if errors.Is(helperCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("API token helper %q timed out", helper.Command)
		}
		return "", fmt.Errorf("API token helper %q failed: %w", helper.Command, err)
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("API token helper %q returned an empty token", helper.Command)
	}
	return token, nil
}

func resolveCLIConnection(ctx context.Context, endpointFlag, tokenFlag string) (string, string, error) {
	config, err := loadCLIConfig()
	if err != nil {
		return "", "", err
	}
	resolvedEndpoint, err := resolveCLIEndpoint(endpointFlag, config)
	if err != nil {
		return "", "", fmt.Errorf("--endpoint not specified and %w", err)
	}
	token, err := resolveCLIToken(ctx, tokenFlag, config)
	if err != nil {
		return "", "", err
	}
	return resolvedEndpoint, token, nil
}
