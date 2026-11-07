package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type recordingTerminationPreparer struct {
	steps *[]string
	err   error
}

func (p recordingTerminationPreparer) PrepareForTermination(context.Context) error {
	*p.steps = append(*p.steps, "stop")
	return p.err
}

func TestPreparePersistentSessionForTerminationRunsEveryStepInOrder(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "session-state.tar.zst")
	if err := os.WriteFile(archive, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", archive)
	var steps []string
	err := preparePersistentSessionForTermination(
		context.Background(),
		recordingTerminationPreparer{steps: &steps, err: errors.New("stop failed")},
		func(context.Context) error {
			steps = append(steps, "backup")
			return errors.New("backup failed")
		},
		func(context.Context) error {
			steps = append(steps, "suspend")
			return nil
		},
	)
	if !reflect.DeepEqual(steps, []string{"stop", "backup", "suspend"}) {
		t.Fatalf("steps = %#v", steps)
	}
	if err == nil || err.Error() != "stop agent: stop failed\nbackup session state: backup failed" {
		t.Fatalf("error = %v", err)
	}
}

func TestPreparePersistentSessionForTerminationKeepsWorkloadWithoutAnySnapshot(t *testing.T) {
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", filepath.Join(t.TempDir(), "session-state.tar.zst"))
	var steps []string
	err := preparePersistentSessionForTermination(
		context.Background(),
		recordingTerminationPreparer{steps: &steps},
		func(context.Context) error {
			steps = append(steps, "backup")
			return errors.New("backup failed")
		},
		func(context.Context) error {
			steps = append(steps, "suspend")
			return nil
		},
	)
	if !reflect.DeepEqual(steps, []string{"stop", "backup"}) {
		t.Fatalf("steps = %#v, want no suspend without any saved snapshot", steps)
	}
	if err == nil || err.Error() != "backup session state: backup failed" {
		t.Fatalf("error = %v", err)
	}
}

func TestPreparePersistentSessionForTerminationSuspendsAfterSuccessfulBackup(t *testing.T) {
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", filepath.Join(t.TempDir(), "session-state.tar.zst"))
	var steps []string
	err := preparePersistentSessionForTermination(
		context.Background(),
		recordingTerminationPreparer{steps: &steps},
		func(context.Context) error {
			steps = append(steps, "backup")
			return nil
		},
		func(context.Context) error {
			steps = append(steps, "suspend")
			return nil
		},
	)
	if err != nil || !reflect.DeepEqual(steps, []string{"stop", "backup", "suspend"}) {
		t.Fatalf("steps = %#v err = %v", steps, err)
	}
}
