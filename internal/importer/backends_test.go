package importer_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/importer"
	"github.com/cc-deck/aprofile/internal/profile"
)

// TestImportBackendFromDefinition covers T087: the importer maps cc-deck's backend
// through the harness definition's backends block, so a harnesses.d file that
// declares a backend makes it importable without a code change (Principle I).
func TestImportBackendFromDefinition(t *testing.T) {
	f := newFixture(t, `profiles:
  claude:
    br:
      backend: bedrock
      region: eu-west-1
      auth:
        credentials: {env: FAKE_AWS_FILE}
`)
	// Without a definition entry the profile is skipped, naming the backend.
	res := f.run(t, importer.Options{DryRun: true})
	a := action(t, res, "claude/br")
	if a.Status != "skip" || !strings.Contains(a.Reason, `backend "bedrock"`) {
		t.Fatalf("undeclared backend: %+v, want a skip naming it", a)
	}

	if err := os.MkdirAll(f.paths.HarnessesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(f.paths.HarnessesDir(), "claude.yaml"), `name: claude
backends:
  bedrock:
    env:
      CLAUDE_CODE_USE_BEDROCK: "1"
      AWS_REGION: "{region}"
      UNUSED_PROJECT: "{project}"
    credentials_var: AWS_SHARED_CREDENTIALS_FILE
`, 0o644)
	set, err := harness.Load(f.paths.HarnessesDir())
	if err != nil || len(set.Errors()) != 0 {
		t.Fatalf("load: %v %v", err, set.Errors())
	}
	f.set = set
	res = f.run(t, importer.Options{})
	if a := action(t, res, "claude/br"); a.Status != "import" {
		t.Fatalf("declared backend: %+v, want import", a)
	}
	p, err := f.store.Load("claude", "br")
	if err != nil {
		t.Fatal(err)
	}
	want := []profile.ExtraEnvVar{
		{Name: "CLAUDE_CODE_USE_BEDROCK", Value: "1", Literal: true},
		{Name: "AWS_REGION", Value: "eu-west-1", Literal: true},
		{Name: "AWS_SHARED_CREDENTIALS_FILE", Value: "env:FAKE_AWS_FILE"},
	}
	if !reflect.DeepEqual(p.ExtraEnv, want) {
		t.Errorf("extra env %+v, want %+v", p.ExtraEnv, want)
	}
}
