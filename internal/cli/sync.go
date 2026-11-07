package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/layer"
	syncpkg "github.com/cc-deck/aprofile/internal/sync"
)

// syncJSON is the `sync --json` schema (FR-041), documented in
// specs/001-aprofile-standalone-profile-cli/contracts/cli.md. With --dry-run it is
// the plan; without it, what the sync did.
type syncJSON struct {
	FormatVersion   int                   `json:"format_version"`
	DryRun          bool                  `json:"dry_run"`
	Changed         bool                  `json:"changed"`
	Profiles        []syncpkg.ProfilePlan `json:"profiles"`
	WrappersRemoved []string              `json:"wrappers_removed"`
	Registry        syncRegistryJSON      `json:"registry"`
	Path            syncPathJSON          `json:"path"`
	Problems        []string              `json:"problems"`
	Notes           []string              `json:"notes"`
}

type syncRegistryJSON struct {
	Path    string   `json:"path"`
	Changed bool     `json:"changed"`
	Removed []string `json:"removed"`
}

type syncPathJSON struct {
	Action      string `json:"action"`
	RCFile      string `json:"rc_file,omitempty"`
	Changed     bool   `json:"changed"`
	NeedsManual bool   `json:"needs_manual"`
	Reason      string `json:"reason,omitempty"`
	Manual      string `json:"manual,omitempty"`
}

func syncReport(res *syncpkg.Result, dryRun bool) syncJSON {
	nonNil := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	out := syncJSON{
		FormatVersion:   schemaVersion,
		DryRun:          dryRun,
		Changed:         res.Changed(),
		Profiles:        res.Profiles,
		WrappersRemoved: nonNil(res.WrappersRemoved),
		Registry:        syncRegistryJSON{Path: res.RegistryPath, Changed: res.RegistryChanged, Removed: nonNil(res.RegistryRemoved)},
		Path: syncPathJSON{Action: res.Path.Action, RCFile: res.Path.RCFile, Changed: res.Path.Changed,
			NeedsManual: res.Path.NeedsManual(), Reason: res.Path.Reason, Manual: res.Path.Manual},
		Problems: nonNil(res.Problems),
		Notes:    nonNil(res.Notes),
	}
	if out.Profiles == nil {
		out.Profiles = []syncpkg.ProfilePlan{}
	}
	return out
}

func newSyncCmd() *cobra.Command {
	var opts syncpkg.Options
	var yes, asJSON bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Write wrappers, prepare config dirs, write the registry, and manage PATH",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd, opts, yes, asJSON)
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&opts.DryRun, "dry-run", false, "list the changes without making them")
	fl.BoolVar(&opts.RequirePath, "require-path", false, "exit non-zero if the bin dir could not be put on PATH")
	fl.BoolVar(&opts.RemovePath, "remove-path", false, "remove aprofile's PATH block from the shell startup file")
	fl.BoolVar(&opts.FixLinks, "fix-links", false, "replace a leaking state/credentials link with an empty private entry; earlier data stays in the default setup")
	fl.BoolVar(&opts.Relink, "relink", false, "relink entries a harness replaced with a real file (backs the file up and asks first)")
	fl.BoolVar(&yes, "yes", false, "with --relink: confirm without asking (required off a terminal)")
	fl.BoolVar(&asJSON, "json", false, "emit the changes (with --dry-run: the planned changes) as machine-readable JSON")
	return cmd
}

func runSync(cmd *cobra.Command, opts syncpkg.Options, yes, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	jsonOut := a.out
	if asJSON {
		// Keep stdout clean JSON: relink prompts and plans go to stderr.
		ac := *a
		ac.out = a.errOut
		a = &ac
	}
	var needConfirm []string
	if opts.Relink {
		// FR-021: show what will change, back up, and ask once per profile.
		opts.Confirm = func(id string, steps []layer.Step) bool {
			fmt.Fprintf(a.out, "relink in %s:\n", id)
			printPlan(a, steps)
			err := confirmOrYes(cmd, fmt.Sprintf("Back up and relink these entries of %s?", id), yes)
			if err != nil && err != errDeclined {
				needConfirm = append(needConfirm, id)
			}
			return err == nil
		}
		opts.Backup = func(h, n string, paths []string) (string, error) {
			dir, err := backupPaths(a.paths, h, n, paths, time.Now())
			if err == nil && dir != "" {
				fmt.Fprintf(a.out, "  backed up to %s\n", dir)
			}
			return dir, err
		}
	}
	res, err := syncpkg.Run(a.paths, a.set, a.store, a.settings, opts)
	if err != nil {
		return err
	}

	if asJSON {
		if err := writeJSON(jsonOut, syncReport(res, opts.DryRun)); err != nil {
			return err
		}
		for _, p := range res.Problems {
			fmt.Fprintf(a.errOut, "aprofile: %s\n", p)
		}
		return syncExit(res, opts, needConfirm)
	}

	for _, c := range res.Changes {
		fmt.Fprintln(a.out, c)
	}
	switch res.Path.Action {
	case "added":
		fmt.Fprintf(a.out, "PATH: added aprofile's bin dir to %s (open a new shell to apply)\n", res.Path.RCFile)
	case "removed":
		fmt.Fprintf(a.out, "PATH: removed aprofile's block from %s\n", res.Path.RCFile)
	case "manual", "disabled":
		if res.Path.Reason != "" {
			fmt.Fprintf(a.errOut, "PATH: %s\n", res.Path.Reason)
		}
		fmt.Fprintf(a.errOut, "PATH: %s\n", res.Path.Manual)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(a.errOut, "aprofile: note: %s\n", n)
	}
	for _, p := range res.Problems {
		fmt.Fprintf(a.errOut, "aprofile: %s\n", p)
	}

	if opts.DryRun {
		if res.Changed() {
			fmt.Fprintln(a.out, "(dry-run) changes listed above")
		} else {
			fmt.Fprintln(a.out, "(dry-run) no changes")
		}
	} else if !res.Changed() {
		fmt.Fprintln(a.out, "already up to date (0 changes)")
	}

	return syncExit(res, opts, needConfirm)
}

// syncExit maps a sync result to the exit status: non-zero on load problems
// (FR-022), on a relink that needs confirmation, and, only with --require-path, on
// every outcome that leaves the PATH setup to the user (FR-024).
func syncExit(res *syncpkg.Result, opts syncpkg.Options, needConfirm []string) error {
	if res.HasProblems() {
		return fmt.Errorf("sync completed with problems (see above)")
	}
	if len(needConfirm) > 0 {
		return exitErrorf(ExitNeedsConfirmation, "relink of %s needs confirmation; re-run with --relink --yes (the other sync changes were applied)", strings.Join(needConfirm, ", "))
	}
	if opts.RequirePath && res.Path.NeedsManual() {
		why := res.Path.Reason
		if why == "" && res.Path.Err != nil {
			why = res.Path.Err.Error()
		}
		return fmt.Errorf("could not put the bin dir on PATH: %s", why)
	}
	return nil
}
