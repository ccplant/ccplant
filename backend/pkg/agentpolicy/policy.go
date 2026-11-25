// Package agentpolicy defines and evaluates the policy.yaml contract used by
// agents when they call ccplant through the local provisioner broker.
package agentpolicy

import (
	"fmt"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

const Version = "v1"

type Actions struct {
	Create bool `json:"create,omitempty" yaml:"create,omitempty"`
	Read   bool `json:"read,omitempty" yaml:"read,omitempty"`
	Update bool `json:"update,omitempty" yaml:"update,omitempty"`
	Delete bool `json:"delete,omitempty" yaml:"delete,omitempty"`
}

type Policy struct {
	Version   string             `json:"version" yaml:"version"`
	Resources map[string]Actions `json:"resources" yaml:"resources"`
}

func Parse(data string) (*Policy, error) {
	if strings.TrimSpace(data) == "" {
		return nil, nil
	}
	var policy Policy
	decoder := yaml.NewDecoder(strings.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&policy); err != nil {
		return nil, fmt.Errorf("invalid policy.yaml: %w", err)
	}
	if policy.Version != Version {
		return nil, fmt.Errorf("invalid policy.yaml: version must be %q", Version)
	}
	if len(policy.Resources) == 0 {
		return nil, fmt.Errorf("invalid policy.yaml: resources must not be empty")
	}
	for resource := range policy.Resources {
		if resource != "*" && !validResource(resource) {
			return nil, fmt.Errorf("invalid policy.yaml: invalid resource %q", resource)
		}
	}
	return &policy, nil
}

func validResource(value string) bool {
	if value == "" || strings.Trim(value, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return false
	}
	return !strings.HasPrefix(value, "-") && !strings.HasSuffix(value, "-")
}

func (p *Policy) Allows(resource, action string) bool {
	if p == nil {
		return false
	}
	actions, ok := p.Resources[resource]
	if !ok {
		actions, ok = p.Resources["*"]
	}
	if !ok {
		return false
	}
	switch action {
	case "create":
		return actions.Create
	case "read":
		return actions.Read
	case "update":
		return actions.Update
	case "delete":
		return actions.Delete
	default:
		return false
	}
}

// RequestOperation maps the public REST API to a stable resource/action pair.
func RequestOperation(r *http.Request) (resource, action string, ok bool) {
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(segments) == 0 || segments[0] == "" || segments[0] == "internal" {
		return "", "", false
	}
	resource = segments[0]
	switch resource {
	case "start":
		resource, action = "sessions", "create"
	case "search":
		resource, action = "sessions", "read"
	case "notifications":
		if r.Method == http.MethodPost && len(segments) == 2 && segments[1] == "send" {
			action = "create"
			break
		}
		fallthrough
	default:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			action = "read"
		case http.MethodDelete:
			action = "delete"
		case http.MethodPut, http.MethodPatch:
			action = "update"
		case http.MethodPost:
			if len(segments) == 1 {
				action = "create"
			} else {
				action = "update"
			}
		default:
			return "", "", false
		}
	}
	return resource, action, true
}
