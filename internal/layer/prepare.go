package layer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
)

// Change describes one filesystem change made (or, under dry-run, that would be
// made) while preparing a config dir. Mutating actions (mkdir, symlink, seed,
// fix-link) alter the filesystem; informational actions (keep-real, leaking-link,
// skip-unclassified) report a condition without changing anything, so an otherwise
// unchanged sync stays idempotent (SC-003).
type Change struct {
	Action string // mkdir | symlink | seed | fix-link | keep-real | leaking-link | skip-unclassified
	Path   string
	Note   string
}

func (c Change) String() string {
	if c.Note != "" {
		return fmt.Sprintf("%s %s (%s)", c.Action, c.Path, c.Note)
	}
	return fmt.Sprintf("%s %s", c.Action, c.Path)
}

// Mutating reports whether the change altered the filesystem. Informational changes
// (reported conditions) return false so callers do not count them as work done.
func (c Change) Mutating() bool {
	switch c.Action {
	case "mkdir", "symlink", "seed", "fix-link":
		return true
	default:
		return false
	}
}

// PrepareOptions controls one config-dir preparation.
type PrepareOptions struct {
	DryRun bool
	// FixLinks replaces a leaking state/owned/credentials symlink with an empty real
	// entry, never deleting the link target (FR-018, edge cases). Without it, such a
	// link is reported but left in place.
	FixLinks bool
}

// PrepareConfigDir creates a profile's config dir according to its layers (FR-013
// through FR-018):
//
//   - the config dir root is created;
//   - state and owned directory entries are created empty and real;
//   - credentials files are left for the harness/login to create;
//   - shared entries are symlinked to the default setup, lazily (only when the
//     default setup already has them) and never over a real file that exists;
//   - mixed files are seeded once from the default setup's file when it exists,
//     otherwise created minimal.
//
// It is idempotent: an already-prepared dir yields no changes.
func PrepareConfigDir(def *harness.Definition, configDir string, dryRun bool) ([]Change, error) {
	return PrepareConfigDirWith(def, configDir, PrepareOptions{DryRun: dryRun})
}

// PrepareConfigDirWith is PrepareConfigDir with explicit options. It additionally
// enforces the isolation rules (FR-014..FR-018): a state/owned/credentials entry
// that is a symlink is a leak (for example a projects/ dir linked to the default
// setup); with FixLinks it is replaced by an empty real entry (the link target is
// never deleted), otherwise it is reported. Entries the definition does not classify
// are reported, never linked or copied (FR-018).
func PrepareConfigDirWith(def *harness.Definition, configDir string, opts PrepareOptions) ([]Change, error) {
	var changes []Change

	made, err := ensureDir(configDir, opts.DryRun)
	if err != nil {
		return nil, err
	}
	if made {
		changes = append(changes, Change{Action: "mkdir", Path: configDir})
	}

	defaultSetup := expandHome(def.DefaultConfigLocation)

	// Pass 1: concrete (non-glob) rules.
	for i := range def.Classification {
		rule := &def.Classification[i]
		if isGlob(rule.Entry) {
			continue
		}
		entryPath := filepath.Join(configDir, filepath.FromSlash(normalize(rule.Entry)))
		isDir := strings.HasSuffix(rule.Entry, "/")
		switch rule.Layer {
		case harness.LayerMixed:
			c, err := seedMixedFile(entryPath, filepath.Join(defaultSetup, filepath.FromSlash(normalize(rule.Entry))), rule, opts.DryRun)
			if err != nil {
				return nil, err
			}
			if c != nil {
				changes = append(changes, *c)
			}
		case harness.LayerState, harness.LayerOwned, harness.LayerCredentials:
			// These must be per-profile real entries, never links (FR-014/015/016).
			if link, target := symlinkInfo(entryPath); link {
				c, err := handleLeakingLink(entryPath, target, rule.Layer, isDir, defaultSetup, opts)
				if err != nil {
					return nil, err
				}
				changes = append(changes, *c)
				if !opts.FixLinks {
					continue // leave the link in place until the user confirms with --fix-links
				}
			}
			// Credentials files are created by the harness/login; only state/owned dir
			// entries are created empty and real here.
			if isDir && rule.Layer != harness.LayerCredentials {
				made, err := ensureDir(entryPath, opts.DryRun)
				if err != nil {
					return nil, err
				}
				if made {
					changes = append(changes, Change{Action: "mkdir", Path: entryPath})
				}
			}
		case harness.LayerShared:
			target := filepath.Join(defaultSetup, filepath.FromSlash(normalize(rule.Entry)))
			if c, err := linkShared(entryPath, target, opts.DryRun); err != nil {
				return nil, err
			} else if c != nil {
				changes = append(changes, *c)
			}
		}
	}

	// Pass 2: the shared catch-all. Enumerate the default setup and link every
	// entry that classifies as shared and is not already present as a real file.
	if hasSharedGlob(def) && dirExists(defaultSetup) {
		entries, err := os.ReadDir(defaultSetup)
		if err != nil {
			return nil, fmt.Errorf("read default setup %s: %w", defaultSetup, err)
		}
		for _, e := range entries {
			res := Classify(def, e.Name())
			if res.Unclassified || res.Layer != harness.LayerShared {
				continue
			}
			entryPath := filepath.Join(configDir, e.Name())
			target := filepath.Join(defaultSetup, e.Name())
			if c, err := linkShared(entryPath, target, opts.DryRun); err != nil {
				return nil, err
			} else if c != nil {
				changes = append(changes, *c)
			}
		}
	}

	// Pass 3: report entries the definition does not classify (FR-018). They are
	// never linked or copied; they are left exactly where they are.
	unclassified, err := Unclassified(def, configDir)
	if err != nil {
		return nil, err
	}
	for _, name := range unclassified {
		changes = append(changes, Change{
			Action: "skip-unclassified",
			Path:   filepath.Join(configDir, name),
			Note:   "entry is not classified by the harness definition; left in place",
		})
	}

	return changes, nil
}

// Unclassified returns the sorted names of the direct entries of configDir that the
// definition's classification does not cover (FR-018). sync and doctor report these;
// they are never linked or copied. A missing config dir yields no entries.
func Unclassified(def *harness.Definition, configDir string) ([]string, error) {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config dir %s: %w", configDir, err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		// A directory that is the prefix of a nested classification entry (for example
		// OpenCode's "data" for "data/opencode/auth.json", or pi's "agent") is a
		// container of classified entries, not itself unclassified.
		if Classify(def, name).Unclassified && !containerOfClassified(def, name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// containerOfClassified reports whether name is the top path segment of some nested
// classification entry, so it holds classified entries rather than being unclassified.
func containerOfClassified(def *harness.Definition, name string) bool {
	prefix := name + "/"
	for _, r := range def.Classification {
		if strings.HasPrefix(filepath.ToSlash(r.Entry), prefix) {
			return true
		}
	}
	return false
}

// symlinkInfo reports whether path is a symlink and, if so, its target.
func symlinkInfo(path string) (isLink bool, target string) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false, ""
	}
	target, _ = os.Readlink(path)
	return true, target
}

// handleLeakingLink reports (and, with FixLinks, repairs) a state/owned/credentials
// entry that is a symlink. Repair removes the link itself and creates an empty real
// entry; it never deletes the link target (edge cases, Constitution II).
func handleLeakingLink(path, target string, l harness.Layer, isDir bool, defaultSetup string, opts PrepareOptions) (*Change, error) {
	if !opts.FixLinks {
		note := fmt.Sprintf("%s entry is a symlink -> %s; run: aprofile sync --fix-links", l, target)
		if leaksToDefault(target, defaultSetup) {
			note = fmt.Sprintf("%s entry leaks to the shared default setup (%s); run: aprofile sync --fix-links", l, target)
		}
		return &Change{Action: "leaking-link", Path: path, Note: note}, nil
	}
	note := "replaced a leaking link with an empty real entry (link target not deleted)"
	if opts.DryRun {
		return &Change{Action: "fix-link", Path: path, Note: note}, nil
	}
	if err := os.Remove(path); err != nil { // removes the symlink, never its target
		return nil, fmt.Errorf("remove leaking link %s: %w", path, err)
	}
	if isDir {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, fmt.Errorf("create real entry %s: %w", path, err)
		}
	} else {
		if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
			return nil, fmt.Errorf("create real entry %s: %w", path, err)
		}
	}
	return &Change{Action: "fix-link", Path: path, Note: note}, nil
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

// linkShared ensures dst is a symlink to target, lazily and non-destructively:
//   - if target does not exist, do nothing (links are created on a later sync once
//     the default setup appears);
//   - if dst already exists as the correct symlink, do nothing;
//   - if dst exists as a real file/dir, keep it and report (FR-017 edge case);
//   - otherwise create the symlink.
func linkShared(dst, target string, dryRun bool) (*Change, error) {
	if !pathExists(target) {
		return nil, nil // lazy
	}
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if cur, _ := os.Readlink(dst); cur == target {
				return nil, nil // already correct
			}
		} else {
			return &Change{Action: "keep-real", Path: dst, Note: "real entry kept; not replaced by a shared link"}, nil
		}
	}
	if dryRun {
		return &Change{Action: "symlink", Path: dst, Note: "-> " + target}, nil
	}
	_ = os.Remove(dst) // remove a stale/incorrect symlink only (real files handled above)
	if err := os.Symlink(target, dst); err != nil {
		return nil, fmt.Errorf("symlink %s -> %s: %w", dst, target, err)
	}
	return &Change{Action: "symlink", Path: dst, Note: "-> " + target}, nil
}

// seedMixedFile creates a mixed file once if it is absent: seeding from the default
// setup's file when it exists (with credential- and state-layer keys removed so a
// new profile never inherits the default account's credentials (FR-014) or state
// (FR-015)), otherwise writing a minimal document. It never rewrites an existing
// file (FR-013). The full owned-only merge and uncovered-key reporting are T029.
func seedMixedFile(dst, defaultFile string, rule *harness.EntryRule, dryRun bool) (*Change, error) {
	if pathExists(dst) {
		return nil, nil
	}
	note := "minimal"
	var content []byte
	if data, err := os.ReadFile(defaultFile); err == nil {
		content = stripNonSeedKeys(data, rule, dst)
		note = "seeded from default setup (credentials and state keys excluded)"
	} else {
		content = minimalDoc(dst)
	}
	if dryRun {
		return &Change{Action: "seed", Path: dst, Note: note}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		return nil, fmt.Errorf("seed mixed file %s: %w", dst, err)
	}
	return &Change{Action: "seed", Path: dst, Note: note}, nil
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
	case ".json":
		return []byte("{}\n")
	default:
		return []byte("")
	}
}

func hasSharedGlob(def *harness.Definition) bool {
	for _, r := range def.Classification {
		if isGlob(r.Entry) && r.Layer == harness.LayerShared {
			return true
		}
	}
	return false
}

func isGlob(s string) bool { return strings.ContainsAny(s, "*?[") }

func ensureDir(path string, dryRun bool) (made bool, err error) {
	if dirExists(path) {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", path, err)
	}
	return true, nil
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// expandHome expands a leading ~ to the current HOME.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}
