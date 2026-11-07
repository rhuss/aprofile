package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func (e *env) binDir() string { return filepath.Join(e.root, ".local", "share", "aprofile", "bin") }

func (e *env) registryPath() string {
	return filepath.Join(e.root, ".local", "share", "aprofile", "registry.json")
}

func (e *env) harnessesDir() string {
	return filepath.Join(e.root, ".config", "aprofile", "harnesses.d")
}

type regEntry map[string]any

func readRegistry(t *testing.T, path string) map[string]regEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var reg struct {
		Profiles []regEntry `json:"profiles"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("parse registry: %v\n%s", err, data)
	}
	out := map[string]regEntry{}
	for _, p := range reg.Profiles {
		out[p["harness"].(string)+"/"+p["name"].(string)] = p
	}
	return out
}

// T068/FR-022: a harnesses.d override that fails to load marks the harness failed.
// Its profiles' wrappers and registry entries stay unchanged (no rendering with the
// pure built-in), sync reports the file and exits non-zero, and the other
// harnesses still sync.
func TestSyncFailedOverrideKeepsWrapperAndRegistry(t *testing.T) {
	e := newEnv(t)
	override := filepath.Join(e.harnessesDir(), "claude.yaml")
	writeFile(t, override, "name: claude\nbinary: claude2\n")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN")
	e.run(nil, "add", "codex", "dev", "--auth-kind", "chatgpt-login", "--label", "Dev")
	e.run(nil, "sync")

	claudeWrapper := filepath.Join(e.binDir(), "claude-work")
	before, err := os.ReadFile(claudeWrapper)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), "exec 'claude2'") {
		t.Fatalf("override not rendered:\n%s", before)
	}
	regBefore := readRegistry(t, e.registryPath())

	// Break the override and change the other harness's profile.
	writeFile(t, override, "name: claude\nbinary: claude2\nwrapper_prefix: \"Not Valid\"\n")
	e.run(nil, "edit", "codex/dev", "--label", "Dev Two")

	out, err := e.tryRun(nil, "sync")
	if err == nil {
		t.Fatalf("sync must exit non-zero when an override fails to load:\n%s", out)
	}
	if !strings.Contains(out, "claude.yaml") {
		t.Errorf("sync should name the failing file:\n%s", out)
	}
	after, err := os.ReadFile(claudeWrapper)
	if err != nil {
		t.Fatalf("claude wrapper removed: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("claude wrapper rewritten despite the failed override:\n%s", after)
	}
	regAfter := readRegistry(t, e.registryPath())
	b, _ := json.Marshal(regBefore["claude/work"])
	a, _ := json.Marshal(regAfter["claude/work"])
	if regAfter["claude/work"] == nil || string(a) != string(b) {
		t.Errorf("claude/work registry entry changed:\nbefore %s\nafter  %s", b, a)
	}
	if regAfter["codex/dev"]["label"] != "Dev Two" {
		t.Errorf("codex/dev must still sync: %v", regAfter["codex/dev"])
	}
	codex, _ := os.ReadFile(filepath.Join(e.binDir(), "codex-dev"))
	if !strings.Contains(string(codex), "Dev Two") {
		t.Errorf("codex wrapper not updated:\n%s", codex)
	}

	// Without a registry entry to carry over, the wrapper is still not removed as stale.
	if err := os.Remove(e.registryPath()); err != nil {
		t.Fatal(err)
	}
	if out, err := e.tryRun(nil, "sync"); err == nil {
		t.Fatalf("sync must still exit non-zero:\n%s", out)
	}
	if _, err := os.Stat(claudeWrapper); err != nil {
		t.Errorf("wrapper of a failed harness was removed: %v", err)
	}
}

// T073/FR-024: --require-path fails for every manual-instructions outcome, and a
// read-only or symlinked rc file is never replaced.
func TestSyncRequirePathManualOutcomes(t *testing.T) {
	t.Run("unsupported shell", func(t *testing.T) {
		e := newEnv(t)
		e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
		if out, err := e.tryRun([]string{"SHELL=/bin/tcsh"}, "sync", "--require-path"); err == nil {
			t.Fatalf("--require-path must fail for an unsupported shell:\n%s", out)
		}
		if out, err := e.tryRun([]string{"SHELL=/bin/tcsh"}, "sync"); err != nil {
			t.Fatalf("without --require-path sync succeeds: %v\n%s", err, out)
		}
	})

	t.Run("read-only rc", func(t *testing.T) {
		e := newEnv(t)
		e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
		rc := filepath.Join(e.root, ".zshrc")
		writeFile(t, rc, "# mine\n")
		if err := os.Chmod(rc, 0o444); err != nil {
			t.Fatal(err)
		}
		fi0, _ := os.Stat(rc)
		out, err := e.tryRun(nil, "sync", "--require-path")
		if err == nil {
			t.Fatalf("--require-path must fail for a read-only rc:\n%s", out)
		}
		if !strings.Contains(out, "export PATH=") {
			t.Errorf("manual instructions missing:\n%s", out)
		}
		fi1, err := os.Stat(rc)
		if err != nil {
			t.Fatal(err)
		}
		if fi1.Mode().Perm() != 0o444 {
			t.Errorf("rc mode changed to %v", fi1.Mode().Perm())
		}
		if fi0.Sys().(*syscall.Stat_t).Ino != fi1.Sys().(*syscall.Stat_t).Ino {
			t.Error("read-only rc file was replaced by a new inode")
		}
		if got := readFile(t, rc); got != "# mine\n" {
			t.Errorf("rc content changed: %q", got)
		}
		if out, err := e.tryRun(nil, "sync"); err != nil {
			t.Fatalf("without --require-path sync succeeds: %v\n%s", err, out)
		}
	})

	t.Run("symlinked rc", func(t *testing.T) {
		e := newEnv(t)
		e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
		target := filepath.Join(e.root, "dotfiles", "zshrc")
		writeFile(t, target, "# dotfiles\n")
		rc := filepath.Join(e.root, ".zshrc")
		if err := os.Symlink(target, rc); err != nil {
			t.Fatal(err)
		}
		if out, err := e.tryRun(nil, "sync", "--require-path"); err == nil {
			t.Fatalf("--require-path must fail for a symlinked rc:\n%s", out)
		}
		if fi, err := os.Lstat(rc); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("symlinked rc replaced: %v", err)
		}
		// Once the user adds the line by hand, the setup is complete.
		writeFile(t, target, "# dotfiles\nexport PATH=\""+e.binDir()+":$PATH\"\n")
		if out, err := e.tryRun(nil, "sync", "--require-path"); err != nil {
			t.Fatalf("a symlinked rc that already has the line satisfies --require-path: %v\n%s", err, out)
		}
	})
}

type syncReport struct {
	FormatVersion int  `json:"format_version"`
	DryRun        bool `json:"dry_run"`
	Changed       bool `json:"changed"`
	Profiles      []struct {
		Profile string `json:"profile"`
		Status  string `json:"status"`
		Wrapper struct {
			Name   string `json:"name"`
			Path   string `json:"path"`
			Action string `json:"action"`
		} `json:"wrapper"`
		ConfigDir struct {
			Path  string `json:"path"`
			Steps []struct {
				Action string `json:"action"`
				Path   string `json:"path"`
			} `json:"steps"`
		} `json:"config_dir"`
		Registry string `json:"registry"`
	} `json:"profiles"`
	WrappersRemoved []string `json:"wrappers_removed"`
	Registry        struct {
		Path    string `json:"path"`
		Changed bool   `json:"changed"`
	} `json:"registry"`
	Path struct {
		Action  string `json:"action"`
		RCFile  string `json:"rc_file"`
		Changed bool   `json:"changed"`
	} `json:"path"`
	Problems []string `json:"problems"`
}

func parseSyncReport(t *testing.T, stdout string) syncReport {
	t.Helper()
	var r syncReport
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("sync --json is not JSON: %v\n%s", err, stdout)
	}
	return r
}

// T079/FR-041: sync --dry-run --json reports the planned changes per profile in a
// versioned schema, and show --json carries format_version.
func TestSyncJSONReport(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")

	stdout, stderr, err := e.tryRunSplit(nil, "sync", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("sync --dry-run --json: %v\n%s\n%s", err, stdout, stderr)
	}
	r := parseSyncReport(t, stdout)
	if r.FormatVersion != 1 || !r.DryRun || !r.Changed {
		t.Errorf("header: %+v", r)
	}
	if len(r.Profiles) != 1 {
		t.Fatalf("want 1 profile, got %+v", r.Profiles)
	}
	p := r.Profiles[0]
	if p.Profile != "claude/work" || p.Status != "ok" || p.Wrapper.Name != "claude-work" ||
		p.Wrapper.Action != "write" || p.Registry != "add" {
		t.Errorf("profile plan: %+v", p)
	}
	if p.ConfigDir.Path != e.configDir("claude", "work") || len(p.ConfigDir.Steps) == 0 {
		t.Errorf("config dir plan: %+v", p.ConfigDir)
	}
	if r.Path.Action != "added" || r.Path.RCFile != filepath.Join(e.root, ".zshrc") {
		t.Errorf("path plan: %+v", r.Path)
	}
	if !r.Registry.Changed || r.Registry.Path != e.registryPath() {
		t.Errorf("registry plan: %+v", r.Registry)
	}
	if _, err := os.Stat(filepath.Join(e.binDir(), "claude-work")); !os.IsNotExist(err) {
		t.Error("dry run wrote the wrapper")
	}

	// A real sync with --json, then an unchanged one.
	stdout, _, err = e.tryRunSplit(nil, "sync", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if r := parseSyncReport(t, stdout); r.DryRun || r.Profiles[0].Wrapper.Action != "write" {
		t.Errorf("real sync report: %+v", r)
	}
	stdout, _, err = e.tryRunSplit(nil, "sync", "--json")
	if err != nil {
		t.Fatal(err)
	}
	r = parseSyncReport(t, stdout)
	if r.Changed || r.Profiles[0].Wrapper.Action != "unchanged" || r.Profiles[0].Registry != "unchanged" || r.Path.Action != "unchanged" {
		t.Errorf("unchanged sync report: %+v", r)
	}

	// A removed profile shows up as a removed wrapper.
	e.run(nil, "remove", "claude", "work")
	stdout, _, err = e.tryRunSplit(nil, "sync", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if r := parseSyncReport(t, stdout); len(r.WrappersRemoved) != 1 || r.WrappersRemoved[0] != "claude-work" || len(r.Profiles) != 0 {
		t.Errorf("removal report: %+v", r)
	}

	e.run(nil, "add", "claude", "other", "--auth-kind", "oauth-login")
	stdout, _, err = e.tryRunSplit(nil, "show", "claude/other", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var sj map[string]any
	if err := json.Unmarshal([]byte(stdout), &sj); err != nil {
		t.Fatal(err)
	}
	if sj["format_version"] != float64(1) {
		t.Errorf("show --json format_version = %v", sj["format_version"])
	}
}
