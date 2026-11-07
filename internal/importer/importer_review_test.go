package importer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/importer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &m); err != nil {
		t.Fatalf("%s is not a JSON object: %v", path, err)
	}
	return m
}

func sortedMapKeys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// M6: a new import starts the mixed file from what sync would seed for the share
// level, then overlays the source's credentials and owned keys. Before, the file
// held only those keys, and since sync seeds a mixed file only when it is absent,
// the imported profile never got the default setup's preferences.
func TestImportSeedsMixedFileByShareLevel(t *testing.T) {
	cases := []struct {
		share profile.Share
		keys  []string
	}{
		// ClaudeJSON's preferences (autoUpdates, showSpinnerTree) are seeded; its
		// credentials (oauthAccount, primaryApiKey) and state never are.
		{profile.ShareContent, []string{"autoUpdates", "mcpServers", "oauthAccount", "showSpinnerTree"}},
		{profile.ShareAll, []string{"autoUpdates", "mcpServers", "oauthAccount", "showSpinnerTree"}},
		// At none nothing comes from the default setup.
		{profile.ShareNone, []string{"mcpServers", "oauthAccount"}},
	}
	for _, c := range cases {
		t.Run(string(c.share), func(t *testing.T) {
			f := newFixture(t, ccConfigYAML)
			testutil.SeedDefaults(t, f.home, "claude")
			f.seedCredentials(t)
			f.run(t, importer.Options{Share: c.share})

			path := filepath.Join(f.paths.ProfileConfigDir("claude", "private"), ".claude.json")
			m := readJSONMap(t, path)
			if got := sortedMapKeys(m); !reflect.DeepEqual(got, c.keys) {
				t.Errorf("keys = %v, want %v", got, c.keys)
			}
			// The source's credential and owned keys win over the seed.
			if !reflect.DeepEqual(m["oauthAccount"], map[string]any{"id": "SRC"}) {
				t.Errorf("oauthAccount = %v, want the source's", m["oauthAccount"])
			}
			if !reflect.DeepEqual(m["mcpServers"], map[string]any{"srv": map[string]any{"command": "x"}}) {
				t.Errorf("mcpServers = %v, want the source's", m["mcpServers"])
			}
			if c.share != profile.ShareNone && m["autoUpdates"] != false {
				t.Errorf("autoUpdates = %v, want the default setup's false", m["autoUpdates"])
			}
			if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("mixed file mode = %v (%v), want 0600", fi, err)
			}

			// A profile without a live config dir gets no mixed file from import: sync
			// seeds it as for any new profile.
			other := filepath.Join(f.paths.ProfileConfigDir("claude", "enmaas"), ".claude.json")
			if _, err := os.Stat(other); !os.IsNotExist(err) {
				t.Errorf("import should leave %s to sync (err=%v)", other, err)
			}
		})
	}
}

// M14 / FR-045: one profile that fails does not stop the run. It is reported as
// failed, a new profile is rolled back (no store file, no config dir), and the
// others are imported.
func TestImportFailureIsPerProfile(t *testing.T) {
	f := newFixture(t, ccConfigYAML)
	f.seedCredentials(t)
	live := filepath.Join(f.sourceData, "profiles", "private", "claude")
	writeFile(t, filepath.Join(live, ".claude.json"), "{not json\n", 0o644)

	res := f.run(t, importer.Options{})
	assertNoSecrets(t, res)

	a := action(t, res, "claude/private")
	if a.Status != "failed" || !strings.Contains(a.Reason, ".claude.json") {
		t.Errorf("claude/private = %+v, want failed naming the mixed file", a)
	}
	if got := res.Failures(); len(got) != 1 || got[0].ID != "claude/private" {
		t.Errorf("Failures() = %+v", got)
	}
	if f.store.Exists("claude", "private") {
		t.Errorf("a failed new profile must not leave a store file")
	}
	if _, err := os.Stat(f.paths.ProfileConfigDir("claude", "private")); !os.IsNotExist(err) {
		t.Errorf("a failed new profile must not leave a config dir (err=%v)", err)
	}
	for _, id := range []string{"claude/enmaas", "claude/filekey", "claude/work", "codex/private"} {
		if a := action(t, res, id); a.Status != "import" {
			t.Errorf("%s: status %q (%s), want import", id, a.Status, a.Reason)
		}
	}
}

// A failing overwrite stops before it writes anything into that profile: the
// sources and targets are checked first.
func TestImportFailedOverwriteWritesNothing(t *testing.T) {
	f := newFixture(t, ccConfigYAML)
	f.seedCredentials(t)
	f.run(t, importer.Options{})

	cfg := f.paths.ProfileConfigDir("claude", "private")
	writeFile(t, filepath.Join(cfg, ".credentials.json"), "LOCAL-CRED\n", 0o600)
	writeFile(t, filepath.Join(cfg, ".claude.json"), "{broken\n", 0o600)

	res := f.run(t, importer.Options{OverwriteCredentials: true})
	a := action(t, res, "claude/private")
	if a.Status != "failed" {
		t.Fatalf("claude/private = %+v, want failed", a)
	}
	if got := readFile(t, filepath.Join(cfg, ".credentials.json")); got != "LOCAL-CRED\n" {
		t.Errorf("a failed overwrite replaced credentials: %q", got)
	}
	if !f.store.Exists("claude", "private") {
		t.Errorf("a failed overwrite must keep the existing profile")
	}
	if a := action(t, res, "claude/work"); a.Status != "overwrite" {
		t.Errorf("claude/work = %+v, want overwrite", a)
	}
}

// buildWarnings drops the per-run "no cc-deck config dir" note, which every
// profile without a live config dir gets.
func buildWarnings(a importer.Action) []string {
	var out []string
	for _, w := range a.Warnings {
		if !strings.HasPrefix(w, "no cc-deck config dir at ") {
			out = append(out, w)
		}
	}
	return out
}

// M16: every credential branch of the cc-deck auth mapping, with the exact auth
// kind, references, extra variables and warnings.
func TestImportCredentialBranches(t *testing.T) {
	f := newFixture(t, `default_profile: claude/ghost
profiles:
  claude:
    tokenenv:
      auth:
        oauth_token: {env: FAKE_OAUTH}
    tokenfile:
      auth:
        oauth_token: {file: /nonexistent/declared/oauth_token}
    both:
      auth:
        api_key: {env: FAKE_KEY}
        oauth_token: {env: FAKE_TOK}
    loginkeys:
      auth:
        login: true
        api_key: {env: FAKE_KEY}
        credentials: {env: FAKE_ADC}
    vertexenv:
      backend: vertex
      project: p1
      region: r1
      auth:
        credentials: {env: FAKE_ADC_PATH}
    nocredvar:
      backend: anthropic
      auth:
        credentials: {env: FAKE_ADC_PATH}
    legacykey:
      api_key_secret: k8s-key
    legacycreds:
      backend: vertex
      credentials_secret: k8s-adc
    legacyshadowed:
      api_key_secret: k8s-key
      auth:
        api_key: {env: FAKE_KEY}
`)
	writeFile(t, filepath.Join(f.source, "profiles", "claude", "tokenfile", "oauth_token"), fakeAPIKey, 0o600)
	res := f.run(t, importer.Options{})
	assertNoSecrets(t, res)

	if want := []string{`cc-deck default_profile "claude/ghost" names no profile; no profile was marked primary`}; !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("run warnings = %q, want %q", res.Warnings, want)
	}

	tokPath := f.credPath("claude", "tokenfile", "oauth_token")
	type want struct {
		kind     string
		refs     map[string]string
		env      []profile.ExtraEnvVar
		warnings []string
	}
	cases := map[string]want{
		"tokenenv": {kind: "oauth-token", refs: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "env:FAKE_OAUTH"}},
		"tokenfile": {kind: "oauth-token", refs: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "file:" + tokPath},
			warnings: []string{"copied credential file " + filepath.Join(f.source, "profiles", "claude", "tokenfile", "oauth_token") + " verbatim to " + tokPath + " (mode 0600); its content is not shown"}},
		"both": {kind: "oauth-token", refs: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "env:FAKE_TOK"},
			warnings: []string{"cc-deck auth.api_key ignored: auth.oauth_token takes precedence, as in cc-deck"}},
		"loginkeys": {kind: "oauth-login",
			warnings: []string{"cc-deck auth.login is set, so its other auth sources are ignored, as in cc-deck"}},
		"vertexenv": {kind: "oauth-login", env: []profile.ExtraEnvVar{
			{Name: "CLAUDE_CODE_USE_VERTEX", Value: "1", Literal: true},
			{Name: "ANTHROPIC_VERTEX_PROJECT_ID", Value: "p1", Literal: true},
			{Name: "CLOUD_ML_REGION", Value: "r1", Literal: true},
			{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "env:FAKE_ADC_PATH"},
		}},
		"nocredvar": {kind: "oauth-login",
			warnings: []string{`cc-deck auth.credentials ignored: backend "anthropic" takes no credentials file`}},
		"legacyshadowed": {kind: "api-key", refs: map[string]string{"ANTHROPIC_API_KEY": "env:FAKE_KEY"}},
	}
	for name, w := range cases {
		a := action(t, res, "claude/"+name)
		if a.Status != "import" {
			t.Errorf("%s: status %q (%s), want import", name, a.Status, a.Reason)
			continue
		}
		p, err := f.store.Load("claude", name)
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		if p.AuthKind != w.kind {
			t.Errorf("%s: auth kind %q, want %q", name, p.AuthKind, w.kind)
		}
		refs := map[string]string{}
		for v, r := range p.AuthRefs {
			refs[v] = r.Raw
		}
		if len(refs) == 0 {
			refs = nil
		}
		if !reflect.DeepEqual(refs, w.refs) {
			t.Errorf("%s: auth refs %v, want %v", name, refs, w.refs)
		}
		if !reflect.DeepEqual(p.ExtraEnv, w.env) {
			t.Errorf("%s: extra env %+v, want %+v", name, p.ExtraEnv, w.env)
		}
		if got := buildWarnings(a); !reflect.DeepEqual(got, w.warnings) {
			t.Errorf("%s: warnings %q, want %q", name, got, w.warnings)
		}
		if p.Primary {
			t.Errorf("%s: no profile may be primary when default_profile names none", name)
		}
	}
	if got := readFile(t, tokPath); got != fakeAPIKey {
		t.Errorf("oauth_token file not copied verbatim")
	}

	// Legacy Kubernetes secret fields map to secret sources, which aprofile cannot
	// reference: the profile is skipped with the field named.
	for name, field := range map[string]string{"legacykey": "api_key", "legacycreds": "credentials"} {
		a := action(t, res, "claude/"+name)
		wantReason := "cannot import cc-deck profile: auth." + field + " uses a Kubernetes secret source, which aprofile cannot reference"
		if a.Status != "skip" || a.Reason != wantReason {
			t.Errorf("%s: %+v, want skip with %q", name, a, wantReason)
		}
	}
}
