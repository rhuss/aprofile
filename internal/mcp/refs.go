package mcp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/secret"
)

// A reference in the neutral record is stored in internal/secret's canonical form
// ("env:NAME" or "file:/abs/path"). Each JSON flavor expresses an env reference with
// its own inline syntax; codex expresses references structurally (see codex.go).

// dollarRefRE matches Claude/pi's `${NAME}` whole-value env reference.
var dollarRefRE = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// braceEnvRefRE matches OpenCode's `{env:NAME}` whole-value env reference.
var braceEnvRefRE = regexp.MustCompile(`^\{env:([A-Za-z_][A-Za-z0-9_]*)\}$`)

// encodeDollarRef renders an env reference as `${NAME}` (Claude, pi). A file
// reference has no inline form here; ok=false reports it as unrepresentable.
func encodeDollarRef(ref string) (string, bool) {
	r, err := secret.ParseReference(ref)
	if err != nil || r.Kind != secret.KindEnv {
		return "", false
	}
	return "${" + r.Value + "}", true
}

// decodeDollarRef turns a `${NAME}` value into the neutral "env:NAME"; any other
// value is returned unchanged as a literal.
func decodeDollarRef(v string) string {
	if m := dollarRefRE.FindStringSubmatch(v); m != nil {
		return "env:" + m[1]
	}
	return v
}

// encodeBraceEnvRef renders an env reference as `{env:NAME}` (OpenCode).
func encodeBraceEnvRef(ref string) (string, bool) {
	r, err := secret.ParseReference(ref)
	if err != nil || r.Kind != secret.KindEnv {
		return "", false
	}
	return "{env:" + r.Value + "}", true
}

// decodeBraceEnvRef turns a `{env:NAME}` value into "env:NAME"; any other value is a
// literal returned unchanged.
func decodeBraceEnvRef(v string) string {
	if m := braceEnvRefRE.FindStringSubmatch(v); m != nil {
		return "env:" + m[1]
	}
	return v
}

// encodeInlineMap renders a neutral env/headers map (name -> reference-or-literal)
// into a native string map using encRef for references, collecting a Drop for every
// file reference (no inline form) and every literal value that matches a secret
// pattern (Constitution IV). fieldKind is "env" or "headers" for drop reporting.
func encodeInlineMap(server, fieldKind string, m map[string]string, encRef func(string) (string, bool)) (map[string]string, []Drop) {
	if len(m) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	var drops []Drop
	for _, k := range sortedKeys(m) {
		v := m[k]
		if secret.IsReference(v) {
			enc, ok := encRef(v)
			if !ok {
				drops = append(drops, Drop{Server: server, Field: fmt.Sprintf("%s[%s]", fieldKind, k), Reason: ReasonUnrepresentable})
				continue
			}
			out[k] = enc
			continue
		}
		if isSecretLiteral(v) {
			drops = append(drops, Drop{Server: server, Field: fmt.Sprintf("%s[%s]", fieldKind, k), Reason: ReasonLiteralSecret})
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil, drops
	}
	return out, drops
}

// decodeInlineMap turns a native string map into the neutral form using decRef to
// recognize references; everything else stays a literal.
func decodeInlineMap(m map[string]string, decRef func(string) string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range m {
		out[k] = decRef(v)
	}
	return out
}

// isSecretLiteral reports whether a literal value looks like it embeds a secret. It
// checks the whole value AND each whitespace-separated token, so a secret hidden
// after a scheme word (for example a header value "Bearer sk-ant-...", "token ghp_...",
// or a long base64 blob after "Basic ") is still caught and never written verbatim to
// a target file (FR-035, Constitution IV).
func isSecretLiteral(v string) bool {
	if secretToken(v) {
		return true
	}
	for _, tok := range strings.Fields(v) {
		if secretToken(tok) {
			return true
		}
	}
	return false
}

// secretToken applies the two FR-028 checks to a single token.
func secretToken(v string) bool {
	return secret.CheckPrefix(v) != nil || secret.CheckLength(v) != nil
}

// isLiteralSecretValue reports whether v is a non-reference literal matching a secret
// pattern (used to warn on verbatim-carried owned values, FR-033a).
func isLiteralSecretValue(v string) bool {
	return !secret.IsReference(v) && isSecretLiteral(v)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
