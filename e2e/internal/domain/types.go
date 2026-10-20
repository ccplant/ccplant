package domain

import "time"

type Category struct {
	ID          string `json:"id" yaml:"id"`
	Description string `json:"description" yaml:"description"`
}

type Scenario struct {
	ID           string         `json:"id" yaml:"id"`
	Category     string         `json:"category" yaml:"category"`
	Description  string         `json:"description" yaml:"description"`
	Kind         string         `json:"kind" yaml:"kind"`
	Package      string         `json:"package,omitempty" yaml:"package,omitempty"`
	Test         string         `json:"test,omitempty" yaml:"test,omitempty"`
	Tag          string         `json:"tag,omitempty" yaml:"tag,omitempty"`
	Timeout      string         `json:"timeout" yaml:"timeout"`
	Capabilities []string       `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

type Catalog struct {
	Categories []Category `json:"categories" yaml:"categories"`
	Scenarios  []Scenario `json:"scenarios" yaml:"scenarios"`
}

type Run struct {
	ID                  string         `json:"id"`
	ScenarioID          string         `json:"scenario_id"`
	ScenarioDescription string         `json:"scenario_description"`
	CategoryID          string         `json:"category_id"`
	CategoryDescription string         `json:"category_description"`
	Kind                string         `json:"kind"`
	Environment         string         `json:"environment"`
	TargetAPIURL        string         `json:"target_api_url"`
	TargetFrontendURL   string         `json:"target_frontend_url,omitempty"`
	AuthType            string         `json:"auth_type"`
	Parameters          map[string]any `json:"parameters"`
	Status              string         `json:"status"`
	CancelRequested     bool           `json:"cancel_requested"`
	WorkerID            string         `json:"worker_id,omitempty"`
	LeaseExpiresAt      *time.Time     `json:"lease_expires_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	StartedAt           *time.Time     `json:"started_at,omitempty"`
	FinishedAt          *time.Time     `json:"finished_at,omitempty"`
}

type Target struct {
	APIURL      string `json:"api_url"`
	FrontendURL string `json:"frontend_url,omitempty"`
	AuthType    string `json:"auth_type"`
	AuthValue   string `json:"auth_value,omitempty"`
}

type Event struct {
	Sequence  int64          `json:"sequence"`
	RunID     string         `json:"run_id"`
	Type      string         `json:"type"`
	Message   string         `json:"message,omitempty"`
	Test      string         `json:"test,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type RunDetail struct {
	Run    Run     `json:"run"`
	Events []Event `json:"events"`
}
