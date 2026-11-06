package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// interactiveKind is an auth kind without variables per built-in harness, so add
// needs no secret reference.
var interactiveKind = map[string]string{
	"claude":   "oauth-login",
	"codex":    "chatgpt-login",
	"opencode": "login",
	"pi":       "login",
}

var allHarnesses = []string{"claude", "codex", "opencode", "pi"}

// backgroundState is the spec's Background list of runtime state that slice one
// linked into the default setup (plus the database companions).
var backgroundState = map[string][]string{
	"claude": {"history.jsonl", "sessions", "session-env", "file-history", "todos", "shell-snapshots", "tasks",
		"plans", "paste-cache", "debug", "telemetry", "jobs", "daemon", "ide", "backups"},
	"codex": {"memories_1.sqlite", "memories_1.sqlite-wal", "memories_1.sqlite-shm", "logs_2.sqlite", "logs_2.sqlite-wal",
		"logs_2.sqlite-shm", "goals_1.sqlite", "goals_1.sqlite-wal", "queue_1.sqlite", "queue_1.sqlite-shm",
		"app-server-daemon", "thread-writer-locks", "session_index.jsonl", "shell_snapshots", "log", "sessions", "history.jsonl"},
}
var allLevels = []string{"all", "content", "none"}

func builtin(t *testing.T, name string) *harness.Definition {
	t.Helper()
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := set.Get(name)
	if !ok {
		t.Fatalf("no built-in %s", name)
	}
	return d
}

// links walks a profile config dir and returns every symlink with its
// profile-relative name.
func links(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			rel, _ := filepath.Rel(dir, path)
			target, _ := os.Readlink(path)
			out[filepath.ToSlash(rel)] = target
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestUS1RuntimeStateNeverShared covers spec US1 and SC-001: for every built-in
// harness at every share level, after sync no credentials or state entry is a link,
// and every link is content, settings, the plugin store or the hook location.
func TestUS1RuntimeStateNeverShared(t *testing.T) {
	e := newEnv(t)
	for _, h := range allHarnesses {
		testutil.SeedDefaultsAt(t, e.root, h)
	}
	for _, h := range allHarnesses {
		for _, lv := range allLevels {
			e.run(nil, "add", h, "p-"+lv, "--auth-kind", interactiveKind[h], "--share", lv)
		}
	}
	e.run(nil, "sync")

	for _, h := range allHarnesses {
		def := builtin(t, h)
		for _, lv := range allLevels {
			dir := e.configDir(h, "p-"+lv)
			for rel, target := range links(t, dir) {
				res := layer.Classify(def, rel)
				if !res.Layer.Borrowable() {
					t.Errorf("%s/p-%s: %s is a link (-> %s) but is %q, not a borrowable category", h, lv, rel, target, res.Layer)
				}
				if lv == "none" {
					t.Errorf("%s/p-none: no entry may be linked at none, found %s", h, rel)
				}
				if lv == "content" && res.Layer != harness.LayerContent && res.Layer != harness.LayerPluginStore {
					t.Errorf("%s/p-content: only content and the plugin store are linked at content (SC-002), found %s (%s)", h, rel, res.Layer)
				}
			}
		}
	}

	// Every runtime-state entry the spec's Background lists, checked against an
	// independent table rather than the definition under test (SC-001).
	for h, entries := range backgroundState {
		def := builtin(t, h)
		for _, name := range entries {
			if r := layer.Classify(def, name); r.Layer != harness.LayerState {
				t.Errorf("%s: Background entry %s classified as %q, want state", h, name, r.Layer)
			}
			for _, lv := range allLevels {
				assertNotLink(t, filepath.Join(e.configDir(h, "p-"+lv), name))
			}
		}
	}
	if r := layer.Classify(builtin(t, "codex"), "hooks.json"); r.Layer != harness.LayerHookLocation {
		t.Errorf("codex hooks.json is the hook location, got %q", r.Layer)
	}

	// Named entries from spec US1 acceptance scenarios 1 and 2.
	for _, lv := range allLevels {
		for _, e2 := range []string{"history.jsonl", "sessions", "todos"} {
			assertNotLink(t, filepath.Join(e.configDir("claude", "p-"+lv), e2))
		}
		for _, e2 := range []string{"memories_1.sqlite", "memories_1.sqlite-wal", "memories_1.sqlite-shm", "history.jsonl", "log"} {
			assertNotLink(t, filepath.Join(e.configDir("codex", "p-"+lv), e2))
		}
	}
	// At content the instructions are linked; at none they are absent.
	if fi, err := os.Lstat(filepath.Join(e.configDir("claude", "p-content"), "CLAUDE.md")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("claude/p-content: CLAUDE.md should be linked (err=%v)", err)
	}
	if _, err := os.Lstat(filepath.Join(e.configDir("claude", "p-none"), "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("claude/p-none: CLAUDE.md must not be present")
	}
	// OpenCode content comes from ~/.config/opencode (default_locations).
	ocSkills := filepath.Join(e.configDir("opencode", "p-content"), "config", "opencode", "skills")
	if target, err := os.Readlink(ocSkills); err != nil || target != filepath.Join(e.root, ".config", "opencode", "skills") {
		t.Errorf("opencode/p-content skills link = %q (err=%v)", target, err)
	}
}

func assertNotLink(t *testing.T, path string) {
	t.Helper()
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("%s must be private, not a link", path)
	}
}

// TestUS1UnknownEntryReported covers US1 scenario 3: an entry the definition does not
// classify is left untouched, never linked, and reported by sync and doctor.
func TestUS1UnknownEntryReported(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	foo := filepath.Join(e.configDir("claude", "work"), "foo.json")
	writeFile(t, foo, `{"mine":true}`)

	out := e.run(nil, "sync")
	if !strings.Contains(out, "foo.json") || !strings.Contains(out, "unclassified") {
		t.Errorf("sync should report the unclassified entry:\n%s", out)
	}
	assertNotLink(t, foo)
	if got := readFile(t, foo); got != `{"mine":true}` {
		t.Errorf("unclassified entry was modified: %q", got)
	}
	dout, _ := e.tryRun(nil, "doctor")
	if !strings.Contains(dout, "claude/work") || !strings.Contains(dout, "foo.json") || !strings.Contains(dout, "fix:") {
		t.Errorf("doctor should report foo.json with profile and fix:\n%s", dout)
	}
}

// TestUS1SliceOneLeakFixed covers US1 scenario 4 and FR-034: a slice one
// history.jsonl link into the default setup is reported as leaking, and
// sync --fix-links replaces the link (never its target) with an empty entry.
func TestUS1SliceOneLeakFixed(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "old", "--auth-kind", "oauth-login", "--share", "all")
	e.run(nil, "sync")
	dir := e.configDir("claude", "old")
	target := filepath.Join(e.root, ".claude", "history.jsonl")
	if err := os.Symlink(target, filepath.Join(dir, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	out := e.run(nil, "sync")
	if !strings.Contains(out, "history.jsonl") || !strings.Contains(out, "leaking-link") || !strings.Contains(out, "--fix-links") {
		t.Errorf("sync should report the leaking link:\n%s", out)
	}
	out = e.run(nil, "sync", "--fix-links")
	if !strings.Contains(out, "earlier data stays in the default setup") {
		t.Errorf("fix-links should say earlier data stays in the default setup:\n%s", out)
	}
	assertNotLink(t, filepath.Join(dir, "history.jsonl"))
	if got := readFile(t, filepath.Join(dir, "history.jsonl")); got != "" {
		t.Errorf("the repaired entry must be empty, got %q", got)
	}
	if got := readFile(t, target); !strings.Contains(got, "default prompt") {
		t.Errorf("the link target must not change: %q", got)
	}
}
