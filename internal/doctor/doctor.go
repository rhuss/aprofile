// Package doctor inspects profiles and their config dirs for problems and returns
// structured findings. It implements the full FR-044 check set: bin dir on PATH,
// wrapper shadowing, stale/missing wrappers, missing harness binaries, unclassified
// entries, state/credentials/owned entries that are links, invalid definitions,
// references that cannot be resolved (FR-028a), the hook auth-status result, and seeded
// mixed-file keys that drifted from the default setup (FR-013). It contains no
// harness-name branching: every decision comes from the definition data (Principle I).
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/hooksets"
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

	findings = append(findings, definitionFindings(set, profiles)...)
	lib, libErrs := hooksets.LoadLibrary(paths.HooksDir())
	for _, e := range libErrs {
		findings = append(findings, Finding{Message: "invalid hook set: " + e.Error(), Fix: "correct the hook set file"})
	}

	for _, p := range profiles {
		def, ok := set.Get(p.Harness)
		if !ok {
			findings = append(findings, Finding{Profile: p.ID(), Message: fmt.Sprintf("no definition for harness %q", p.Harness), Fix: "install the harness definition"})
			continue
		}
		configDir := paths.ProfileConfigDir(p.Harness, p.Name)
		findings = append(findings, isolationFindings(def, configDir, p.ID())...)
		findings = append(findings, shareFindings(paths, def, p, lib, configDir)...)
		findings = append(findings, binaryFindings(def, p.ID())...)
		findings = append(findings, wrapperFindings(paths, def, p)...)
		findings = append(findings, referenceFindings(p)...)
		findings = append(findings, authStatusFindings(def, configDir, p)...)
	}
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
	}
	return out
}

// shareFindings reports the share-level checks of FR-020 for one profile: linked
// entries replaced by real files, diverged managed hooks, missing hook sets and
// scripts (problems); settings that differ from the default setup and $HOME
// escapes (information).
func shareFindings(paths *config.Paths, def *harness.Definition, p *profile.Profile, lib map[string]*hooksets.Set, configDir string) []Finding {
	var out []Finding
	id := p.ID()
	if entries, err := layer.Treatments(def, p, "", configDir); err == nil {
		for _, e := range entries {
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
		if d != "" && sameDir(d, binDir) {
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
// would regenerate.
func wrapperFindings(paths *config.Paths, def *harness.Definition, p *profile.Profile) []Finding {
	var out []Finding
	wrapperName := def.WrapperPrefix + "-" + p.Name
	path := filepath.Join(paths.BinDir, wrapperName)

	existing, readErr := os.ReadFile(path)
	if readErr != nil {
		out = append(out, Finding{Profile: p.ID(), Message: fmt.Sprintf("wrapper %q is missing", wrapperName), Fix: "run: aprofile sync"})
	}

	// Shadowing: a same-named executable earlier on PATH than the bin dir wins (FR-044).
	if resolved, lerr := exec.LookPath(wrapperName); lerr == nil && !sameDir(filepath.Dir(resolved), paths.BinDir) {
		out = append(out, Finding{
			Profile: p.ID(),
			Message: fmt.Sprintf("wrapper %q is shadowed by %s earlier on PATH", wrapperName, resolved),
			Fix:     "remove the shadowing executable or put aprofile's bin dir first on PATH",
		})
	}

	if readErr != nil {
		return out
	}

	// Stale: render what sync would write and compare.
	configDir := paths.ProfileConfigDir(p.Harness, p.Name)
	hookEnv, herr := harness.RunEnvHook(def, harness.HookContext{
		Harness: p.Harness, Profile: p.Name, ConfigDir: configDir,
		AuthKind: p.AuthKind, Model: p.Model, Endpoint: p.Endpoint,
	})
	if herr != nil {
		return out // the env-hook problem is surfaced by sync; do not double-report here
	}
	want, rerr := wrapper.RenderWith(p, def, configDir, wrapper.RenderOptions{HookEnv: hookEnv})
	if rerr == nil && string(existing) != want {
		out = append(out, Finding{Profile: p.ID(), Message: fmt.Sprintf("wrapper %q is stale", wrapperName), Fix: "run: aprofile sync"})
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
// are reported.
func authStatusFindings(def *harness.Definition, configDir string, p *profile.Profile) []Finding {
	st, err := harness.RunAuthStatusHook(def, harness.HookContext{
		Harness: p.Harness, Profile: p.Name, ConfigDir: configDir, AuthKind: p.AuthKind,
	})
	if err != nil {
		return []Finding{{Profile: p.ID(), Message: "auth-status hook failed: " + err.Error(), Fix: "check the aprofile-harness-" + p.Harness + " hook"}}
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

// sameDir reports whether two paths denote the same directory after cleaning and
// resolving to absolute form.
func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

// isolationFindings reports the US2 isolation problems for one profile config dir:
// unclassified entries, state/credentials entries that are links (leaks), and seeded
// mixed-file shared keys that drifted from the default setup.
func isolationFindings(def *harness.Definition, configDir, id string) []Finding {
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
				out = append(out, mixedDrift(def, rule, rel, entryPath, id)...)
				continue
			}
			// A state/owned/credentials entry must be a real per-profile entry; a
			// symlink leaks data across profiles (FR-014/015/016).
			fi, err := os.Lstat(entryPath)
			if err != nil || fi.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, _ := os.Readlink(entryPath)
			msg := fmt.Sprintf("%s entry %q is a symlink -> %s (leaks across profiles)", rule.Layer, rule.Entry, target)
			if leaksToDefault(target, defaultSetup) {
				msg = fmt.Sprintf("%s entry %q leaks to the shared default setup (%s)", rule.Layer, rule.Entry, target)
			}
			out = append(out, Finding{Profile: id, Message: msg, Fix: "run: aprofile sync --fix-links"})

		}
	}
	return out
}

// mixedDrift reports, as information, seeded keys of a mixed file declared with
// the slice one `shared` key layer that drifted from the default setup (FR-013).
func mixedDrift(def *harness.Definition, rule *harness.EntryRule, rel, entryPath, id string) []Finding {
	profileFile, err1 := os.ReadFile(entryPath)
	defaultFile, err2 := os.ReadFile(def.DefaultPath(rel))
	if err1 != nil || err2 != nil {
		return nil
	}
	drift, err := layer.MixedSharedDrift(rule, profileFile, defaultFile, strings.ToLower(filepath.Ext(rel)))
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
