package analyzer

import (
	"encoding/json"
	"fmt"
	"os"
)

func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if len(cfg.Modules) == 0 {
		return Config{}, fmt.Errorf("config must define at least one module")
	}
	if cfg.MaxFanOut <= 0 {
		cfg.MaxFanOut = 5
	}
	if cfg.MaxCouplingLoad <= 0 {
		cfg.MaxCouplingLoad = 5
	}
	if cfg.HistoryCommits <= 0 {
		cfg.HistoryCommits = 100
	}
	if cfg.MinModuleChanges <= 0 {
		cfg.MinModuleChanges = 5
	}
	if cfg.MinCochange <= 0 {
		cfg.MinCochange = 3
	}
	if cfg.MinLocalityRatio <= 0 {
		cfg.MinLocalityRatio = 0.8
	}
	if cfg.PolicyMode == "" {
		cfg.PolicyMode = "enforced"
	}
	if cfg.PolicyMode != "enforced" && cfg.PolicyMode != "observational" {
		return Config{}, fmt.Errorf("policyMode must be enforced or observational")
	}
	seen := map[string]bool{}
	for _, m := range cfg.Modules {
		if m.Name == "" || len(m.Paths) == 0 {
			return Config{}, fmt.Errorf("every module needs name and paths")
		}
		if seen[m.Name] {
			return Config{}, fmt.Errorf("duplicate module %q", m.Name)
		}
		seen[m.Name] = true
	}
	for _, m := range cfg.Modules {
		for _, dependency := range m.MayDependOn {
			if !seen[dependency] {
				return Config{}, fmt.Errorf("module %q references unknown dependency %q", m.Name, dependency)
			}
		}
	}
	return cfg, nil
}
