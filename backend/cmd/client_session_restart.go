package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

func init() {
	for _, action := range []string{"restart", "pause", "restart-status"} {
		action := action
		reload := true
		inputFile := ""
		command := &cobra.Command{Use: action, Short: "Manage session settings reload and conversation restart", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			c, id, err := resolveClient()
			if err != nil {
				return err
			}
			var inputs []json.RawMessage
			if inputFile != "" {
				raw, err := os.ReadFile(inputFile)
				if err != nil {
					return err
				}
				if !json.Valid(raw) {
					return fmt.Errorf("startup input must be JSON")
				}
				inputs = append(inputs, json.RawMessage(raw))
			}
			result, err := c.SessionLifecycle(cmd.Context(), id, action, reload, inputs...)
			if err != nil {
				return err
			}
			data, err := json.Marshal(result)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return err
		}}
		if action == "restart" {
			command.Flags().StringVar(&inputFile, "startup-input-file", "", "JSON start request to replace explicit overrides or initialize a legacy session")
			command.Flags().BoolVar(&reload, "reload-settings", true, "Resolve all settings again before restarting")
		}
		ClientCmd.AddCommand(command)
	}
}
