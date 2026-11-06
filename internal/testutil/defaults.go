package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// The fake default setups below reproduce the entries research.md R1 observed on
// the maintainer's machine (Claude Code 2.1.318, Codex CLI 0.166.0, OpenCode 2.1.4,
// pi 1.3.0), as a table instead of files on disk: several names (auth.json,
// .credentials.json) are git-ignored so they can never be committed by accident.
// Paths are profile-relative; SeedDefaultsAt places each one at the harness
// definition's default-setup location (DefaultPathUnder), so the same table serves
// OpenCode's XDG spread. A trailing slash makes a directory; content may be empty.
//
// Tests never read the real directories (Constitution VII).

// ClaudeSettingsJSON is the fake default Claude Code settings.json: permissions, a
// status line, enabled plugins and one hook group.
const ClaudeSettingsJSON = `{
  "permissions": {"allow": ["Bash(ls:*)", "Read(~/notes/**)"]},
  "statusLine": {"type": "command", "command": "~/.claude/statusline.sh"},
  "enabledPlugins": {"superpowers@market": true},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "~/.claude/hooks/rtk.sh"}]}
    ]
  }
}
`

// CodexHooksJSON is the fake default Codex hooks.json.
const CodexHooksJSON = `{
  "hooks": {
    "PostToolUse": [
      {"matcher": "shell", "hooks": [{"type": "command", "command": "~/.codex/hooks/log.sh"}]}
    ]
  }
}
`

// DefaultEntries lists the fake default-setup entries per harness.
var DefaultEntries = map[string]map[string]string{
	"claude": {
		".credentials.json":      `{"token":"default-account"}`,
		".credentials.lock":      "",
		".claude.json":           `{"oauthAccount":{"email":"me@example.com"},"mcpServers":{"fs":{"command":"mcp-fs"}},"projects":{"/x":{}},"numStartups":3}`,
		"projects/":              "",
		"projects/p1/s.jsonl":    "{}",
		"history.jsonl":          `{"display":"default prompt"}`,
		"sessions/":              "",
		"session-env/":           "",
		"file-history/":          "",
		"todos/":                 "",
		"shell-snapshots/":       "",
		"tasks/":                 "",
		"plans/":                 "",
		"paste-cache/":           "",
		"debug/":                 "",
		"telemetry/":             "",
		"jobs/":                  "",
		"daemon/":                "",
		"ide/":                   "",
		"ide/1234.lock":          "{}",
		"backups/":               "",
		"logs/":                  "",
		"statsig/":               "",
		"CLAUDE.md":              "# default instructions\n",
		"skills/review/SKILL.md": "# review skill\n",
		"agents/helper.md":       "# helper agent\n",
		"commands/ship.md":       "# ship command\n",
		"settings.json":          ClaudeSettingsJSON,
		"settings.local.json":    `{"permissions": {"allow": ["Bash(make:*)"]}}`,
		"plugins/installed.json": `{"plugins":{}}`,
		"RTK.md":                 "# rtk notes (user extra)\n",
		"cpr.sh":                 "#!/bin/sh\n",
		"hooks/rtk.sh":           "#!/bin/sh\n",
		"statusline.sh":          "#!/bin/sh\n",
	},
	"codex": {
		"auth.json":              "{}",
		"config.toml":            "model = \"gpt-5\"\n",
		"sessions/":              "",
		"history.jsonl":          `{"text":"default prompt"}`,
		"session_index.jsonl":    "",
		"log/":                   "",
		"memories_1.sqlite":      "db",
		"memories_1.sqlite-wal":  "wal",
		"memories_1.sqlite-shm":  "shm",
		"logs_2.sqlite":          "db",
		"logs_2.sqlite-wal":      "wal",
		"logs_2.sqlite-shm":      "shm",
		"goals_1.sqlite":         "db",
		"queue_1.sqlite":         "db",
		"app-server-daemon/":     "",
		"thread-writer-locks/":   "",
		"shell_snapshots/":       "",
		"AGENTS.md":              "# default agents\n",
		"prompts/plan.md":        "# plan prompt\n",
		"skills/review/SKILL.md": "# review skill\n",
		"rules/default.rules":    "allow ls\n",
		"plugins/":               "",
		"hooks.json":             CodexHooksJSON,
	},
	"opencode": {
		"data/opencode/auth.json":                "{}",
		"data/opencode/opencode.db":              "db",
		"data/opencode/opencode.db-wal":          "wal",
		"data/opencode/opencode.db-shm":          "shm",
		"data/opencode/storage/":                 "",
		"state/opencode/prompt-history.jsonl":    "",
		"cache/opencode/models.json":             "{}",
		"config/opencode/opencode.json":          `{"theme":"dark","mcp":{"fs":{"type":"local","command":["mcp-fs"]}}}`,
		"config/opencode/skills/review/SKILL.md": "# review skill\n",
		"config/opencode/agent/helper.md":        "# helper agent\n",
		"config/opencode/tui.jsonc":              `{"theme":"dark"}`,
		"config/opencode/node_modules/":          "",
		"config/opencode/package.json":           `{"dependencies":{}}`,
		"config/opencode/plugins/x.ts":           "export default {}\n",
	},
	"pi": {
		"agent/auth.json":              "{}",
		"agent/mcp.json":               `{"mcpServers":{}}`,
		"agent/sessions/":              "",
		"agent/logs/":                  "",
		"agent/trust.json":             "{}",
		"agent/settings.json":          `{"theme":"dark","lastChangelogVersion":"1.3.0"}`,
		"agent/skills/review/SKILL.md": "# review skill\n",
		"agent/prompts/plan.md":        "# plan prompt\n",
		"agent/extensions/y.ts":        "export default {}\n",
	},
}

// SeedDefaults creates the fake default setup of one built-in harness under the
// hermetic home.
func SeedDefaults(t *testing.T, home Home, harnessName string) {
	t.Helper()
	SeedDefaultsAt(t, home.Root, harnessName)
}

// SeedDefaultsAt creates the fake default setup of one built-in harness with home
// as $HOME. Integration tests pass the temporary root their aprofile process uses
// as HOME; the test process's own HOME is never consulted.
func SeedDefaultsAt(t *testing.T, home, harnessName string) {
	t.Helper()
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatalf("testutil: load built-ins: %v", err)
	}
	def, ok := set.Get(harnessName)
	if !ok {
		t.Fatalf("testutil: no built-in harness %q", harnessName)
	}
	entries, ok := DefaultEntries[harnessName]
	if !ok {
		t.Fatalf("testutil: no fake default setup for %q", harnessName)
	}
	for rel, content := range entries {
		path := def.DefaultPathUnder(home, rel)
		if !strings.HasPrefix(path, home+string(filepath.Separator)) {
			t.Fatalf("testutil: default path %s escapes the temporary home", path)
		}
		if strings.HasSuffix(rel, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatalf("testutil: mkdir %s: %v", path, err)
			}
			continue
		}
		WriteFile(t, path, content)
	}
}

// DefaultDir returns where a harness's default setup lives under home (its
// default_config_location, expanded).
func DefaultDir(t *testing.T, home, harnessName string) string {
	t.Helper()
	set, _ := harness.LoadBuiltins()
	def, ok := set.Get(harnessName)
	if !ok {
		t.Fatalf("testutil: no built-in harness %q", harnessName)
	}
	return def.DefaultPathUnder(home, "")
}
