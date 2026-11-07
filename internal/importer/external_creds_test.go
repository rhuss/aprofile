package importer_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/importer"
)

// TestImportListsExternalCredentialFiles covers T096 (FR-045, Notable-1): import
// copies whatever credential file cc-deck's config names, so the report lists each
// one it copies (dry run: would copy) from outside cc-deck's own config and data
// folders. Paths only, never content.
func TestImportListsExternalCredentialFiles(t *testing.T) {
	f := newFixture(t, `profiles:
  claude:
    outside:
      backend: vertex
      project: p
      region: r
      auth:
        credentials: {file: ~/fake-gcloud/adc.json}
    keyout:
      auth:
        api_key: {file: ~/keys/api_key}
    cccopy:
      auth:
        api_key: {file: ~/keys/shadowed}
    indata:
      auth:
        api_key: {file: DATA/keys/api_key}
    missing:
      auth:
        api_key: {file: ~/keys/missing}
    envonly:
      auth:
        api_key: {env: FAKE_KEY}
`)
	outside := filepath.Join(f.home.Root, "fake-gcloud", "adc.json")
	keyout := filepath.Join(f.home.Root, "keys", "api_key")
	writeFile(t, outside, fakeADC, 0o600)
	writeFile(t, keyout, fakeAPIKey, 0o600)
	writeFile(t, filepath.Join(f.home.Root, "keys", "shadowed"), fakeAPIKey, 0o600)
	// cc-deck's own copy wins over the declared file, so nothing is read from outside.
	writeFile(t, filepath.Join(f.source, "profiles", "claude", "cccopy", "api_key"), fakeAPIKey, 0o600)
	// A declared file inside cc-deck's data folder is cc-deck's own.
	inData := filepath.Join(f.sourceData, "keys", "api_key")
	writeFile(t, inData, fakeAPIKey, 0o600)
	cfg := readFile(t, filepath.Join(f.source, "config.yaml"))
	writeFile(t, filepath.Join(f.source, "config.yaml"), strings.ReplaceAll(cfg, "DATA/keys/api_key", inData), 0o644)

	want := map[string][]string{
		"claude/outside": {outside},
		"claude/keyout":  {keyout},
	}
	for _, dry := range []bool{true, false} {
		res := f.run(t, importer.Options{DryRun: dry})
		assertNoSecrets(t, res)
		for _, a := range res.Actions {
			if a.Status != "import" {
				t.Fatalf("dry=%v %s: %+v, want import", dry, a.ID, a)
			}
			if got := a.ExternalCredentialFiles; !reflect.DeepEqual(got, want[a.ID]) {
				t.Errorf("dry=%v %s: external credential files %q, want %q", dry, a.ID, got, want[a.ID])
			}
		}
	}

	// A rerun with --overwrite keeps the imported credentials, so it reads nothing
	// from outside; --overwrite-credentials replaces them and lists the files again.
	res := f.run(t, importer.Options{Overwrite: true, DryRun: true})
	for _, a := range res.Actions {
		if len(a.ExternalCredentialFiles) != 0 {
			t.Errorf("--overwrite %s: external credential files %q, want none (credentials kept)", a.ID, a.ExternalCredentialFiles)
		}
	}
	res = f.run(t, importer.Options{OverwriteCredentials: true, DryRun: true})
	for _, a := range res.Actions {
		if got := a.ExternalCredentialFiles; !reflect.DeepEqual(got, want[a.ID]) {
			t.Errorf("--overwrite-credentials %s: external credential files %q, want %q", a.ID, got, want[a.ID])
		}
	}
}
