package services

import (
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestNetworkFilterWaitsForProvisionerBeforeStopping(t *testing.T) {
	manager := &KubernetesSessionManager{k8sConfig: &config.KubernetesSessionConfig{}}
	_, sidecar, _ := manager.buildSandboxContainers(nil)
	if sidecar.Lifecycle == nil || sidecar.Lifecycle.PreStop == nil || sidecar.Lifecycle.PreStop.Exec == nil {
		t.Fatal("network-filter has no preStop exec hook")
	}
	command := strings.Join(sidecar.Lifecycle.PreStop.Exec.Command, " ")
	if !strings.Contains(command, "[ -S /tmp/ccplant/provisioner.sock ]") {
		t.Fatalf("preStop does not wait for the provisioner socket: %s", command)
	}
	if !strings.Contains(command, `-lt 25`) {
		t.Fatalf("preStop wait is not bounded by the provisioner termination budget: %s", command)
	}
}
