// Package importer performs a one-time migration of cc-deck profiles into aprofile
// (FR-045). Every cc-deck file is opened read-only; the importer never modifies or
// deletes a cc-deck file (Constitution Principle II). The cc-deck layout it reads is
// documented in ccdeck.go.
//
// For each cc-deck profile the importer writes:
//
//   - the aprofile profile store file, mapped from cc-deck's config.yaml entry
//     (auth, env, model, backend/project/region, color, icon; default_profile
//     becomes primary);
//   - the file-sourced credentials cc-deck holds (api_key, oauth_token,
//     credentials), copied verbatim at 0600 into
//     <aprofile data dir>/credentials/<harness>/<name>/<field> and referenced from
//     the profile (file: reference or a path variable), with a warning;
//   - the credentials- and owned-layer entries of cc-deck's live harness config
//     dir (for a mixed file, only its credential and owned keys, merged onto
//     what sync seeds for the share level), classified by the harness
//     definition. State is never copied; content, settings, the plugin store and
//     the hook location are left for `aprofile sync`.
//
// A profile that fails partway is reported as failed and rolled back if it was
// new; the other profiles are still imported (FR-045).
package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// Options controls one import run.
type Options struct {
	DryRun               bool
	Overwrite            bool
	OverwriteCredentials bool
	// Share is the share level imported profiles get (FR-007): --share or the
	// global default. Empty means content, the built-in default.
	Share profile.Share
	// SourceDataDir is cc-deck's data dir, which holds the live harness config
	// dirs. Empty means the cc-deck dir beside the aprofile data dir.
	SourceDataDir string
	// Now supplies the backup timestamp; nil uses time.Now.
	Now func() time.Time
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// StatusFailed marks a profile whose import failed partway; a new profile is
// rolled back, an existing one keeps what was written before the failure.
const StatusFailed = "failed"

// Action records the outcome for one cc-deck profile.
type Action struct {
	ID                  string   // "<harness>/<name>"
	Harness             string   // harness id
	Name                string   // profile name
	Status              string   // "import" | "overwrite" | "skip" | "failed"
	Reason              string   // populated for a skip or a failure
	SkipExisting        bool     // true when skipped because (harness,name) already exists
	SkipClash           bool     // true when skipped because another profile has its wrapper name (FR-002)
	CredentialsReplaced bool     // true when --overwrite-credentials replaced credentials
	BackupDir           string   // where the replaced credentials were backed up
	PrimaryMovedFrom    []string // profiles of the same harness that lost the primary flag (FR-001)
	Warnings            []string // non-fatal issues; never carry credential contents
}

// Result is the full import report.
type Result struct {
	SourceDir     string
	SourceDataDir string
	Warnings      []string // run-wide issues (for example an ambiguous default_profile)
	Actions       []Action
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

// Failures returns the actions whose import failed partway (Status "failed"). The
// other profiles of the run are still imported; the caller exits non-zero (FR-045).
func (r *Result) Failures() []Action {
	var out []Action
	for _, a := range r.Actions {
		if a.Status == StatusFailed {
			out = append(out, a)
		}
	}
	return out
}

// ClashSkips returns the actions skipped because another profile already has the
// wrapper name the imported profile would get (FR-002).
func (r *Result) ClashSkips() []Action {
	var out []Action
	for _, a := range r.Actions {
		if a.SkipClash {
			out = append(out, a)
		}
	}
	return out
}

// CredentialsDir is where import keeps the credential files it copies from cc-deck
// for one profile. It lies outside the profile config dir so the harness never sees
// an unclassified entry.
func CredentialsDir(paths *config.Paths, harnessName, name string) string {
	return paths.CredentialsDir(harnessName, name)
}

// Import reads cc-deck profiles from sourceDir (cc-deck's config dir) and creates
// aprofile profiles. It never writes to cc-deck's dirs. A profile whose
// (harness, name) already exists is skipped unless --overwrite is set; the caller
// exits non-zero when any such skip occurs (FR-045).
func Import(paths *config.Paths, set *harness.Set, store *profile.Store, sourceDir string, opts Options) (*Result, error) {
	dataDir := opts.SourceDataDir
	if dataDir == "" {
		dataDir = filepath.Join(filepath.Dir(paths.DataDir), "cc-deck")
	}
	res := &Result{SourceDir: sourceDir, SourceDataDir: dataDir}
	entries, warnings, err := readCCDeck(sourceDir)
	if err != nil {
		return nil, err
	}
	res.Warnings = warnings

	// Every stored profile, plus each one this run creates (or, in a dry run, would
	// create), so a wrapper-name clash is caught against both (FR-002).
	keys, err := storedKeys(store)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		act := Action{ID: e.Harness + "/" + e.Name, Harness: e.Harness, Name: e.Name}

		def, ok := set.Get(e.Harness)
		if !ok {
			act.Status = "skip"
			act.Reason = fmt.Sprintf("no harness definition for %q", e.Harness)
			res.Actions = append(res.Actions, act)
			continue
		}

		// --overwrite-credentials implies --overwrite: it additionally replaces
		// credentials (FR-045).
		proceed := opts.Overwrite || opts.OverwriteCredentials
		exists := store.Exists(e.Harness, e.Name)
		if exists && !proceed {
			act.Status = "skip"
			act.SkipExisting = true
			act.Reason = "profile already exists (use --overwrite or --overwrite-credentials)"
			res.Actions = append(res.Actions, act)
			continue
		}

		if other, clash := set.WrapperClash(e.Harness, e.Name, keys, ""); clash {
			wrapper, _ := set.WrapperName(e.Harness, e.Name)
			act.Status = "skip"
			act.SkipClash = true
			act.Reason = fmt.Sprintf("wrapper name %q is already used by profile %s; import it under another name", wrapper, other.ID())
			res.Actions = append(res.Actions, act)
			continue
		}

		pl, berr := buildProfile(e, def, opts.Share, sourceDir, CredentialsDir(paths, e.Harness, e.Name))
		if berr != nil {
			act.Status = "skip"
			act.Reason = "cannot import cc-deck profile: " + berr.Error()
			res.Actions = append(res.Actions, act)
			continue
		}
		act.Warnings = append(act.Warnings, pl.warnings...)

		if exists {
			act.Status = "overwrite"
		} else {
			act.Status = "import"
		}

		if opts.DryRun {
			if !exists {
				keys = append(keys, harness.ProfileKey{Harness: e.Harness, Name: e.Name})
			}
			if pl.profile.Primary {
				act.PrimaryMovedFrom = otherPrimaries(store, pl.profile)
			}
			res.Actions = append(res.Actions, act)
			continue
		}

		// A failure is recorded for this profile and the run goes on: the others are
		// still imported and the caller exits non-zero (FR-045).
		if err := importOne(paths, store, def, pl, dataDir, exists, opts, &act); err != nil {
			act.Status = StatusFailed
			act.Reason = err.Error()
			res.Actions = append(res.Actions, act)
			continue
		}
		if !exists {
			keys = append(keys, harness.ProfileKey{Harness: e.Harness, Name: e.Name})
		}
		res.Actions = append(res.Actions, act)
	}
	return res, nil
}

// importOne writes one profile. applyConfig checks every mixed file of the source
// and target before it writes anything; the config dir and credential files come
// next and the store file last, so the profile becomes visible only when its files
// are in place. When a new profile fails, everything import created for it is
// removed again (the store file, and the config and credentials dirs unless they
// existed before); an existing profile keeps what was written before the failure,
// with the replaced credentials in the backup dir.
func importOne(paths *config.Paths, store *profile.Store, def *harness.Definition, pl *importPlan, dataDir string, exists bool, opts Options, act *Action) (err error) {
	p := pl.profile
	destDir := paths.ProfileConfigDir(p.Harness, p.Name)
	if !exists {
		var created []string
		for _, d := range []string{destDir, CredentialsDir(paths, p.Harness, p.Name)} {
			if !layer.PathExists(d) {
				created = append(created, d)
			}
		}
		defer func() {
			if err == nil {
				return
			}
			var cleanup []string
			if rerr := os.Remove(store.Path(p.Harness, p.Name)); rerr != nil && !os.IsNotExist(rerr) {
				cleanup = append(cleanup, rerr.Error())
			}
			for _, d := range created {
				if rerr := os.RemoveAll(d); rerr != nil {
					cleanup = append(cleanup, rerr.Error())
				}
			}
			if len(cleanup) > 0 {
				err = fmt.Errorf("%w (cleanup also failed: %s)", err, strings.Join(cleanup, "; "))
			}
		}()
	}

	bk := &backup{paths: paths, harness: p.Harness, name: p.Name, now: opts.now, act: act}
	live := ccLiveDir(dataDir, p.Harness, p.Name)
	if err := applyConfig(def, live, destDir, exists, p.ShareLevel(), opts, bk, act); err != nil {
		return err
	}
	if err := applyCredentialFiles(pl.creds, exists, opts, bk, act); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	if err := store.Save(p); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	if p.Primary {
		moved, err := movePrimary(store, p)
		if err != nil {
			return fmt.Errorf("move primary flag: %w", err)
		}
		act.PrimaryMovedFrom = moved
	}
	return nil
}

// storedKeys lists the (harness, name) of every stored profile, including those
// whose file fails to load.
func storedKeys(store *profile.Store) ([]harness.ProfileKey, error) {
	profiles, failures, err := store.List()
	if err != nil {
		return nil, err
	}
	keys := make([]harness.ProfileKey, 0, len(profiles)+len(failures))
	for _, p := range profiles {
		keys = append(keys, harness.ProfileKey{Harness: p.Harness, Name: p.Name})
	}
	for _, f := range failures {
		keys = append(keys, harness.ProfileKey{Harness: f.Harness, Name: f.Name})
	}
	return keys, nil
}

// otherPrimaries lists the profiles of p's harness, other than p, that carry the
// primary flag. Store files that fail to load are skipped (List reports them
// elsewhere).
func otherPrimaries(store *profile.Store, p *profile.Profile) []string {
	all, _, err := store.List()
	if err != nil {
		return nil
	}
	var out []string
	for _, peer := range all {
		if peer.Harness == p.Harness && peer.Name != p.Name && peer.Primary {
			out = append(out, peer.ID())
		}
	}
	return out
}

// movePrimary clears the primary flag of every other profile of p's harness, so
// at most one stays primary (FR-001), and returns the profiles it cleared.
func movePrimary(store *profile.Store, p *profile.Profile) ([]string, error) {
	ids := otherPrimaries(store, p)
	for _, id := range ids {
		peer, err := store.Load(p.Harness, strings.TrimPrefix(id, p.Harness+"/"))
		if err != nil {
			return nil, err
		}
		peer.Primary = false
		if err := store.Save(peer); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// credFile is one file-sourced cc-deck credential to copy.
type credFile struct {
	field string // cc-deck auth field; also the file name in both credential dirs
	src   string // the file to copy; "" when none was found
	dest  string // the aprofile copy the profile references
}

type importPlan struct {
	profile  *profile.Profile
	creds    []credFile
	warnings []string
}

// buildProfile maps a cc-deck entry to an aprofile profile using only the harness
// definition's data and the cc-deck field and backend tables in ccdeck.go. Auth
// values stay references (Principle IV); file-sourced credentials are planned as
// copies. An error means the profile cannot be expressed in aprofile.
func buildProfile(e ccEntry, def *harness.Definition, share profile.Share, sourceDir, credDir string) (*importPlan, error) {
	if share == "" {
		share = profile.Share(config.BuiltinDefaultShare)
	}
	cp := e.Profile
	pl := &importPlan{}
	warn := func(format string, args ...any) { pl.warnings = append(pl.warnings, fmt.Sprintf(format, args...)) }
	p := &profile.Profile{
		Share:   share,
		Harness: e.Harness,
		Name:    e.Name,
		Primary: e.Primary,
		Model:   cp.Model,
	}
	pl.profile = p

	// Appearance (FR-001). cc-deck shows a name-derived color when none is valid.
	switch {
	case cp.Color == "":
		p.Color = ccDeriveColor(e.Name)
	case profile.ValidateColor(cp.Color) != nil:
		p.Color = ccDeriveColor(e.Name)
		warn("cc-deck color %q is not an ANSI color name or #rrggbb; using %s, the color cc-deck derived from the name", cp.Color, p.Color)
	default:
		p.Color = cp.Color
	}
	if err := profile.ValidateIcon(cp.Icon); err != nil {
		warn("cc-deck icon %q is not a single grapheme; left empty", cp.Icon)
	} else {
		p.Icon = cp.Icon
	}

	backend, ok := ccBackends[cp.Backend]
	if !ok {
		return nil, fmt.Errorf("unknown cc-deck backend %q", cp.Backend)
	}

	auth := ccAuth{}
	if cp.Auth != nil {
		auth = *cp.Auth
	}
	// Legacy Kubernetes secret fields map onto secret sources, as cc-deck does.
	if cp.APIKeySecret != "" && auth.APIKey == nil {
		auth.APIKey = &ccSource{Secret: cp.APIKeySecret}
	}
	if cp.CredentialsSecret != "" && auth.Credentials == nil {
		auth.Credentials = &ccSource{Secret: cp.CredentialsSecret}
	}
	sources := map[string]*ccSource{fieldAPIKey: auth.APIKey, fieldOAuthToken: auth.OAuthToken, fieldCredentials: auth.Credentials}
	for _, field := range []string{fieldAPIKey, fieldOAuthToken, fieldCredentials} {
		if src := sources[field]; src != nil && src.Env == "" && src.File == "" {
			if src.Secret != "" {
				return nil, fmt.Errorf("auth.%s uses a Kubernetes secret source, which aprofile cannot reference", field)
			}
			return nil, fmt.Errorf("auth.%s names no env or file source", field)
		}
	}

	// fileRef plans the copy of a file-sourced credential and returns the path the
	// profile will reference.
	fileRef := func(field string, src *ccSource) string {
		cf := credFile{field: field, dest: filepath.Join(credDir, field)}
		candidates := []string{ccCredCopy(sourceDir, e.Harness, e.Name, field)}
		if declared := expandHome(src.File); filepath.IsAbs(declared) {
			candidates = append(candidates, declared)
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && fi.Mode().IsRegular() {
				cf.src = c
				break
			}
		}
		if cf.src == "" {
			warn("credential file for auth.%s not found (looked at %s); the profile references %s, which stays missing until you create it", field, strings.Join(candidates, ", "), cf.dest)
		}
		pl.creds = append(pl.creds, cf)
		return cf.dest
	}

	// Auth kind. cc-deck's wrapper ignores every key when login is set, and prefers
	// oauth_token over api_key.
	var keyField, keyKind string
	var keySrc *ccSource
	if !auth.Login {
		for _, k := range keyAuthKinds {
			src := sources[k.field]
			if src == nil {
				continue
			}
			if keySrc != nil {
				warn("cc-deck auth.%s ignored: auth.%s takes precedence, as in cc-deck", k.field, keyField)
				continue
			}
			keyField, keyKind, keySrc = k.field, k.kind, src
		}
	} else if auth.APIKey != nil || auth.OAuthToken != nil || auth.Credentials != nil {
		warn("cc-deck auth.login is set, so its other auth sources are ignored, as in cc-deck")
	}
	if keySrc != nil {
		kind, ok := def.AuthKind(keyKind)
		if !ok {
			return nil, fmt.Errorf("harness definition %q declares no %q auth kind for cc-deck auth.%s", def.Name, keyKind, keyField)
		}
		vars := kind.VarNames()
		if len(vars) != 1 {
			return nil, fmt.Errorf("auth kind %q of harness %q sets %d variables; cc-deck auth.%s needs exactly one", keyKind, def.Name, len(vars), keyField)
		}
		raw := "env:" + keySrc.Env
		if keySrc.Env == "" {
			raw = "file:" + fileRef(keyField, keySrc)
		}
		ref, err := secret.ParseReference(raw)
		if err != nil {
			return nil, fmt.Errorf("auth.%s: %w", keyField, err)
		}
		p.AuthKind = kind.Name
		p.AuthRefs = map[string]secret.Reference{vars[0]: ref}
	} else {
		name, ok := loginKind(def)
		if !ok {
			return nil, fmt.Errorf("harness definition %q declares no auth kind without variables for cc-deck's login", def.Name)
		}
		p.AuthKind = name
	}

	// Environment: backend variables first, then the profile's env, which wins on a
	// clash (cc-deck exports env after the backend lines).
	extra := &envList{}
	for _, kv := range backend.expand(cp) {
		extra.set(profile.ExtraEnvVar{Name: kv[0], Value: kv[1], Literal: true})
	}
	if auth.Credentials != nil && !auth.Login {
		switch {
		case backend.credentialsVar == "":
			warn("cc-deck auth.credentials ignored: backend %q takes no credentials file", cp.Backend)
		case auth.Credentials.Env != "":
			extra.set(profile.ExtraEnvVar{Name: backend.credentialsVar, Value: "env:" + auth.Credentials.Env})
		default:
			extra.set(profile.ExtraEnvVar{Name: backend.credentialsVar, Value: fileRef(fieldCredentials, auth.Credentials), Literal: true})
		}
	}
	reserved := map[string]string{}
	for _, v := range def.AllAuthVars() {
		reserved[v] = "is an auth variable; cc-deck auth maps to the profile's auth kind instead"
	}
	for _, ce := range def.ConfigEnv {
		reserved[ce.Var] = "is the harness's config dir redirect"
	}
	for _, name := range sortedKeys(cp.Env) {
		val := cp.Env[name]
		switch {
		case def.ModelVar != "" && name == def.ModelVar:
			p.Model = val
			continue
		case def.EndpointVar != "" && name == def.EndpointVar:
			p.Endpoint = val
			continue
		case !secret.ValidEnvVarName(name):
			warn("cc-deck env %q dropped: not a valid variable name", name)
			continue
		case strings.HasPrefix(name, "APROFILE_"):
			warn("cc-deck env %s dropped: APROFILE_ variables are set by aprofile", name)
			continue
		case reserved[name] != "":
			warn("cc-deck env %s dropped: it %s", name, reserved[name])
			continue
		}
		// One secret rule (internal/secret): a value with a secret prefix (in any
		// token) is dropped; one that matches only the length check is imported as a
		// literal with a warning, since the store skips the length check for literals.
		if secret.HasSecretPrefix(val) {
			warn("cc-deck env %s dropped: its value looks like a secret; add it as a reference with: aprofile edit %s/%s --env %s=env:VAR", name, e.Harness, e.Name, name)
			continue
		}
		if secret.LooksLikeLiteralSecret(val, secret.Options{}) {
			warn("cc-deck env %s imported as a literal although it is long and has no spaces; check that it is not a secret", name)
		}
		extra.set(profile.ExtraEnvVar{Name: name, Value: val, Literal: true})
	}
	p.ExtraEnv = extra.vars
	if p.Model != "" && def.ModelVar == "" {
		warn("model %q kept in the profile, but harness %q declares no model variable, so the wrapper does not set it", p.Model, def.Name)
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}
	return pl, nil
}

// loginKind returns the harness's auth kind without variables (an interactive
// login), preferring the definition's default kind.
func loginKind(def *harness.Definition) (string, bool) {
	if k, ok := def.AuthKind(def.DefaultAuthKind); ok && len(k.Vars) == 0 {
		return k.Name, true
	}
	for _, k := range def.AuthKinds {
		if len(k.Vars) == 0 {
			return k.Name, true
		}
	}
	return "", false
}

// envList is an ordered set of extra variables where a later set replaces an
// earlier one in place.
type envList struct{ vars []profile.ExtraEnvVar }

func (l *envList) set(v profile.ExtraEnvVar) {
	for i := range l.vars {
		if l.vars[i].Name == v.Name {
			l.vars[i] = v
			return
		}
	}
	l.vars = append(l.vars, v)
}

// backup creates the per-profile backup dir on first use (FR-045).
type backup struct {
	paths   *config.Paths
	harness string
	name    string
	now     func() time.Time
	act     *Action
	dir     string
}

func (b *backup) ensure() (string, error) {
	if b.dir == "" {
		ts := b.now().UTC().Format("20060102-150405.000000")
		b.dir = b.paths.BackupDir(b.harness, b.name, ts)
		if err := os.MkdirAll(b.dir, 0o700); err != nil {
			return "", err
		}
		b.act.BackupDir = b.dir
	}
	return b.dir, nil
}

// save copies path (privately, 0600) into the backup dir under rel.
func (b *backup) save(path, rel string) error {
	dir, err := b.ensure()
	if err != nil {
		return err
	}
	return privateCopier(b.act).copy(path, filepath.Join(dir, filepath.FromSlash(rel)))
}

func privateCopier(act *Action) copier {
	return copier{private: true, warn: func(s string) { act.Warnings = append(act.Warnings, s) }}
}

func plainCopier(act *Action) copier {
	return copier{warn: func(s string) { act.Warnings = append(act.Warnings, s) }}
}

// applyCredentialFiles copies cc-deck's file-sourced credentials verbatim (0600,
// atomic) into the profile's credentials dir. A new profile gets them; --overwrite
// keeps existing files (adding only missing ones); --overwrite-credentials backs
// up and replaces them (FR-045).
func applyCredentialFiles(creds []credFile, exists bool, opts Options, bk *backup, act *Action) error {
	for _, cf := range creds {
		if cf.src == "" {
			continue
		}
		have := layer.PathExists(cf.dest)
		switch {
		case have && exists && !opts.OverwriteCredentials:
			continue // --overwrite keeps credentials
		case have && opts.OverwriteCredentials:
			if err := bk.save(cf.dest, "imported-credentials/"+cf.field); err != nil {
				return err
			}
			act.CredentialsReplaced = true
		}
		if err := privateCopier(act).copy(cf.src, cf.dest); err != nil {
			return err
		}
		act.Warnings = append(act.Warnings, fmt.Sprintf("copied credential file %s verbatim to %s (mode 0600); its content is not shown", cf.src, cf.dest))
	}
	return nil
}

// applyConfig copies the credentials and owned layers from cc-deck's live config
// dir into the new profile config dir, honoring the overwrite flags and backing up
// replaced credentials (FR-045). State is never copied; content, settings, the
// plugin store and the hook location are left for sync. Every mixed file is merged
// in memory first, so a malformed source or target fails the profile before any
// file is written.
func applyConfig(def *harness.Definition, srcDir, destDir string, exists bool, level profile.Share, opts Options, bk *backup, act *Action) error {
	if fi, err := os.Stat(srcDir); err != nil || !fi.IsDir() {
		act.Warnings = append(act.Warnings, fmt.Sprintf("no cc-deck config dir at %s; no harness credential or owned files imported from it", srcDir))
		return nil
	}

	type entry struct {
		rule          *harness.EntryRule
		rel, src, dst string
		mixed         *mixedWrite
	}
	var entries []entry
	for i := range def.Classification {
		rule := &def.Classification[i]
		if rule.IsGlob() {
			continue // glob rules classify entries; they name nothing to import
		}
		rel := strings.TrimSuffix(rule.Entry, "/")
		e := entry{rule: rule, rel: rel,
			src: filepath.Join(srcDir, filepath.FromSlash(rel)),
			dst: filepath.Join(destDir, filepath.FromSlash(rel))}
		// Defense in depth: the loader already rejects ".."/absolute entries, but a copy
		// here (and the copier's RemoveAll) is destructive, so never act on a dst that
		// resolves outside the profile dir.
		if !within(destDir, e.dst) {
			act.Warnings = append(act.Warnings, fmt.Sprintf("skipped entry %q: resolves outside the profile dir", rule.Entry))
			continue
		}
		if rule.Layer == harness.LayerMixed {
			mw, err := planMixed(def, rule, e.src, e.dst, rel, exists, level, opts, act)
			if err != nil {
				return err
			}
			e.mixed = mw
		}
		entries = append(entries, e)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		switch e.rule.Layer {
		case harness.LayerOwned:
			// Owned is replaced on every proceeding import (new, --overwrite,
			// --overwrite-credentials).
			if layer.PathExists(e.src) {
				if err := plainCopier(act).copy(e.src, e.dst); err != nil {
					return err
				}
			}
		case harness.LayerCredentials:
			switch {
			case !exists:
				if layer.PathExists(e.src) {
					if err := privateCopier(act).copy(e.src, e.dst); err != nil {
						return err
					}
				}
			case opts.OverwriteCredentials:
				if layer.PathExists(e.dst) {
					if err := bk.save(e.dst, e.rel); err != nil {
						return err
					}
				}
				if layer.PathExists(e.src) {
					if err := privateCopier(act).copy(e.src, e.dst); err != nil {
						return err
					}
					act.CredentialsReplaced = true
				}
			default:
				// --overwrite (not credentials): keep existing credentials.
			}
		case harness.LayerMixed:
			if e.mixed != nil {
				if err := e.mixed.write(bk, act); err != nil {
					return err
				}
			}
		default:
			// State stays fresh. Content, settings, the plugin store and the hook
			// location are never imported: sync links or copies them from the default
			// setup according to the profile's share level.
		}
	}
	return nil
}

// mixedWrite is a mixed file's merged content, computed before anything is written.
type mixedWrite struct {
	dst, rel    string
	data        []byte
	backup      bool // back up the existing target first (--overwrite-credentials)
	credentials bool // the write replaces an existing profile's credential keys
}

func (mw *mixedWrite) write(bk *backup, act *Action) error {
	if mw.backup {
		if err := bk.save(mw.dst, mw.rel); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(mw.dst), 0o755); err != nil {
		return err
	}
	// The file holds credential keys: private to the user, like a seeded one.
	if err := config.WriteFileAtomic(mw.dst, mw.data, 0o600); err != nil {
		return err
	}
	if mw.credentials {
		act.CredentialsReplaced = true
	}
	return nil
}

// planMixed merges a mixed file with the layer helpers (FR-013, FR-045). The keys
// taken from cc-deck's file are its owned keys and, for a new profile or with
// --overwrite-credentials, its credentials keys; every other key of the target stays
// as it is. The target is the profile's file when it has one, else what sync seeds
// for the share level, so an imported profile starts with the same preferences as
// a new one (sync seeds a mixed file only when it is absent). It returns nil when
// cc-deck's file holds none of those keys: a missing target is then left for sync
// to seed, an existing one is unchanged.
func planMixed(def *harness.Definition, rule *harness.EntryRule, src, dst, rel string, exists bool, level profile.Share, opts Options, act *Action) (*mixedWrite, error) {
	ext := filepath.Ext(dst)
	if !layer.IsJSONExt(ext) {
		act.Warnings = append(act.Warnings, fmt.Sprintf("mixed file %q is not JSON; skipped", rel))
		return nil, nil
	}
	srcData, _, err := readOptional(src)
	if err != nil {
		return nil, fmt.Errorf("read source mixed file %s: %w", src, err)
	}
	base, haveDst, err := readOptional(dst)
	if err != nil {
		return nil, fmt.Errorf("read target mixed file %s: %w", dst, err)
	}
	if !haveDst {
		base = layer.SeedMixed(def, rule, level, "").Data
	}
	layers := []harness.Layer{harness.LayerOwned}
	if !exists || opts.OverwriteCredentials {
		layers = append(layers, harness.LayerCredentials)
	}
	out, copied, err := layer.MergeMixedKeys(rule, base, srcData, ext, layers...)
	if err != nil {
		return nil, fmt.Errorf("mixed file %s: %w", rel, err)
	}
	if len(copied) == 0 {
		return nil, nil
	}
	mw := &mixedWrite{dst: dst, rel: rel, data: out, backup: haveDst && opts.OverwriteCredentials}
	if exists {
		for _, k := range copied {
			if l, _ := layer.ClassifyKey(rule, k); l == harness.LayerCredentials {
				mw.credentials = true
			}
		}
	}
	return mw, nil
}

// --- small filesystem and JSON helpers (leaf; no harness-name branching) ---

// within reports whether target is base or a path inside it.
func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// readOptional reads a file; a missing file yields no data and false.
func readOptional(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}
