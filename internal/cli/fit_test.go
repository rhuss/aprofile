package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/registry"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// handEdit replaces old with new in a profile's store file, as a user editing it by
// hand would.
func handEdit(t *testing.T, h, n, old, new string) {
	t.Helper()
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	file := paths.ProfileStorePath(h, n)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if old == "" {
		data = append(data, []byte(new)...)
	} else {
		if !strings.Contains(string(data), old) {
			t.Fatalf("%s does not contain %q:\n%s", file, old, data)
		}
		data = []byte(strings.Replace(string(data), old, new, 1))
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// FR-006: a hand-edited auth kind the harness definition does not declare fails the
// profile in sync (wrapper and registry entry kept, exit non-zero), and doctor and
// show name the profile, the unknown kind, the valid kinds and the fix.
func TestUnknownAuthKindIsReported(t *testing.T) {
	testutil.HermeticHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	if out, err := runCLI(t, "add", "claude", "work", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	paths, _ := config.ResolvePaths()
	wrapperPath := filepath.Join(paths.BinDir, "claude-work")
	before, err := os.ReadFile(wrapperPath)
	if err != nil {
		t.Fatal(err)
	}

	handEdit(t, "claude", "work", "auth_kind: oauth-login", "auth_kind: nope")

	out, err := runCLI(t, "sync")
	if err == nil {
		t.Fatalf("sync must exit non-zero for an unknown auth kind:\n%s", out)
	}
	for _, want := range []string{`claude/work`, `auth kind "nope"`, "api-key", "oauth-login", "aprofile edit claude/work --auth-kind <kind>"} {
		if !strings.Contains(out, want) {
			t.Errorf("sync output lacks %q:\n%s", want, out)
		}
	}
	if after, _ := os.ReadFile(wrapperPath); string(after) != string(before) {
		t.Errorf("sync must keep the existing wrapper of a failed profile")
	}
	reg, err := registry.Read(paths.RegistryPath())
	if err != nil || reg == nil || len(reg.Profiles) != 1 || reg.Profiles[0].AuthKind != "oauth-login" {
		t.Errorf("sync must keep the existing registry entry: %+v %v", reg, err)
	}
	out, _ = runCLI(t, "sync", "--json")
	if !strings.Contains(out, `"status": "failed"`) {
		t.Errorf("sync --json should mark the profile failed:\n%s", out)
	}

	out, err = runCLI(t, "doctor")
	if err == nil {
		t.Errorf("doctor must report a problem")
	}
	for _, want := range []string{`[claude/work] auth kind "nope" is not declared by harness claude (valid kinds: api-key, auth-token, oauth-token, oauth-login)`,
		"fix: aprofile edit claude/work --auth-kind <kind>"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `wrapper "claude-work" is stale`) {
		t.Errorf("doctor must not suggest sync for a wrapper sync will not rewrite:\n%s", out)
	}

	out, err = runCLI(t, "show", "claude/work")
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	if !strings.Contains(out, `auth kind:      nope (not declared by harness claude; valid kinds: api-key, auth-token, oauth-token, oauth-login; fix: aprofile edit claude/work --auth-kind <kind>)`) {
		t.Errorf("show should flag the unknown auth kind:\n%s", out)
	}
	out, _ = runCLI(t, "show", "claude/work", "--json")
	if !strings.Contains(out, `"problems": [`) || !strings.Contains(out, `auth kind \"nope\" is not declared`) {
		t.Errorf("show --json should list the problem:\n%s", out)
	}

	// login refuses before it starts anything.
	out, err = runCLI(t, "login", "claude/work")
	if err == nil || !strings.Contains(out+err.Error(), `auth kind "nope" is not declared`) {
		t.Errorf("login must refuse an unknown auth kind: %v\n%s", err, out)
	}

	// The fix works.
	if out, err := runCLI(t, "edit", "claude/work", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("edit: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync after the fix: %v\n%s", err, out)
	}
}

// FR-006: a hand-edited model or endpoint on a harness without the backing variable
// is reported by doctor (problem) and sync (note) instead of silently dropped.
func TestUnusedModelAndEndpointAreReported(t *testing.T) {
	testutil.HermeticHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	if out, err := runCLI(t, "add", "pi", "box", "--auth-kind", "login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	handEdit(t, "pi", "box", "", "model: big-model\nendpoint: https://llm.example.com\n")

	out, err := runCLI(t, "sync")
	if err != nil {
		t.Fatalf("sync should still succeed: %v\n%s", err, out)
	}
	for _, want := range []string{`profile pi/box: model "big-model" is ignored: harness pi declares no model variable (model_var)`,
		`profile pi/box: endpoint "https://llm.example.com" is ignored: harness pi declares no endpoint variable (endpoint_var)`} {
		if !strings.Contains(out, want) {
			t.Errorf("sync should note %q:\n%s", want, out)
		}
	}
	out, _ = runCLI(t, "doctor")
	for _, want := range []string{`[pi/box] model "big-model" is ignored: harness pi declares no model variable (model_var)`,
		`[pi/box] endpoint "https://llm.example.com" is ignored`, `fix: aprofile edit pi/box --model ""`, `fix: aprofile edit pi/box --endpoint ""`} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor should report %q:\n%s", want, out)
		}
	}
	out, _ = runCLI(t, "show", "pi/box")
	if !strings.Contains(out, `model:          big-model (ignored: harness pi declares no model variable`) {
		t.Errorf("show should flag the ignored model:\n%s", out)
	}
}
