package launch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
)

// StepKind names what a plan step does to one placed entry.
type StepKind string

const (
	// StepSet places an entry that is not in the file.
	StepSet StepKind = "set"
	// StepRestore writes the profile's value over a placed entry the user changed or
	// deleted, or updates a placed entry whose desired value changed (FR-005).
	StepRestore StepKind = "restore"
	// StepReplaceUser places an entry over a user's own entry of the same name; it is
	// placed (recorded) from then on (FR-005).
	StepReplaceUser StepKind = "replace-user"
	// StepRemove removes a placed entry the profile no longer sets.
	StepRemove StepKind = "remove"
)

// Step is one planned change: an entry (named, never its value) in a file.
type Step struct {
	Kind     StepKind `json:"kind"`
	File     string   `json:"file"`
	Location string   `json:"location"`
	Note     string   `json:"note,omitempty"`
}

// Result is a plan: the settings with their carriers, the steps that bring the
// files to the desired state, the launch entries in the files aprofile did not place
// (FR-020b), the record after the steps, and notes.
type Result struct {
	Settings  []Setting
	Steps     []Step
	Unplaced  []Entry
	NewRecord Record
	// Notes are conditions that change nothing (a file that does not parse while the
	// profile has nothing to place in it).
	Notes []string
	// Reencoded lists the TOML files the plan rewrites (comments are dropped).
	Reencoded []string

	writes map[string][]byte
}

// Writes returns the new content of every file the plan changes.
func (r Result) Writes() map[string][]byte {
	if r.writes == nil {
		return map[string][]byte{}
	}
	return r.writes
}

// Stale reports whether the folder needs `aprofile sync` (FR-020a).
func (r Result) Stale() bool { return len(r.Steps) > 0 }

// Plan computes the steps that bring the profile's folder_launch files to the
// desired placed entries, given their current content and the record (pure).
func Plan(p *profile.Profile, def *harness.Definition, doc Doc, rec Record) (Result, error) {
	return PlanWith(p, def, doc, rec, nil)
}

// envEntry is one desired env entry.
type envEntry struct{ name, value string }

// desired is the full desired state of placed entries.
type desired struct {
	env      []envEntry
	shellCmd string // "" when no shell key command is desired
	auth     map[string]any
}

// PlanWith is Plan with the exec hook's env output (listed in APROFILE_SET_VARS).
func PlanWith(p *profile.Profile, def *harness.Definition, doc Doc, rec Record, hookEnv []harness.EnvVar) (Result, error) {
	res := Result{Settings: DecideWith(p, def, doc, hookEnv), NewRecord: Record{FormatVersion: RecordFormatVersion}}
	fl := def.FolderLaunch
	featAll := FeatureDesired(def, p)
	if fl == nil {
		if len(rec.Entries) == 0 && len(featAll) == 0 {
			return res, nil
		}
		// The definition no longer declares folder locations (a harnesses.d override
		// removed them): the placed entries are removed all the same (FR-005).
		fl = &harness.FolderLaunch{}
	}
	want, err := desiredState(def, res.Settings, canCarryEnv(p, def, doc))
	if err != nil {
		return res, err
	}

	// Feature-derived desired entries (spec 006): those targeting the folder env
	// block ride the env placement path; the rest go to the generic nested paths.
	featEnv, featRest := envFeatureEntries(fl, def, featAll)
	featEnvNames := map[string]bool{}
	supplied := map[string]bool{}
	for _, s := range res.Settings {
		if s.IsVariable() {
			supplied[s.Variable] = true
		}
	}
	for _, e := range featEnv {
		// FR-015: a collision between a feature's entries and a hand-set extra env
		// var is never silently resolved; at plan time (a hand-edited record) it is
		// a problem naming the variable.
		if supplied[e.name] {
			return res, fmt.Errorf("features: env %s is also set as the profile's own env variable; disable one (aprofile features %s --disable <feature>, or drop the variable with: aprofile edit %s --env %s=)", e.name, p.ID(), p.ID(), e.name)
		}
		featEnvNames[e.name] = true
		want.env = append(want.env, e)
	}

	// Every file the definition names or the record lists.
	fileSet := map[string]bool{}
	for _, f := range files(&harness.Definition{FolderLaunch: fl}) {
		fileSet[f] = true
	}
	for _, f := range featureFiles(def) {
		fileSet[f] = true
	}
	for _, e := range rec.Entries {
		fileSet[e.File] = true
	}
	var all []string
	for f := range fileSet {
		all = append(all, f)
	}
	sort.Strings(all)

	for _, f := range all {
		fd := doc.Files[f]
		envHere := fl.Env != "" && fl.EnvFile() == f
		kc := fl.KeyCommand
		keyHere := kc != nil && kc.File() == f
		recHere := recordIn(rec, f)
		featHere := featureEntriesIn(featRest, f)
		hasWant := (envHere && len(want.env) > 0) || (keyHere && (want.shellCmd != "" || want.auth != nil)) || len(featHere) > 0

		if fd.Linked {
			if len(featHere) > 0 || (envHere && len(featEnv) > 0) {
				// Spec 006 edge case: feature entries are never written into a file
				// linked to the default setup; a level change by other means leaves
				// them unplaceable and sync reports a problem.
				return res, fmt.Errorf("%s: is linked to the default setup; the profile's feature entries cannot be placed (fix: aprofile edit %s --share content)", f, p.ID())
			}
			// Never written through; its record entries are dropped (FR-008, FR-008a).
			res.Unplaced = append(res.Unplaced, unplaced(p, def, fl, f, fd, Record{}, want)...)
			continue
		}
		if fd.Err != nil {
			if !hasWant && len(recHere) == 0 {
				res.Notes = append(res.Notes, fmt.Sprintf("%s: cannot be read (%v); it holds no launch settings of this profile and is left unchanged", f, fd.Err))
				continue
			}
			key := fl.EnvKey()
			if keyHere && !envHere {
				key = kc.Key()
			}
			return res, fmt.Errorf("%s: cannot place launch settings: %v (key %s)", f, fd.Err, key)
		}
		if isTOML(f) {
			if envHere || (keyHere && kc.Form != harness.KeyFormProviderAuth) {
				return res, fmt.Errorf("%s: cannot place launch settings: env entries and shell key commands need a JSON file", f)
			}
			if err := planTOML(&res, fl, f, fd, rec, want, keyHere, featureLocationSet(def), featHere); err != nil {
				return res, err
			}
		} else {
			if keyHere && kc.Form == harness.KeyFormProviderAuth {
				return res, fmt.Errorf("%s: cannot place launch settings: the provider-auth form needs a TOML file", f)
			}
			if err := planJSON(&res, p.ID(), fl, f, fd, rec, want, envHere, keyHere, featEnvNames, featHere); err != nil {
				return res, err
			}
		}
		res.Unplaced = append(res.Unplaced, unplaced(p, def, fl, f, fd, rec, want)...)
	}
	sort.SliceStable(res.Unplaced, func(i, j int) bool {
		if res.Unplaced[i].File != res.Unplaced[j].File {
			return res.Unplaced[i].File < res.Unplaced[j].File
		}
		return res.Unplaced[i].Location < res.Unplaced[j].Location
	})
	res.NewRecord = res.NewRecord.sorted()
	return res, nil
}

// desiredState computes the desired placed entries from the settings
// (data-model.md "Desired entries for a profile"). envOK says whether the folder
// can carry env entries (a folder env location and an env file that is the
// profile's own, not linked to the default setup).
func desiredState(def *harness.Definition, settings []Setting, envOK bool) (desired, error) {
	var d desired
	var names []string
	folderVar := false
	var key *Setting
	for i, s := range settings {
		if s.IsVariable() {
			names = append(names, s.Variable)
			if s.Carrier == CarrierFolder {
				folderVar = true
				d.env = append(d.env, envEntry{s.Variable, s.Value})
			}
		}
		if s.Kind == KindKey && s.Carrier == CarrierFolder {
			key = &settings[i]
		}
	}
	if folderVar {
		d.env = append(d.env, envEntry{"APROFILE_SET_VARS", strings.Join(names, " ")})
	}
	// Spec 004 FR-006a, widened by spec 005 FR-001: whenever the folder can carry
	// env entries, it neutralizes the auth-clearing list, whatever the auth kind and
	// whether or not it carries the key, except names the profile supplies itself
	// (spec 005 FR-002): an empty folder entry would beat the value the wrapper sets.
	if envOK {
		// Supplied names: every launch variable of the profile (model, endpoint, extra
		// and exec-hook variables, whichever carrier) and the variable of a key the
		// wrapper carries. A folder-carried key's own variable stays blanked: the key
		// command serves it (spec 004 research R2).
		supplied := map[string]bool{}
		for _, s := range settings {
			if s.IsVariable() || (s.Kind == KindKey && s.Carrier == CarrierWrapper && s.Variable != "") {
				supplied[s.Variable] = true
			}
		}
		for _, v := range def.UnsetAuthVars() {
			if !supplied[v] {
				d.env = append(d.env, envEntry{v, ""})
			}
		}
	}
	if key == nil {
		return d, nil
	}
	fl := def.FolderLaunch
	switch fl.KeyCommand.Form {
	case harness.KeyFormShell:
		cmd, err := ShellCommand(*key.Ref)
		if err != nil {
			return d, err
		}
		d.shellCmd = cmd
	case harness.KeyFormProviderAuth:
		auth, err := ProviderAuth(*key.Ref)
		if err != nil {
			return d, err
		}
		d.auth = auth
	}
	return d, nil
}

func recordIn(rec Record, file string) []Entry {
	var out []Entry
	for _, e := range rec.Entries {
		if e.File == file {
			out = append(out, e)
		}
	}
	return out
}

// placeKind decides the step for a desired entry: equal and recorded needs none.
func placeKind(exists, equal, recorded bool) (StepKind, bool) {
	switch {
	case exists && equal && recorded:
		return "", false
	case exists && equal:
		return StepReplaceUser, true
	case !exists && recorded:
		return StepRestore, true
	case !exists:
		return StepSet, true
	case recorded:
		return StepRestore, true
	default:
		return StepReplaceUser, true
	}
}

// jsonEmptyValue reports whether raw is null or the empty JSON string.
func jsonEmptyValue(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return bytes.Equal(t, []byte("null")) || bytes.Equal(t, []byte(`""`))
}

// jsonStringEqual reports whether raw is the JSON string s.
func jsonStringEqual(raw json.RawMessage, s string) bool {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	str, ok := v.(string)
	return ok && str == s
}

func planJSON(res *Result, id string, fl *harness.FolderLaunch, f string, fd FileDoc, rec Record, want desired, envHere, keyHere bool, featEnvNames map[string]bool, featHere []DesiredEntry) error {
	handled := map[string]bool{} // record locations the env and key steps decided
	top := map[string]json.RawMessage{}
	for k, v := range fd.JSON {
		top[k] = v
	}
	changed := false
	step := func(kind StepKind, loc, note string) {
		res.Steps = append(res.Steps, Step{Kind: kind, File: f, Location: loc, Note: note})
	}

	if envHere {
		ek := fl.EnvKey()
		envObj := map[string]json.RawMessage{}
		envExisted := false
		if raw, ok := top[ek]; ok {
			envExisted = true
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				m, err := layer.DecodeJSONObject(raw)
				if err != nil {
					return fmt.Errorf("%s: cannot place launch settings: the value is not an object (key %s)", f, ek)
				}
				envObj = m
			}
		}
		envChanged := false
		wantNames := map[string]bool{}
		for _, e := range want.env {
			wantNames[e.name] = true
			loc := ek + "." + e.name
			cur, exists := envObj[e.name]
			recorded := rec.Has(f, loc)
			kind, ok := placeKind(exists, exists && jsonStringEqual(cur, e.value), recorded)
			entry := Entry{File: f, Location: loc}
			if featEnvNames[e.name] && exists && !recorded && !jsonEmptyValue(cur) && !jsonStringEqual(cur, e.value) {
				// A feature entry replaces a hand-written value: record it for
				// restore on disable and say so, naming the variable but never its
				// value (spec 006 FR-011, T011).
				entry.Replaced = map[string]string{"": string(bytes.TrimSpace(cur))}
				res.Notes = append(res.Notes, fmt.Sprintf("%s: env %s held a value the feature replaces; it is restored when the feature is disabled", f, e.name))
			}
			res.NewRecord.Entries = append(res.NewRecord.Entries, entry)
			if !ok {
				continue
			}
			if exists && e.value == "" && !jsonEmptyValue(cur) {
				// A neutralizing entry drops a non-empty value in the profile's own
				// file (a user entry it replaces, or a hand edit of a placed entry it
				// restores): say so on its own line, naming the variable but never its
				// value, with the way to keep it (spec 005 review finding).
				res.Notes = append(res.Notes, fmt.Sprintf("%s: the profile's own env %s is replaced by an empty entry, so a launch from the folder no longer uses it; to keep it, set it on the profile: aprofile edit %s --env %s=<value>", f, e.name, id, e.name))
			}
			raw, _ := json.Marshal(e.value)
			envObj[e.name] = raw
			envChanged = true
			step(kind, loc, "")
		}
		for _, e := range recordIn(rec, f) {
			name, ok := strings.CutPrefix(e.Location, ek+".")
			if !ok {
				continue
			}
			handled[e.Location] = true
			if wantNames[name] {
				continue
			}
			if old, has := e.Replaced[""]; has {
				// A removed feature entry restores the hand-written value it
				// replaced (T011), so disabling is byte-exact (SC-002). Only
				// feature entries record a whole-value Replaced in the env block.
				envObj[name] = json.RawMessage(old)
				envChanged = true
				step(StepRemove, e.Location, "restored the previous value")
				continue
			}
			if _, exists := envObj[name]; exists {
				delete(envObj, name)
				envChanged = true
			}
			step(StepRemove, e.Location, "")
		}
		if envChanged {
			raw, err := layer.EncodeJSONCompact(envObj)
			if err != nil {
				return err
			}
			if len(envObj) > 0 || envExisted {
				top[ek] = raw
			}
			changed = true
		}
	}

	if keyHere && fl.KeyCommand.Form == harness.KeyFormShell {
		k := fl.KeyCommand.Key()
		handled[k] = true
		cur, exists := top[k]
		recorded := rec.Has(f, k)
		if want.shellCmd != "" {
			res.NewRecord.Entries = append(res.NewRecord.Entries, Entry{File: f, Location: k})
			if kind, ok := placeKind(exists, exists && jsonStringEqual(cur, want.shellCmd), recorded); ok {
				raw, _ := json.Marshal(want.shellCmd)
				top[k] = raw
				changed = true
				step(kind, k, "")
			}
		} else if recorded {
			if exists {
				delete(top, k)
				changed = true
			}
			step(StepRemove, k, "")
		}
	}

	// Feature entries at JSON locations outside the folder env block (spec 006):
	// placed like env entries, one nesting level (the loader restricts JSON
	// feature locations to env.<NAME>).
	for _, e := range featHere {
		handled[e.Location] = true
		obj, name, nested := strings.Cut(e.Location, ".")
		if !nested {
			return fmt.Errorf("%s: cannot place feature entry %s: the location must be env.<NAME> in a JSON file", f, e.Location)
		}
		value, isStr := e.Value.(string)
		if !isStr {
			return fmt.Errorf("%s: cannot place feature entry %s: a JSON value must be a string", f, e.Location)
		}
		sub := map[string]json.RawMessage{}
		if raw, ok := top[obj]; ok {
			m, err := layer.DecodeJSONObject(raw)
			if err != nil {
				return fmt.Errorf("%s: cannot place feature entry %s: %s is not an object", f, e.Location, obj)
			}
			sub = m
		}
		cur, exists := sub[name]
		recorded := rec.Has(f, e.Location)
		kind, ok := placeKind(exists, exists && jsonStringEqual(cur, value), recorded)
		res.NewRecord.Entries = append(res.NewRecord.Entries, Entry{File: f, Location: e.Location})
		if ok {
			raw, _ := json.Marshal(value)
			sub[name] = raw
			enc, err := layer.EncodeJSONCompact(sub)
			if err != nil {
				return err
			}
			top[obj] = enc
			changed = true
			step(kind, e.Location, "")
		}
	}

	// Placed entries at locations the definition no longer names (an override moved
	// or removed them) are removed as well (FR-005).
	for _, e := range recordIn(rec, f) {
		if handled[e.Location] {
			continue
		}
		if obj, name, nested := strings.Cut(e.Location, "."); nested {
			if raw, ok := top[obj]; ok {
				if m, err := layer.DecodeJSONObject(raw); err == nil {
					if _, has := m[name]; has {
						delete(m, name)
						enc, err := layer.EncodeJSONCompact(m)
						if err != nil {
							return err
						}
						top[obj] = enc
						changed = true
					}
				}
			}
		} else if _, has := top[e.Location]; has {
			delete(top, e.Location)
			changed = true
		}
		step(StepRemove, e.Location, "")
	}

	if changed {
		data, err := layer.EncodeJSON(top)
		if err != nil {
			return err
		}
		if res.writes == nil {
			res.writes = map[string][]byte{}
		}
		res.writes[f] = data
	}
	return nil
}

func planTOML(res *Result, fl *harness.FolderLaunch, f string, fd FileDoc, rec Record, want desired, keyHere bool, featLocs map[string]bool, featHere []DesiredEntry) error {
	kc := fl.KeyCommand
	top := deepCopy(fd.TOML)
	if top == nil {
		top = map[string]any{}
	}
	m := top
	pk := ""
	var providers map[string]any
	if keyHere {
		pk = kc.Key()
		if v, ok := m[pk]; ok {
			pm, isMap := v.(map[string]any)
			if !isMap {
				return fmt.Errorf("%s: cannot place launch settings: the value is not a table (key %s)", f, pk)
			}
			providers = pm
		}
	}
	loc := func(name string) string { return pk + "." + name + ".auth" }
	sel, selOK := "", false
	if keyHere {
		sel, selOK = selectedProvider(m, kc)
	}
	desiredLoc := ""
	if keyHere && want.auth != nil && selOK {
		desiredLoc = loc(sel)
	}
	changed := false
	step := func(kind StepKind, l, note string) {
		res.Steps = append(res.Steps, Step{Kind: kind, File: f, Location: l, Note: note})
	}

	// Feature-derived entries (spec 006, FR-012): nested dotted-path placement
	// with Replaced subtree capture and restore; sibling hand-written keys are
	// preserved and only the keys aprofile placed are removed.
	featWant := map[string]bool{}
	for _, e := range featHere {
		featWant[e.Location] = true
		path := strings.Split(e.Location, ".")
		cur, exists := getTOMLPath(m, path)
		recorded := rec.Has(f, e.Location)
		kind, need := placeKind(exists, exists && tomlEqual(cur, e.Value), recorded)
		entry := Entry{File: f, Location: e.Location}
		if exists && !recorded && !tomlEmptyValue(cur) && !tomlEqual(cur, e.Value) {
			enc, err := encodeTOMLSubtree(cur)
			if err != nil {
				return fmt.Errorf("%s: cannot place feature entry %s: %v", f, e.Location, err)
			}
			entry.Replaced = map[string]string{"": enc}
			// The replacement is never silent: sync names the key path, never the
			// value (FR-012).
			res.Notes = append(res.Notes, fmt.Sprintf("%s: %s held a value the feature replaces; it is restored when the feature is disabled", f, e.Location))
		}
		res.NewRecord.Entries = append(res.NewRecord.Entries, entry)
		if need {
			if err := setTOMLPath(m, path, deepCopy(e.Value)); err != nil {
				return fmt.Errorf("%s: cannot place feature entry %s: %v", f, e.Location, err)
			}
			changed = true
			step(kind, e.Location, "")
		}
	}
	// Remove feature entries the profile no longer wants, restoring what they
	// replaced; hand-written sibling keys survive.
	for _, e := range recordIn(rec, f) {
		if !featLocs[f+"#"+e.Location] || featWant[e.Location] {
			continue
		}
		path := strings.Split(e.Location, ".")
		note := ""
		if raw, has := e.Replaced[""]; has {
			if old, err := decodeTOMLSubtree(raw); err == nil {
				if err := setTOMLPath(m, path, old); err == nil {
					note = "restored the previous value"
				}
			}
		} else if deleteTOMLPath(m, path) {
			// removed
		}
		changed = true
		step(StepRemove, e.Location, note)
	}

	// Remove placed auth blocks that are no longer desired, restoring what they replaced.
	for _, e := range recordIn(rec, f) {
		if featLocs[f+"#"+e.Location] {
			continue // a feature entry, handled above
		}
		if e.Location == desiredLoc {
			continue
		}
		// The record location is "<table>.<provider>.auth"; the table may differ from
		// the definition's current one after an override.
		table, name, _ := strings.Cut(strings.TrimSuffix(e.Location, ".auth"), ".")
		tbl := providers
		if table != pk {
			tbl, _ = m[table].(map[string]any)
		}
		var restored []string
		if blk, ok := tbl[name].(map[string]any); ok {
			if _, has := blk["auth"]; has {
				delete(blk, "auth")
				changed = true
			}
			for _, k := range sortedKeys(e.Replaced) {
				if _, has := blk[k]; has {
					continue
				}
				v, err := decodeTOMLValue(e.Replaced[k])
				if err != nil {
					continue
				}
				blk[k] = v
				restored = append(restored, k)
				changed = true
			}
		}
		note := ""
		if len(restored) > 0 {
			note = "restored " + strings.Join(restored, ", ")
		}
		step(StepRemove, e.Location, note)
	}

	if desiredLoc != "" {
		blk, ok := providers[sel].(map[string]any)
		if !ok {
			return fmt.Errorf("%s: cannot place launch settings: the selected provider is not a table (key %s.%s)", f, pk, sel)
		}
		recEntry, recorded := rec.Get(f, desiredLoc)
		replaced := map[string]string{}
		for k, v := range recEntry.Replaced {
			replaced[k] = v
		}
		var replacedNow []string
		for _, r := range kc.Replaces {
			v, has := blk[r]
			if !has {
				continue
			}
			if _, already := replaced[r]; !already {
				enc, err := encodeTOMLValue(v)
				if err != nil {
					return fmt.Errorf("%s: cannot place launch settings: %v (key %s.%s.%s)", f, err, pk, sel, r)
				}
				replaced[r] = enc
			}
			delete(blk, r)
			replacedNow = append(replacedNow, r)
			changed = true
		}
		cur, has := blk["auth"]
		kind, need := placeKind(has, has && tomlEqual(cur, want.auth), recorded)
		if need {
			blk["auth"] = deepCopy(want.auth)
			changed = true
		}
		if !need && len(replacedNow) > 0 {
			kind, need = StepRestore, true
		}
		if need {
			note := ""
			if len(replacedNow) > 0 {
				note = "replaces " + strings.Join(replacedNow, ", ")
			}
			step(kind, desiredLoc, note)
		}
		entry := Entry{File: f, Location: desiredLoc}
		if len(replaced) > 0 {
			entry.Replaced = replaced
		}
		res.NewRecord.Entries = append(res.NewRecord.Entries, entry)
	}

	if changed {
		data, err := toml.Marshal(top)
		if err != nil {
			return fmt.Errorf("%s: encode: %w", f, err)
		}
		if res.writes == nil {
			res.writes = map[string][]byte{}
		}
		res.writes[f] = data
		res.Reencoded = append(res.Reencoded, f)
	}
	return nil
}

// unplaced returns the launch entries in a file that aprofile did not place and the
// profile does not want placed (FR-020b): env entries named in launch_clear, the
// auth-clearing list, the model or endpoint variable or the profile's extra
// variables; a key command; an auth block in the selected provider.
func unplaced(p *profile.Profile, def *harness.Definition, fl *harness.FolderLaunch, f string, fd FileDoc, rec Record, want desired) []Entry {
	if fd.Err != nil || !fd.Exists {
		return nil
	}
	var out []Entry
	if fl.Env != "" && fl.EnvFile() == f && fd.JSON != nil {
		cand := map[string]bool{}
		for _, v := range def.LaunchClear {
			cand[v] = true
		}
		for _, v := range def.UnsetAuthVars() {
			cand[v] = true
		}
		for _, v := range []string{def.ModelVar, def.EndpointVar} {
			if v != "" {
				cand[v] = true
			}
		}
		for _, e := range p.ExtraEnv {
			cand[e.Name] = true
		}
		wanted := map[string]bool{}
		for _, e := range want.env {
			wanted[e.name] = true
		}
		if raw, ok := fd.JSON[fl.EnvKey()]; ok {
			if envObj, err := layer.DecodeJSONObject(raw); err == nil {
				for name := range envObj {
					loc := fl.EnvKey() + "." + name
					if cand[name] && !wanted[name] && !rec.Has(f, loc) {
						out = append(out, Entry{File: f, Location: loc})
					}
				}
			}
		}
	}
	kc := fl.KeyCommand
	if kc != nil && kc.File() == f {
		switch {
		case kc.Form == harness.KeyFormShell && fd.JSON != nil:
			if _, has := fd.JSON[kc.Key()]; has && want.shellCmd == "" && !rec.Has(f, kc.Key()) {
				out = append(out, Entry{File: f, Location: kc.Key()})
			}
		case kc.Form == harness.KeyFormProviderAuth && fd.TOML != nil:
			if sel, ok := selectedProvider(fd.TOML, kc); ok {
				providers, _ := fd.TOML[kc.Key()].(map[string]any)
				blk, _ := providers[sel].(map[string]any)
				loc := kc.Key() + "." + sel + ".auth"
				if _, has := blk["auth"]; has && want.auth == nil && !rec.Has(f, loc) {
					out = append(out, Entry{File: f, Location: loc})
				}
			}
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// deepCopy copies a TOML/JSON-shaped value so a plan never mutates the Doc.
func deepCopy[T any](v T) T {
	return any(copyAny(any(v))).(T)
}

func copyAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		if t == nil {
			return t
		}
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = copyAny(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = copyAny(e)
		}
		return out
	}
	return v
}

// tomlEqual compares two TOML values by their canonical JSON form.
func tomlEqual(a, b any) bool {
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(ja, jb)
}

// setTOMLPath sets the value at a dotted path, creating the intermediate tables.
// It errors when a non-table value blocks the path.
func setTOMLPath(m map[string]any, path []string, v any) error {
	cur := m
	for _, seg := range path[:len(path)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			if _, exists := cur[seg]; exists {
				return fmt.Errorf("key %s is not a table", seg)
			}
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
	cur[path[len(path)-1]] = v
	return nil
}

// getTOMLPath returns the value at a dotted path, or ok=false.
func getTOMLPath(m map[string]any, path []string) (any, bool) {
	cur := m
	for _, seg := range path[:len(path)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	v, ok := cur[path[len(path)-1]]
	return v, ok
}

// deleteTOMLPath removes the leaf key at a dotted path, keeping the parent
// tables. It reports whether anything was removed.
func deleteTOMLPath(m map[string]any, path []string) bool {
	cur := m
	for _, seg := range path[:len(path)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			return false
		}
		cur = next
	}
	last := path[len(path)-1]
	if _, ok := cur[last]; !ok {
		return false
	}
	delete(cur, last)
	return true
}

// tomlEmptyValue reports whether v is the empty TOML string (a hand-written
// empty value is replaced without record, like a JSON empty).
func tomlEmptyValue(v any) bool {
	s, ok := v.(string)
	return ok && s == ""
}

// encodeTOMLSubtree renders a replaced TOML value (scalar or nested table) as
// TOML source for the record, wrapped under a single key so a table round-trips.
func encodeTOMLSubtree(v any) (string, error) {
	data, err := toml.Marshal(map[string]any{"value": v})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// decodeTOMLSubtree parses what encodeTOMLSubtree wrote.
func decodeTOMLSubtree(raw string) (any, error) {
	m := map[string]any{}
	if err := toml.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m["value"], nil
}

// encodeTOMLValue renders a scalar TOML value as TOML source (a string as a basic
// string), for the record of replaced keys.
func encodeTOMLValue(v any) (string, error) {
	if s, ok := v.(string); ok {
		return tomlBasicString(s), nil
	}
	data, err := toml.Marshal(map[string]any{"v": v})
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	enc, ok := strings.CutPrefix(line, "v = ")
	if !ok || strings.Contains(enc, "\n") {
		return "", fmt.Errorf("value cannot be recorded")
	}
	return enc, nil
}

func decodeTOMLValue(raw string) (any, error) {
	m := map[string]any{}
	if err := toml.Unmarshal([]byte("v = "+raw), &m); err != nil {
		return nil, err
	}
	return m["v"], nil
}

// tomlBasicString quotes s as a TOML basic string.
func tomlBasicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
