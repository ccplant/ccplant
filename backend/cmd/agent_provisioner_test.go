package cmd

import (
	"context"
	"errors"
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
