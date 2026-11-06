package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/layer"
	syncpkg "github.com/cc-deck/aprofile/internal/sync"
)

func newSyncCmd() *cobra.Command {
	var opts syncpkg.Options
	var yes bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Write wrappers, prepare config dirs, write the registry, and manage PATH",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd, opts, yes)
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&opts.DryRun, "dry-run", false, "list the changes without making them")
	fl.BoolVar(&opts.RequirePath, "require-path", false, "exit non-zero if the bin dir could not be put on PATH")
	fl.BoolVar(&opts.RemovePath, "remove-path", false, "remove aprofile's PATH block from the shell startup file")
	fl.BoolVar(&opts.FixLinks, "fix-links", false, "replace a leaking state/credentials link with an empty private entry; earlier data stays in the default setup")
	fl.BoolVar(&opts.Relink, "relink", false, "relink entries a harness replaced with a real file (backs the file up and asks first)")
	fl.BoolVar(&yes, "yes", false, "with --relink: confirm without asking (required off a terminal)")
	return cmd
}

func runSync(cmd *cobra.Command, opts syncpkg.Options, yes bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
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

	for _, c := range res.Changes {
		fmt.Fprintln(a.out, c)
	}
	switch res.Path.Action {
	case "added":
		fmt.Fprintf(a.out, "PATH: added aprofile's bin dir to %s (open a new shell to apply)\n", res.Path.RCFile)
	case "removed":
		fmt.Fprintf(a.out, "PATH: removed aprofile's block from %s\n", res.Path.RCFile)
	case "manual", "disabled":
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

	// Exit non-zero on load problems (FR-022), and on PATH failure only with
	// --require-path (FR-024).
	if res.HasProblems() {
		return fmt.Errorf("sync completed with problems (see above)")
	}
	if len(needConfirm) > 0 {
		return exitErrorf(ExitNeedsConfirmation, "relink of %s needs confirmation; re-run with --relink --yes (the other sync changes were applied)", strings.Join(needConfirm, ", "))
	}
	if opts.RequirePath && res.Path.Err != nil {
		return fmt.Errorf("could not put the bin dir on PATH: %v", res.Path.Err)
	}
	return nil
}
