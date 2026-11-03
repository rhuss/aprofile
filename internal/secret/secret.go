// Package secret parses and validates the secret references aprofile stores in
// place of plaintext secrets (FR-028), and renders the shell that resolves them
// at wrapper launch. No secret value is ever written to a generated artifact; the
// only thing stored is a reference to an environment variable or a file.
package secret

import (
	"fmt"
	"regexp"
	"strings"
)

// Kind identifies the two reference forms.
type Kind string

const (
	// KindEnv reads the secret from an environment variable at launch.
	KindEnv Kind = "env"
	// KindFile reads the first line of a file at launch.
	KindFile Kind = "file"
)

// nameRE is the pattern an environment variable name must match (FR-028).
var nameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidEnvVarName reports whether name is a valid environment-variable identifier
// ([A-Za-z_][A-Za-z0-9_]*). A value that fails this MUST NOT be interpolated into a
// generated wrapper as an assignment target, export or unset, because such a name
// could carry shell metacharacters and inject code (FR-028b). It is the single
// source of truth used by the profile store, the harness loader and the CLI.
func ValidEnvVarName(name string) bool { return nameRE.MatchString(name) }

// KnownSecretPrefixes are prefixes that mark a value as an obvious secret
// (FR-028 prefix check). The list is deliberately conservative but covers the
// examples the spec names (sk-, ghp_, xox) plus a few common siblings.
var KnownSecretPrefixes = []string{
	"sk-",         // OpenAI / Anthropic-style API keys
	"sk_",         // Stripe-style
	"ghp_",        // GitHub personal access token
	"gho_",        // GitHub OAuth token
	"ghu_",        // GitHub user-to-server token
	"ghs_",        // GitHub server-to-server token
	"ghr_",        // GitHub refresh token
	"github_pat_", // GitHub fine-grained PAT
	"xox",         // Slack tokens (xoxb-, xoxp-, ...)
	"xapp-",       // Slack app-level token
	"AKIA",        // AWS access key id
}

// Reference is a parsed env:/file: reference. It round-trips through its Raw form.
type Reference struct {
	Kind  Kind   // env or file
	Value string // the variable NAME (env) or the absolute path (file)
	Raw   string // the original "env:NAME" / "file:/abs/path" text
}

// String returns the canonical reference text, so a Reference marshals back to
// exactly what the user typed.
func (r Reference) String() string { return r.Raw }

// MarshalYAML encodes a Reference as its raw string form.
func (r Reference) MarshalYAML() (any, error) { return r.Raw, nil }

// UnmarshalYAML decodes a Reference from a scalar string, validating it.
func (r *Reference) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	ref, err := ParseReference(s)
	if err != nil {
		return err
	}
	*r = ref
	return nil
}

// IsReference reports whether s has one of the reference prefixes. It does not
// validate the body; use ParseReference for that.
func IsReference(s string) bool {
	return strings.HasPrefix(s, "env:") || strings.HasPrefix(s, "file:")
}

// ParseReference parses an env:/file: reference and validates its body. Anything
// that is not a well-formed reference is an error (FR-028: auth options accept
// only references).
func ParseReference(s string) (Reference, error) {
	switch {
	case strings.HasPrefix(s, "env:"):
		name := strings.TrimPrefix(s, "env:")
		if !nameRE.MatchString(name) {
			return Reference{}, fmt.Errorf("invalid env reference %q: variable name must match [A-Za-z_][A-Za-z0-9_]*", s)
		}
		return Reference{Kind: KindEnv, Value: name, Raw: s}, nil
	case strings.HasPrefix(s, "file:"):
		path := strings.TrimPrefix(s, "file:")
		if path == "" || !strings.HasPrefix(path, "/") {
			return Reference{}, fmt.Errorf("invalid file reference %q: path must be absolute", s)
		}
		if strings.ContainsAny(path, "\n\r") {
			return Reference{}, fmt.Errorf("invalid file reference %q: path must not contain newlines", s)
		}
		return Reference{Kind: KindFile, Value: path, Raw: s}, nil
	default:
		return Reference{}, fmt.Errorf("value %q is not a reference: use env:NAME or file:/absolute/path", redact(s))
	}
}

// CheckPrefix rejects values that begin with a known secret prefix (FR-028). It
// applies on every load/write path, including hook env output and literal values.
func CheckPrefix(value string) error {
	for _, p := range KnownSecretPrefixes {
		if strings.HasPrefix(value, p) {
			return fmt.Errorf("value looks like a secret (prefix %q); pass it as an env:/file: reference instead", p)
		}
	}
	return nil
}

// CheckLength rejects a value of 32 or more characters with no spaces (FR-028).
// It is skipped for literal-marked values and for hook env output.
func CheckLength(value string) error {
	if len(value) >= 32 && !strings.ContainsAny(value, " \t") {
		return fmt.Errorf("value looks like a secret (%d characters, no spaces); pass it as an env:/file: reference or use --env-literal", len(value))
	}
	return nil
}

// ValidateValue applies the FR-028 checks to a supplied literal value. When
// literal is true the length check is skipped (the value was given with
// --env-literal) but the prefix check still applies.
func ValidateValue(value string, literal bool) error {
	if err := CheckPrefix(value); err != nil {
		return err
	}
	if literal {
		return nil
	}
	return CheckLength(value)
}

// ParseAuthReference parses a value supplied for an auth option. Auth values
// accept only references and reject anything else (FR-028).
func ParseAuthReference(s string) (Reference, error) {
	return ParseReference(s)
}

// redact hides a value that is not a reference (it may be a pasted secret) when
// it appears in an error message.
func redact(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:2] + "***"
}
