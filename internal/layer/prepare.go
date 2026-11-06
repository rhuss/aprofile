package layer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// Change describes one config dir change made (or, under dry-run, that would be
// made) while preparing a profile. Mutating actions alter the filesystem;
// informational ones (report, needs-confirm) only report a condition, so an
// otherwise unchanged sync stays idempotent (SC-003).
type Change struct {
	Action string // a StepKind, or "needs-confirm" for a skipped Confirm step
	Path   string
	Note   string
	Step   Step
}

func (c Change) String() string {
	if c.Note != "" {
		return fmt.Sprintf("%s %s (%s)", c.Action, c.Path, c.Note)
	}
	return fmt.Sprintf("%s %s", c.Action, c.Path)
}

// Mutating reports whether the change altered the filesystem.
func (c Change) Mutating() bool {
	return c.Action != string(StepReport) && c.Action != "needs-confirm"
}

// PrepareOptions controls one config-dir preparation.
type PrepareOptions struct {
	DryRun bool
	// FixLinks replaces a leaking credentials/state/owned symlink with an empty real
	// entry, never deleting the link target (FR-034).
	FixLinks bool
	// Home is the directory ~ expands to in default locations ("" means $HOME).
	Home string
	// Relink also applies the replace-with-link steps (a real entry where a link is
	// expected), after Confirm returns true and with Backup (sync --relink, FR-021).
	Relink  bool
	Confirm func(steps []Step) bool
	Backup  func(paths []string) (string, error)
}

// PrepareConfigDir prepares a profile's config dir for its share level in safe
// mode: it applies every step that adds links or creates entries and reports the
// steps that would replace profile data (FR-017a). It is idempotent.
func PrepareConfigDir(def *harness.Definition, p *profile.Profile, configDir string, dryRun bool) ([]Change, error) {
	return PrepareConfigDirWith(def, p, configDir, PrepareOptions{DryRun: dryRun})
}

// PrepareConfigDirWith is PrepareConfigDir with explicit options. It is the thin
// wrapper over Plan and Apply that sync, login and copy use (plan Design Decision
// 3). A nil profile is treated as share level all.
func PrepareConfigDirWith(def *harness.Definition, p *profile.Profile, configDir string, opts PrepareOptions) ([]Change, error) {
	level := profile.ShareAll
	upgrade := false
	if p != nil {
		level = p.ShareLevel()
		upgrade = p.ShareImplicit
	}
	steps, err := PlanWith(def, level, opts.Home, configDir, PlanOptions{FixLinks: opts.FixLinks, Relink: opts.Relink, Upgrade: upgrade})
	if err != nil {
		return nil, err
	}
	applyOpts := ApplyOptions{DryRun: opts.DryRun, Safe: true}
	if opts.Relink {
		// Only relink steps may run unattended here; any other step that replaces
		// profile data stays with edit --share.
		var relink []Step
		for i := range steps {
			if steps[i].Confirm && steps[i].Kind == StepReplaceWithLink {
				relink = append(relink, steps[i])
			}
		}
		if len(relink) > 0 && !opts.DryRun && opts.Confirm != nil && opts.Confirm(relink) {
			applyOpts = ApplyOptions{Yes: true, Backup: opts.Backup}
		}
	}
	applied, skipped, err := Apply(steps, applyOpts)
	var changes []Change
	for _, s := range applied {
		changes = append(changes, Change{Action: string(s.Kind), Path: s.Path, Note: s.Note, Step: s})
	}
	for _, s := range skipped {
		changes = append(changes, Change{Action: "needs-confirm", Path: s.Path, Note: string(s.Kind) + ": " + s.Note, Step: s})
	}
	// On error the changes made so far are returned too, so callers can report them.
	return changes, err
}

// Unclassified returns the sorted profile-relative names of the entries of
// configDir that the definition does not classify (FR-001, FR-018). Container
// directories of nested entries are descended into, glob matches and database
// companions count as classified. sync, show and doctor report these; they are never
// linked or copied. A missing config dir yields no entries.
func Unclassified(def *harness.Definition, configDir string) ([]string, error) {
	found, err := walk(def, configDir, "")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, we := range found {
		if we.Result.Unclassified {
			out = append(out, we.Rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

// stripNonSeedKeys removes credential- and state-layer keys from a JSON mixed
// file's seed content (FR-014, FR-015). Non-JSON mixed files are returned verbatim
// (no built-in has one in this slice); if the default file cannot be parsed, a
// minimal document is returned rather than risk copying credentials.
func stripNonSeedKeys(data []byte, rule *harness.EntryRule, dst string) []byte {
	if !isJSONExt(filepath.Ext(dst)) {
		return data
	}
	m, err := decodeJSONObject(data)
	if err != nil {
		return minimalDoc(dst)
	}
	for k, l := range rule.Keys {
		if l == harness.LayerCredentials || l == harness.LayerState {
			delete(m, k)
		}
	}
	out, err := encodeJSONObject(m)
	if err != nil {
		return minimalDoc(dst)
	}
	return out
}

// minimalDoc returns an empty document appropriate to the file's extension.
func minimalDoc(path string) []byte {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".jsonc":
		return []byte("{}\n")
	default:
		return []byte("")
	}
}

func isGlob(s string) bool { return strings.ContainsAny(s, "*?[") }

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
