package schedule

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSecretResponseScheduleIsWriteOnly(t *testing.T) {
	const secret = "formal-secret-sentinel"
	response := scheduleSessionConfigResponse(SessionConfig{
		Environment: map[string]string{"TOKEN": secret},
		Params: &entities.SessionParams{
			GithubToken: secret,
			Docker:      &entities.DockerParams{Registries: []entities.DockerRegistry{{Server: "registry", Password: secret}}},
		},
	})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("schedule response disclosed secret: %s", encoded)
	}
}

func TestSecretPreserveScheduleEnvironmentOnOmittedUpdate(t *testing.T) {
	got := mergeScheduleSecretMap(map[string]string{"TOKEN": "old"}, nil)
	if got["TOKEN"] != "old" {
		t.Fatalf("omitted environment erased existing secret: %#v", got)
	}
}
