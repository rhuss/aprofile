package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/doctor"
)

type doctorJSON struct {
	FormatVersion int              `json:"format_version"`
	Findings      []doctor.Finding `json:"findings"`
}

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check profiles and config dirs for isolation and configuration problems",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func runDoctor(cmd *cobra.Command, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	findings, err := doctor.Run(a.paths, a.set, a.store)
	if err != nil {
		return err
	}

	if asJSON {
		// Emit findings as JSON on stdout regardless of count so the output is always
		// machine-parseable; the exit code signals whether problems were found.
		if err := writeJSON(a.out, doctorJSON{FormatVersion: schemaVersion, Findings: findings}); err != nil {
			return err
		}
	} else if len(findings) == 0 {
		fmt.Fprintln(a.out, "no problems found")
	} else {
		for _, f := range findings {
			if f.Profile != "" {
				fmt.Fprintf(a.out, "[%s] %s\n", f.Profile, f.Message)
			} else {
				fmt.Fprintf(a.out, "%s\n", f.Message)
			}
			if f.Fix != "" {
				fmt.Fprintf(a.out, "    fix: %s\n", f.Fix)
			}
		}
	}

	if len(findings) > 0 {
		return fmt.Errorf("doctor found %d problem(s)", len(findings))
	}
	return nil
}
