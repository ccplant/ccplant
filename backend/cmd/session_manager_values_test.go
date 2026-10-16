package cmd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingInstallCommandRunner struct {
	name string
	args []string
}

func (runner *recordingInstallCommandRunner) Run(_ context.Context, stdout, _ io.Writer, name string, args ...string) error {
	runner.name = name
	runner.args = append([]string(nil), args...)
	_, _ = io.WriteString(stdout, "session:\n  tolerations: []\n")
	return nil
}

func TestSessionManagerValues(t *testing.T) {
	t.Parallel()
	runner := &recordingInstallCommandRunner{}
	var stdout bytes.Buffer

	err := runSessionManagerValues(context.Background(), &stdout, io.Discard, sessionManagerValuesOptions{
		namespace: "builders",
		release:   "remote-manager",
	}, runner)

	require.NoError(t, err)
	require.Equal(t, "helm", runner.name)
	require.Equal(t, "get values remote-manager --namespace builders --output yaml", strings.Join(runner.args, " "))
	require.Equal(t, "session:\n  tolerations: []\n", stdout.String())
}

func TestSessionManagerValuesAll(t *testing.T) {
	t.Parallel()
	runner := &recordingInstallCommandRunner{}

	err := runSessionManagerValues(context.Background(), io.Discard, io.Discard, sessionManagerValuesOptions{
		namespace: "builders",
		release:   "remote-manager",
		all:       true,
	}, runner)

	require.NoError(t, err)
	require.Equal(t, "get values remote-manager --namespace builders --output yaml --all", strings.Join(runner.args, " "))
}

func TestSessionManagerValuesRejectsEmptyReleaseCoordinates(t *testing.T) {
	t.Parallel()
	runner := &recordingInstallCommandRunner{}

	err := runSessionManagerValues(context.Background(), io.Discard, io.Discard, sessionManagerValuesOptions{release: "manager"}, runner)
	require.ErrorContains(t, err, "--namespace must not be empty")
	err = runSessionManagerValues(context.Background(), io.Discard, io.Discard, sessionManagerValuesOptions{namespace: "sessions"}, runner)
	require.ErrorContains(t, err, "--release must not be empty")
}
