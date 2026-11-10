package secret_test

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/secret"
)

func TestParseReference(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		kind    secret.Kind
		value   string
	}{
		{"env:WORK_TOKEN", false, secret.KindEnv, "WORK_TOKEN"},
		{"env:_X9", false, secret.KindEnv, "_X9"},
		{"file:/abs/path/to/secret", false, secret.KindFile, "/abs/path/to/secret"},
		{"env:1BAD", true, "", ""},           // may not start with digit
		{"env:has-dash", true, "", ""},       // dash not allowed
		{"file:relative/path", true, "", ""}, // must be absolute
		{"file:/has\nnewline", true, "", ""}, // no newlines
		{"plainvalue", true, "", ""},         // not a reference
		{"sk-literalsecret", true, "", ""},   // not a reference
	}
	for _, c := range cases {
		ref, err := secret.ParseReference(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseReference(%q) expected error, got %+v", c.in, ref)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseReference(%q) unexpected error: %v", c.in, err)
			continue
		}
		if ref.Kind != c.kind || ref.Value != c.value || ref.Raw != c.in {
			t.Errorf("ParseReference(%q) = %+v, want kind=%s value=%s", c.in, ref, c.kind, c.value)
		}
	}
}

func TestLiteralSecretRejectedByPrefix(t *testing.T) {
	// A literal secret must be rejected by the prefix check whether or not it is
	// marked literal (FR-028: prefix check always applies).
	for _, v := range []string{"sk-abc", "ghp_abc", "xoxb-abc", "AKIAEXAMPLE"} {
		if err := secret.ValidateValue(v, false); err == nil {
			t.Errorf("ValidateValue(%q, literal=false) should fail prefix check", v)
		}
		if err := secret.ValidateValue(v, true); err == nil {
			t.Errorf("ValidateValue(%q, literal=true) should still fail prefix check", v)
		}
	}
}

func TestLiteralSecretRejectedByLength(t *testing.T) {
	long := strings.Repeat("a", 40) // 40 chars, no spaces, no known prefix
	if err := secret.ValidateValue(long, false); err == nil {
		t.Errorf("ValidateValue(long, literal=false) should fail length check")
	}
	// --env-literal skips the length check.
	if err := secret.ValidateValue(long, true); err != nil {
		t.Errorf("ValidateValue(long, literal=true) should skip length check, got %v", err)
	}
}

func TestLengthCheckAllowsSpacedValues(t *testing.T) {
	spaced := "this is a long human readable sentence over thirty-two chars"
	if err := secret.ValidateValue(spaced, false); err != nil {
		t.Errorf("value with spaces should pass length check, got %v", err)
	}
}

func TestHookEnvPrefixOnly(t *testing.T) {
	// Hook env output gets the prefix check only, so a long path/URL is allowed
	// but a known secret prefix is not (FR-011, FR-028).
	longURL := "https://example.com/" + strings.Repeat("a", 60)
	if err := secret.CheckPrefix(longURL); err != nil {
		t.Errorf("long URL should pass prefix check: %v", err)
	}
	if err := secret.CheckPrefix("sk-secret"); err == nil {
		t.Errorf("sk- value should fail prefix check")
	}
}

func TestResolveShellEnvFailClosed(t *testing.T) {
	ref, err := secret.ParseReference("env:WORK_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	sh := ref.ResolveShell("ANTHROPIC_AUTH_TOKEN", "claude/work", quoteForTest, "")
	// Fail-closed guard present.
	if !strings.Contains(sh, `if [ -z "${WORK_TOKEN:-}" ]; then`) {
		t.Errorf("missing unset guard:\n%s", sh)
	}
	if !strings.Contains(sh, "exit 1") {
		t.Errorf("missing fail-closed exit:\n%s", sh)
	}
	if !strings.Contains(sh, `ANTHROPIC_AUTH_TOKEN="${WORK_TOKEN}"`) {
		t.Errorf("missing assignment:\n%s", sh)
	}
	if !strings.Contains(sh, "export ANTHROPIC_AUTH_TOKEN") {
		t.Errorf("missing export:\n%s", sh)
	}
	// No secret value is present (only the variable name).
	if strings.Contains(sh, "WORK_TOKEN_VALUE") {
		t.Errorf("unexpected content")
	}
}

func TestResolveShellFileFailClosed(t *testing.T) {
	ref, err := secret.ParseReference("file:/etc/secret")
	if err != nil {
		t.Fatal(err)
	}
	sh := ref.ResolveShell("ANTHROPIC_AUTH_TOKEN", "claude/work", quoteForTest, "")
	if !strings.Contains(sh, "IFS= read -r ANTHROPIC_AUTH_TOKEN <") {
		t.Errorf("file ref should read first line via read builtin:\n%s", sh)
	}
	if !strings.Contains(sh, "exit 1") {
		t.Errorf("missing fail-closed exit:\n%s", sh)
	}
}

// quoteForTest is a minimal POSIX single-quote implementation for tests.
func quoteForTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":    "'plain'",
		"it's":     `'it'\''s'`,
		"$(rm -x)": "'$(rm -x)'",
	}
	for in, want := range cases {
		if got := secret.ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
