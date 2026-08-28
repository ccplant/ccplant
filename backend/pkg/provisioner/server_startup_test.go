package provisioner

import (
	"context"
	"testing"
	"time"
)

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
