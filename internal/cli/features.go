package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/feature"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/profile"
)

// The features command (spec 006): bare status lists every feature the
// profile's harness supports, grouped, with state and parameter values
// (FR-001); mutation flags update the stored feature state and remind the user
// to run sync, following edit's output contract (FR-002). add and edit gain no
// feature flags: this is the only mutation path (FR-008).

// featureJSON is one feature object of the status and mutation --json schemas
// and of show's features array (contracts/features-cli.md; a public interface).
type featureJSON struct {
	ID      string            `json:"id"`
	Enabled bool              `json:"enabled"`
	Params  map[string]string `json:"params"`
}

type featuresFlags struct {
	enable   string
	disable  string
	traces   string
	logs     string
	endpoint string
	protocol string
	asJSON   bool
	dryRun   bool
}

func newFeaturesCmd() *cobra.Command {
	f := &featuresFlags{}
	cmd := &cobra.Command{
		Use:   "features <harness>/<name>",
		Short: "Show or change a profile's harness capability features (telemetry, automode-log)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatures(cmd, args[0], f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.enable, "enable", "", "enable a feature")
	fl.StringVar(&f.disable, "disable", "", "disable a feature; its entries are removed by the next sync")
	fl.StringVar(&f.traces, "traces", "", "telemetry param (on|off); implies enabling telemetry")
	fl.StringVar(&f.logs, "logs", "", "telemetry param (on|off); implies enabling telemetry")
	fl.StringVar(&f.endpoint, "endpoint", "", "telemetry param: the OTLP endpoint URL; implies enabling telemetry")
	fl.StringVar(&f.protocol, "protocol", "", "telemetry param: the OTLP protocol (values per harness); implies enabling telemetry")
	fl.BoolVar(&f.asJSON, "json", false, "emit machine-readable JSON")
	fl.BoolVar(&f.dryRun, "dry-run", false, "with a mutation: report the would-be changes without storing them")
	cmd.MarkFlagsMutuallyExclusive("enable", "disable")
	cmd.MarkFlagsMutuallyExclusive("disable", "traces")
	cmd.MarkFlagsMutuallyExclusive("disable", "logs")
	cmd.MarkFlagsMutuallyExclusive("disable", "endpoint")
	cmd.MarkFlagsMutuallyExclusive("disable", "protocol")
	return cmd
}

// runFeatures dispatches between the bare status form and a mutation.
func runFeatures(cmd *cobra.Command, ref string, f *featuresFlags) error {
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
	mutating := f.enable != "" || f.disable != "" || cmd.Flags().Changed("traces") ||
		cmd.Flags().Changed("logs") || cmd.Flags().Changed("endpoint") || cmd.Flags().Changed("protocol")
	if !mutating {
		return featuresStatus(a, p, def, f.asJSON)
	}
	// FR-013: a mutation on a profile at share level all is refused with the way
	// out; the bare status form still works.
	if p.ShareLevel() == profile.ShareAll {
		return fmt.Errorf("profile %s uses share level all, whose settings are linked to the default setup; change it first with: aprofile edit %s --share content", p.ID(), p.ID())
	}
	return featuresMutate(a, cmd, p, def, f)
}

// featuresStatus prints the bare status: every feature the harness supports,
// grouped, with its state and parameter values including effective defaults
// (FR-001, FR-017).
func featuresStatus(a *app, p *profile.Profile, def *harness.Definition, asJSON bool) error {
	type groupJSON struct {
		Name     string        `json:"name"`
		Features []featureJSON `json:"features"`
	}
	enabled := map[string]map[string]string{}
	for _, fs := range p.Features {
		enabled[fs.Feature] = fs.Params
	}
	out := struct {
		Profile string      `json:"profile"`
		Harness string      `json:"harness"`
		Groups  []groupJSON `json:"groups"`
	}{Profile: p.ID(), Harness: p.Harness, Groups: []groupJSON{}}
	var lines []string
	for _, g := range feature.Groups() {
		gj := groupJSON{Name: g, Features: []featureJSON{}}
		for _, cat := range feature.ByGroup(g) {
			if !def.SupportsFeature(cat.ID) {
				continue // features the harness does not support are not listed
			}
			fj := featureJSON{ID: cat.ID, Params: map[string]string{}}
			if params, on := enabled[cat.ID]; on {
				fj.Enabled = true
				if resolved, err := def.ResolveFeatureParams(cat.ID, params); err == nil {
					fj.Params = resolved
				} else {
					// Invalid stored state (a hand-edited record): shown as on with
					// the raw params; the placement skips it and the load warned.
					fj.Params = params
				}
			}
			gj.Features = append(gj.Features, fj)
		}
		if len(gj.Features) == 0 {
			continue
		}
		out.Groups = append(out.Groups, gj)
		if asJSON {
			continue
		}
		lines = append(lines, "  "+g+":")
		for _, fj := range gj.Features {
			state := "off"
			if fj.Enabled {
				state = "on"
			}
			lines = append(lines, fmt.Sprintf("    %-13s %s", fj.ID+":", state))
			if fj.Enabled {
				for _, cp := range catParams(fj.ID) {
					lines = append(lines, fmt.Sprintf("      %-11s %s", cp+":", fj.Params[cp]))
				}
			}
		}
	}
	if asJSON {
		return writeJSON(a.out, out)
	}
	fmt.Fprintln(a.out, p.ID())
	for _, l := range lines {
		fmt.Fprintln(a.out, l)
	}
	return nil
}

// catParams lists a feature's catalog param names in order.
func catParams(id string) []string {
	f, ok := feature.ByID(id)
	if !ok {
		return nil
	}
	var out []string
	for _, p := range f.Params {
		out = append(out, p.Name)
	}
	return out
}

// paramFlagFeature is the catalog feature the per-parameter flags belong to:
// the spec's Clarifications fix them to telemetry's parameters. The flags
// themselves are command surface (FR-002); everything else about the feature is
// catalog data.
const paramFlagFeature = "telemetry"

// featuresMutate applies --enable/--disable and the per-parameter flags
// (FR-002), validates everything the spec demands at mutation time, and stores
// the state with the sync reminder.
func featuresMutate(a *app, cmd *cobra.Command, p *profile.Profile, def *harness.Definition, f *featuresFlags) error {
	ch := cmd.Flags().Changed
	paramFlags := map[string]string{
		"traces": f.traces, "logs": f.logs, "endpoint": f.endpoint, "protocol": f.protocol,
	}
	paramGiven := map[string]bool{"traces": ch("traces"), "logs": ch("logs"), "endpoint": ch("endpoint"), "protocol": ch("protocol")}
	anyParam := paramGiven["traces"] || paramGiven["logs"] || paramGiven["endpoint"] || paramGiven["protocol"]

	target := f.enable
	if target == "" && f.disable == "" {
		// A per-parameter flag implies enabling the feature (FR-002).
		target = paramFlagFeature
	}
	if f.enable != "" && f.enable != paramFlagFeature && anyParam {
		return fmt.Errorf("--enable %s cannot be combined with %s's parameter flags (--traces, --logs, --endpoint, --protocol)", f.enable, paramFlagFeature)
	}

	var changed featureJSON
	if f.disable != "" {
		if _, ok := feature.ByID(f.disable); !ok {
			return fmt.Errorf("unknown feature %q (catalog: %s)", f.disable, strings.Join(featureIDs(), ", "))
		}
		if !def.SupportsFeature(f.disable) {
			return fmt.Errorf("harness %q does not support the %s feature (--disable %s)", def.Name, f.disable, f.disable)
		}
		kept := p.Features[:0]
		for _, fs := range p.Features {
			if fs.Feature != f.disable {
				kept = append(kept, fs)
			}
		}
		p.Features = kept
		changed = featureJSON{ID: f.disable, Params: map[string]string{}}
	} else {
		if _, ok := feature.ByID(target); !ok {
			return fmt.Errorf("unknown feature %q (catalog: %s)", target, strings.Join(featureIDs(), ", "))
		}
		if !def.SupportsFeature(target) {
			// FR-007: unsupported is an error naming the harness and the flag,
			// never a no-op.
			return fmt.Errorf("harness %q does not support the %s feature (--enable %s)", def.Name, target, target)
		}
		// Merge: flags given here override; the rest keeps the stored value of an
		// enabled feature, or the harness default (signals default to off,
		// FR-011).
		params := map[string]string{}
		for _, fs := range p.Features {
			if fs.Feature == target {
				for k, v := range fs.Params {
					params[k] = v
				}
			}
		}
		for name, given := range paramFlags {
			if !paramGiven[name] {
				continue
			}
			cat, _ := feature.ByID(target)
			cp, ok := cat.Param(name)
			if !ok {
				return fmt.Errorf("feature %q has no parameter --%s", target, name)
			}
			if cp.Type == feature.TypeBool && given != "on" && given != "off" {
				return fmt.Errorf("--%s must be on or off, not %q", name, given)
			}
			if given == "" && cp.Type != feature.TypeString {
				return fmt.Errorf("--%s needs a value", name)
			}
			params[name] = given
		}
		resolved, err := def.ResolveFeatureParams(target, params)
		if err != nil {
			return err // FR-004: values outside the harness's list are rejected
		}
		// Edge cases (spec 006): the catalog's enable-time constraints, checked
		// through data so the mutation path stays feature-agnostic (FR-006).
		cat, _ := feature.ByID(target)
		if err := cat.CheckConstraints(resolved); err != nil {
			return err
		}
		// FR-015: a collision with a hand-set extra env var is a hard error naming
		// the variable, in this direction and the other (edit checks its side).
		// There is no flag that removes an extra env var, so the fix names the
		// profile file it lives in.
		names := def.FeatureEnvNames(target)
		for _, e := range p.ExtraEnv {
			for _, v := range names {
				if e.Name == v {
					return fmt.Errorf("extra env %s collides with an entry the %s feature places; remove the variable from the profile's extra_env (profile file: %s), or keep the variable and leave the feature off", v, target, a.store.Path(p.Harness, p.Name))
				}
			}
		}
		kept := p.Features[:0]
		for _, fs := range p.Features {
			if fs.Feature != target {
				kept = append(kept, fs)
			}
		}
		p.Features = append(kept, profile.FeatureState{Feature: target, Params: resolved})
		p.NormalizeFeatures()
		changed = featureJSON{ID: target, Enabled: true, Params: resolved}
	}

	if f.dryRun {
		if f.asJSON {
			return writeJSON(a.out, featuresMutationJSON{Changed: []featureJSON{changed}, Reminder: "sync", DryRun: true})
		}
		fmt.Fprintf(a.out, "(dry-run) %s\n", featureChangeLine(changed))
		fmt.Fprintln(a.out, "run aprofile sync to apply")
		return nil
	}
	if err := a.store.Save(p); err != nil {
		return err
	}
	if f.asJSON {
		return writeJSON(a.out, featuresMutationJSON{Changed: []featureJSON{changed}, Reminder: "sync"})
	}
	fmt.Fprintln(a.out, featureChangeLine(changed))
	fmt.Fprintln(a.out, "run aprofile sync to apply")
	return nil
}

type featuresMutationJSON struct {
	Changed  []featureJSON `json:"changed"`
	Reminder string        `json:"reminder"`
	DryRun   bool          `json:"dry_run,omitempty"`
}

// featureChangeLine renders one mutation's human line.
func featureChangeLine(f featureJSON) string {
	if !f.Enabled {
		return fmt.Sprintf("%s: disabled", f.ID)
	}
	var parts []string
	for _, name := range catParams(f.ID) {
		parts = append(parts, fmt.Sprintf("%s %s", name, f.Params[name]))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s: enabled", f.ID)
	}
	return fmt.Sprintf("%s: enabled (%s)", f.ID, strings.Join(parts, ", "))
}

func featureIDs() []string {
	var out []string
	for _, f := range feature.All() {
		out = append(out, f.ID)
	}
	return out
}

// checkFeatureEnvCollision returns an error when an extra env variable would
// collide with an entry an enabled feature places (FR-015, edit's direction):
// setting a hand-set variable over a feature's entry is never silently
// accepted.
func checkFeatureEnvCollision(p *profile.Profile, def *harness.Definition, name string) error {
	if def == nil {
		return nil
	}
	for _, fs := range p.Features {
		for _, v := range def.FeatureEnvNames(fs.Feature) {
			if v == name {
				return fmt.Errorf("extra env %s collides with an entry the %s feature places; disable the feature first: aprofile features %s --disable %s", name, fs.Feature, p.ID(), fs.Feature)
			}
		}
	}
	return nil
}

// featureDriftWarnings reports drift between a profile's recorded feature state
// and its placed entries (FR-016): entries that differ from the derived want,
// and feature entries still placed for a feature that is no longer enabled.
// It never fails: a plan error (for example an env collision, FR-015) is
// returned as a warning.
func featureDriftWarnings(a *app, p *profile.Profile, def *harness.Definition) []string {
	if def == nil {
		return nil // no harness definition: nothing to resolve drift against
	}
	if len(p.Features) == 0 && len(def.Features) == 0 {
		return nil
	}
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	recPath := a.paths.ManagedLaunchPath(p.Harness, p.Name)
	rec, err := launch.Load(recPath)
	if err != nil {
		return []string{err.Error()}
	}
	res, err := launch.Plan(p, def, launch.ReadDocFor(def, configDir, rec), rec)
	if err != nil {
		return []string{fmt.Sprintf("features: %v", err)}
	}
	stepAt := map[string]bool{}
	for _, s := range res.Steps {
		stepAt[s.File+"#"+s.Location] = true
	}
	var out []string
	seen := map[string]bool{}
	// Enabled features with pending steps at their locations.
	desired := map[string]map[string]bool{} // feature -> file#location
	for _, e := range featureDesiredFor(def, p) {
		if desired[e.Feature] == nil {
			desired[e.Feature] = map[string]bool{}
		}
		desired[e.Feature][e.File+"#"+e.Location] = true
	}
	for featureID, locs := range desired {
		for l := range locs {
			if stepAt[l] {
				if !seen[featureID] {
					seen[featureID] = true
					out = append(out, fmt.Sprintf("%s: the placed entries differ from the recorded feature state (run: aprofile sync)", featureID))
				}
				break
			}
		}
	}
	// Feature locations still recorded but no longer desired (a disabled or
	// cleared feature whose entries have not been removed yet).
	for _, l := range def.FeatureLocations() {
		key := l.File + "#" + l.Location
		if desired[l.Feature][key] || !rec.Has(l.File, l.Location) {
			continue
		}
		if !seen["orphan"] {
			seen["orphan"] = true
			out = append(out, "entries of a disabled feature are still placed (run: aprofile sync)")
		}
		break
	}
	sort.Strings(out)
	return out
}

// featureDesiredFor is featureDesired for the CLI (the launch package's
// resolution of a profile's feature states into desired entries).
func featureDesiredFor(def *harness.Definition, p *profile.Profile) []launch.DesiredEntry {
	return launch.FeatureDesired(def, p)
}
