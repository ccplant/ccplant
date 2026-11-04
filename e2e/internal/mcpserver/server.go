package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/ccplant/ccplant/e2e/internal/api"
	"github.com/ccplant/ccplant/e2e/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
	TargetAPIURL      string
	TargetFrontendURL string
	AuthType          string
	AuthValue         string
}

type StartInput struct {
	ScenarioID  string         `json:"scenario_id" jsonschema:"ID of the scenario to run"`
	Environment string         `json:"environment,omitempty" jsonschema:"Label stored with the run"`
	Parameters  map[string]any `json:"parameters,omitempty" jsonschema:"Scenario-specific parameter overrides"`
}

type StartOutput struct {
	RunID      string `json:"run_id" jsonschema:"Created run ID"`
	ScenarioID string `json:"scenario_id" jsonschema:"Scenario ID"`
	Status     string `json:"status" jsonschema:"Initial run status"`
}

type ReloadOutput struct {
	Categories int `json:"categories" jsonschema:"Number of loaded categories"`
	Scenarios  int `json:"scenarios" jsonschema:"Number of loaded scenarios"`
}

type service struct {
	api    *api.Server
	config Config
}

func New(apiServer *api.Server, config Config) *mcp.Server {
	if config.AuthType == "" {
		if config.AuthValue == "" {
			config.AuthType = "none"
		} else {
			config.AuthType = "bearer"
		}
	}
	svc := &service{api: apiServer, config: config}
	server := mcp.NewServer(&mcp.Implementation{Name: "ccplant-e2e", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "start_e2e",
		Description: "Start one ccplant E2E scenario using the target and credential configured in the MCP server environment.",
	}, svc.start)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "reload_e2e_scenarios",
		Description: "Reload and validate the E2E scenario catalog. The previous catalog remains active if validation fails.",
	}, svc.reload)
	return server
}

func (s *service) start(ctx context.Context, _ *mcp.CallToolRequest, input StartInput) (*mcp.CallToolResult, StartOutput, error) {
	if strings.TrimSpace(input.ScenarioID) == "" {
		return nil, StartOutput{}, fmt.Errorf("scenario_id is required")
	}
	if s.config.TargetAPIURL == "" {
		return nil, StartOutput{}, fmt.Errorf("E2E_TARGET_API_URL is required")
	}
	run, err := s.api.CreateRun(ctx, api.RunRequest{
		ScenarioID: input.ScenarioID, Environment: input.Environment, Parameters: input.Parameters,
		Target: domain.Target{
			APIURL: s.config.TargetAPIURL, FrontendURL: s.config.TargetFrontendURL,
			AuthType: s.config.AuthType, AuthValue: s.config.AuthValue,
		},
	})
	if err != nil {
		return nil, StartOutput{}, err
	}
	return nil, StartOutput{RunID: run.ID, ScenarioID: run.ScenarioID, Status: run.Status}, nil
}

func (s *service) reload(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ReloadOutput, error) {
	result, err := s.api.ReloadCatalog()
	if err != nil {
		return nil, ReloadOutput{}, err
	}
	return nil, ReloadOutput{Categories: result.Categories, Scenarios: result.Scenarios}, nil
}
