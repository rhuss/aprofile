package mcp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cc-deck/aprofile/internal/secret"
)

// Embedded references. A native value may hold env references inside a larger
// string ("Bearer ${TOKEN}", "--token={env:TOK}", "${MODE:-fast}"). The neutral record
// keeps such a value as a template: tmplPrefix followed by the text, with each
// reference written `${env:NAME}` or `${env:NAME:-default}` and a literal `$` written
// `$$`. A whole-value env/header reference without a default stays in the plain
// "env:NAME" form (internal/secret); args and url values never use that form, since
// a literal argument may legitimately start with "env:".
//
// Each flavor states, as data (repCaps.refs), which fields it expands references in
// and with which syntax (refSyntax). An encoder translates a template into the
// target's syntax where the field expands references, and otherwise drops the value
// and reports it as unrepresentable, so a dead `${X}` literal is never written.
// Every encoded value is parsed back with the target syntax and must read back as
// exactly the neutral parts; a literal the target would expand, substitute or run is
// dropped and reported instead.

const tmplPrefix = "tmpl:"

// cmdPrefix marks a neutral env/header value that is a pi shell command (pi runs a
// value starting with `!` and uses its output). Only the pi decoder sets it, and only
// a syntax with commandBang writes it back (as `!...`); every other target reports
// it unrepresentable. A literal that merely starts with `!` is never a command: pi
// gets it escaped as `$!...`. A literal that collides with a neutral marker is kept
// as a template (neutralLiteral), so it can never turn into a command or a reference.
const cmdPrefix = "cmd:"

// tmplPart is one piece of a template: a literal, an env reference with an optional
// default, or an opaque substitution (native) a brace syntax performs, such as
// OpenCode's `{file:path}`. cmd marks a whole-value pi command (lit holds the text).
type tmplPart struct {
	lit    string
	ref    string  // env var name; "" for a literal part
	def    *string // default for `${NAME:-default}`; nil when none
	native string  // the inside of an opaque `{word:...}` substitution, e.g. "file:~/.k"
	cmd    bool    // the whole value is a shell command (lit holds the command text)
}

// isLit reports whether the part is plain literal text.
func (p tmplPart) isLit() bool { return p.ref == "" && p.native == "" && !p.cmd }

// refSyntax describes how a flavor writes an env reference inside a string.
type refSyntax struct {
	braceEnv     bool // `{env:NAME}` (OpenCode) instead of `${NAME}`; any other `{word:...}` is an opaque substitution
	defaults     bool // `${NAME:-default}` is understood
	bare         bool // `$NAME` is a reference too
	dollarEscape bool // `$$` is a literal `$` (and `$!` a literal `!`)
	commandBang  bool // a value starting with `!` is a shell command
}

var (
	dollarDefaultRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)
	dollarPlainRE   = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	// braceSubstRE matches every `{word:...}` form a brace syntax may substitute
	// (OpenCode: `{env:NAME}`, `{file:path}`); only a valid `{env:NAME}` is a reference.
	braceSubstRE    = regexp.MustCompile(`\{([A-Za-z_]+):([^}]*)\}`)
	neutralRefRE    = regexp.MustCompile(`^\$\{env:([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)
	neutralNativeRE = regexp.MustCompile(`^\$\{native:([^}]*)\}`)
	envNamePrefixRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)
)

// parse splits a native value into parts using the flavor's syntax.
func (syn refSyntax) parse(v string) []tmplPart {
	if syn.commandBang && strings.HasPrefix(v, "!") {
		return []tmplPart{{lit: v[1:], cmd: true}}
	}
	if syn.dollarEscape || syn.bare {
		return syn.scan(v)
	}
	if syn.braceEnv {
		return parseBrace(v)
	}
	re := dollarPlainRE
	if syn.defaults {
		re = dollarDefaultRE
	}
	var parts []tmplPart
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(v, -1) {
		parts = appendLit(parts, v[last:m[0]])
		p := tmplPart{ref: v[m[2]:m[3]]}
		if len(m) > 4 && m[4] >= 0 {
			d := v[m[4]:m[5]]
			p.def = &d
		}
		parts = append(parts, p)
		last = m[1]
	}
	return appendLit(parts, v[last:])
}

// parseBrace is the parser for a brace syntax (OpenCode): `{env:NAME}` is a
// reference, and any other `{word:...}` (`{file:path}`, an `{env:...}` with an
// invalid name) is an opaque substitution, so a literal holding one never compares
// equal to its literal text and cannot be written there.
func parseBrace(v string) []tmplPart {
	var parts []tmplPart
	last := 0
	for _, m := range braceSubstRE.FindAllStringSubmatchIndex(v, -1) {
		parts = appendLit(parts, v[last:m[0]])
		word, body := v[m[2]:m[3]], v[m[4]:m[5]]
		if word == "env" && secret.ValidEnvVarName(body) {
			parts = append(parts, tmplPart{ref: body})
		} else {
			parts = append(parts, tmplPart{native: v[m[0]+1 : m[1]-1]})
		}
		last = m[1]
	}
	return appendLit(parts, v[last:])
}

// scan is the parser for a `$$`-escaping syntax (pi): `$$` and `$!` are literals,
// `${NAME}` and (when bare) `$NAME` are references, any other `$` is literal.
func (syn refSyntax) scan(v string) []tmplPart {
	var parts []tmplPart
	i := 0
	for i < len(v) {
		j := strings.IndexByte(v[i:], '$')
		if j < 0 {
			parts = appendLit(parts, v[i:])
			break
		}
		parts = appendLit(parts, v[i:i+j])
		i += j
		rest := v[i+1:]
		switch {
		case syn.dollarEscape && (strings.HasPrefix(rest, "$") || strings.HasPrefix(rest, "!")):
			parts = appendLit(parts, rest[:1])
			i += 2
		case strings.HasPrefix(rest, "{"):
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				parts = appendLit(parts, "$")
				i++
				continue
			}
			name := rest[1:end]
			if secret.ValidEnvVarName(name) {
				parts = append(parts, tmplPart{ref: name})
			} else {
				parts = appendLit(parts, v[i:i+end+2])
			}
			i += end + 2
		case syn.bare && envNamePrefixRE.MatchString(rest):
			name := envNamePrefixRE.FindString(rest)
			parts = append(parts, tmplPart{ref: name})
			i += 1 + len(name)
		default:
			parts = appendLit(parts, "$")
			i++
		}
	}
	return parts
}

// render writes parts in the flavor's syntax. A default the syntax cannot express is
// left out and reported through defaultLost. A command or opaque substitution is
// written in its own form; encodeValue's read-back check rejects it for a syntax
// that would not read it back as such.
func (syn refSyntax) render(parts []tmplPart) (out string, defaultLost bool) {
	if len(parts) == 1 && parts[0].cmd {
		return "!" + parts[0].lit, false
	}
	var b strings.Builder
	for _, p := range parts {
		switch {
		case p.native != "":
			b.WriteString("{" + p.native + "}")
			continue
		case p.ref == "":
			if syn.dollarEscape {
				b.WriteString(strings.ReplaceAll(p.lit, "$", "$$"))
			} else {
				b.WriteString(p.lit)
			}
			continue
		}
		switch {
		case syn.braceEnv:
			b.WriteString("{env:" + p.ref + "}")
		case p.def != nil && syn.defaults:
			b.WriteString("${" + p.ref + ":-" + *p.def + "}")
		default:
			b.WriteString("${" + p.ref + "}")
		}
		if p.def != nil && !syn.defaults {
			defaultLost = true
		}
	}
	out = b.String()
	if syn.commandBang && syn.dollarEscape && strings.HasPrefix(out, "!") {
		out = "$" + out // a leading literal `!` must not turn the value into a command
	}
	return out, defaultLost
}

func appendLit(parts []tmplPart, s string) []tmplPart {
	if s == "" {
		return parts
	}
	if n := len(parts); n > 0 && parts[n-1].isLit() {
		parts[n-1].lit += s
		return parts
	}
	return append(parts, tmplPart{lit: s})
}

// allLit reports whether every part is plain literal text.
func allLit(parts []tmplPart) bool {
	for _, p := range parts {
		if !p.isLit() {
			return false
		}
	}
	return true
}

func joinLits(parts []tmplPart) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.lit)
	}
	return b.String()
}

// stripDefaults returns parts without defaults (what a target lacking defaults keeps).
func stripDefaults(parts []tmplPart) []tmplPart {
	out := make([]tmplPart, len(parts))
	for i, p := range parts {
		p.def = nil
		out[i] = p
	}
	return out
}

func sameParts(a, b []tmplPart) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].lit != b[i].lit || a[i].ref != b[i].ref || a[i].native != b[i].native || a[i].cmd != b[i].cmd ||
			(a[i].def == nil) != (b[i].def == nil) || (a[i].def != nil && *a[i].def != *b[i].def) {
			return false
		}
	}
	return true
}

// isTemplate reports whether a neutral value is a template.
func isTemplate(v string) bool { return strings.HasPrefix(v, tmplPrefix) }

// formatTemplate renders parts as a neutral template value. An opaque substitution
// is written `${native:<inside>}`. A command part never appears in a template.
func formatTemplate(parts []tmplPart) string {
	var b strings.Builder
	b.WriteString(tmplPrefix)
	for _, p := range parts {
		switch {
		case p.native != "":
			b.WriteString("${native:" + p.native + "}")
		case p.ref == "":
			b.WriteString(strings.ReplaceAll(p.lit, "$", "$$"))
		default:
			b.WriteString("${env:" + p.ref)
			if p.def != nil {
				b.WriteString(":-" + *p.def)
			}
			b.WriteString("}")
		}
	}
	return b.String()
}

// parseTemplate splits a neutral template value (with its prefix) into parts.
func parseTemplate(v string) []tmplPart {
	v = strings.TrimPrefix(v, tmplPrefix)
	var parts []tmplPart
	for i := 0; i < len(v); {
		if v[i] != '$' {
			j := strings.IndexByte(v[i:], '$')
			if j < 0 {
				j = len(v) - i
			}
			parts = appendLit(parts, v[i:i+j])
			i += j
			continue
		}
		if strings.HasPrefix(v[i:], "$$") {
			parts = appendLit(parts, "$")
			i += 2
			continue
		}
		if m := neutralRefRE.FindStringSubmatchIndex(v[i:]); m != nil {
			p := tmplPart{ref: v[i+m[2] : i+m[3]]}
			if m[4] >= 0 {
				d := v[i+m[4] : i+m[5]]
				p.def = &d
			}
			parts = append(parts, p)
			i += m[1]
			continue
		}
		if m := neutralNativeRE.FindStringSubmatchIndex(v[i:]); m != nil && m[3] > m[2] {
			parts = append(parts, tmplPart{native: v[i+m[2] : i+m[3]]})
			i += m[1]
			continue
		}
		parts = appendLit(parts, "$")
		i++
	}
	return parts
}

// neutralParts returns the parts of a neutral value. In a map value (env, headers)
// an "env:NAME" reference is a whole-value reference and a "cmd:" value a pi
// command; elsewhere both are literals. ok=false marks a reference with no inline
// form (a file: reference). The empty value has no parts, exactly as a syntax's parse
// of "" has none, so it encodes as "" and reads back as itself in every flavor.
func neutralParts(v string, mapValue bool) (parts []tmplPart, ok bool) {
	switch {
	case v == "":
		return nil, true
	case isTemplate(v):
		return parseTemplate(v), true
	case mapValue && strings.HasPrefix(v, cmdPrefix):
		return []tmplPart{{lit: strings.TrimPrefix(v, cmdPrefix), cmd: true}}, true
	case mapValue && secret.IsReference(v):
		r, err := secret.ParseReference(v)
		if err != nil || r.Kind != secret.KindEnv {
			return nil, false
		}
		return []tmplPart{{ref: r.Value}}, true
	default:
		return []tmplPart{{lit: v}}, true
	}
}

// neutralLiteral returns the neutral form of a literal value: the value itself, or,
// when it would be read back as something else (a template, a pi command or, in a
// map value, a reference), a template holding just that literal.
func neutralLiteral(v string, mapValue bool) string {
	if isTemplate(v) || (mapValue && (strings.HasPrefix(v, cmdPrefix) || secret.IsReference(v))) {
		return formatTemplate([]tmplPart{{lit: v}})
	}
	return v
}

// decodeValue turns a native value into its neutral form. syn is the source
// flavor's syntax for the field, or nil when the field takes literals only.
func decodeValue(v string, syn *refSyntax, mapValue bool) string {
	if syn == nil {
		return neutralLiteral(v, mapValue)
	}
	parts := syn.parse(v)
	switch {
	case len(parts) == 1 && parts[0].cmd:
		if !mapValue {
			return neutralLiteral(v, false) // commands exist only in env/header values
		}
		return cmdPrefix + parts[0].lit
	case allLit(parts):
		return neutralLiteral(joinLits(parts), mapValue)
	case mapValue && len(parts) == 1 && parts[0].ref != "" && parts[0].def == nil:
		return "env:" + parts[0].ref
	default:
		return formatTemplate(parts)
	}
}

// encodeValue renders a neutral value for one target field. syn is the target's
// syntax for the field (nil: the target expands nothing there). secretCheck flags a
// literal piece that must not be written (Constitution IV); a pi command's text gets
// the prefix check (it typically names a secret store path, which the length check
// would flag). It returns the native value and ok=false when the value is dropped;
// every drop and every lost default is reported under field.
func encodeValue(server, field, v string, syn *refSyntax, mapValue bool, secretCheck func(string) bool) (string, bool, []Drop) {
	parts, ok := neutralParts(v, mapValue)
	if !ok {
		return "", false, []Drop{{Server: server, Field: field, Reason: ReasonUnrepresentable}}
	}
	for _, p := range parts {
		switch {
		case p.cmd && secret.HasSecretPrefix(p.lit),
			p.isLit() && secretCheck != nil && secretCheck(p.lit):
			return "", false, []Drop{{Server: server, Field: field, Reason: ReasonLiteralSecret}}
		}
	}
	if syn == nil {
		if !allLit(parts) {
			return "", false, []Drop{{Server: server, Field: field, Reason: ReasonUnrepresentable}}
		}
		return joinLits(parts), true, nil
	}
	out, lost := syn.render(parts)
	// The target must read back exactly what was meant: a literal it would expand
	// as a reference or substitution, or run as a command, cannot be written safely,
	// nor can a command or substitution it would read as literal text.
	want := parts
	if !syn.defaults {
		want = stripDefaults(parts)
	}
	if !sameParts(syn.parse(out), want) {
		return "", false, []Drop{{Server: server, Field: field, Reason: ReasonUnrepresentable}}
	}
	var drops []Drop
	if lost {
		drops = append(drops, Drop{Server: server, Field: field + ".default", Reason: ReasonUnrepresentable})
	}
	return out, true, drops
}

// singleRef reports whether a neutral map value is exactly one env reference
// (with or without a default), as Codex's env_vars / env_http_headers can hold.
func singleRef(v string) (name string, def *string, ok bool) {
	parts, pok := neutralParts(v, true)
	if !pok || len(parts) != 1 || parts[0].ref == "" {
		return "", nil, false
	}
	return parts[0].ref, parts[0].def, true
}

// bearerRef reports whether a neutral header value is `Bearer <one env reference>`.
func bearerRef(v string) (name string, def *string, ok bool) {
	parts, pok := neutralParts(v, true)
	if !pok || len(parts) != 2 || !parts[0].isLit() || parts[1].ref == "" {
		return "", nil, false
	}
	scheme := strings.TrimRight(parts[0].lit, " \t")
	if !strings.EqualFold(scheme, "bearer") || len(scheme) == len(parts[0].lit) {
		return "", nil, false
	}
	return parts[1].ref, parts[1].def, true
}

// literalPartsSecret applies check to every literal piece of a neutral map value and
// the prefix check to a pi command's text (see encodeValue).
func literalPartsSecret(v string, check func(string) bool) bool {
	parts, ok := neutralParts(v, true)
	if !ok {
		return false
	}
	for _, p := range parts {
		if (p.isLit() && check(p.lit)) || (p.cmd && secret.HasSecretPrefix(p.lit)) {
			return true
		}
	}
	return false
}

// foldAuth moves a bearer token between the neutral Auth field and an Authorization
// header, by what the target can hold (c): a target with a bearer-token field takes
// `Authorization: Bearer <ref>` as Auth; a target without one but with references in
// headers takes Auth as that header. A default the bearer field cannot hold is
// reported. The input server is not modified.
func foldAuth(s Server, c repCaps) (Server, []Drop) {
	var drops []Drop
	switch {
	case c.auth && s.Auth == "":
		for _, k := range sortedKeys(s.Headers) {
			if !strings.EqualFold(k, "Authorization") {
				continue
			}
			name, def, ok := bearerRef(s.Headers[k])
			if !ok {
				continue
			}
			s.Headers = copyWithout(s.Headers, k)
			s.Auth = "env:" + name
			if def != nil {
				drops = append(drops, Drop{Server: s.Name, Field: fmt.Sprintf("headers[%s].default", k), Reason: ReasonUnrepresentable})
			}
			break
		}
	case !c.auth && s.Auth != "" && c.refs.headers != nil:
		r, err := secret.ParseReference(s.Auth)
		if err != nil || r.Kind != secret.KindEnv {
			break
		}
		for k := range s.Headers {
			if strings.EqualFold(k, "Authorization") {
				return s, nil // both set: auth is reported unrepresentable
			}
		}
		h := copyWithout(s.Headers, "")
		h["Authorization"] = formatTemplate([]tmplPart{{lit: "Bearer "}, {ref: r.Value}})
		s.Headers = h
		s.Auth = ""
	}
	return s, drops
}

func copyWithout(m map[string]string, drop string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if k != drop {
			out[k] = v
		}
	}
	return out
}
