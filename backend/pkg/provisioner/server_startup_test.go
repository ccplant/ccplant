package provisioner

import (
	"context"
	"testing"
	"time"
)

func TestClassifyProvisionError(t *testing.T) {
	tests := []struct {
		name, message, want string
	}{
		{"codex auth", "ACP session creation failed: json-rpc error -32000: Authentication required", codexAuthRequiredErrorCode},
		{"case insensitive", "AUTHENTICATION REQUIRED", codexAuthRequiredErrorCode},
		{"other agent failure", "agent process exited: exit status 1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyProvisionError(tt.message); got != tt.want {
				t.Fatalf("classifyProvisionError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunStartupScriptSkipsDefaultNetworkInstall(t *testing.T) {
	t.Setenv("PROVISIONER_PRE_SCRIPT", "")

	s := New(0, "")
	s.runStartupScript(context.Background())

	select {
	case <-s.startupDone:
	case <-time.After(time.Second):
		t.Fatal("startup did not complete without a pre-script")
	}
}
