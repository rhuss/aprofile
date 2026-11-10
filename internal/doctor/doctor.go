// Package doctor inspects profiles and their config dirs for problems and returns
// structured findings. It implements the full FR-044 check set: bin dir on PATH,
// wrapper shadowing, stale/missing wrappers, missing harness binaries, unclassified
// entries, state/credentials/owned entries that are links, invalid definitions,
// references that cannot be resolved (FR-028a), declared exec hooks that are not
// installed or fail, the hook auth-status result, and seeded mixed-file keys that
// drifted from the default setup (FR-013). It contains no
// harness-name branching: every decision comes from the definition data (Principle I).
package doctor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/history"
	"github.com/cc-deck/aprofile/internal/hooksets"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// Severities (feature 002, FR-020): only problems make doctor exit non-zero.
const (
	SeverityProblem = "problem"
	SeverityInfo    = "info"
)

// Finding is one reported condition. Profile is the "<harness>/<name>" it concerns
// (empty for installation-wide findings); Fix names a remedy; Severity is problem
// or info.
type Finding struct {
	Profile  string `json:"profile,omitempty"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

// HasProblems reports whether any finding has problem severity (FR-020).
func HasProblems(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity != SeverityInfo {
			return true
		}
	}
	return false
}

// Problems counts the problem findings.
func Problems(findings []Finding) int {
	n := 0
	for _, f := range findings {
		if f.Severity != SeverityInfo {
			n++
		}
	}
	return n
}

// Run collects findings for every profile in the store plus installation-wide findings
// (invalid definitions, the bin dir not on PATH). A profile that fails to load or has
// no definition is itself a finding; the others are still checked.
func Run(paths *config.Paths, set *harness.Set, store *profile.Store) ([]Finding, error) {
	var findings []Finding

	for _, e := range set.Errors() {
		findings = append(findings, Finding{Message: "invalid harness definition: " + e.Error(), Fix: "correct the definition file"})
	}

	// Installation-wide: the bin dir must be on PATH for wrappers to be found (FR-044).
	findings = append(findings, pathFindings(paths.BinDir)...)

	profiles, failures, err := store.List()
	if err != nil {
		return nil, err
	}
	for _, f := range failures {
		findings = append(findings, Finding{Profile: f.ID(), Message: "profile failed to load: " + f.Err.Error(), Fix: "fix the profile file or remove it"})
	}

	findings = append(findings, leftoverFindings(paths.BinDir, set, profiles, failures)...)
	findings = append(findings, definitionFindings(set, profiles)...)
	findings = append(findings, missingHookFindings(set, profiles)...)
	lib, libErrs := hooksets.LoadLibrary(paths.HooksDir())
	for _, e := range libErrs {
		findings = append(findings, Finding{Message: "invalid hook set: " + e.Error(), Fix: "correct the hook set file"})
	}

	groups := history.NewManager(paths, set, store)
	for _, p := range profiles {
		def, ok := set.Get(p.Harness)
		if !ok {
			findings = append(findings, Finding{Profile: p.ID(), Message: fmt.Sprintf("no definition for harness %q", p.Harness), Fix: "install the harness definition"})
			continue
		}
		configDir := paths.ProfileConfigDir(p.Harness, p.Name)
		// The profile's history store (spec 003): nil for an isolated profile or one
		// whose group does not exist (reported by the group checks).
		var hs layer.HistoryStore
		st, gerr := groups.StoreFor(def, p)
		switch {
		case gerr != nil:
			findings = append(findings, Finding{Profile: p.ID(), Message: gerr.Error() + "; sync leaves its history entries unchanged",
				Fix: "aprofile edit " + p.ID() + " --group <group> (an existing group), or aprofile edit " + p.ID() + " --no-group"})
		case st != nil:
			hs = st
		}
		findings = append(findings, fitFindings(def, p)...)
		findings = append(findings, isolationFindings(def, configDir, p.ID(), p.ShareLevel() != profile.ShareNone, hs)...)
		findings = append(findings, shareFindings(paths, def, p, lib, configDir, hs)...)
		findings = append(findings, launchFindings(paths, def, p, configDir)...)
		findings = append(findings, setAsideFindings(paths, p)...)
		findings = append(findings, binaryFindings(def, p.ID())...)
		findings = append(findings, wrapperFindings(paths, def, p)...)
		findings = append(findings, referenceFindings(p)...)
		findings = append(findings, authStatusFindings(def, configDir, p)...)
	}
	findings = append(findings, groupFindings(groups, set)...)
	for i := range findings {
		if findings[i].Severity == "" {
			findings[i].Severity = SeverityProblem
		}
	}
	return findings, nil
}

// definitionFindings reports definition-level conditions once per harness: user
// rules with the deprecated shared alias or a borrowable catch-all (problems), and
// default-setup entries no rule covers (information, FR-001a), for harnesses that
// have a profile borrowing from the default setup.
func definitionFindings(set *harness.Set, profiles []*profile.Profile) []Finding {
	var out []Finding
	borrowing := map[string]bool{}
	for _, p := range profiles {
		if p.ShareLevel() != profile.ShareNone {
			borrowing[p.Harness] = true
		}
	}
	for _, d := range set.All() {
		for _, r := range d.Classification {
			if r.CompanionOf != "" {
				continue
			}
			if r.Deprecated {
				out = append(out, Finding{Message: fmt.Sprintf("%s: rule %q in %s uses the deprecated layer shared (read as content)", d.Name, r.Entry, r.Origin),
					Fix: "change it to content, settings or plugin-store"})
			}
			if r.IsGlob() && r.Layer.Borrowable() {
				out = append(out, Finding{Message: fmt.Sprintf("%s: rule %q in %s is a catch-all: every unknown entry is linked to the default setup", d.Name, r.Entry, r.Origin),
					Fix: "classify entries one by one (aprofile harness classify) and remove the catch-all"})
			}
		}
		if !borrowing[d.Name] {
			continue
		}
		if unb, err := layer.UnborrowedDefaultEntries(d); err == nil && len(unb) > 0 {
			out = append(out, Finding{Severity: SeverityInfo,
				Message: fmt.Sprintf("%s: default setup entries not borrowed by any profile because they are unclassified: %s", d.Name, strings.Join(unb, ", ")),
				Fix:     fmt.Sprintf("aprofile harness classify %s <entry> --as content (or settings, state, ...)", d.Name)})
		}
		out = append(out, undeclaredKeyFindings(d)...)
	}
	return out
}

// undeclaredKeyFindings reports, once per harness and as information, the top-level
// keys of a mixed file in the default setup that the definition does not declare
// (FR-013). They are seeded into new profiles at all and content, except keys whose
// name looks like a credential. Only key names are printed, never values.
func undeclaredKeyFindings(d *harness.Definition) []Finding {
	und, err := layer.UndeclaredDefaultKeys(d)
	if err != nil {
		return nil
	}
	var out []Finding
	for _, u := range und {
		var parts []string
		if len(u.Seeded) > 0 {
			parts = append(parts, "seeded into new profiles: "+strings.Join(u.Seeded, ", "))
		}
		if len(u.NotSeeded) > 0 {
			parts = append(parts, "not seeded because the name looks like a credential: "+strings.Join(u.NotSeeded, ", "))
		}
		out = append(out, Finding{Severity: SeverityInfo,
			Message: fmt.Sprintf("%s: keys of %s in the default setup are not declared in the definition (%s)", d.Name, u.Entry, strings.Join(parts, "; ")),
			Fix:     fmt.Sprintf("declare each key under keys: of the %s rule in a harnesses.d override for %s (credentials or state keeps it private, settings seeds it)", u.Entry, d.Name)})
	}
	return out
}

// shareFindings reports the share-level checks of FR-020 for one profile: linked
// entries replaced by real files, diverged managed hooks, missing hook sets and
// scripts (problems); settings that differ from the default setup and $HOME
// escapes (information).
func shareFindings(paths *config.Paths, def *harness.Definition, p *profile.Profile, lib map[string]*hooksets.Set, configDir string, hs layer.HistoryStore) []Finding {
	var out []Finding
	id := p.ID()
	if entries, err := layer.TreatmentsWithPlaced(def, p, "", configDir, hs, launch.LoadPlaced(paths.ManagedLaunchPath(p.Harness, p.Name))); err == nil {
		for _, e := range entries {
			if hs != nil && e.Treatment == layer.TreatmentLinked && strings.HasPrefix(e.Reason, "history group ") {
				// A member's history entry (spec 003, FR-021).
				switch e.Status {
				case layer.StatusLinkReplacedByFile:
					out = append(out, Finding{Profile: id, Message: fmt.Sprintf("history entry %s is a real entry instead of a link to the history group %s store: the profile started its own history again", e.Entry, hs.Name()),
						Fix: fmt.Sprintf("aprofile sync --relink (backs it up and asks first), or merge it with: aprofile edit %s --group %s", id, hs.Name())})
				case layer.StatusWrongHistoryLink:
					out = append(out, Finding{Profile: id, Message: fmt.Sprintf("history entry %s links somewhere other than the history group %s store (%s)", e.Entry, hs.Name(), hs.EntryPath(e.Entry)),
						Fix: "aprofile sync (relinks it)"})
				}
				continue
			}
			switch e.Status {
			case layer.StatusLinkReplacedByFile:
				out = append(out, Finding{Profile: id, Message: fmt.Sprintf("the profile stopped sharing %s: the link was replaced by a real file", e.Entry),
					Fix: "aprofile sync --relink (backs the file up and asks first)"})
			case layer.StatusKeptFromUpgrade:
				out = append(out, Finding{Profile: id, Severity: SeverityInfo,
					Message: fmt.Sprintf("%s is the profile's own from before share levels and is kept, not shared", e.Entry),
					Fix:     "keep it, or share the default setup's with: aprofile sync --relink (backs it up and asks first)"})
			case layer.StatusLeakingLink:
				if isGlobMatch(def, e.Entry) {
					out = append(out, Finding{Profile: id, Message: fmt.Sprintf("%s entry %q is a link (leaks across profiles)", e.Category, e.Entry), Fix: "run: aprofile sync --fix-links"})
				}
			}
		}
		if diff := layer.SettingsDiffer(entries); len(diff) > 0 && p.ShareLevel() == profile.ShareContent {
			out = append(out, Finding{Profile: id, Severity: SeverityInfo,
				Message: fmt.Sprintf("settings differ from the default setup: %s", strings.Join(diff, ", ")),
				Fix:     "aprofile settings pull " + id + " --dry-run (to refresh)"})
		}
	}
	for _, he := range def.HomeEscapes {
		out = append(out, Finding{Profile: id, Severity: SeverityInfo,
			Message: fmt.Sprintf("%s (%s) is read from $HOME: shared with every profile; no share level can separate this", he.Path, he.What)})
	}
	if p.ShareLevel() == profile.ShareAll && len(p.HookSets) > 0 {
		// Only a hand-edited file gets here: attaching a set to an all profile fails
		// (FR-027). The sets have no effect, so say so instead of staying silent.
		out = append(out, Finding{Profile: id, Severity: SeverityInfo,
			Message: fmt.Sprintf("hook sets %s are attached but ignored: share level all uses the hooks of the default setup", strings.Join(p.HookSets, ", ")),
			Fix:     "aprofile edit " + id + " --share content (to use them), or remove hook_sets from the profile file"})
	}
	if def.Hooks == nil || p.ShareLevel() == profile.ShareAll {
		return out
	}
	rec, err := hooksets.LoadRecord(paths.ManagedHooksPath(p.Harness, p.Name))
	if err != nil {
		return append(out, Finding{Profile: id, Message: err.Error(), Fix: "remove the record file; the next sync rewrites it"})
	}
	hp, err := hooksets.PlanHooks(def, p, lib, rec, configDir, hooksets.PlanOptions{})
	if err != nil {
		return append(out, Finding{Profile: id, Message: "hook sets: " + err.Error(), Fix: "correct the file"})
	}
	for _, m := range hp.MissingSets {
		out = append(out, Finding{Profile: id, Message: fmt.Sprintf("hook set %q is referenced but missing from %s", m, paths.HooksDir()),
			Fix: "restore the set, or remove it with: aprofile edit " + id + " --hook-sets <others>"})
	}
	for _, m := range hp.InvalidSets {
		out = append(out, Finding{Profile: id, Message: m, Fix: "correct the hook set's body for this harness"})
	}
	for _, m := range hp.MissingScripts {
		out = append(out, Finding{Profile: id, Message: m, Fix: "create the script or correct the hook set"})
	}
	for _, d := range hp.Divergences {
		out = append(out, Finding{Profile: id, Message: d.String(), Fix: "aprofile hook-sets reapply " + id + " --dry-run"})
	}
	return out
}

// isGlobMatch reports whether an entry is classified by a glob rule (named rules
// are covered by isolationFindings).
func isGlobMatch(def *harness.Definition, entry string) bool {
	r := layer.Classify(def, entry)
	return r.Rule != nil && r.Rule.IsGlob()
}

// pathFindings reports when aprofile's bin dir is not on the current PATH, so wrappers
// would not be found in new shells (FR-044).
func pathFindings(binDir string) []Finding {
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d != "" && wrapper.SameDir(d, binDir) {
			return nil
		}
	}
	return []Finding{{
		Message: fmt.Sprintf("bin dir %s is not on PATH; wrappers will not be found", binDir),
		Fix:     "run: aprofile sync (it adds a PATH block), or add the dir to PATH by hand",
	}}
}

// binaryFindings reports a missing harness binary (FR-044): the wrapper would print a
// clear error when started, but doctor surfaces it ahead of time.
func binaryFindings(def *harness.Definition, id string) []Finding {
	if _, err := exec.LookPath(def.Binary); err != nil {
		return []Finding{{
			Profile: id,
			Message: fmt.Sprintf("harness binary %q is not installed", def.Binary),
			Fix:     "install the harness, or the wrapper will fail when started",
		}}
	}
	return nil
}

// wrapperFindings reports a missing or stale wrapper and wrapper shadowing (FR-044,
// FR-006b). It renders what sync would write (weaving in the exec-hook env output, as
// sync does) and compares it to the on-disk wrapper, so "stale" here matches what sync
// would regenerate. A failing env operation is reported with the hook's error; a hook
// that is not installed is reported once per harness by missingHookFindings.
func wrapperFindings(paths *config.Paths, def *harness.Definition, p *profile.Profile) []Finding {
	var out []Finding
	wrapperName := def.WrapperName(p.Name)
	path := filepath.Join(paths.BinDir, wrapperName)

	existing, readErr := os.ReadFile(path)
	if readErr != nil {
		out = append(out, Finding{Profile: p.ID(), Message: fmt.Sprintf("wrapper %q is missing", wrapperName), Fix: "run: aprofile sync"})
	}

	// Shadowing: a same-named executable earlier on PATH than the bin dir wins (FR-044).
	if shadow, ok := wrapper.ShadowedBy(paths.BinDir, wrapperName); ok {
		out = append(out, Finding{
			Profile: p.ID(),
			Message: wrapper.ShadowMessage(wrapperName, shadow),
			Fix:     wrapper.ShadowFix,
		})
	}

	if readErr != nil {
		return out
	}

	// A profile that does not fit its definition is not rendered by sync, so its
	// wrapper is not stale in a way sync fixes; fitFindings names the fix.
	if wrapper.AuthKindIssue(p, def) != nil {
		return out
	}

	// Stale: render what sync would write and compare.
	want, err := wrapper.Expected(p, def, paths.ProfileConfigDir(p.Harness, p.Name))
	switch {
	case errors.Is(err, harness.ErrHookNotFound):
		// reported once per harness by missingHookFindings
	case err != nil:
		out = append(out, Finding{Profile: p.ID(),
			Message: fmt.Sprintf("cannot tell whether wrapper %q is stale: %v", wrapperName, err),
			Fix:     hookFix(def, "env")})
	case string(existing) != want:
		out = append(out, Finding{Profile: p.ID(), Message: fmt.Sprintf("wrapper %q is stale", wrapperName), Fix: "run: aprofile sync"})
	}
	return out
}

// fitFindings reports, as problems, where a hand-edited profile does not fit its
// definition (FR-006): an auth kind the definition does not declare (sync refuses
// the profile) and a model or endpoint the harness has no variable for (the wrapper
// drops it).
func fitFindings(def *harness.Definition, p *profile.Profile) []Finding {
	var out []Finding
	for _, fi := range wrapper.FitIssues(p, def) {
		out = append(out, Finding{Profile: p.ID(), Message: fi.Message, Fix: fi.Fix})
	}
	return out
}

// leftoverFindings reports every aprofile-marked wrapper in the bin dir that belongs
// to no existing profile (FR-006b): remove and rename leave the old wrapper until
// the next sync removes it. Files without the marker are never reported.
func leftoverFindings(binDir string, set *harness.Set, profiles []*profile.Profile, failures []profile.LoadFailure) []Finding {
	keys := make([]harness.ProfileKey, 0, len(profiles)+len(failures))
	for _, p := range profiles {
		keys = append(keys, harness.ProfileKey{Harness: p.Harness, Name: p.Name})
	}
	for _, f := range failures {
		keys = append(keys, harness.ProfileKey{Harness: f.Harness, Name: f.Name})
	}
	names, err := wrapper.LeftoversFor(binDir, set, keys)
	if err != nil {
		return []Finding{{Message: fmt.Sprintf("cannot read bin dir %s: %v", binDir, err), Fix: "check the directory's permissions"}}
	}
	var out []Finding
	for _, n := range names {
		out = append(out, Finding{
			Message: fmt.Sprintf("leftover wrapper %s in %s belongs to no profile (left by remove or rename)", n, binDir),
			Fix:     "aprofile sync",
		})
	}
	return out
}

// hookFix names how to look into a failing exec-hook operation.
func hookFix(def *harness.Definition, op string) string {
	bin := harness.HookExecutable(def.Name)
	return fmt.Sprintf("check %s: run it by hand (printf 'harness=%s\\n' | %s %s), fix it, or remove %s from exec_hook.operations in a harnesses.d override",
		bin, def.Name, bin, op, op)
}

// missingHookFindings reports, once per harness that has profiles, a declared exec
// hook whose executable is not on PATH (FR-011, FR-044): sync cannot render the
// wrapper (env), the wrapper refuses to start (pre-launch), the config is not
// written (write-config), and show and doctor cannot tell the login state
// (auth-status). A login falls back to login_command.
func missingHookFindings(set *harness.Set, profiles []*profile.Profile) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, p := range profiles {
		def, ok := set.Get(p.Harness)
		if !ok || seen[def.Name] || def.ExecHook == nil || len(def.ExecHook.Operations) == 0 {
			continue
		}
		seen[def.Name] = true
		bin := harness.HookExecutable(def.Name)
		if _, err := exec.LookPath(bin); err == nil {
			continue
		}
		ops := def.ExecHook.Operations
		out = append(out, Finding{
			Message: fmt.Sprintf("%s: exec hook %s is not on PATH; the definition declares its operations %s",
				def.Name, bin, strings.Join(ops, ", ")),
			Fix: fmt.Sprintf("install %s on PATH, or remove the operations from exec_hook.operations in a harnesses.d override for %s", bin, def.Name),
		})
	}
	return out
}

// referenceFindings reports secret references that cannot be resolved now (FR-028a,
// FR-044): an env variable that is unset/empty or a file that is missing, unreadable or
// empty. It reports only the reference text and the reason, never a resolved value
// (FR-028, SC-006).
func referenceFindings(p *profile.Profile) []Finding {
	var out []Finding
	for v, ref := range p.AuthRefs {
		if _, err := secret.Resolve(ref); err != nil {
			out = append(out, Finding{
				Profile: p.ID(),
				Message: fmt.Sprintf("auth variable %s: %v", v, err),
				Fix:     "set the environment variable or file, or correct the reference",
			})
		}
	}
	for _, e := range p.ExtraEnv {
		if e.Literal || !secret.IsReference(e.Value) {
			continue
		}
		ref, err := secret.ParseReference(e.Value)
		if err != nil {
			continue // already reported by profile validation on load
		}
		if _, err := secret.Resolve(ref); err != nil {
			out = append(out, Finding{
				Profile: p.ID(),
				Message: fmt.Sprintf("extra env %s: %v", e.Name, err),
				Fix:     "set the environment variable or file, or correct the reference",
			})
		}
	}
	return out
}

// authStatusFindings reports the exec-hook auth-status result where the hook implements
// it (FR-011, FR-044). status=ok yields no finding; missing/unknown and any hook failure
// are reported. A hook that is not installed is reported once per harness by
// missingHookFindings.
func authStatusFindings(def *harness.Definition, configDir string, p *profile.Profile) []Finding {
	st, err := harness.RunAuthStatusHook(def, wrapper.HookContext(p, configDir))
	if err != nil {
		return []Finding{{Profile: p.ID(), Message: "auth-status hook failed: " + err.Error(), Fix: hookFix(def, "auth-status")}}
	}
	switch st.Status {
	case "missing":
		return []Finding{{Profile: p.ID(), Message: "auth-status: credentials missing" + detailSuffix(st.Detail), Fix: "run: aprofile login " + p.ID()}}
	case "unknown":
		return []Finding{{Profile: p.ID(), Message: "auth-status: unknown" + detailSuffix(st.Detail), Fix: "verify the profile's login"}}
	default:
		return nil
	}
}

func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

// isolationFindings reports the US2 isolation problems for one profile config dir:
// unclassified entries, state/credentials entries that are links (leaks), and, for a
// profile whose mixed files were seeded (share level all or content), seeded
// mixed-file keys that drifted from the default setup.
//
// hs is the profile's history group store (nil when isolated): a member's history
// entry that links to its store path is correct, not a leak (spec 003, research R7).
func isolationFindings(def *harness.Definition, configDir, id string, seeded bool, hs layer.HistoryStore) []Finding {
	var out []Finding
	defaultSetup := harness.ExpandHome(def.DefaultConfigLocation)

	// Unclassified entries (FR-018).
	if unc, err := layer.Unclassified(def, configDir); err == nil {
		for _, name := range unc {
			out = append(out, Finding{
				Profile: id,
				Message: fmt.Sprintf("unclassified config-dir entry %q", name),
				Fix:     "classify it in the harness definition, or remove it from the config dir",
			})
		}
	}

	for i := range def.Classification {
		rule := &def.Classification[i]
		if strings.ContainsAny(rule.Entry, "*?[") {
			continue
		}
		rel := strings.TrimSuffix(rule.Entry, "/")
		entryPath := filepath.Join(configDir, filepath.FromSlash(rel))

		switch rule.Layer {
		case harness.LayerState, harness.LayerCredentials, harness.LayerOwned, harness.LayerMixed:
			if rule.Layer == harness.LayerMixed {
				if fi, err := os.Lstat(entryPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
					target, _ := os.Readlink(entryPath)
					out = append(out, Finding{Profile: id, Message: fmt.Sprintf("mixed entry %q is a symlink -> %s (leaks across profiles)", rule.Entry, target), Fix: "run: aprofile sync --fix-links"})
					continue
				}
				if seeded {
					out = append(out, mixedDrift(def, rule, rel, entryPath, id)...)
				}
				continue
			}
			// A state/owned/credentials entry must be a real per-profile entry; a
			// symlink leaks data across profiles (FR-014/015/016).
			fi, err := os.Lstat(entryPath)
			if err != nil || fi.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, _ := os.Readlink(entryPath)
			if hs != nil && rule.History {
				// A member's history link: shareFindings reports a link to the wrong
				// place with the history-specific fix (plain sync relinks it).
				continue
			}
			msg := fmt.Sprintf("%s entry %q is a symlink -> %s (leaks across profiles)", rule.Layer, rule.Entry, target)
			if leaksToDefault(target, defaultSetup) || leaksToDefault(target, def.DefaultPath(rel)) {
				msg = fmt.Sprintf("%s entry %q leaks to the shared default setup (%s)", rule.Layer, rule.Entry, target)
			}
			out = append(out, Finding{Profile: id, Message: msg, Fix: "run: aprofile sync --fix-links"})

		}
	}
	return out
}

// mixedDrift reports, as information, every seeded key of a mixed file (not owned,
// credentials or state) whose value differs from the default setup (FR-013, FR-044).
func mixedDrift(def *harness.Definition, rule *harness.EntryRule, rel, entryPath, id string) []Finding {
	profileFile, err1 := os.ReadFile(entryPath)
	defaultFile, err2 := os.ReadFile(def.DefaultPath(rel))
	if err1 != nil || err2 != nil {
		return nil
	}
	drift, err := layer.MixedSeededDrift(rule, profileFile, defaultFile, strings.ToLower(filepath.Ext(rel)))
	if err != nil {
		return nil
	}
	var out []Finding
	for _, key := range drift {
		out = append(out, Finding{
			Severity: SeverityInfo,
			Profile:  id,
			Message:  fmt.Sprintf("mixed file %q seeded key %q differs from the default setup", rule.Entry, key),
			Fix:      "reconcile the key with the default setup or accept the profile's value",
		})
	}
	return out
}

// leaksToDefault reports whether a symlink target points into the default setup.
func leaksToDefault(target, defaultSetup string) bool {
	if target == "" || defaultSetup == "" {
		return false
	}
	t := filepath.Clean(target)
	d := filepath.Clean(defaultSetup)
	return t == d || strings.HasPrefix(t, d+string(filepath.Separator))
}

// groupFindings reports the installation-wide history group conditions (spec 003,
// FR-021): a missing store of a non-home group (problem) and kept stores of
// deleted groups (information).
func groupFindings(groups *history.Manager, set *harness.Set) []Finding {
	var out []Finding
	for _, def := range set.All() {
		if !def.SupportsGroups() {
			continue
		}
		list, err := groups.List(def.Name)
		if err != nil {
			continue
		}
		for _, g := range list {
			if g.Builtin {
				continue
			}
			if _, err := os.Stat(g.Store); os.IsNotExist(err) {
				out = append(out, Finding{Message: fmt.Sprintf("history group %s: its store %s is missing; the history it held is gone", g.ID(), g.Store),
					Fix: "aprofile sync (recreates the store's entries empty)"})
			}
		}
		for _, kept := range groups.KeptStores(def.Name) {
			out = append(out, Finding{Severity: SeverityInfo, Message: fmt.Sprintf("kept store of a deleted history group of %s: %s", def.Name, kept),
				Fix: "move or remove it by hand when you no longer need its history"})
		}
	}
	return out
}

// setAsideFindings lists a profile's set-aside history as information (FR-021).
func setAsideFindings(paths *config.Paths, p *profile.Profile) []Finding {
	runs, err := history.ListSetAside(paths, p.Harness, p.Name)
	if err != nil {
		return nil
	}
	var out []Finding
	for _, r := range runs {
		out = append(out, Finding{Profile: p.ID(), Severity: SeverityInfo,
			Message: fmt.Sprintf("set-aside history from a history group join: %s (%d items); the harness never reads it", r.Dir, len(r.Items)),
			Fix:     "move items back by hand if you need them; aprofile remove --purge deletes it with the profile"})
	}
	return out
}

// launchFindings reports the profile folder's launch settings (spec 004 FR-020 to
// FR-020b): as information whether it is self-contained and each wrapper-only
// setting with its reason; as problems a stale folder and launch entries aprofile
// did not place. Names only, never values.
func launchFindings(paths *config.Paths, def *harness.Definition, p *profile.Profile, configDir string) []Finding {
	id := p.ID()
	st, err := launch.Status(p, def, configDir, paths.ManagedLaunchPath(p.Harness, p.Name))
	if err != nil {
		return []Finding{{Profile: id, Message: fmt.Sprintf("cannot tell whether the profile folder is current: %v", err),
			Fix: "fix the file, then run: aprofile sync"}}
	}
	f := st.Folder()
	out := []Finding{{Profile: id, Severity: SeverityInfo, Message: "profile folder: " + f.StatusText(),
		Fix: "details: aprofile show " + id}}
	for _, w := range f.WrapperOnly {
		out = append(out, Finding{Profile: id, Severity: SeverityInfo,
			Message: fmt.Sprintf("%s is wrapper-only: %s", w.SettingName(), launch.ReasonText(w.Reason, p, def)),
			Fix:     "start the profile through its wrapper " + def.WrapperName(p.Name) + " to apply it"})
	}
	if st.Sync == launch.SyncStale {
		out = append(out, Finding{Profile: id, Message: "profile folder is stale", Fix: "run: aprofile sync"})
	}
	for _, e := range st.Unplaced {
		msg, fix, _ := st.UnplacedText(e)
		out = append(out, Finding{Profile: id, Message: msg, Fix: fix})
	}
	return out
}
