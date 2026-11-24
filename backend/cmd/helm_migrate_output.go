package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

func writeMigrationPlan(w io.Writer, format string, plan migrationPlan) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		return writeMigrationBytes(w, append(data, '\n'))
	case "yaml":
		data, err := yaml.Marshal(plan)
		if err != nil {
			return err
		}
		return writeMigrationBytes(w, data)
	}
	status := "READY"
	if !plan.Ready {
		status = "BLOCKED"
	}
	if _, err := fmt.Fprintf(w, "Migration preflight: %s\nNamespace: %s\nTarget: %s %s (%s)\nShadow values: %s\n", status, plan.Namespace, plan.Chart, plan.Version, plan.TargetRelease, plan.ValuesFile); err != nil {
		return err
	}
	for _, finding := range plan.Findings {
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(finding.Level), finding.Subject, finding.Message); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Inventory: sessions=%d legacy=%d runtime-secrets=%d session-pvcs=%d\n", plan.Inventory.Sessions, plan.Inventory.LegacySessions, plan.Inventory.RuntimeSecrets, plan.Inventory.SessionPVCs); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Suggested commands (not executed):"); err != nil {
		return err
	}
	for _, command := range plan.Commands {
		if _, err := fmt.Fprintln(w, "  "+command); err != nil {
			return err
		}
	}
	return nil
}

func writeMigrationVerify(w io.Writer, format string, verify migrationVerify) error {
	if format == "json" {
		data, err := json.MarshalIndent(verify, "", "  ")
		if err != nil {
			return err
		}
		return writeMigrationBytes(w, append(data, '\n'))
	}
	if format == "yaml" {
		data, err := yaml.Marshal(verify)
		if err != nil {
			return err
		}
		return writeMigrationBytes(w, data)
	}
	status := "PASS"
	if !verify.OK {
		status = "FAIL"
	}
	if _, err := fmt.Fprintf(w, "Shadow verification: %s\nNamespace: %s\nTarget: %s\nPhase: %s\n", status, verify.Namespace, verify.TargetRelease, verify.Phase); err != nil {
		return err
	}
	for _, finding := range verify.Findings {
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(finding.Level), finding.Subject, finding.Message); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Inventory: sessions=%d legacy=%d runtime-secrets=%d session-pvcs=%d\n", verify.Inventory.Sessions, verify.Inventory.LegacySessions, verify.Inventory.RuntimeSecrets, verify.Inventory.SessionPVCs); err != nil {
		return err
	}
	return nil
}

func writeMigrationBytes(destination io.Writer, data []byte) error {
	written, err := destination.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
