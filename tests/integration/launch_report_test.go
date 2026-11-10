package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Feature 004 US5: show, doctor and list report whether a profile folder is
// self-contained, stale, or holds launch entries aprofile did not place.

type folderJSON struct {
	Status       string   `json:"status"`
	EnvDependent []string `json:"env_dependent"`
	WrapperOnly  []struct {
		Setting  string `json:"setting"`
		Variable string `json:"variable"`
		Reason   string `json:"reason"`
	} `json:"wrapper_only"`
	Sync     string `json:"sync"`
	Unplaced []struct {
		File     string `json:"file"`
		Location string `json:"location"`
	} `json:"unplaced"`
}

func (e *env) folder(ref string) folderJSON {
	e.t.Helper()
	out, _, err := e.tryRunSplit(nil, "show", ref, "--json")
	if err != nil {
		e.t.Fatalf("show %s --json: %v\n%s", ref, err, out)
	}
	var sj struct {
		Folder *folderJSON `json:"folder"`
	}
	if err := json.Unmarshal([]byte(out), &sj); err != nil || sj.Folder == nil {
		e.t.Fatalf("show %s --json has no folder object: %v\n%s", ref, err, out)
	}
	return *sj.Folder
}

func reasonOf(f folderJSON, setting string) string {
	for _, w := range f.WrapperOnly {
		if w.Setting == setting {
			return w.Reason
		}
	}
	return ""
}

func TestLaunchReportShow(t *testing.T) {
	e := newEnv(t)
	e.writeDefaultClaudeSettings(`{"env":{"ANTHROPIC_BASE_URL":"http://default"}}`)
	key := e.keyFile("k", "sk-report-canary\n")
	e.run(nil, "add", "claude", "self", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+key, "--model", "m")
	e.run(nil, "add", "claude", "envkey", "--share", "content", "--auth-kind", "api-key", "--auth-key", "env:MY_KEY_VAR")
	e.run(nil, "add", "claude", "oauth", "--share", "content", "--auth-kind", "oauth-token", "--auth-key", "env:TOK")
	e.run(nil, "add", "claude", "shared", "--share", "all", "--auth-kind", "oauth-login", "--model", "m")
	e.run(nil, "add", "claude", "refextra", "--share", "content", "--auth-kind", "oauth-login", "--env", "OTEL_TOKEN=file:"+key)
	e.run(nil, "add", "claude", "cleared", "--share", "content", "--auth-kind", "auth-token", "--auth-key", "env:ANTHROPIC_AUTH_TOKEN")
	e.run(nil, "add", "codex", "cx", "--share", "content", "--auth-kind", "chatgpt-login", "--model", "1.5")
	e.run(nil, "add", "codex", "noprov", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+key)
	e.run(nil, "add", "opencode", "oc", "--share", "content", "--auth-kind", openCodeKind(t, e), "--env", "FOO=bar")
	e.run(nil, "sync")

	if f := e.folder("claude/self"); f.Status != "self-contained" || f.Sync != "synced" || len(f.WrapperOnly) != 0 {
		t.Errorf("claude/self: %+v", f)
	}
	if f := e.folder("claude/envkey"); f.Status != "self-contained-env" || len(f.EnvDependent) != 1 || f.EnvDependent[0] != "MY_KEY_VAR" {
		t.Errorf("claude/envkey: %+v", f)
	}
	if f := e.folder("claude/oauth"); f.Status != "not-self-contained" || reasonOf(f, "key") != "auth-kind" {
		t.Errorf("claude/oauth: %+v", f)
	}
	if f := e.folder("claude/shared"); f.Status != "not-self-contained" || reasonOf(f, "model") != "share-all" {
		t.Errorf("claude/shared: %+v", f)
	}
	if f := e.folder("claude/refextra"); reasonOf(f, "extra:OTEL_TOKEN") != "reference-value" {
		t.Errorf("claude/refextra: %+v", f)
	}
	if f := e.folder("claude/cleared"); reasonOf(f, "key") != "cleared-source" {
		t.Errorf("claude/cleared: %+v", f)
	}
	if f := e.folder("codex/cx"); reasonOf(f, "model") != "harness-rewrites" {
		t.Errorf("codex/cx: %+v", f)
	}
	if f := e.folder("codex/noprov"); f.Status != "not-self-contained" || reasonOf(f, "key") != "no-provider" {
		t.Errorf("codex/noprov: %+v", f)
	}
	if f := e.folder("opencode/oc"); reasonOf(f, "extra:FOO") != "no-support" {
		t.Errorf("opencode/oc: %+v", f)
	}

	text := e.run(nil, "show", "claude/envkey")
	if !strings.Contains(text, "folder:") || !strings.Contains(text, "self-contained (needs MY_KEY_VAR in the launching environment)") {
		t.Errorf("show text:\n%s", text)
	}
	text = e.run(nil, "show", "codex/cx")
	if !strings.Contains(text, "wrapper-only: model (passed by the wrapper because the harness rewrites it)") {
		t.Errorf("show codex text:\n%s", text)
	}

	// At share level all the linked default settings.json sets the endpoint.
	f := e.folder("claude/shared")
	if len(f.Unplaced) != 1 || f.Unplaced[0].Location != "env.ANTHROPIC_BASE_URL" {
		t.Errorf("claude/shared unplaced: %+v", f.Unplaced)
	}

	// An edited placed entry makes the folder stale; list shows it.
	settings := filepath.Join(e.configDir("claude", "self"), "settings.json")
	m := readSettings(t, settings)
	envBlock(t, m)["ANTHROPIC_MODEL"] = "user-pick"
	envBlock(t, m)["ANTHROPIC_DEFAULT_OPUS_MODEL"] = "hand"
	data, _ := json.Marshal(m)
	if err := os.WriteFile(settings, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The one-time settings copy brought the default setup's env.ANTHROPIC_BASE_URL
	// in, and the hand edit added env.ANTHROPIC_DEFAULT_OPUS_MODEL: both unplaced.
	if f := e.folder("claude/self"); f.Sync != "stale" || len(f.Unplaced) != 2 ||
		f.Unplaced[0].Location != "env.ANTHROPIC_BASE_URL" || f.Unplaced[1].Location != "env.ANTHROPIC_DEFAULT_OPUS_MODEL" {
		t.Errorf("after edits: %+v", f)
	}
	list := e.run(nil, "list")
	if !strings.Contains(list, "folder stale") {
		t.Errorf("list lacks folder stale:\n%s", list)
	}
	lj, _, _ := e.tryRunSplit(nil, "list", "--json")
	if !strings.Contains(lj, `"folder_sync": "stale"`) {
		t.Errorf("list --json lacks folder_sync:\n%s", lj)
	}

	// doctor: info for wrapper-only settings, problems for the stale folder and the
	// unplaced entries.
	dj, _, _ := e.tryRunSplit(nil, "doctor", "--json")
	var findings struct {
		Findings []struct {
			Profile, Severity, Message, Fix string
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(dj), &findings); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, dj)
	}
	has := func(profile, severity, msgPart, fix string) bool {
		for _, f := range findings.Findings {
			if f.Profile == profile && f.Severity == severity && strings.Contains(f.Message, msgPart) && strings.Contains(f.Fix, fix) {
				return true
			}
		}
		return false
	}
	checks := []struct{ profile, severity, msg, fix string }{
		{"claude/oauth", "info", "wrapper-only", ""},
		{"claude/self", "problem", "profile folder is stale", "run: aprofile sync"},
		{"claude/self", "problem", "settings.json sets env ANTHROPIC_DEFAULT_OPUS_MODEL, which this profile does not set", "remove the entry or set it on the profile"},
		{"claude/shared", "problem", "the linked settings.json sets env ANTHROPIC_BASE_URL, which overrides the value the wrapper sets", "remove the entry from the default setup or use share level content"},
	}
	checks = append(checks, []struct{ profile, severity, msg, fix string }{
		{"claude/envkey", "info", "needs MY_KEY_VAR in the launching environment", ""},
		{"claude/cleared", "info", "key referenced through a variable aprofile clears", ""},
		{"codex/noprov", "info", "selects no provider defined in the file", ""},
		{"opencode/oc", "info", "FOO is wrapper-only", ""},
	}...)
	for _, c := range checks {
		if !has(c.profile, c.severity, c.msg, c.fix) {
			t.Errorf("doctor lacks %s %s %q (fix %q):\n%s", c.profile, c.severity, c.msg, c.fix, dj)
		}
	}
	// FR-020: self-containment and wrapper-only settings are information, never a
	// problem.
	for _, f := range findings.Findings {
		if f.Severity == "problem" && (strings.Contains(f.Message, "wrapper-only") || strings.Contains(f.Message, "profile folder: ")) {
			t.Errorf("doctor reports folder information as a problem: %+v", f)
		}
	}

	// No key value in any output.
	for _, args := range [][]string{{"show", "claude/self", "--json"}, {"show", "claude/self"}, {"doctor", "--json"}, {"list", "--json"}} {
		so, se, _ := e.tryRunSplit([]string{"MY_KEY_VAR=sk-report-canary"}, args...)
		if strings.Contains(so+se, "sk-report-canary") {
			t.Errorf("%v printed the key", args)
		}
	}
}
