package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type sessionManagerValuesOptions struct {
	namespace string
	release   string
	all       bool
}

func newSessionManagerValuesCommand() *cobra.Command {
	var opts sessionManagerValuesOptions
	command := &cobra.Command{
		Use:   "values",
		Short: "Print the Helm values for an installed session manager",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSessionManagerValues(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, execInstallCommandRunner{})
		},
	}
	flags := command.Flags()
	flags.StringVarP(&opts.namespace, "namespace", "n", "ccplant-session", "Kubernetes namespace containing the Helm release")
	flags.StringVar(&opts.release, "release", "session-manager", "Helm release name")
	flags.BoolVar(&opts.all, "all", false, "include computed chart defaults")
	return command
}

func runSessionManagerValues(ctx context.Context, stdout, stderr io.Writer, opts sessionManagerValuesOptions, runner installCommandRunner) error {
	if strings.TrimSpace(opts.namespace) == "" {
		return errors.New("--namespace must not be empty")
	}
	if strings.TrimSpace(opts.release) == "" {
		return errors.New("--release must not be empty")
	}
	args := []string{"get", "values", opts.release, "--namespace", opts.namespace, "--output", "yaml"}
	if opts.all {
		args = append(args, "--all")
	}
	if err := runner.Run(ctx, stdout, stderr, "helm", args...); err != nil {
		return fmt.Errorf("read Helm values for %s/%s: %w", opts.namespace, opts.release, err)
	}
	return nil
}
