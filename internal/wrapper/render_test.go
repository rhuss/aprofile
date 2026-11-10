package wrapper_test

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

func claudeDef(t testing.TB) *harness.Definition {
	t.Helper()
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, _ := set.Get("claude")
	return d
}

func renderWork(t *testing.T) string {
	t.Helper()
	ref, _ := secret.ParseReference("env:WORK_TOKEN")
	p := &profile.Profile{
		Harness:  "claude",
		Name:     "work",
		Label:    "Work",
		Color:    "blue",
		Icon:     "W",
		AuthKind: "oauth-token",
		AuthRefs: map[string]secret.Reference{"CLAUDE_CODE_OAUTH_TOKEN": ref},
		Model:    "sonnet",
		Endpoint: "https://api.example.com",
		ExtraEnv: []profile.ExtraEnvVar{{Name: "TEAM", Value: "platform", Literal: true}},
	}
	out, err := wrapper.Render(p, claudeDef(t), "/data/profiles/claude/work")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

func TestRenderSetsConfigEnvAndContract(t *testing.T) {
	s := renderWork(t)
	must := []string{
		"CLAUDE_CONFIG_DIR='/data/profiles/claude/work'",
		"APROFILE_HARNESS='claude'",
		"APROFILE_NAME='work'",
		"APROFILE_LABEL='Work'",
		"APROFILE_COLOR='blue'",
		"APROFILE_ICON='W'",
		"APROFILE_CONFIG_DIR='/data/profiles/claude/work'",
		"APROFILE_SHARE='all'",      // a profile without a stored level is all (FR-033)
		"APROFILE_HISTORY_GROUP=''", // isolated: always set, empty (spec 003, FR-024)
	}
	for _, m := range must {
		if !strings.Contains(s, m) {
			t.Errorf("wrapper missing %q\n---\n%s", m, s)
		}
	}
}

func TestRenderMarkerPresent(t *testing.T) {
	s := renderWork(t)
	if !wrapper.IsWrapper([]byte(s)) {
		t.Error("rendered wrapper not recognized by IsWrapper")
	}
	if !strings.HasPrefix(s, "#!/bin/sh") {
		t.Error("wrapper must start with a POSIX sh shebang")
	}
}

func TestRenderAuthVarsSetAndOthersUnset(t *testing.T) {
	s := renderWork(t)
	// The reference source is captured into a temp BEFORE the auth vars are cleared, so a
	// reference that reads from an auth variable still sees the caller's value after the
	// unset (FR-028a).
	capLine := `_aprofile_src_WORK_TOKEN="${WORK_TOKEN-}"`
	capIdx := strings.Index(s, capLine)
	// ANTHROPIC_API_KEY appears only in the auth-clearing unset line (it is an auth var,
	// not a model/endpoint var cleared by the nested-session cleanup), so it anchors the
	// auth unset reliably.
	unsetIdx := strings.Index(s, "ANTHROPIC_API_KEY")
	if capIdx < 0 {
		t.Errorf("reference source not captured before unset:\n%s", s)
	} else if unsetIdx >= 0 && capIdx > unsetIdx {
		t.Errorf("reference captured after the unset (collision risk):\n%s", s)
	}
	// Selected kind's var is resolved fail-closed from the captured temp and assigned.
	if !strings.Contains(s, `if [ -z "${_aprofile_src_WORK_TOKEN:-}" ]; then`) {
		t.Errorf("auth var not resolved fail-closed from the captured temp:\n%s", s)
	}
	if !strings.Contains(s, `CLAUDE_CODE_OAUTH_TOKEN="${_aprofile_src_WORK_TOKEN}"`) {
		t.Errorf("auth var not assigned from the captured temp:\n%s", s)
	}
	// Competing auth kinds' vars are unset.
	if !strings.Contains(s, "unset ANTHROPIC_API_KEY") || !strings.Contains(s, "ANTHROPIC_AUTH_TOKEN") {
		t.Errorf("competing auth vars not unset:\n%s", s)
	}
}

// Feature 004 (FR-013): a wrapper-carried setting (here: the profile is at share
// level all, so settings.json is linked and the wrapper carries everything) is set
// unconditionally; a caller's value never overrides it.
func TestRenderWrapperCarriedSetUnconditionally(t *testing.T) {
	s := renderWork(t)
	if strings.Contains(s, `${ANTHROPIC_MODEL:-}`) || strings.Contains(s, `${ANTHROPIC_BASE_URL:-}`) || strings.Contains(s, "if [ -z \"${TEAM") {
		t.Errorf("wrapper still offers caller overrides:\n%s", s)
	}
	for _, want := range []string{"ANTHROPIC_MODEL='sonnet'", "ANTHROPIC_BASE_URL='https://api.example.com'", "export ANTHROPIC_MODEL"} {
		if !strings.Contains(s, want) {
			t.Errorf("wrapper lacks %q:\n%s", want, s)
		}
	}
}

func TestRenderExportsSetVars(t *testing.T) {
	s := renderWork(t)
	if !strings.Contains(s, "APROFILE_SET_VARS=") {
		t.Errorf("APROFILE_SET_VARS not exported:\n%s", s)
	}
	// It must list the model, endpoint and extra var names it set.
	for _, name := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL", "TEAM"} {
		if !strings.Contains(s, name) {
			t.Errorf("APROFILE_SET_VARS context missing %q:\n%s", name, s)
		}
	}
}

// TestWrapperFullAProfileContract asserts the complete APROFILE_* contract set from
// contracts/registry-and-env.md is assigned and exported (FR-026).
func TestWrapperFullAProfileContract(t *testing.T) {
	s := renderWork(t)
	contractVars := []string{
		"APROFILE_HARNESS", "APROFILE_NAME", "APROFILE_LABEL", "APROFILE_COLOR",
		"APROFILE_ICON", "APROFILE_CONFIG_DIR", "APROFILE_SHARE", "APROFILE_HISTORY_GROUP", "APROFILE_SET_VARS",
	}
	for _, name := range contractVars {
		if !strings.Contains(s, name+"=") {
			t.Errorf("wrapper missing contract variable assignment %q:\n%s", name, s)
		}
	}
	if !strings.Contains(s, "export APROFILE_HARNESS APROFILE_NAME APROFILE_LABEL APROFILE_COLOR APROFILE_ICON APROFILE_CONFIG_DIR APROFILE_SHARE APROFILE_HISTORY_GROUP") {
		t.Errorf("the always-set contract variables are not exported together:\n%s", s)
	}
	if !strings.Contains(s, "export APROFILE_SET_VARS") {
		t.Errorf("APROFILE_SET_VARS is not exported:\n%s", s)
	}
}

func TestRenderConfigDirGuardAndExec(t *testing.T) {
	s := renderWork(t)
	if !strings.Contains(s, `if [ ! -d '/data/profiles/claude/work' ]; then`) {
		t.Errorf("missing config-dir existence guard:\n%s", s)
	}
	if !strings.Contains(s, "aprofile sync") {
		t.Errorf("config-dir guard should tell the user to run aprofile sync:\n%s", s)
	}
	// The binary is shell-quoted in the exec line too (FR-028b), matching the quoted
	// form used in the command -v guard, so a binary name can never inject shell code.
	if !strings.Contains(s, `exec 'claude' "$@"`) {
		t.Errorf("wrapper must exec the shell-quoted harness binary passing args through:\n%s", s)
	}
}

func TestRenderHerdrAgentOnlyWhenDeclared(t *testing.T) {
	s := renderWork(t)
	if strings.Contains(s, "HERDR_AGENT") {
		t.Errorf("HERDR_AGENT must not appear unless declared:\n%s", s)
	}
	// Now with it declared.
	ref, _ := secret.ParseReference("env:WORK_TOKEN")
	p := &profile.Profile{
		Harness: "claude", Name: "work", AuthKind: "oauth-token",
		AuthRefs:   map[string]secret.Reference{"CLAUDE_CODE_OAUTH_TOKEN": ref},
		HerdrAgent: "worker",
	}
	out, err := wrapper.Render(p, claudeDef(t), "/d/work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "HERDR_AGENT='worker'") {
		t.Errorf("HERDR_AGENT not set when declared:\n%s", out)
	}
}

func TestRenderLiteralExtraShellQuoted(t *testing.T) {
	s := renderWork(t)
	if !strings.Contains(s, "TEAM='platform'") {
		t.Errorf("literal extra var not shell-quoted:\n%s", s)
	}
}

// Feature 004 (FR-014): every wrapper clears the inherited APROFILE_SET_VARS names
// and the definition's launch_clear names unconditionally, without comparing profile
// names.
func TestRenderUnconditionalCleanup(t *testing.T) {
	s := renderWork(t)
	if strings.Contains(s, "_aprofile_prev_name") || strings.Contains(s, "APROFILE_NAME:-") {
		t.Errorf("cleanup still compares profile names:\n%s", s)
	}
	if !strings.Contains(s, `for _v in ${_aprofile_prev_setvars}; do`) {
		t.Errorf("missing cleanup of inherited APROFILE_SET_VARS:\n%s", s)
	}
	line := unsetLine(t, s, "ANTHROPIC_DEFAULT_FABLE_MODEL")
	for _, v := range []string{"ANTHROPIC_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL", "TEAM", "APROFILE_SET_VARS"} {
		if !strings.Contains(" "+line+" ", " "+v+" ") {
			t.Errorf("cleanup unset line lacks %s: %s", v, line)
		}
	}
}

func TestRenderNoShellInjection(t *testing.T) {
	// A malicious model value must be rendered as a quoted literal, not executed.
	p := &profile.Profile{
		Harness: "claude", Name: "work", AuthKind: "oauth-login",
		Model: "$(touch /tmp/pwned)",
	}
	out, err := wrapper.Render(p, claudeDef(t), "/d/work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `ANTHROPIC_MODEL='$(touch /tmp/pwned)'`) {
		t.Errorf("model value not safely single-quoted:\n%s", out)
	}
}

// spec 003 FR-024: a member's wrapper exports its history group; the value is never
// inherited from the caller.
func TestRenderHistoryGroupContract(t *testing.T) {
	p := &profile.Profile{Harness: "claude", Name: "lab", AuthKind: "oauth-login", HistoryGroup: "home"}
	out, err := wrapper.Render(p, claudeDef(t), "/data/profiles/claude/lab")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "APROFILE_HISTORY_GROUP='home'") {
		t.Errorf("wrapper lacks APROFILE_HISTORY_GROUP='home':\n%s", out)
	}
	env, err := wrapper.Environ(&profile.Profile{Harness: "claude", Name: "solo", AuthKind: "oauth-login"}, claudeDef(t),
		"/data/profiles/claude/solo", wrapper.RenderOptions{}, []string{"PATH=/usr/bin:/bin", "APROFILE_HISTORY_GROUP=inherited"})
	if err != nil {
		t.Fatalf("Environ: %v", err)
	}
	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "APROFILE_HISTORY_GROUP=") {
			found = true
			if kv != "APROFILE_HISTORY_GROUP=" {
				t.Errorf("Environ: %s, want empty for an isolated profile", kv)
			}
		}
	}
	if !found {
		t.Error("Environ does not set APROFILE_HISTORY_GROUP")
	}
}
