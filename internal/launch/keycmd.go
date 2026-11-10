package launch

import (
	"fmt"

	"github.com/cc-deck/aprofile/internal/secret"
)

// Key commands (FR-006, research R5): the harness runs them under /bin/sh, so they
// use the wrapper's own `IFS= read -r` and print exactly the value the wrapper
// resolves for the same reference (backslashes, surrounding whitespace and a
// carriage return kept; only the first line). They hold a path or a variable name,
// never a key value (Principle IV).

// fileReadScript is the provider-auth script; the path arrives as "$1", so no
// profile value is ever part of the script text.
const fileReadScript = `k=; IFS= read -r k < "$1" || [ -n "$k" ]; printf "%s" "$k"`

// ShellCommand returns the shell-form key command (Claude Code apiKeyHelper) for a
// reference. The path is single-quoted and a variable name validated, so no value
// can inject shell code (FR-006, spec 001 FR-028b).
func ShellCommand(r secret.Reference) (string, error) {
	switch r.Kind {
	case secret.KindFile:
		if r.Value == "" {
			return "", fmt.Errorf("reference %s: empty path", r.Raw)
		}
		return fmt.Sprintf(`k=; IFS= read -r k < %s || [ -n "$k" ]; printf '%%s' "$k"`, secret.ShellQuote(r.Value)), nil
	case secret.KindEnv:
		if !secret.ValidEnvVarName(r.Value) {
			return "", fmt.Errorf("reference %s: invalid variable name", r.Raw)
		}
		return fmt.Sprintf(`printf '%%s' "${%s:-}"`, r.Value), nil
	}
	return "", fmt.Errorf("reference %s: unknown kind %q", r.Raw, r.Kind)
}

// ProviderAuth returns the provider-auth table (Codex [model_providers.<p>.auth]):
// command "sh" with the read script and the path as a separate argument, or the
// env form printing the validated variable.
func ProviderAuth(r secret.Reference) (map[string]any, error) {
	switch r.Kind {
	case secret.KindFile:
		if r.Value == "" {
			return nil, fmt.Errorf("reference %s: empty path", r.Raw)
		}
		return map[string]any{"command": "sh", "args": []any{"-c", fileReadScript, "aprofile-key", r.Value}}, nil
	case secret.KindEnv:
		if !secret.ValidEnvVarName(r.Value) {
			return nil, fmt.Errorf("reference %s: invalid variable name", r.Raw)
		}
		return map[string]any{"command": "sh", "args": []any{"-c", fmt.Sprintf(`printf "%%s" "${%s:-}"`, r.Value)}}, nil
	}
	return nil, fmt.Errorf("reference %s: unknown kind %q", r.Raw, r.Kind)
}
