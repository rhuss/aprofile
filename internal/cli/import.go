package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/importer"
	"github.com/cc-deck/aprofile/internal/profile"
)

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import profiles from another tool (never modifies the source)",
	}
	cmd.AddCommand(newImportCCDeckCmd())
	return cmd
}

func newImportCCDeckCmd() *cobra.Command {
	var source, sourceData string
	var dryRun, overwrite, overwriteCreds, asJSON bool
	var share string
	cmd := &cobra.Command{
		Use:   "cc-deck",
		Short: "Import cc-deck profiles; never modifies or deletes a cc-deck file (FR-045)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runImportCCDeck(cmd, importArgs{
				source:         source,
				sourceData:     sourceData,
				dryRun:         dryRun,
				overwrite:      overwrite,
				overwriteCreds: overwriteCreds,
				asJSON:         asJSON,
				share:          share,
			})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&source, "source", "", "cc-deck config directory holding config.yaml (default: the cc-deck dir beside the aprofile config dir)")
	fl.StringVar(&sourceData, "source-data", "", "cc-deck data directory holding the live harness config dirs (default: the cc-deck dir beside the aprofile data dir)")
	fl.BoolVar(&dryRun, "dry-run", false, "list what would be imported and change nothing")
	fl.BoolVar(&overwrite, "overwrite", false, "replace an existing profile's settings and owned layer, keep its credentials and state")
	fl.BoolVar(&overwriteCreds, "overwrite-credentials", false, "additionally replace credentials, backing up the old ones first")
	fl.BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	fl.StringVar(&share, "share", "", "share level of the imported profiles (default: settings default_share, else content); "+shareFlagHelp())
	return cmd
}

type importArgs struct {
	share          string
	source         string
	sourceData     string
	dryRun         bool
	overwrite      bool
	overwriteCreds bool
	asJSON         bool
}

type importActionJSON struct {
	ID                  string   `json:"id"`
	Status              string   `json:"status"`
	Reason              string   `json:"reason,omitempty"`
	CredentialsReplaced bool     `json:"credentials_replaced,omitempty"`
	BackupDir           string   `json:"backup_dir,omitempty"`
	PrimaryMovedFrom    []string `json:"primary_moved_from,omitempty"`
	// ExternalCredentialFiles: credential files read from outside cc-deck's own
	// config and data folders (paths only; FR-045).
	ExternalCredentialFiles []string `json:"external_credential_files,omitempty"`
	Warnings                []string `json:"warnings,omitempty"`
}

type importCollisionJSON struct {
	WrapperName string `json:"wrapper_name"`
	Path        string `json:"path"`
}

type importJSON struct {
	FormatVersion int                   `json:"format_version"`
	SourceDir     string                `json:"source_dir"`
	SourceDataDir string                `json:"source_data_dir"`
	DryRun        bool                  `json:"dry_run"`
	Warnings      []string              `json:"warnings,omitempty"`
	Actions       []importActionJSON    `json:"actions"`
	Collisions    []importCollisionJSON `json:"collisions,omitempty"`
}

func runImportCCDeck(cmd *cobra.Command, args importArgs) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}

	source := args.source
	if source == "" {
		source = filepath.Join(filepath.Dir(a.paths.ConfigDir), "cc-deck")
	}

	share := args.share
	if share == "" {
		share = a.settings.EffectiveDefaultShare()
	}
	level, err := profile.ParseShare(share)
	if err != nil {
		return err
	}
	res, err := importer.Import(a.paths, a.set, a.store, source, importer.Options{
		Share:                level,
		SourceDataDir:        args.sourceData,
		DryRun:               args.dryRun,
		Overwrite:            args.overwrite,
		OverwriteCredentials: args.overwriteCreds,
	})
	if err != nil {
		return err
	}

	collisions := detectWrapperCollisions(a, res)

	if args.asJSON {
		out := importJSON{FormatVersion: schemaVersion, SourceDir: res.SourceDir, SourceDataDir: res.SourceDataDir, DryRun: args.dryRun, Warnings: res.Warnings,
			Actions: []importActionJSON{}}
		for _, act := range res.Actions {
			out.Actions = append(out.Actions, importActionJSON{
				ID:                      act.ID,
				Status:                  act.Status,
				Reason:                  act.Reason,
				CredentialsReplaced:     act.CredentialsReplaced,
				BackupDir:               act.BackupDir,
				PrimaryMovedFrom:        act.PrimaryMovedFrom,
				ExternalCredentialFiles: act.ExternalCredentialFiles,
				Warnings:                act.Warnings,
			})
		}
		out.Collisions = collisions
		if err := writeJSON(a.out, out); err != nil {
			return err
		}
	} else {
		printImportReport(a, res, collisions, args.dryRun)
	}

	// A profile that already exists is skipped and reported; the command then exits
	// non-zero (FR-045).
	// A profile whose wrapper name another profile has is rejected the same way
	// (FR-002).
	skips, clashes, failures := res.ExistingSkips(), res.ClashSkips(), res.Failures()
	var problems []string
	if len(failures) > 0 {
		// A profile that failed partway is reported; the others were imported
		// (FR-045). A new profile that failed was rolled back.
		problems = append(problems, fmt.Sprintf("%d profile(s) failed to import (see the report above)", len(failures)))
	}
	switch {
	case len(skips) > 0 && len(clashes) > 0:
		problems = append(problems, fmt.Sprintf("%d profile(s) already existed and were skipped (rerun with --overwrite to replace them); %d profile(s) were skipped because another profile has their wrapper name", len(skips), len(clashes)))
	case len(skips) > 0:
		problems = append(problems, fmt.Sprintf("%d profile(s) already existed and were skipped; rerun with --overwrite to replace them", len(skips)))
	case len(clashes) > 0:
		problems = append(problems, fmt.Sprintf("%d profile(s) were skipped because another profile has their wrapper name; rename that profile or the cc-deck one", len(clashes)))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func printImportReport(a *app, res *importer.Result, collisions []importCollisionJSON, dryRun bool) {
	if dryRun {
		fmt.Fprintf(a.out, "Dry run (no changes) against cc-deck dir: %s\n", res.SourceDir)
	} else {
		fmt.Fprintf(a.out, "Import from cc-deck dir: %s\n", res.SourceDir)
	}
	fmt.Fprintf(a.out, "  cc-deck data dir: %s\n", res.SourceDataDir)
	for _, wmsg := range res.Warnings {
		fmt.Fprintf(a.out, "  warning: %s\n", wmsg)
	}
	if len(res.Actions) == 0 {
		fmt.Fprintln(a.out, "  no cc-deck profiles found")
	}
	for _, act := range res.Actions {
		switch act.Status {
		case "skip":
			fmt.Fprintf(a.out, "  skip     %s (%s)\n", act.ID, act.Reason)
		case importer.StatusFailed:
			fmt.Fprintf(a.out, "  failed   %s (%s)\n", act.ID, act.Reason)
		case "overwrite":
			detail := "settings + owned replaced"
			if act.CredentialsReplaced {
				detail += ", credentials replaced (backup: " + act.BackupDir + ")"
			} else {
				detail += ", credentials kept"
			}
			fmt.Fprintf(a.out, "  %s %s (%s)\n", act.Status, act.ID, detail)
		default:
			fmt.Fprintf(a.out, "  %s   %s\n", act.Status, act.ID)
		}
		for _, from := range act.PrimaryMovedFrom {
			verb := "moved"
			if dryRun {
				verb = "would move"
			}
			fmt.Fprintf(a.out, "           %s primary flag from %s to %s\n", verb, from, act.ID)
		}
		for _, path := range act.ExternalCredentialFiles {
			verb := "copied"
			if dryRun {
				verb = "would copy"
			}
			fmt.Fprintf(a.out, "           %s credential file from outside cc-deck's folders: %s\n", verb, path)
		}
		for _, wmsg := range act.Warnings {
			fmt.Fprintf(a.out, "           warning: %s\n", wmsg)
		}
	}
	if len(collisions) > 0 {
		fmt.Fprintln(a.out, "wrapper-name collisions on PATH:")
		for _, c := range collisions {
			fmt.Fprintf(a.out, "  %s is already on PATH at %s\n", c.WrapperName, c.Path)
		}
		fmt.Fprintln(a.out, "  After 'aprofile sync', whichever wrapper is earlier on PATH wins.")
		fmt.Fprintln(a.out, "  Remove the old cc-deck wrappers (delete the files above) so the aprofile wrappers take effect.")
	}
}

// detectWrapperCollisions reports, for each imported profile, any executable with the
// same wrapper name already on PATH outside the aprofile bin dir (the stale cc-deck
// wrapper). It reports the first match in PATH order (the one that wins).
func detectWrapperCollisions(a *app, res *importer.Result) []importCollisionJSON {
	var out []importCollisionJSON
	seen := map[string]bool{}
	for _, act := range res.Actions {
		if act.Status == "skip" || act.Status == importer.StatusFailed {
			continue
		}
		name, ok := a.set.WrapperName(act.Harness, act.Name)
		if !ok {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if hit := firstExecutableOnPath(name, a.paths.BinDir); hit != "" {
			out = append(out, importCollisionJSON{WrapperName: name, Path: hit})
		}
	}
	return out
}

// firstExecutableOnPath returns the first PATH directory's executable named name
// that is not under excludeDir, or "" when none exists.
func firstExecutableOnPath(name, excludeDir string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || dir == excludeDir {
			continue
		}
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		if fi.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return ""
}
