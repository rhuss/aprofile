package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenario6CanarySweep covers SC-006 / FR-028: a secret value held behind an
// env:/file: reference must never appear in any artifact aprofile writes (wrappers,
// registry, profile store, harness definitions) or in any --json output. The canary
// secret lives only in environment variables the profile references; the sweep asserts
// it never leaks, even when every command runs with the canary present in the
// environment.
func TestScenario6CanarySweep(t *testing.T) {
	e := newEnv(t)

	const canary = "CANARYv4l2e7secretzzzneverleak"
	secretEnv := []string{
		"WORK_TOKEN=" + canary,
		"EXTRA_SECRET=" + canary,
	}

	// A profile whose auth key and extra variables reference the canary via BOTH an env
	// reference and a file reference (the file's contents must never leak; only the
	// "file:<path>" text is stored).
	secretFile := filepath.Join(e.root, "secret.txt")
	if err := os.WriteFile(secretFile, []byte(canary+"\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	e.run(secretEnv, "add", "claude", "work",
		"--label", "Work", "--color", "blue", "--icon", "W",
		"--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN",
		"--env", "MY_SECRET=env:EXTRA_SECRET",
		"--env", "FILE_SECRET=file:"+secretFile)
	// A second profile and copy targets so the copy / mcp-copy conversion reports (the
	// richest --json producers) carry real content to sweep.
	e.run(secretEnv, "add", "claude", "other", "--auth-kind", "oauth-login")
	e.run(secretEnv, "copy", "claude/work", "claude/copytarget")
	e.run(secretEnv, "copy", "claude/work", "codex/cross")
	e.run(secretEnv, "sync")

	// 1) Sweep every file aprofile wrote under its data and config dirs (wrappers,
	// registry, profile stores, any harness definitions) for the canary value.
	sweepFiles := func() {
		for _, root := range []string{
			filepath.Join(e.root, ".local", "share", "aprofile"),
			filepath.Join(e.root, ".config", "aprofile"),
		} {
			walkAndAssertNoCanary(t, root, canary)
		}
	}
	sweepFiles()

	// 2) Sweep every --json and reporting command's output (stdout and stderr), run with
	// the canary present in the environment, including the copy and mcp-copy conversion
	// reports and the dry-run reports, so a command that resolved a reference is caught.
	jsonCommands := [][]string{
		{"list", "--json"},
		{"show", "claude/work", "--json"},
		{"harness", "list", "--json"},
		{"harness", "show", "claude", "--json"},
		{"mcp", "list", "claude/work", "--json"},
		{"doctor", "--json"},
		{"registry", "path"},
		{"sync", "--dry-run", "--json"},
		{"import", "cc-deck", "--dry-run", "--json"},
		{"copy", "claude/work", "claude/copytarget", "--overwrite", "--yes", "--json"},
		{"copy", "claude/work", "codex/cross", "--overwrite", "--yes", "--json"},
		{"mcp", "copy", "claude/work", "claude/other", "--json"},
	}
	for _, args := range jsonCommands {
		stdout, stderr, _ := e.tryRunSplit(secretEnv, args...)
		if strings.Contains(stdout, canary) || strings.Contains(stderr, canary) {
			t.Errorf("canary leaked in output of 'aprofile %s':\nstdout:\n%s\nstderr:\n%s",
				strings.Join(args, " "), stdout, stderr)
		}
	}
	// Re-sweep files: the copies above wrote new profile stores and config dirs.
	sweepFiles()

	// 3) The registry file itself (named by registry path) must be canary-free.
	regPath := strings.TrimSpace(e.run(secretEnv, "registry", "path"))
	assertFileNoCanary(t, regPath, canary)

	// Sanity: the reference text (not the value) is what is stored, so it is present.
	store := filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "work.yaml")
	data, err := os.ReadFile(store)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if !strings.Contains(string(data), "env:WORK_TOKEN") {
		t.Errorf("profile store should keep the reference text, got:\n%s", data)
	}
}

func walkAndAssertNoCanary(t *testing.T, root, canary string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // a missing subtree is not a failure for the sweep
		}
		if d.IsDir() {
			return nil
		}
		assertFileNoCanary(t, path, canary)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func assertFileNoCanary(t *testing.T, path, canary string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if strings.Contains(string(data), canary) {
		t.Errorf("canary secret leaked into %s", path)
	}
}

// Feature 004 (SC-003): with API-key profiles whose keys the folder carries through
// file: and env: references, no written file (settings.json, config.toml, the
// managed-launch records, wrappers, registry) and no sync, show or doctor output
// contains the key.
func TestSC003LaunchCanarySweep(t *testing.T) {
	e := newEnv(t)
	const canary = "CANARYlaunchk3yzzzneverleak"
	secretEnv := []string{"LAUNCH_KEY=" + canary}
	keyFile := filepath.Join(e.root, "launch-key.txt")
	if err := os.WriteFile(keyFile, []byte(canary+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.run(secretEnv, "add", "claude", "filekey", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+keyFile, "--model", "m", "--endpoint", "http://e")
	e.run(secretEnv, "add", "claude", "envkey", "--share", "content", "--auth-kind", "auth-token", "--auth-key", "env:LAUNCH_KEY", "--model", "m")
	e.run(secretEnv, "add", "codex", "cxfile", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+keyFile, "--model", "1.5")
	e.run(secretEnv, "add", "codex", "cxenv", "--share", "content", "--auth-kind", "api-key", "--auth-key", "env:LAUNCH_KEY")
	for _, n := range []string{"cxfile", "cxenv"} {
		cfg := e.configDir("codex", n)
		if err := os.MkdirAll(cfg, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("model_provider = \"gw\"\n[model_providers.gw]\nbase_url = \"http://gw\"\nenv_key = \"OPENAI_API_KEY\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"sync", "--dry-run", "--json"}, {"sync", "--json"}, {"sync"}} {
		so, se, _ := e.tryRunSplit(secretEnv, args...)
		if strings.Contains(so+se, canary) {
			t.Errorf("canary leaked in 'aprofile %s'", strings.Join(args, " "))
		}
	}
	// The folders carry the key commands (sanity: the sweep looks at real files).
	if data, _ := os.ReadFile(filepath.Join(e.configDir("claude", "filekey"), "settings.json")); !strings.Contains(string(data), "apiKeyHelper") {
		t.Fatalf("settings.json lacks apiKeyHelper:\n%s", data)
	}
	if data, _ := os.ReadFile(filepath.Join(e.configDir("codex", "cxenv"), "config.toml")); !strings.Contains(string(data), "LAUNCH_KEY") {
		t.Fatalf("config.toml lacks the env key command:\n%s", data)
	}
	for _, root := range []string{
		filepath.Join(e.root, ".local", "share", "aprofile"),
		filepath.Join(e.root, ".config", "aprofile"),
	} {
		walkAndAssertNoCanary(t, root, canary)
	}
	for _, args := range [][]string{
		{"show", "claude/filekey", "--json"}, {"show", "claude/envkey", "--json"}, {"show", "codex/cxfile", "--json"},
		{"show", "claude/envkey"}, {"doctor", "--json"}, {"list", "--json"},
	} {
		so, se, _ := e.tryRunSplit(secretEnv, args...)
		if strings.Contains(so+se, canary) {
			t.Errorf("canary leaked in 'aprofile %s'", strings.Join(args, " "))
		}
	}
}
