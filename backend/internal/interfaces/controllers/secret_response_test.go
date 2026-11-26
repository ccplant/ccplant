package controllers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSecretResponseSessionProfileIsWriteOnly(t *testing.T) {
	const secret = "formal-secret-sentinel"
	profile := entities.NewSessionProfile("profile", "profile", "user")
	config := entities.NewSessionProfileConfig()
	config.SetEnvironment(map[string]string{"TOKEN": secret})
	config.SetProfileFiles([]entities.ProfileFile{{Path: "/secret", Content: secret}})
	server := entities.NewMCPServer("example", "stdio")
	server.SetEnv(map[string]string{"MCP_TOKEN": secret})
	server.SetHeaders(map[string]string{"Authorization": secret})
	servers := entities.NewMCPServersSettings()
	servers.SetServer("example", server)
	config.SetMCPServers(servers)
	profile.SetConfig(config)

	encoded, err := json.Marshal(NewSessionProfileController(nil, nil).toResponse(profile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("session profile response disclosed secret: %s", encoded)
	}
}

func TestSecretResponseManagedFileIsWriteOnly(t *testing.T) {
	const secret = "formal-secret-sentinel"
	encoded, err := json.Marshal(toFileResponse(entities.NewUserFile("id", "key", "/key", secret, "0600")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("managed file response disclosed content: %s", encoded)
	}
}

func TestSecretPreserveSessionProfileEnvironmentOnOmittedUpdate(t *testing.T) {
	got := mergeProfileSecretMap(map[string]string{"TOKEN": "old"}, nil)
	if got["TOKEN"] != "old" {
		t.Fatalf("omitted environment erased existing secret: %#v", got)
	}
}
