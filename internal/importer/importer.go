// Package importer performs a one-time migration of cc-deck profiles into aprofile
// (FR-045). Every cc-deck file is opened read-only; the importer never modifies or
// deletes a cc-deck file (Constitution Principle II). Credentials are copied verbatim
// into the new profile's config dir; the state layer is left fresh (empty).
//
// ASSUMED cc-deck on-disk layout (VERIFICATION ITEM: reconcile with the real cc-deck
// repository before release). The importer reads:
//
//	<source>/profiles/<harness>/<name>.yaml
//
// each file carrying:
//
//	harness, name, label, color, icon, primary, model, endpoint, auth_kind,
//	auth_refs: { VAR: "env:NAME" | "file:/abs/path" },
//	config_dir: <absolute path to cc-deck's live config dir for this profile>
//
// The live config dir holds the harness's real files. The importer classifies each
// entry with the harness definition and copies the credentials- and owned-layer
// entries (and, for a mixed file, only its credential and owned keys) into the new
// profile config dir. State entries are never copied; shared entries are left for a
// later `aprofile sync` to symlink. This layout is a placeholder: the field names and
// the credential source location must be confirmed against the real cc-deck schema.
package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// Options controls one import run.
type Options struct {
	DryRun               bool
	Overwrite            bool
	OverwriteCredentials bool
	// Now supplies the backup timestamp; nil uses time.Now.
	Now func() time.Time
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Action records the outcome for one cc-deck profile.
type Action struct {
	ID                  string   // "<harness>/<name>"
	Harness             string   // harness id
	Name                string   // profile name
	Status              string   // "import" | "overwrite" | "skip"
	Reason              string   // populated for a skip
	SkipExisting        bool     // true when skipped because (harness,name) already exists
	CredentialsReplaced bool     // true when --overwrite-credentials replaced credentials
	BackupDir           string   // where the replaced credentials were backed up
	Warnings            []string // non-fatal issues
}

// Result is the full import report.
type Result struct {
	SourceDir string
	Actions   []Action
}

// ExistingSkips returns the actions skipped because the profile already exists.
func (r *Result) ExistingSkips() []Action {
	var out []Action
	for _, a := range r.Actions {
		if a.SkipExisting {
			out = append(out, a)
		}
	}
	return out
}

// ccProfile is the assumed cc-deck metadata record (see the package comment).
type ccProfile struct {
	Harness   string            `yaml:"harness"`
	Name      string            `yaml:"name"`
	Label     string            `yaml:"label"`
	Color     string            `yaml:"color"`
	Icon      string            `yaml:"icon"`
	Primary   bool              `yaml:"primary"`
	Model     string            `yaml:"model"`
	Endpoint  string            `yaml:"endpoint"`
	AuthKind  string            `yaml:"auth_kind"`
	AuthRefs  map[string]string `yaml:"auth_refs"`
	ConfigDir string            `yaml:"config_dir"`
}

// Import reads cc-deck profiles from sourceDir and creates aprofile profiles. It
// never writes to sourceDir. A profile whose (harness, name) already exists is
// skipped unless --overwrite is set; the caller exits non-zero when any such skip
// occurs (FR-045).
func Import(paths *config.Paths, set *harness.Set, store *profile.Store, sourceDir string, opts Options) (*Result, error) {
	res := &Result{SourceDir: sourceDir}
	profiles, err := readCCProfiles(sourceDir)
	if err != nil {
		return nil, err
	}
	for _, cp := range profiles {
		act := Action{ID: cp.Harness + "/" + cp.Name, Harness: cp.Harness, Name: cp.Name}

		def, ok := set.Get(cp.Harness)
		if !ok {
			act.Status = "skip"
			act.Reason = fmt.Sprintf("no harness definition for %q", cp.Harness)
			res.Actions = append(res.Actions, act)
			continue
		}

		// --overwrite-credentials implies --overwrite: it additionally replaces
		// credentials (FR-045).
		proceed := opts.Overwrite || opts.OverwriteCredentials
		exists := store.Exists(cp.Harness, cp.Name)
		if exists && !proceed {
			act.Status = "skip"
			act.SkipExisting = true
			act.Reason = "profile already exists (use --overwrite or --overwrite-credentials)"
			res.Actions = append(res.Actions, act)
			continue
		}

		p, berr := buildProfile(cp)
		if berr != nil {
			act.Status = "skip"
			act.Reason = "invalid cc-deck profile: " + berr.Error()
			res.Actions = append(res.Actions, act)
			continue
		}

		if exists {
			act.Status = "overwrite"
		} else {
			act.Status = "import"
		}

		if opts.DryRun {
			res.Actions = append(res.Actions, act)
			continue
		}

		if err := store.Save(p); err != nil {
			return nil, fmt.Errorf("save profile %s: %w", act.ID, err)
		}
		destDir := paths.ProfileConfigDir(cp.Harness, cp.Name)
		if err := applyConfig(paths, def, cp, destDir, exists, opts, &act); err != nil {
			return nil, fmt.Errorf("import %s: %w", act.ID, err)
		}
		res.Actions = append(res.Actions, act)
	}
	return res, nil
}

// readCCProfiles reads every <source>/profiles/<harness>/*.yaml file. A missing
// profiles directory yields no profiles (not an error).
func readCCProfiles(sourceDir string) ([]ccProfile, error) {
	root := filepath.Join(sourceDir, "profiles")
	harnesses, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cc-deck profiles %s: %w", root, err)
	}
	var out []ccProfile
	for _, h := range harnesses {
		if !h.IsDir() {
			continue
		}
		hdir := filepath.Join(root, h.Name())
		files, err := os.ReadDir(hdir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdir, err)
		}
		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".yaml" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(hdir, f.Name()))
			if err != nil {
				return nil, err
			}
			var cp ccProfile
			if err := yaml.Unmarshal(data, &cp); err != nil {
				return nil, fmt.Errorf("parse cc-deck profile %s: %w", f.Name(), err)
			}
			if cp.Harness == "" {
				cp.Harness = h.Name()
			}
			if cp.Name == "" {
				cp.Name = strings.TrimSuffix(f.Name(), ".yaml")
			}
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Harness != out[j].Harness {
			return out[i].Harness < out[j].Harness
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// buildProfile maps a cc-deck record to an aprofile profile, keeping auth values as
// references (FR-045, Principle IV).
func buildProfile(cp ccProfile) (*profile.Profile, error) {
	p := &profile.Profile{
		Harness:  cp.Harness,
		Name:     cp.Name,
		Label:    cp.Label,
		Color:    cp.Color,
		Icon:     cp.Icon,
		Primary:  cp.Primary,
		Model:    cp.Model,
		Endpoint: cp.Endpoint,
		AuthKind: cp.AuthKind,
	}
	if len(cp.AuthRefs) > 0 {
		p.AuthRefs = map[string]secret.Reference{}
		for v, raw := range cp.AuthRefs {
			ref, err := secret.ParseReference(raw)
			if err != nil {
				return nil, fmt.Errorf("auth ref %q: %w", v, err)
			}
			p.AuthRefs[v] = ref
		}
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// applyConfig copies the credentials and owned layers from the cc-deck live config
// dir into the new profile config dir, honoring the overwrite flags and backing up
// replaced credentials (FR-045). State is never copied; shared is left for sync.
func applyConfig(paths *config.Paths, def *harness.Definition, cp ccProfile, destDir string, exists bool, opts Options, act *Action) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	srcDir := cp.ConfigDir
	if srcDir == "" || !filepath.IsAbs(srcDir) {
		act.Warnings = append(act.Warnings, "cc-deck config_dir is empty or not absolute; credentials and owned files were not imported")
		return nil
	}

	var backupDir string
	ensureBackup := func() (string, error) {
		if backupDir == "" {
			ts := opts.now().UTC().Format("20060102-150405.000000")
			backupDir = paths.BackupDir(cp.Harness, cp.Name, ts)
			if err := os.MkdirAll(backupDir, 0o755); err != nil {
				return "", err
			}
			act.BackupDir = backupDir
		}
		return backupDir, nil
	}

	for i := range def.Classification {
		rule := &def.Classification[i]
		if isGlob(rule.Entry) {
			continue // shared catch-all: handled by sync, never imported
		}
		rel := strings.TrimSuffix(rule.Entry, "/")
		src := filepath.Join(srcDir, filepath.FromSlash(rel))
		dst := filepath.Join(destDir, filepath.FromSlash(rel))
		// Defense in depth: the loader already rejects ".."/absolute entries, but a copy
		// here (and copyTree's RemoveAll) is destructive, so never act on a dst that
		// resolves outside the profile dir.
		if !within(destDir, dst) {
			act.Warnings = append(act.Warnings, fmt.Sprintf("skipped entry %q: resolves outside the profile dir", rule.Entry))
			continue
		}

		switch rule.Layer {
		case harness.LayerOwned:
			// Owned is replaced on every proceeding import (new, --overwrite,
			// --overwrite-credentials).
			if pathExists(src) {
				if err := copyPath(src, dst); err != nil {
					return err
				}
			}
		case harness.LayerCredentials:
			switch {
			case !exists:
				if pathExists(src) {
					if err := copyPath(src, dst); err != nil {
						return err
					}
				}
			case opts.OverwriteCredentials:
				if pathExists(dst) {
					bdir, err := ensureBackup()
					if err != nil {
						return err
					}
					if err := copyPath(dst, filepath.Join(bdir, filepath.FromSlash(rel))); err != nil {
						return err
					}
				}
				if pathExists(src) {
					if err := copyPath(src, dst); err != nil {
						return err
					}
					act.CredentialsReplaced = true
				}
			default:
				// --overwrite (not credentials): keep existing credentials.
			}
		case harness.LayerMixed:
			if err := applyMixed(rule, src, dst, rel, exists, opts, ensureBackup, act); err != nil {
				return err
			}
		case harness.LayerState, harness.LayerShared:
			// State stays fresh; shared is symlinked by sync.
		}
	}
	return nil
}

// applyMixed handles a mixed JSON file: on a new import it writes the credential and
// owned keys from the source; on an overwrite it replaces owned keys (and, with
// --overwrite-credentials, credential keys after backing up the whole file), leaving
// every other key untouched (FR-045, FR-013).
func applyMixed(rule *harness.EntryRule, src, dst, rel string, exists bool, opts Options, ensureBackup func() (string, error), act *Action) error {
	if !isJSONExt(filepath.Ext(dst)) {
		act.Warnings = append(act.Warnings, fmt.Sprintf("mixed file %q is not JSON; skipped", rel))
		return nil
	}
	srcMap, err := readJSONObject(src)
	if err != nil {
		return fmt.Errorf("read source mixed file %s: %w", src, err)
	}

	if !exists {
		out := map[string]json.RawMessage{}
		for k, l := range rule.Keys {
			if l == harness.LayerCredentials || l == harness.LayerOwned {
				if v, ok := srcMap[k]; ok {
					out[k] = v
				}
			}
		}
		return writeJSONObject(dst, out)
	}

	dstMap, err := readJSONObject(dst)
	if err != nil {
		return fmt.Errorf("read target mixed file %s: %w", dst, err)
	}
	if opts.OverwriteCredentials && pathExists(dst) {
		bdir, err := ensureBackup()
		if err != nil {
			return err
		}
		if err := copyPath(dst, filepath.Join(bdir, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	for k, l := range rule.Keys {
		if l == harness.LayerOwned {
			if v, ok := srcMap[k]; ok {
				dstMap[k] = v
			}
		}
	}
	if opts.OverwriteCredentials {
		for k, l := range rule.Keys {
			if l == harness.LayerCredentials {
				if v, ok := srcMap[k]; ok {
					dstMap[k] = v
					act.CredentialsReplaced = true
				}
			}
		}
	}
	return writeJSONObject(dst, dstMap)
}

// --- small filesystem and JSON helpers (leaf; no harness-name branching) ---

func isGlob(s string) bool { return strings.ContainsAny(s, "*?[") }

// within reports whether target is base or a path inside it.
func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isJSONExt(ext string) bool { return strings.EqualFold(ext, ".json") }

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// copyPath copies a file or directory tree verbatim, replacing any existing dst.
func copyPath(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return copyTree(src, dst)
	}
	return copyFile(src, dst, fi.Mode())
}

func copyTree(src, dst string) error {
	_ = os.RemoveAll(dst)
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// readJSONObject reads a JSON object as key->raw-value, preserving each value's
// bytes. A missing or empty file yields an empty map.
func readJSONObject(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]json.RawMessage{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}

func writeJSONObject(path string, m map[string]json.RawMessage) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, append(out, '\n'), 0o600)
}
