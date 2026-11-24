package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestRunCycleMaxCountReached(t *testing.T) {
	// Pre-write the count at the limit and write message into CYCLE_ENABLED
	if err := os.MkdirAll("/tmp/check", 0o755); err != nil {
		t.Fatalf("failed to create /tmp/check: %v", err)
	}
	if err := os.WriteFile(cycleEnabledPath, []byte("this should not be sent"), 0o644); err != nil {
		t.Fatalf("failed to write CYCLE_ENABLED: %v", err)
	}
	if err := writeCycleCount(5); err != nil {
		t.Fatalf("failed to write cycle count: %v", err)
	}
	defer func() { _ = os.Remove(cycleCountPath) }()

	cycleMaxCount = 5
	defer func() { cycleMaxCount = 0 }()

	oldStdout := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp

	err := runCycle(&cobra.Command{}, []string{})

	_ = wp.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(rp)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "Cycle count limit reached")
	assert.Contains(t, output, "5/5")
	// CYCLE_ENABLED must have been deleted when the limit was reached
	_, statErr := os.Stat(cycleEnabledPath)
	assert.True(t, os.IsNotExist(statErr), "CYCLE_ENABLED should have been removed when max-count was reached")
}
