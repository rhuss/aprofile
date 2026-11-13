package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	syncpkg "github.com/cc-deck/aprofile/internal/sync"
)

func newSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Refresh a content profile's own settings from the default setup",
	}
	cmd.AddCommand(newSettingsPullCmd())
	return cmd
}

type settingsPullFlags struct {
	replace bool
	dryRun  bool
	yes     bool
	asJSON  bool
}

func newSettingsPullCmd() *cobra.Command {
	f := &settingsPullFlags{}
	cmd := &cobra.Command{
		Use:   "pull <harness>/<name> [key...]",
		Short: "Merge (or with --replace, copy) the default setup's settings into a content profile",
		Long: "Merges the current settings of the default setup into the profile's own copy: objects merge by key,\n" +
			"lists gain missing entries, scalars take the default's value, and keys only the profile has are kept.\n" +
			"--replace makes the pulled keys (or the whole file) an exact copy instead. The hook location and owned\n" +
			"keys are never touched. Only content profiles have settings of their own to pull into (FR-018).",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsPull(cmd, args[0], args[1:], f)
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.replace, "replace", false, "make the pulled keys (or the whole file) an exact copy of the default setup's")
	fl.BoolVar(&f.dryRun, "dry-run", false, "list the keys that would change without writing")
	fl.BoolVar(&f.yes, "yes", false, "confirm without asking (required off a terminal)")
	fl.BoolVar(&f.asJSON, "json", false, "emit the change plan as JSON")
	return cmd
}

type settingsChangeJSON struct {
	Entry    string   `json:"entry"`
	Kind     string   `json:"kind"`
	Changed  []string `json:"changed"`
	Removed  []string `json:"removed"`
	Warnings []string `json:"secret_warnings,omitempty"`
	Note     string   `json:"note,omitempty"`
	Backup   string   `json:"backup,omitempty"`
}

type settingsPullJSON struct {
	FormatVersion int                  `json:"format_version"`
	Profile       string               `json:"profile"`
	Mode          string               `json:"mode"` // merge | replace
	DryRun        bool                 `json:"dry_run"`
	Changes       []settingsChangeJSON `json:"changes"`
	// Launch lists the launch entries placed again after the pull (spec 004 FR-010).
	Launch []string `json:"launch,omitempty"`
}

func runSettingsPull(cmd *cobra.Command, ref string, keys []string, f *settingsPullFlags) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	h, n, err := parseProfileRef(ref)
	if err != nil {
		return err
	}
	p, err := a.store.Load(h, n)
	if err != nil {
		return err
	}
	def, ok := a.set.Get(h)
	if !ok {
		return fmt.Errorf("unknown harness %q", h)
	}
	switch p.ShareLevel() {
	case profile.ShareAll:
		return exitErrorf(ExitInvalidForLevel, "profile %s uses share level all: its settings are the default setup's live file, so there is nothing to pull", p.ID())
	case profile.ShareNone:
		return exitErrorf(ExitInvalidForLevel, "profile %s uses share level none: it borrows nothing from the default setup; move it to --share content first if it should", p.ID())
	}
	configDir := a.paths.ProfileConfigDir(h, n)
	steps, report, err := planSettingsPull(def, configDir, keys, f.replace)
	if err != nil {
		return err
	}
	out := settingsPullJSON{FormatVersion: schemaVersion, Profile: p.ID(), Mode: "merge", DryRun: f.dryRun, Changes: report}
	if f.replace {
		out.Mode = "replace"
	}
	if out.Changes == nil {
		out.Changes = []settingsChangeJSON{}
	}
	if !f.asJSON {
		printSettingsPlan(a, out)
		// Spec 004 FR-010: entries aprofile placed in these files are placed again
		// right after the pull, so a change listed for them above is undone.
		if rec, rerr := launch.Load(a.paths.ManagedLaunchPath(h, n)); rerr == nil && len(rec.Entries) > 0 && len(steps) > 0 {
			fmt.Fprintln(a.out, "  launch settings aprofile placed are placed again after the pull")
		}
	}
	if f.dryRun || len(steps) == 0 {
		if f.asJSON {
			return writeJSON(a.out, out)
		}
		return nil
	}
	err = confirmOrYes(cmd, fmt.Sprintf("Update the settings of %s (a backup is made first)?", p.ID()), f.yes)
	if err == errDeclined {
		fmt.Fprintln(a.out, "aborted; nothing changed")
		return nil
	}
	if err != nil {
		return err
	}
	applied, _, err := layer.Apply(steps, layer.ApplyOptions{Yes: true, Backup: func(paths []string) (string, error) {
		return backupPaths(a.paths, h, n, paths, time.Now())
	}})
	if err != nil {
		return err
	}
	for i, s := range applied {
		for j := range out.Changes {
			if out.Changes[j].Entry == s.Entry {
				out.Changes[j].Backup = s.Backup
			}
		}
		if !f.asJSON && s.Backup != "" && (i == 0 || applied[i-1].Backup != s.Backup) {
			fmt.Fprintf(a.out, "backed up to %s\n", s.Backup)
		}
	}
	// Spec 004 FR-010: the pull may have replaced or dropped the profile's placed
	// launch entries (they live in the same env block); place them again now.
	lr, lerr := launch.Apply(p, def, configDir, a.paths.ManagedLaunchPath(h, n), false)
	for _, st := range lr.Steps {
		line, _ := syncpkg.LaunchStepText(st)
		out.Launch = append(out.Launch, st.File+": "+line)
	}
	if f.asJSON {
		if err := writeJSON(a.out, out); err != nil {
			return err
		}
	} else {
		for _, l := range out.Launch {
			fmt.Fprintf(a.out, "launch settings: %s\n", l)
		}
		fmt.Fprintf(a.out, "updated the settings of %s\n", p.ID())
	}
	if lerr != nil {
		return fmt.Errorf("profile %s: the settings were pulled, but the launch settings could not be placed again: %w (run: aprofile sync)", p.ID(), lerr)
	}
	return nil
}

// planSettingsPull builds one merge-settings or replace-settings step per settings
// entry that would change. JSON files are merged by key; other formats and
// directories are copied as whole entries under --replace and reported otherwise.
func planSettingsPull(def *harness.Definition, configDir string, keys []string, replace bool) ([]layer.Step, []settingsChangeJSON, error) {
	var steps []layer.Step
	var report []settingsChangeJSON
	for _, rule := range def.Classification {
		if rule.Layer != harness.LayerSettings || rule.IsGlob() {
			continue
		}
		rel := rule.Path()
		path := filepath.Join(configDir, filepath.FromSlash(rel))
		src := def.DefaultPath(rel)
		pfi, perr := os.Lstat(path)
		sfi, serr := os.Stat(src)
		if serr != nil {
			continue // nothing in the default setup to pull
		}
		if perr == nil && pfi.Mode()&os.ModeSymlink != 0 {
			report = append(report, settingsChangeJSON{Entry: rel, Kind: "skip", Note: "still a link; run: aprofile sync"})
			continue
		}
		kind := layer.StepMergeSettings
		if replace {
			kind = layer.StepReplaceSettings
		}
		if sfi.IsDir() || !layer.IsJSONExt(filepath.Ext(rel)) {
			if !replace {
				report = append(report, settingsChangeJSON{Entry: rel, Kind: "skip", Note: "not a JSON file: only --replace copies it as a whole"})
				continue
			}
			if sameTree(src, path) {
				continue // already identical: no backup, no prompt
			}
			step := layer.Step{Kind: kind, Path: path, Entry: rel, Source: src, IsDir: sfi.IsDir(), Confirm: true, Mode: sfi.Mode().Perm(),
				Note: "copied as a whole from the default setup"}
			if !sfi.IsDir() {
				data, err := os.ReadFile(src)
				if err != nil {
					return nil, nil, err
				}
				step.Data = data // the exact content, read at plan time
			}
			steps = append(steps, step)
			report = append(report, settingsChangeJSON{Entry: rel, Kind: string(kind), Changed: []string{"(whole entry)"}, Removed: []string{}})
			continue
		}
		var profileDoc []byte
		if perr == nil {
			var err error
			if profileDoc, err = os.ReadFile(path); err != nil {
				return nil, nil, err
			}
		}
		defaultDoc, err := os.ReadFile(src)
		if err != nil {
			return nil, nil, err
		}
		exclude := []string{}
		if key := def.HookKeyIn(rel); key != "" {
			exclude = append(exclude, key)
		}
		var data []byte
		var removed, changed []string
		if replace {
			data, removed, changed, err = layer.ReplaceSettings(profileDoc, defaultDoc, keys, exclude)
		} else {
			data, removed, changed, err = layer.MergeSettings(profileDoc, defaultDoc, keys, exclude)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", rel, err)
		}
		if len(removed) == 0 && len(changed) == 0 {
			continue
		}
		if removed == nil {
			removed = []string{}
		}
		if changed == nil {
			changed = []string{}
		}
		report = append(report, settingsChangeJSON{Entry: rel, Kind: string(kind), Changed: changed, Removed: removed, Warnings: layer.SecretWarnings(defaultDoc)})
		steps = append(steps, layer.Step{Kind: kind, Path: path, Entry: rel, Source: src, Data: data, Confirm: true,
			Note: fmt.Sprintf("%d changed, %d removed; hooks untouched", len(changed), len(removed))})
	}
	return steps, report, nil
}

// sameTree reports whether two files or directory trees have identical content.
func sameTree(a, b string) bool {
	fa, err1 := os.Lstat(a)
	fb, err2 := os.Lstat(b)
	if err1 != nil || err2 != nil || fa.IsDir() != fb.IsDir() {
		return false
	}
	if !fa.IsDir() {
		da, e1 := os.ReadFile(a)
		db, e2 := os.ReadFile(b)
		return e1 == nil && e2 == nil && string(da) == string(db)
	}
	ea, e1 := os.ReadDir(a)
	eb, e2 := os.ReadDir(b)
	if e1 != nil || e2 != nil || len(ea) != len(eb) {
		return false
	}
	for i := range ea {
		if ea[i].Name() != eb[i].Name() || !sameTree(filepath.Join(a, ea[i].Name()), filepath.Join(b, eb[i].Name())) {
			return false
		}
	}
	return true
}

func printSettingsPlan(a *app, out settingsPullJSON) {
	prefix := ""
	if out.DryRun {
		prefix = "(dry-run) "
	}
	fmt.Fprintf(a.out, "%ssettings pull (%s) for %s:\n", prefix, out.Mode, out.Profile)
	if len(out.Changes) == 0 {
		fmt.Fprintln(a.out, "  nothing to change: the profile's settings already match")
		return
	}
	for _, c := range out.Changes {
		if c.Kind == "skip" {
			fmt.Fprintf(a.out, "  %s: skipped (%s)\n", c.Entry, c.Note)
			continue
		}
		fmt.Fprintf(a.out, "  %s:\n", c.Entry)
		for _, k := range c.Changed {
			fmt.Fprintf(a.out, "    changed: %s\n", k)
		}
		for _, k := range c.Removed {
			fmt.Fprintf(a.out, "    removed: %s\n", k)
		}
		for _, w := range c.Warnings {
			fmt.Fprintf(a.errOut, "aprofile: warning: %s %s looks like a secret and is copied verbatim\n", c.Entry, w)
		}
	}
	fmt.Fprintln(a.out, "  the hook location and owned keys are never touched")
}
