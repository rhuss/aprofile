package importer_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/importer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// Fake credential contents. They never appear in a warning or report.
const (
	fakeADC    = `{"type":"authorized_user","client_id":"FAKE-CLIENT","refresh_token":"FAKE-REFRESH"}` + "\n"
	fakeAPIKey = "FAKE-API-KEY-CONTENT\n"
	fakeLogin  = `{"claudeAiOauth":{"accessToken":"FAKE-LOGIN"}}` + "\n"
)

// ccConfigYAML mirrors cc-deck's real config.yaml schema (cc-deck
// internal/config/config.go and profile.go): profiles.<harness>.<name> with auth
// {login | api_key | oauth_token | credentials}{env|file}, env, color, icon,
// backend, project, region, model, plus the top-level default_profile. Every
// value is fake.
const ccConfigYAML = `default_profile: claude/private
defaults:
  container:
    image: quay.io/example/image:latest
profiles:
  claude:
    private:
      auth:
        login: true
      color: '#5AC0E2'
    work:
      backend: vertex
      project: fake-project-123
      region: us-east5
      auth:
        credentials:
          file: ~/fake-gcloud/adc.json
      color: '#E27C5A'
    enmaas:
      auth:
        api_key:
          env: FAKE_ENMAAS_TOKEN
      env:
        ANTHROPIC_BASE_URL: https://llm.example.com/v1
        ANTHROPIC_MODEL: fake-model-1
        TEAM_NAME: platform
      color: '#C05AE2'
      icon: E
    filekey:
      auth:
        api_key:
          file: /nonexistent/declared/api_key
      model: fake-sonnet
  codex:
    private:
      auth:
        api_key:
          env: OPENAI_API_KEY
      color: '#7CE25A'
badges:
  - name: flow
    file: .x
    format: json
    extract: a
`

type fixture struct {
	home       testutil.Home
	paths      *config.Paths
	set        *harness.Set
	store      *profile.Store
	source     string // cc-deck config dir (~/.config/cc-deck)
	sourceData string // cc-deck data dir (~/.local/share/cc-deck)
}

func newFixture(t *testing.T, configYAML string) *fixture {
	t.Helper()
	home := testutil.HermeticHome(t)
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		home:       home,
		paths:      paths,
		set:        set,
		store:      profile.NewStore(paths),
		source:     filepath.Join(home.ConfigHome, "cc-deck"),
		sourceData: filepath.Join(home.DataHome, "cc-deck"),
	}
	writeFile(t, filepath.Join(f.source, "config.yaml"), configYAML, 0o644)
	return f
}

// seedCredentials writes cc-deck's credential copies under
// <cc-deck config>/profiles/<harness>/<name>/ and a live config dir under
// <cc-deck data>/profiles/<name>/<harness>/.
func (f *fixture) seedCredentials(t *testing.T) {
	t.Helper()
	writeFile(t, filepath.Join(f.source, "profiles", "claude", "work", "credentials"), fakeADC, 0o600)
	writeFile(t, filepath.Join(f.source, "profiles", "claude", "filekey", "api_key"), fakeAPIKey, 0o600)
	live := filepath.Join(f.sourceData, "profiles", "private", "claude")
	writeFile(t, filepath.Join(live, ".credentials.json"), fakeLogin, 0o644)
	writeFile(t, filepath.Join(live, ".claude.json"),
		`{"oauthAccount":{"id":"SRC"},"mcpServers":{"srv":{"command":"x"}},"projects":{"/x":{}},"numStartups":3}`+"\n", 0o644)
	writeFile(t, filepath.Join(live, "projects", "p1", "session"), "STATE\n", 0o644)
}

func (f *fixture) run(t *testing.T, opts importer.Options) *importer.Result {
	t.Helper()
	opts.SourceDataDir = f.sourceData
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC) }
	}
	res, err := importer.Import(f.paths, f.set, f.store, f.source, opts)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return res
}

func (f *fixture) credPath(harnessName, name, field string) string {
	return filepath.Join(f.paths.DataDir, "credentials", harnessName, name, field)
}

func writeFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func action(t *testing.T, res *importer.Result, id string) importer.Action {
	t.Helper()
	for _, a := range res.Actions {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no action for %s in %+v", id, res.Actions)
	return importer.Action{}
}

func envValue(p *profile.Profile, name string) (profile.ExtraEnvVar, bool) {
	for _, e := range p.ExtraEnv {
		if e.Name == name {
			return e, true
		}
	}
	return profile.ExtraEnvVar{}, false
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			out[rel] = "dir " + info.Mode().String()
			return nil
		}
		data, _ := os.ReadFile(path)
		out[rel] = info.Mode().String() + " " + string(data)
		return nil
	})
	return out
}

// assertNoSecrets fails when any warning or reason carries a fake credential value.
func assertNoSecrets(t *testing.T, res *importer.Result) {
	t.Helper()
	all := strings.Join(res.Warnings, "\n")
	for _, a := range res.Actions {
		all += "\n" + a.Reason + "\n" + strings.Join(a.Warnings, "\n")
	}
	for _, s := range []string{"FAKE-REFRESH", "FAKE-CLIENT", "FAKE-API-KEY-CONTENT", "FAKE-LOGIN", "sk-ant-fake"} {
		if strings.Contains(all, s) {
			t.Errorf("report leaks credential content %q:\n%s", s, all)
		}
	}
}

func TestImportRealSchemaMapsEveryProfile(t *testing.T) {
	f := newFixture(t, ccConfigYAML)
	f.seedCredentials(t)
	srcBefore, dataBefore := snapshot(t, f.source), snapshot(t, f.sourceData)

	res := f.run(t, importer.Options{})
	assertNoSecrets(t, res)

	for _, id := range []string{"claude/enmaas", "claude/filekey", "claude/private", "claude/work", "codex/private"} {
		if a := action(t, res, id); a.Status != "import" {
			t.Errorf("%s: status %q (%s), want import", id, a.Status, a.Reason)
		}
	}

	// Login profile: the harness's auth kind without variables; default_profile -> primary.
	priv, err := f.store.Load("claude", "private")
	if err != nil {
		t.Fatal(err)
	}
	if priv.AuthKind != "oauth-login" || len(priv.AuthRefs) != 0 {
		t.Errorf("claude/private auth = %q %v, want oauth-login without refs", priv.AuthKind, priv.AuthRefs)
	}
	if !priv.Primary {
		t.Errorf("default_profile claude/private should be primary")
	}
	if priv.Color != "#5AC0E2" {
		t.Errorf("color = %q", priv.Color)
	}

	// api_key from env: the definition's api-key kind; env model/endpoint vars map to
	// the profile's model and endpoint; other env entries stay literal extras.
	enm, _ := f.store.Load("claude", "enmaas")
	if enm.AuthKind != "api-key" || enm.AuthRefs["ANTHROPIC_API_KEY"].Raw != "env:FAKE_ENMAAS_TOKEN" {
		t.Errorf("claude/enmaas auth = %q %v", enm.AuthKind, enm.AuthRefs)
	}
	if enm.Endpoint != "https://llm.example.com/v1" || enm.Model != "fake-model-1" {
		t.Errorf("claude/enmaas endpoint/model = %q / %q", enm.Endpoint, enm.Model)
	}
	if _, ok := envValue(enm, "ANTHROPIC_BASE_URL"); ok {
		t.Errorf("endpoint var should not also be an extra env entry: %+v", enm.ExtraEnv)
	}
	if e, ok := envValue(enm, "TEAM_NAME"); !ok || e.Value != "platform" || !e.Literal {
		t.Errorf("TEAM_NAME extra env = %+v", e)
	}
	if enm.Icon != "E" || enm.Primary {
		t.Errorf("claude/enmaas icon/primary = %q/%v", enm.Icon, enm.Primary)
	}

	// Vertex backend: provider switch, project and region become extra env entries;
	// the credentials file is copied verbatim at 0600 and referenced by path.
	work, _ := f.store.Load("claude", "work")
	if work.AuthKind != "oauth-login" {
		t.Errorf("vertex profile auth kind = %q, want the kind without variables", work.AuthKind)
	}
	want := map[string]string{
		"CLAUDE_CODE_USE_VERTEX":         "1",
		"ANTHROPIC_VERTEX_PROJECT_ID":    "fake-project-123",
		"CLOUD_ML_REGION":                "us-east5",
		"GOOGLE_APPLICATION_CREDENTIALS": f.credPath("claude", "work", "credentials"),
	}
	for k, v := range want {
		e, ok := envValue(work, k)
		if !ok || e.Value != v || !e.Literal {
			t.Errorf("claude/work env %s = %+v (present %v), want literal %q", k, e, ok, v)
		}
	}
	adc := f.credPath("claude", "work", "credentials")
	if got := readFile(t, adc); got != fakeADC {
		t.Errorf("credentials file not copied verbatim")
	}
	if fi, _ := os.Stat(adc); fi.Mode().Perm() != 0o600 {
		t.Errorf("credentials file mode = %v, want 0600", fi.Mode().Perm())
	}
	if !strings.Contains(strings.Join(action(t, res, "claude/work").Warnings, "\n"), "verbatim") {
		t.Errorf("copying a credential file must warn: %v", action(t, res, "claude/work").Warnings)
	}

	// api_key from a file: cc-deck's own copy under profiles/<h>/<n>/ wins over the
	// declared source path; the auth ref points at the aprofile copy.
	fk, _ := f.store.Load("claude", "filekey")
	keyPath := f.credPath("claude", "filekey", "api_key")
	if fk.AuthKind != "api-key" || fk.AuthRefs["ANTHROPIC_API_KEY"].Raw != "file:"+keyPath {
		t.Errorf("claude/filekey auth = %q %v", fk.AuthKind, fk.AuthRefs)
	}
	if readFile(t, keyPath) != fakeAPIKey || fk.Model != "fake-sonnet" {
		t.Errorf("claude/filekey key file or model wrong (model %q)", fk.Model)
	}

	// Codex: the same api_key form maps to codex's api-key kind and variable.
	cx, _ := f.store.Load("codex", "private")
	if cx.AuthKind != "api-key" || cx.AuthRefs["OPENAI_API_KEY"].Raw != "env:OPENAI_API_KEY" {
		t.Errorf("codex/private auth = %q %v", cx.AuthKind, cx.AuthRefs)
	}
	if cx.Primary {
		t.Errorf("codex/private must not be primary")
	}

	// Live config dir: credentials layer copied at 0600, owned keys of the mixed file
	// carried, state left fresh.
	cfg := f.paths.ProfileConfigDir("claude", "private")
	creds := filepath.Join(cfg, ".credentials.json")
	if readFile(t, creds) != fakeLogin {
		t.Errorf(".credentials.json not copied verbatim")
	}
	if fi, _ := os.Stat(creds); fi.Mode().Perm() != 0o600 {
		t.Errorf(".credentials.json mode = %v, want 0600", fi.Mode().Perm())
	}
	if mixed := readFile(t, filepath.Join(cfg, ".claude.json")); !strings.Contains(mixed, "mcpServers") || strings.Contains(mixed, "numStartups") {
		t.Errorf("mixed file = %s", mixed)
	}
	if _, err := os.Stat(filepath.Join(cfg, "projects")); !os.IsNotExist(err) {
		t.Errorf("state must stay fresh")
	}

	// cc-deck is never modified.
	if after := snapshot(t, f.source); len(after) != len(srcBefore) {
		t.Errorf("cc-deck config dir changed")
	}
	for k, v := range srcBefore {
		if snapshot(t, f.source)[k] != v {
			t.Errorf("cc-deck file %s changed", k)
		}
	}
	for k, v := range dataBefore {
		if snapshot(t, f.sourceData)[k] != v {
			t.Errorf("cc-deck data file %s changed", k)
		}
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	f := newFixture(t, ccConfigYAML)
	f.seedCredentials(t)
	cfgBefore, dataBefore := snapshot(t, f.paths.ConfigDir), snapshot(t, f.paths.DataDir)
	res := f.run(t, importer.Options{DryRun: true})
	if len(res.Actions) != 5 {
		t.Fatalf("dry run actions = %+v", res.Actions)
	}
	if len(snapshot(t, f.paths.ConfigDir)) != len(cfgBefore) || len(snapshot(t, f.paths.DataDir)) != len(dataBefore) {
		t.Errorf("dry run wrote into aprofile dirs")
	}
}

func TestImportDeclaredFileFallback(t *testing.T) {
	cfg := `profiles:
  claude:
    work:
      backend: vertex
      project: p
      region: r
      auth:
        credentials:
          file: ~/fake-gcloud/adc.json
`
	f := newFixture(t, cfg)
	// No cc-deck copy under profiles/claude/work/: the declared source is used.
	writeFile(t, filepath.Join(f.home.Root, "fake-gcloud", "adc.json"), fakeADC, 0o644)
	res := f.run(t, importer.Options{})
	assertNoSecrets(t, res)
	if got := readFile(t, f.credPath("claude", "work", "credentials")); got != fakeADC {
		t.Errorf("declared credentials file not copied")
	}
}

func TestImportMissingCredentialFileWarns(t *testing.T) {
	cfg := `profiles:
  claude:
    filekey:
      auth:
        api_key:
          file: /nonexistent/declared/api_key
`
	f := newFixture(t, cfg)
	res := f.run(t, importer.Options{})
	a := action(t, res, "claude/filekey")
	if a.Status != "import" || !strings.Contains(strings.Join(a.Warnings, "\n"), "not found") {
		t.Errorf("missing credential file should import with a warning: %+v", a)
	}
}

func TestImportShortDefaultProfile(t *testing.T) {
	f := newFixture(t, `default_profile: solo
profiles:
  claude:
    solo:
      auth: {login: true}
  codex:
    other:
      auth: {login: true}
`)
	f.run(t, importer.Options{})
	p, _ := f.store.Load("claude", "solo")
	if !p.Primary {
		t.Errorf("unambiguous short default_profile should become primary")
	}
}

func TestImportAmbiguousDefaultProfileWarns(t *testing.T) {
	f := newFixture(t, `default_profile: private
profiles:
  claude:
    private:
      auth: {login: true}
  codex:
    private:
      auth: {login: true}
`)
	res := f.run(t, importer.Options{})
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "ambiguous") {
		t.Errorf("ambiguous default_profile should warn: %v", res.Warnings)
	}
	for _, h := range []string{"claude", "codex"} {
		if p, _ := f.store.Load(h, "private"); p.Primary {
			t.Errorf("%s/private must not become primary", h)
		}
	}
}

// FR-001: importing a primary profile moves the flag from the harness's previous
// primary and reports the move.
func TestImportMovesPrimary(t *testing.T) {
	f := newFixture(t, ccConfigYAML)
	if err := f.store.Save(&profile.Profile{Harness: "claude", Name: "old", AuthKind: "oauth-login", Primary: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Save(&profile.Profile{Harness: "codex", Name: "keep", AuthKind: "chatgpt-login", Primary: true}); err != nil {
		t.Fatal(err)
	}

	dry := f.run(t, importer.Options{DryRun: true})
	if got := action(t, dry, "claude/private").PrimaryMovedFrom; len(got) != 1 || got[0] != "claude/old" {
		t.Errorf("dry run should report the move, got %v", got)
	}
	if old, _ := f.store.Load("claude", "old"); !old.Primary {
		t.Fatalf("dry run must not move the flag")
	}

	res := f.run(t, importer.Options{})
	if got := action(t, res, "claude/private").PrimaryMovedFrom; len(got) != 1 || got[0] != "claude/old" {
		t.Errorf("PrimaryMovedFrom = %v", got)
	}
	if old, _ := f.store.Load("claude", "old"); old.Primary {
		t.Errorf("previous primary claude/old should lose the flag")
	}
	if keep, _ := f.store.Load("codex", "keep"); !keep.Primary {
		t.Errorf("another harness's primary must stay")
	}
}

func TestImportColorAndIconChecks(t *testing.T) {
	f := newFixture(t, `profiles:
  claude:
    nocolor:
      auth: {login: true}
    badcolor:
      auth: {login: true}
      color: not-a-color
      icon: TooLong
`)
	res := f.run(t, importer.Options{})
	// cc-deck shows a color derived from the name (FNV-1a over its palette) when none
	// is valid; the import keeps that exact look.
	for n, want := range map[string]string{"nocolor": "#8686EA", "badcolor": "#E25A9E"} {
		p, err := f.store.Load("claude", n)
		if err != nil {
			t.Fatalf("load %s: %v", n, err)
		}
		if p.Color != want {
			t.Errorf("%s color = %q, want %q", n, p.Color, want)
		}
	}
	bad, _ := f.store.Load("claude", "badcolor")
	if bad.Icon != "" {
		t.Errorf("invalid icon should be dropped, got %q", bad.Icon)
	}
	wantWarnings := []string{
		`cc-deck color "not-a-color" is not an ANSI color name or #rrggbb; using #E25A9E, the color cc-deck derived from the name`,
		`cc-deck icon "TooLong" is not a single grapheme; left empty`,
	}
	if got := buildWarnings(action(t, res, "claude/badcolor")); !reflect.DeepEqual(got, wantWarnings) {
		t.Errorf("badcolor warnings = %q, want %q", got, wantWarnings)
	}
	if got := buildWarnings(action(t, res, "claude/nocolor")); len(got) != 0 {
		t.Errorf("a missing color needs no warning: %q", got)
	}
}

func TestImportDropsSecretLookingEnv(t *testing.T) {
	f := newFixture(t, `profiles:
  claude:
    work:
      auth: {login: true}
      env:
        LEAKY: sk-ant-fake-0123456789
        HEADER: "Bearer ghp_fakefake"
        ANTHROPIC_API_KEY: plain
        LONG_VALUE: abcdefghijklmnopqrstuvwxyz0123456789
`)
	res := f.run(t, importer.Options{})
	assertNoSecrets(t, res)
	p, _ := f.store.Load("claude", "work")
	if _, ok := envValue(p, "LEAKY"); ok {
		t.Errorf("secret-looking value must not be stored")
	}
	if _, ok := envValue(p, "HEADER"); ok {
		t.Errorf("a secret after a scheme word must not be stored (same rule as copy)")
	}
	if _, ok := envValue(p, "ANTHROPIC_API_KEY"); ok {
		t.Errorf("an auth variable must not become an extra env entry")
	}
	if e, ok := envValue(p, "LONG_VALUE"); !ok || !e.Literal {
		t.Errorf("long non-secret-prefixed value should import as a literal: %+v", e)
	}
	w := strings.Join(action(t, res, "claude/work").Warnings, "\n")
	for _, k := range []string{"LEAKY", "HEADER", "ANTHROPIC_API_KEY", "LONG_VALUE"} {
		if !strings.Contains(w, k) {
			t.Errorf("warnings should name %s: %s", k, w)
		}
	}
}

func TestImportSkipsUnsupportedSources(t *testing.T) {
	f := newFixture(t, `profiles:
  claude:
    k8s:
      auth:
        api_key:
          secret: my-k8s-secret
    bedrock:
      backend: bedrock
      auth: {login: true}
    ok:
      auth: {login: true}
  opencode:
    keyed:
      auth:
        api_key:
          env: SOME_KEY
`)
	res := f.run(t, importer.Options{})
	for id, want := range map[string]string{
		"claude/k8s":     "secret",
		"claude/bedrock": "backend",
		"opencode/keyed": "api-key",
	} {
		a := action(t, res, id)
		if a.Status != "skip" || !strings.Contains(a.Reason, want) {
			t.Errorf("%s: %+v, want skip mentioning %q", id, a, want)
		}
		if a.SkipExisting {
			t.Errorf("%s: invalid source is not an existing-profile skip", id)
		}
	}
	if action(t, res, "claude/ok").Status != "import" {
		t.Errorf("other profiles still import")
	}
}

func TestImportOverwriteKeepsAndOverwriteCredentialsReplacesFiles(t *testing.T) {
	f := newFixture(t, ccConfigYAML)
	f.seedCredentials(t)
	f.run(t, importer.Options{})
	adc := f.credPath("claude", "work", "credentials")
	writeFile(t, adc, "LOCAL-CRED\n", 0o600)

	f.run(t, importer.Options{Overwrite: true})
	if readFile(t, adc) != "LOCAL-CRED\n" {
		t.Errorf("--overwrite must keep credential files")
	}

	res := f.run(t, importer.Options{OverwriteCredentials: true})
	if readFile(t, adc) != fakeADC {
		t.Errorf("--overwrite-credentials must replace credential files")
	}
	a := action(t, res, "claude/work")
	if !a.CredentialsReplaced || a.BackupDir == "" {
		t.Fatalf("expected a credentials replacement with backup: %+v", a)
	}
	bak := filepath.Join(a.BackupDir, "imported-credentials", "credentials")
	if readFile(t, bak) != "LOCAL-CRED\n" {
		t.Errorf("backup does not hold the old credential")
	}
	if fi, _ := os.Stat(bak); fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestImportNoConfigFile(t *testing.T) {
	home := testutil.HermeticHome(t)
	paths, _ := config.ResolvePaths()
	set, _ := harness.LoadBuiltins()
	res, err := importer.Import(paths, set, profile.NewStore(paths), filepath.Join(home.ConfigHome, "cc-deck"), importer.Options{})
	if err != nil || len(res.Actions) != 0 {
		t.Fatalf("missing config.yaml should yield no profiles: %v %+v", err, res)
	}
}
