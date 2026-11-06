package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/mcp"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/tui"
)

// copyReportJSON is the --json schema for copy (FR-041), the full ConversionReport
// (data-model): MCP conversion plus skills, plugins, dropped settings and the
// same-harness secret warnings (FR-033a).
type copyReportJSON struct {
	FormatVersion   int                  `json:"format_version"`
	Source          string               `json:"source"`
	Target          string               `json:"target"`
	Mode            string               `json:"mode"` // same-harness | cross-harness
	MCP             mcp.ConversionReport `json:"mcp"`
	SkillsCarried   bool                 `json:"skills_carried"`
	SkillsReport    []string             `json:"skills_report,omitempty"`
	DroppedSettings []string             `json:"dropped_settings,omitempty"`
	PluginsSkipped  []string             `json:"plugins_skipped,omitempty"`
	SecretWarnings  []mcp.SecretWarning  `json:"secret_warnings,omitempty"`
	// Feature 002 (FR-007a).
	Share            string   `json:"share"`
	SettingsCarried  []string `json:"settings_carried,omitempty"`
	HookSetsCarried  []string `json:"hook_sets_carried,omitempty"`
	HookSetsLeftOut  []string `json:"hook_sets_left_out,omitempty"`
	SettingsWarnings []string `json:"settings_warnings,omitempty"`
}

type copyFlags struct {
	overwrite bool
	auth      string
	label     string
	color     string
	icon      string
	asJSON    bool
	// Feature 002.
	share           string
	hookSets        []string
	excludeHookSets []string
	setHookSets     bool
	setExclude      bool
}

// copyFlagsFromForm maps the copy form's values onto the copy flags, the same path
// the command uses (Constitution VI).
func copyFlagsFromForm(v tui.CopyValues) *copyFlags {
	return &copyFlags{
		overwrite:       v.Overwrite,
		auth:            v.Auth,
		label:           v.Label,
		color:           v.Color,
		icon:            v.Icon,
		share:           v.Share,
		hookSets:        v.HookSets,
		excludeHookSets: v.ExcludeHookSets,
		setHookSets:     len(v.HookSets) > 0,
		setExclude:      len(v.ExcludeHookSets) > 0,
	}
}

func newCopyCmd() *cobra.Command {
	f := &copyFlags{}
	cmd := &cobra.Command{
		Use:   "copy <harness>/<name> <harness>/<name>",
		Short: "Copy a profile within or across harnesses (does not write wrappers; run sync after)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.setHookSets = cmd.Flags().Changed("hook-sets")
			f.setExclude = cmd.Flags().Changed("exclude-hook-sets")
			return runCopy(cmd, args[0], args[1], f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.share, "share", "", "share level of the target (default: the source's); "+shareFlagHelp())
	fl.StringSliceVar(&f.hookSets, "hook-sets", nil, "hook sets of the target (default: the source's, where the target harness has a body)")
	fl.StringSliceVar(&f.excludeHookSets, "exclude-hook-sets", nil, "every-profile hook sets the target skips (default: the source's)")
	fl.BoolVar(&f.overwrite, "overwrite", false, "replace only what the copy writes if the target exists")
	fl.StringVar(&f.auth, "auth", "", "auth kind for a cross-harness target (defaults to the target's default)")
	fl.StringVar(&f.label, "label", "", "override the carried label")
	fl.StringVar(&f.color, "color", "", "override the carried color")
	fl.StringVar(&f.icon, "icon", "", "override the carried icon")
	fl.BoolVar(&f.asJSON, "json", false, "emit a machine-readable ConversionReport")
	return cmd
}

func runCopy(cmd *cobra.Command, srcRef, dstRef string, f *copyFlags) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	sh, sn, err := parseProfileRef(srcRef)
	if err != nil {
		return err
	}
	dh, dn, err := parseProfileRef(dstRef)
	if err != nil {
		return err
	}
	if !harness.NameValid(dn) {
		return fmt.Errorf("target name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", dn)
	}
	if sh == dh && sn == dn {
		return fmt.Errorf("source and target are the same profile")
	}

	src, err := a.store.Load(sh, sn)
	if err != nil {
		return err
	}
	srcDef, ok := a.set.Get(sh)
	if !ok {
		return fmt.Errorf("unknown harness %q", sh)
	}
	dstDef, ok := a.set.Get(dh)
	if !ok {
		return fmt.Errorf("unknown harness %q", dh)
	}
	if err := validateColor(f.color); err != nil {
		return err
	}
	if err := validateIcon(f.icon); err != nil {
		return err
	}

	// Target existence (FR-033): fail without --overwrite.
	existingTarget, loadErr := a.store.Load(dh, dn)
	targetExists := loadErr == nil
	if targetExists && !f.overwrite {
		return fmt.Errorf("target profile %s/%s already exists; pass --overwrite to replace what the copy writes", dh, dn)
	}

	srcConfigDir := a.paths.ProfileConfigDir(sh, sn)
	dstConfigDir := a.paths.ProfileConfigDir(dh, dn)

	report := copyReportJSON{FormatVersion: schemaVersion, Source: src.ID(), Target: dh + "/" + dn}

	// Build the target profile (settings), preserving target-only fields on overwrite.
	var target *profile.Profile
	if targetExists {
		target = existingTarget
	} else {
		target = &profile.Profile{Harness: dh, Name: dn}
	}

	// Share level (FR-007a): --share, else the source's.
	level := src.ShareLevel()
	if f.share != "" {
		if level, err = profile.ParseShare(f.share); err != nil {
			return err
		}
	}
	if targetExists && existingTarget.ShareLevel() != level {
		// A level change goes through edit --share, which plans it, refuses while
		// the profile runs and confirms replacements (FR-017).
		return exitErrorf(ExitInvalidForLevel, "target %s uses share level %s; change it first with: aprofile edit %s --share %s, then copy again",
			existingTarget.ID(), existingTarget.ShareLevel(), existingTarget.ID(), level)
	}
	target.Share = level
	report.Share = string(level)

	// Hook sets travel with the copy when the target harness has a body for them
	// (FR-007a); explicit flags replace them.
	carried, leftOut := a.carryHookSets(src, dstDef, level, f)
	target.HookSets, target.ExcludeHookSets = carried.named, carried.excluded
	report.HookSetsCarried = append(append([]string(nil), carried.named...), carried.excluded...)
	report.HookSetsLeftOut = leftOut
	if err := a.validateHookSets(target, dstDef); err != nil {
		return err
	}

	// When source and target both have their own settings, the target's settings
	// start as a copy of the source's (hook location excluded). This runs before the
	// config dir is prepared, so preparation copies from the default setup only what
	// is still absent.
	if sh == dh && src.ShareLevel() != profile.ShareAll && level != profile.ShareAll {
		names, warnings, err := carrySettings(srcDef, srcConfigDir, dstConfigDir)
		if err != nil {
			return err
		}
		report.SettingsCarried = names
		report.SettingsWarnings = warnings
	}

	// Prepare the target config dir for its level: empty credentials + state, owned
	// dirs, seeded mixed files, links or own copies per category (idempotent; never
	// touches existing credentials/state on overwrite).
	if _, err := layer.PrepareConfigDir(dstDef, target, dstConfigDir, false); err != nil {
		return fmt.Errorf("prepare target config dir: %w", err)
	}

	if sh == dh {
		report.Mode = "same-harness"
		applySameHarnessSettings(target, src, f)
		warnings, err := carryOwnedLayer(dstDef, srcConfigDir, dstConfigDir)
		if err != nil {
			return err
		}
		report.SecretWarnings = warnings
		// The whole owned layer (including the MCP file) is carried verbatim;
		// record the carried servers in the MCP report for symmetry.
		if names, err := mcpServerNames(dstDef, dstConfigDir); err == nil {
			report.MCP.Converted = names
		}
		carriedSkills, skillsReport := carrySkills(srcDef, dstDef, level, srcConfigDir, dstConfigDir)
		report.SkillsCarried = carriedSkills
		report.SkillsReport = skillsReport
	} else {
		report.Mode = "cross-harness"
		if err := applyCrossHarnessSettings(target, src, dstDef, f, targetExists); err != nil {
			return err
		}
		report.DroppedSettings = droppedCrossSettings(src)
		// MCP conversion through the neutral record.
		mcpRep, err := convertMCP(srcDef, dstDef, srcConfigDir, dstConfigDir, f.overwrite)
		if err != nil {
			return err
		}
		report.MCP = mcpRep
		// Skills (FR-036).
		carriedSkills, skillsReport := carrySkills(srcDef, dstDef, level, srcConfigDir, dstConfigDir)
		report.SkillsCarried = carriedSkills
		report.SkillsReport = skillsReport
		// Plugins are never carried across harnesses (FR-036).
		if srcDef.PluginMechanism != "" {
			report.PluginsSkipped = []string{"plugins are not carried across harnesses (source uses: " + srcDef.PluginMechanism + ")"}
		}
	}

	if err := a.store.Save(target); err != nil {
		return err
	}

	if f.asJSON {
		return writeJSON(a.out, report)
	}
	printCopyReport(a, report)
	return nil
}

// applySameHarnessSettings carries every setting except the primary flag (FR-033a),
// honoring label/color/icon/auth overrides.
func applySameHarnessSettings(target, src *profile.Profile, f *copyFlags) {
	target.Label = pick(f.label, src.Label)
	target.Color = pick(f.color, src.Color)
	target.Icon = pick(f.icon, src.Icon)
	target.AuthKind = src.AuthKind
	target.AuthRefs = cloneRefs(src.AuthRefs)
	target.Model = src.Model
	target.Endpoint = src.Endpoint
	target.ExtraEnv = cloneExtra(src.ExtraEnv)
	target.HerdrAgent = src.HerdrAgent
	// Primary is not carried; an overwritten target keeps its own primary flag.
}

// applyCrossHarnessSettings carries label/color/icon only and drops auth/model/endpoint
// /extra (FR-033b). A new target gets the target default auth kind unless --auth; an
// overwritten target keeps its existing auth/model/endpoint/extra unless --auth is set.
func applyCrossHarnessSettings(target, src *profile.Profile, dstDef *harness.Definition, f *copyFlags, targetExists bool) error {
	target.Label = pick(f.label, src.Label)
	target.Color = pick(f.color, src.Color)
	target.Icon = pick(f.icon, src.Icon)
	switch {
	case f.auth != "":
		if _, ok := dstDef.AuthKind(f.auth); !ok {
			return fmt.Errorf("harness %q has no auth kind %q", dstDef.Name, f.auth)
		}
		target.AuthKind = f.auth
		target.AuthRefs = nil
	case !targetExists:
		target.AuthKind = dstDef.DefaultAuthKind
		target.AuthRefs = nil
		target.Model = ""
		target.Endpoint = ""
		target.ExtraEnv = nil
	}
	return nil
}

func droppedCrossSettings(src *profile.Profile) []string {
	var d []string
	if src.Model != "" {
		d = append(d, "model")
	}
	if src.Endpoint != "" {
		d = append(d, "endpoint")
	}
	if src.AuthKind != "" || len(src.AuthRefs) > 0 {
		d = append(d, "auth")
	}
	if len(src.ExtraEnv) > 0 {
		d = append(d, "extra_env")
	}
	return d
}

// carryOwnedLayer copies every owned-layer entry verbatim from srcDir to dstDir and
// merges owned keys of mixed files (FR-033a), then returns warnings for owned MCP
// values matching a secret pattern.
func carryOwnedLayer(def *harness.Definition, srcDir, dstDir string) ([]mcp.SecretWarning, error) {
	for i := range def.Classification {
		rule := &def.Classification[i]
		switch rule.Layer {
		case harness.LayerOwned:
			if isGlobEntry(rule.Entry) {
				continue
			}
			rel := filepath.FromSlash(strings.TrimSuffix(rule.Entry, "/"))
			s := filepath.Join(srcDir, rel)
			if !pathExists(s) {
				continue
			}
			if err := copyTree(s, filepath.Join(dstDir, rel)); err != nil {
				return nil, err
			}
		case harness.LayerMixed:
			rel := filepath.FromSlash(rule.Entry)
			s := filepath.Join(srcDir, rel)
			srcData, err := os.ReadFile(s)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			d := filepath.Join(dstDir, rel)
			dstData, _ := os.ReadFile(d) // may be absent → empty
			merged, err := layer.MergeMixedOwned(rule, dstData, srcData, filepath.Ext(rel))
			if err != nil {
				return nil, err
			}
			// A mixed owned file can carry literal secrets the harness wrote (FR-033a), so
			// default a new file to 0o600 and preserve an existing file's mode rather than
			// broadening access.
			mode := os.FileMode(0o600)
			if fi, serr := os.Stat(d); serr == nil {
				mode = fi.Mode().Perm()
			}
			if err := config.WriteFileAtomic(d, merged, mode); err != nil {
				return nil, err
			}
		}
	}
	// Warn on owned MCP values matching a secret pattern (FR-033a).
	if def.MCP != nil {
		if rel, ok := mcpFileRel(def); ok {
			if data, err := os.ReadFile(filepath.Join(dstDir, rel)); err == nil {
				return mcp.SecretWarnings(mcp.Flavor(def.MCP.Flavor), data)
			}
		}
	}
	return nil, nil
}

// convertMCP converts the source MCP into the target's native format (FR-034/FR-035).
func convertMCP(srcDef, dstDef *harness.Definition, srcDir, dstDir string, overwrite bool) (mcp.ConversionReport, error) {
	if srcDef.MCP == nil {
		return mcp.ConversionReport{}, nil
	}
	srcRel, _ := mcpFileRel(srcDef)
	srcData, err := os.ReadFile(filepath.Join(srcDir, srcRel))
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.ConversionReport{}, nil
		}
		return mcp.ConversionReport{}, err
	}
	servers, err := mcp.ParseDoc(mcp.Flavor(srcDef.MCP.Flavor), srcData)
	if err != nil {
		return mcp.ConversionReport{}, fmt.Errorf("parse source MCP: %w", err)
	}
	if dstDef.MCP == nil || !dstDef.HasCapability("mcp") {
		return mcp.SkipAll(servers, "target harness has no mcp capability"), nil
	}
	dstRel, _ := mcpFileRel(dstDef)
	dstPath := filepath.Join(dstDir, dstRel)
	existing, _ := os.ReadFile(dstPath)
	out, rep, err := mcp.MergeInto(mcp.Flavor(dstDef.MCP.Flavor), existing, servers, overwrite)
	if err != nil {
		return mcp.ConversionReport{}, err
	}
	if err := config.WriteFileAtomic(dstPath, out, 0o644); err != nil {
		return mcp.ConversionReport{}, err
	}
	return rep, nil
}

// carrySkills copies skills only when they are the source's own (FR-007a): skills are
// content, linked at all and content, so only a none profile has a skills
// directory of its own. They are written only into a target at none; a target at
// all or content links the default setup's skills instead, which is reported.
func carrySkills(srcDef, dstDef *harness.Definition, level profile.Share, srcDir, dstDir string) (bool, []string) {
	if srcDef.SkillsLocation == nil {
		return false, nil
	}
	srcSkills := skillsPath(srcDef, srcDir)
	if fi, err := os.Lstat(srcSkills); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return false, nil // linked to the default setup, or absent: nothing of the source's own
	}
	if entries, _ := os.ReadDir(srcSkills); len(entries) == 0 {
		return false, nil
	}
	if dstDef.SkillsLocation == nil {
		return false, []string{"target harness declares no skills location; the source's own skills were not copied"}
	}
	if level != profile.ShareNone {
		return false, []string{fmt.Sprintf("the source's own skills were not copied: the target (share level %s) links the default setup's skills", level)}
	}
	dst := skillsPath(dstDef, dstDir)
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return false, []string{"target skills are a link; the source's own skills were not copied"}
	}
	if err := copyTree(srcSkills, dst); err != nil {
		return false, []string{"skills copy failed: " + err.Error()}
	}
	return true, nil
}

// carrySettings copies the source's own settings entries into an absent target
// entry, leaving out the hook location key (FR-007a). It returns the copied entry
// names and per-key secret warnings (research R6).
func carrySettings(def *harness.Definition, srcDir, dstDir string) ([]string, []string, error) {
	var names, warnings []string
	for _, rule := range def.Classification {
		if rule.Layer != harness.LayerSettings || rule.IsGlob() {
			continue
		}
		rel := filepath.FromSlash(rule.Path())
		s := filepath.Join(srcDir, rel)
		d := filepath.Join(dstDir, rel)
		fi, err := os.Lstat(s)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || pathExists(d) {
			continue
		}
		if fi.IsDir() {
			if err := copyTree(s, d); err != nil {
				return nil, nil, err
			}
			names = append(names, rule.Path())
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			return nil, nil, err
		}
		if key := def.HookKeyIn(rule.Path()); key != "" {
			if data, err = layer.WithoutKey(data, key, s); err != nil {
				return nil, nil, err
			}
		}
		for _, k := range layer.SecretWarnings(data) {
			warnings = append(warnings, rule.Path()+": "+k)
		}
		if err := config.WriteFileAtomic(d, data, fi.Mode().Perm()); err != nil {
			return nil, nil, err
		}
		names = append(names, rule.Path())
	}
	return names, warnings, nil
}

type hookSetLists struct{ named, excluded []string }

// carryHookSets decides the target's hook sets: explicit flags win; otherwise the
// source's named and excluded sets travel when the target harness has a body for
// them, and the others are reported as left out (FR-007a).
func (a *app) carryHookSets(src *profile.Profile, dstDef *harness.Definition, level profile.Share, f *copyFlags) (hookSetLists, []string) {
	out := hookSetLists{}
	if f.setHookSets {
		out.named = f.hookSets
	}
	if f.setExclude {
		out.excluded = f.excludeHookSets
	}
	var leftOut []string
	lib, _ := a.loadHookLibrary()
	keep := func(name, what string) bool {
		s, ok := lib[name]
		switch {
		case !ok:
			leftOut = append(leftOut, name+" ("+what+"; not in the library)")
		case s.LoadErr != nil:
			leftOut = append(leftOut, name+" ("+what+"; its library file failed to load: fix it, then attach it with edit --hook-sets)")
		case dstDef.Hooks == nil:
			leftOut = append(leftOut, name+" ("+what+"; harness "+dstDef.Name+" does not support hook sets)")
		case s.Bodies[dstDef.Name].Events == nil && s.Bodies[dstDef.Name].Files == nil:
			leftOut = append(leftOut, name+" ("+what+"; no body for "+dstDef.Name+")")
		case what == "named" && level == profile.ShareAll:
			leftOut = append(leftOut, name+" (named; the target uses share level all)")
		default:
			return true
		}
		return false
	}
	if !f.setHookSets {
		for _, n := range src.HookSets {
			if keep(n, "named") {
				out.named = append(out.named, n)
			}
		}
	}
	if !f.setExclude {
		for _, n := range src.ExcludeHookSets {
			if keep(n, "excluded") {
				out.excluded = append(out.excluded, n)
			}
		}
	}
	return out, leftOut
}

func printCopyReport(a *app, r copyReportJSON) {
	fmt.Fprintf(a.out, "copied %s -> %s (%s) (run: aprofile sync)\n", r.Source, r.Target, r.Mode)
	if len(r.MCP.Converted) > 0 {
		fmt.Fprintf(a.out, "  MCP servers: %s\n", strings.Join(r.MCP.Converted, ", "))
	}
	for _, s := range r.MCP.Skipped {
		fmt.Fprintf(a.out, "  MCP server skipped: %s (%s)\n", s.Name, s.Reason)
	}
	for _, d := range r.MCP.Drops {
		fmt.Fprintf(a.out, "  MCP dropped: %s %s (%s)\n", d.Server, d.Field, d.Reason)
	}
	if len(r.DroppedSettings) > 0 {
		fmt.Fprintf(a.out, "  dropped settings: %s\n", strings.Join(r.DroppedSettings, ", "))
	}
	for _, s := range r.SkillsReport {
		fmt.Fprintf(a.out, "  %s\n", s)
	}
	for _, p := range r.PluginsSkipped {
		fmt.Fprintf(a.out, "  %s\n", p)
	}
	fmt.Fprintf(a.out, "  share level: %s\n", r.Share)
	if len(r.SettingsCarried) > 0 {
		fmt.Fprintf(a.out, "  settings copied from the source: %s (hooks excluded)\n", strings.Join(r.SettingsCarried, ", "))
	}
	if len(r.HookSetsCarried) > 0 {
		fmt.Fprintf(a.out, "  hook sets carried: %s\n", strings.Join(r.HookSetsCarried, ", "))
	}
	for _, h := range r.HookSetsLeftOut {
		fmt.Fprintf(a.out, "  hook set left out: %s\n", h)
	}
	for _, w := range r.SettingsWarnings {
		fmt.Fprintf(a.errOut, "aprofile: warning: settings value %s looks like a secret and was copied verbatim\n", w)
	}
	for _, w := range r.SecretWarnings {
		fmt.Fprintf(a.errOut, "aprofile: warning: owned value %s %s looks like a secret and was duplicated verbatim\n", w.Server, w.Field)
	}
}

// --- small helpers ---

func pick(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

func cloneRefs(in map[string]secret.Reference) map[string]secret.Reference {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]secret.Reference, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneExtra(in []profile.ExtraEnvVar) []profile.ExtraEnvVar {
	if len(in) == 0 {
		return nil
	}
	return append([]profile.ExtraEnvVar(nil), in...)
}

// mcpFileRel returns the MCP file path (before the '#key' suffix) relative to the
// profile config dir.
func mcpFileRel(def *harness.Definition) (string, bool) {
	if def.MCP == nil || def.MCP.Location == "" {
		return "", false
	}
	loc := def.MCP.Location
	if i := strings.IndexByte(loc, '#'); i >= 0 {
		loc = loc[:i]
	}
	return filepath.FromSlash(loc), loc != ""
}

// mcpServerNames lists the server names in a profile's MCP file (used to report a
// same-harness verbatim carry).
func mcpServerNames(def *harness.Definition, configDir string) ([]string, error) {
	if def.MCP == nil {
		return nil, nil
	}
	rel, ok := mcpFileRel(def)
	if !ok {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(configDir, rel))
	if err != nil {
		return nil, err
	}
	servers, err := mcp.ParseDoc(mcp.Flavor(def.MCP.Flavor), data)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range servers {
		names = append(names, s.Name)
	}
	return names, nil
}

func skillsPath(def *harness.Definition, configDir string) string {
	if def.SkillsLocation == nil {
		return ""
	}
	return filepath.Join(configDir, filepath.FromSlash(def.SkillsLocation.Path))
}

func isGlobEntry(s string) bool { return strings.ContainsAny(s, "*?[") }

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// copyTree copies a file or directory tree, skipping symlinks (owned entries are real
// per-profile files; a link is never carried).
func copyTree(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if fi.IsDir() {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(dst, data, fi.Mode().Perm())
}
