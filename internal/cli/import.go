package cli

import (
	"fmt"
	"os"
	"path/filepath"

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
	var source string
	var dryRun, overwrite, overwriteCreds, asJSON bool
	var share string
	cmd := &cobra.Command{
		Use:   "cc-deck",
		Short: "Import cc-deck profiles; never modifies or deletes a cc-deck file (FR-045)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runImportCCDeck(cmd, importArgs{
				source:         source,
				dryRun:         dryRun,
				overwrite:      overwrite,
				overwriteCreds: overwriteCreds,
				asJSON:         asJSON,
				share:          share,
			})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&source, "source", "", "cc-deck directory (default: the cc-deck dir beside the aprofile config dir)")
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
	Warnings            []string `json:"warnings,omitempty"`
}

type importCollisionJSON struct {
	WrapperName string `json:"wrapper_name"`
	Path        string `json:"path"`
}

type importJSON struct {
	FormatVersion int                   `json:"format_version"`
	SourceDir     string                `json:"source_dir"`
	DryRun        bool                  `json:"dry_run"`
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
		DryRun:               args.dryRun,
		Overwrite:            args.overwrite,
		OverwriteCredentials: args.overwriteCreds,
	})
	if err != nil {
		return err
	}

	collisions := detectWrapperCollisions(a, res)

	if args.asJSON {
		out := importJSON{FormatVersion: schemaVersion, SourceDir: res.SourceDir, DryRun: args.dryRun}
		for _, act := range res.Actions {
			out.Actions = append(out.Actions, importActionJSON{
				ID:                  act.ID,
				Status:              act.Status,
				Reason:              act.Reason,
				CredentialsReplaced: act.CredentialsReplaced,
				BackupDir:           act.BackupDir,
				Warnings:            act.Warnings,
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
	if skips := res.ExistingSkips(); len(skips) > 0 {
		return fmt.Errorf("%d profile(s) already existed and were skipped; rerun with --overwrite to replace them", len(skips))
	}
	return nil
}

func printImportReport(a *app, res *importer.Result, collisions []importCollisionJSON, dryRun bool) {
	if dryRun {
		fmt.Fprintf(a.out, "Dry run (no changes) against cc-deck dir: %s\n", res.SourceDir)
	} else {
		fmt.Fprintf(a.out, "Import from cc-deck dir: %s\n", res.SourceDir)
	}
	if len(res.Actions) == 0 {
		fmt.Fprintln(a.out, "  no cc-deck profiles found")
	}
	for _, act := range res.Actions {
		switch act.Status {
		case "skip":
			fmt.Fprintf(a.out, "  skip     %s (%s)\n", act.ID, act.Reason)
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
		if act.Status == "skip" {
			continue
		}
		def, ok := a.set.Get(act.Harness)
		if !ok {
			continue
		}
		name := def.WrapperPrefix + "-" + act.Name
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
