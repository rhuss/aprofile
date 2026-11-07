package mcp

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cc-deck/aprofile/internal/secret"
)

// A reference in the neutral record is stored in internal/secret's canonical form
// ("env:NAME" or "file:/abs/path"), or, when it is embedded in a larger value or has
// a default, as a template (template.go). Each JSON flavor expresses a reference
// with its own inline syntax (repCaps.refs); codex expresses references
// structurally (see codex.go).

// encodeInlineMap renders a neutral env/headers map (name -> reference, template or
// literal) into a native string map in the target syntax syn, collecting a Drop for
// every value the target cannot express (a file reference, an embedded reference in
// a field it does not expand, a literal it would misread) and every literal piece
// that matches a secret pattern (Constitution IV). fieldKind is "env" or "headers".
// keySyn is the target's syntax for object keys (repCaps.refs.keys): a name it would
// substitute is dropped and reported unrepresentable with its value.
func encodeInlineMap(server, fieldKind string, m map[string]string, syn, keySyn *refSyntax) (map[string]string, []Drop) {
	if len(m) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	var drops []Drop
	for _, k := range sortedKeys(m) {
		if !literalKey(k, keySyn) {
			drops = append(drops, Drop{Server: server, Field: fmt.Sprintf("%s[%s]", fieldKind, k), Reason: ReasonUnrepresentable})
			continue
		}
		v, ok, d := encodeValue(server, fmt.Sprintf("%s[%s]", fieldKind, k), m[k], syn, true, isSecretLiteral)
		drops = append(drops, d...)
		if ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil, drops
	}
	return out, drops
}

// decodeInlineMap turns a native string map into the neutral form using the source
// syntax syn to recognize references; everything else stays a literal.
func decodeInlineMap(m map[string]string, syn *refSyntax) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range m {
		out[k] = decodeValue(v, syn, true)
	}
	return out
}

// decodeArgs turns native args into neutral form (templates where syn expands refs).
func decodeArgs(args []string, syn *refSyntax) []string {
	if len(args) == 0 {
		return args
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = decodeValue(a, syn, false)
	}
	return out
}

// isSecretLiteral reports whether a literal value matches a secret pattern
// (secret.LooksLikeLiteralSecret: both FR-028 checks on the whole value and on each
// whitespace-separated token), so a secret hidden after a scheme word (a header value
// "Bearer sk-ant-...", "token ghp_...", or a long base64 blob after "Basic ") is
// still caught and never written verbatim to a target file (FR-035, Constitution IV).
func isSecretLiteral(v string) bool {
	return secret.LooksLikeLiteralSecret(v, secret.Options{})
}

// isLiteralSecretValue reports whether v is a non-reference literal, or a template
// with a literal piece, matching a secret pattern (used to warn on verbatim-carried
// owned values, FR-033a).
func isLiteralSecretValue(v string) bool {
	return literalPartsSecret(v, isSecretLiteral)
}

// isSecretArg reports whether one args element looks like a secret: the whole element
// (and its tokens), or the value part of a `--flag=value` element. A value that reads
// as a path, a package name or a URL gets the prefix check only (secret.Options
// AllowPaths): server commands routinely pass long paths
// (`/Users/x/mcp/server/dist/index.js`) or scoped packages
// (`@modelcontextprotocol/server-filesystem`), which the length check would flag.
// A `--flag=value` or `-f=value` element is judged as its flag name and its value
// separately, so the path allowance applies to the value
// (`--dir=/Users/x/very/long/path`); both still get the prefix check.
func isSecretArg(a string) bool {
	o := secret.Options{AllowPaths: true}
	if name, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(name, "-") && !strings.ContainsAny(name, " \t") {
		return secret.LooksLikeLiteralSecret(name, o) || secret.LooksLikeLiteralSecret(v, o)
	}
	if secret.LooksLikeLiteralSecret(a, o) {
		return true
	}
	if _, v, ok := strings.Cut(a, "="); ok && secret.LooksLikeLiteralSecret(v, o) {
		return true
	}
	return false
}

// encodeArgs applies the FR-028 checks to every args element and drops each hit,
// reported as literal-secret under its source position (FR-035). syn is the
// target's reference syntax for args (nil: taken literally); an element with an
// embedded reference the target cannot expand is dropped as unrepresentable.
func encodeArgs(server string, args []string, syn *refSyntax) ([]string, []Drop) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(args))
	var drops []Drop
	for i, a := range args {
		v, ok, d := encodeValue(server, fmt.Sprintf("args[%d]", i), a, syn, false, isSecretArg)
		drops = append(drops, d...)
		if ok {
			out = append(out, v)
		}
	}
	return out, drops
}

// encodeURLValue renders a neutral url for a target whose url reference syntax is
// syn (nil: taken literally). The url first goes through scrubURL's secret checks,
// a template url included (see urlShape: its references are stood in for while the
// url is checked, so only its literal text is judged); then the scrubbed value is
// rendered for the target.
func encodeURLValue(server, raw string, syn *refSyntax) (string, []Drop) {
	u, drops := encodeURL(server, raw)
	if u == "" {
		return "", drops
	}
	v, ok, d := encodeValue(server, "url", u, syn, false, nil)
	if !ok {
		return "", append(drops, d...)
	}
	return v, append(drops, d...)
}

// redactMark replaces a secret-looking url part in displayed output.
const redactMark = "***"

// encodeURL checks a neutral server URL (a literal or a template) for literal secrets
// (FR-035) and returns it with the offending parts removed (see scrubURL); a template
// stays a template with its references unchanged.
func encodeURL(server, raw string) (string, []Drop) {
	if !isTemplate(raw) {
		return scrubURL(server, raw, false, nil)
	}
	sh := newURLShape(parseTemplate(raw))
	out, drops := scrubURL(server, sh.text, false, sh.pieces)
	switch {
	case out == "":
		return "", drops
	case out == sh.text:
		return raw, drops // unchanged: keep the template byte-for-byte
	}
	parts, ok := sh.back(out)
	if !ok {
		return "", append(drops, Drop{Server: server, Field: "url", Reason: ReasonUnrepresentable})
	}
	return formatTemplate(parts), drops
}

// RedactURL returns a neutral url for display (mcp list), with every part a
// conversion would drop as a literal secret replaced by "***" and references shown
// as ${NAME}.
func RedactURL(v string) string {
	if !isTemplate(v) {
		out, _ := scrubURL("", v, true, nil)
		return out
	}
	sh := newURLShape(parseTemplate(v))
	out, _ := scrubURL("", sh.text, true, sh.pieces)
	parts, ok := sh.back(out)
	if !ok {
		return redactMark
	}
	var b strings.Builder
	for _, p := range parts {
		if p.isLit() {
			b.WriteString(p.lit)
			continue
		}
		b.WriteString(displayPart(p))
	}
	return b.String()
}

// urlShape is a template url with each reference (or opaque substitution) replaced by
// a placeholder, so url parsing and re-encoding keep it intact: letters only, framed by
// a tag that starts and ends with "z" and is absent from the url's literal text. The
// index between the tags uses only a..y, so a placeholder can never extend into the
// next one, even for adjacent references (${A}${B}). A reference that is the whole
// port (http://host:${PORT}/) gets a numeric placeholder instead, so the url still
// parses. scrubURL checks the shape exactly like a literal url, but judges only the
// literal text of each part (pieces), so a reference neither hides a secret next to it
// nor counts toward one; back maps the scrubbed shape to template parts again.
type urlShape struct {
	text    string
	refs    []tmplPart
	re      *regexp.Regexp
	tag     string
	portPH  string // numeric placeholder of a port reference, or ""
	portRef int
}

func newURLShape(parts []tmplPart) urlShape {
	var lits strings.Builder
	for _, p := range parts {
		lits.WriteString(strings.ToLower(p.lit))
	}
	tag := "zaprofilerefz"
	for strings.Contains(lits.String(), tag) {
		tag = "z" + tag + "z"
	}
	sh := urlShape{tag: tag, re: regexp.MustCompile(tag + `([a-y]{1,6})` + tag), portRef: -1}
	var b strings.Builder
	for i, p := range parts {
		if p.isLit() {
			b.WriteString(p.lit)
			continue
		}
		if sh.portPH == "" && isPortPosition(b.String(), parts[i+1:]) {
			sh.portPH = numericPlaceholder(lits.String())
			sh.portRef = len(sh.refs)
			b.WriteString(sh.portPH)
			sh.refs = append(sh.refs, p)
			continue
		}
		b.WriteString(tag + placeholderIndex(len(sh.refs)) + tag)
		sh.refs = append(sh.refs, p)
	}
	sh.text = b.String()
	return sh
}

// isPortPosition reports whether a reference following the text before it is the
// whole port of the url's authority: the text ends with ':' inside the authority
// (after "://", before the first '/'), and the next part starts the path, query or
// fragment, or ends the url.
func isPortPosition(before string, rest []tmplPart) bool {
	i := strings.Index(before, "://")
	if i < 0 || !strings.HasSuffix(before, ":") || strings.ContainsAny(before[i+3:], "/?#") {
		return false
	}
	if len(rest) == 0 {
		return true
	}
	if !rest[0].isLit() {
		return false
	}
	return rest[0].lit == "" || strings.ContainsRune("/?#", rune(rest[0].lit[0]))
}

// numericPlaceholder returns a five-digit port number absent from the literal text.
func numericPlaceholder(lits string) string {
	for n := 19876; ; n++ {
		ph := strconv.Itoa(n)
		if !strings.Contains(lits, ph) {
			return ph
		}
	}
}

// placeholderIndex writes i in base 25 with the letters a..y (z frames the tag).
func placeholderIndex(i int) string {
	s := ""
	for {
		s = string(rune('a'+i%25)) + s
		i /= 25
		if i == 0 {
			return s
		}
	}
}

func placeholderNumber(s string) int {
	n := 0
	for _, r := range s {
		n = n*25 + int(r-'a')
	}
	return n
}

// pieces returns the literal text of one url part (placeholders removed).
func (sh urlShape) pieces(s string) []string {
	if sh.portPH != "" {
		s = strings.Replace(s, ":"+sh.portPH, ":", 1)
	}
	return sh.re.Split(s, -1)
}

// back turns a (scrubbed) shape into template parts, restoring each reference. ok is
// false when a placeholder cannot be mapped back (it was altered), in which case the
// caller drops the url instead of writing a corrupted one.
func (sh urlShape) back(s string) (parts []tmplPart, ok bool) {
	if sh.portPH != "" {
		s = strings.Replace(s, ":"+sh.portPH, ":"+sh.tag+placeholderIndex(sh.portRef)+sh.tag, 1)
	}
	last := 0
	for _, m := range sh.re.FindAllStringSubmatchIndex(s, -1) {
		parts = appendLit(parts, s[last:m[0]])
		i := placeholderNumber(s[m[2]:m[3]])
		if i < 0 || i >= len(sh.refs) {
			return nil, false
		}
		parts = append(parts, sh.refs[i])
		last = m[1]
	}
	return appendLit(parts, s[last:]), true
}

// DisplayValue renders a neutral value for display: a template with its references
// as ${NAME} (${NAME:-default}), anything else unchanged.
func DisplayValue(v string) string {
	if !isTemplate(v) {
		return v
	}
	var b strings.Builder
	for _, p := range parseTemplate(v) {
		if p.isLit() {
			b.WriteString(p.lit)
			continue
		}
		b.WriteString(displayPart(p))
	}
	return b.String()
}

func displayPart(p tmplPart) string {
	switch {
	case p.native != "":
		return "{" + p.native + "}"
	case p.def != nil:
		return "${" + p.ref + ":-" + *p.def + "}"
	default:
		return "${" + p.ref + "}"
	}
}

// scrubURL checks a literal url for secrets (FR-035): the whole value gets the prefix
// check (a URL is routinely 32+ characters, so the length check would flag every
// one); the userinfo, each query parameter and each fragment parameter get both
// checks; each path segment gets the prefix check and the length check with the path
// allowance (pathSegmentSecret). With mask=false an offending userinfo or query
// parameter is removed, a secret fragment removes the fragment, and a secret path
// segment (which cannot be removed without changing the endpoint) drops the whole
// url; every hit is reported by position, so the report never contains the secret.
// With mask=true each offending part is replaced by "***" instead (display). The rest
// of the URL is kept byte-for-byte when nothing is found.
//
// pieces splits one part into the literal text the checks judge; nil means the part
// is all literal. A template url passes its urlShape's pieces, so a reference
// placeholder is never judged, and the literal text on either side of it is judged
// on its own.
func scrubURL(server, raw string, mask bool, pieces func(string) []string) (string, []Drop) {
	if raw == "" {
		return "", nil
	}
	if pieces == nil {
		pieces = func(s string) []string { return []string{s} }
	}
	hit := func(s string, check func(string) bool) bool {
		for _, p := range pieces(s) {
			if p != "" && check(p) {
				return true
			}
		}
		return false
	}
	drop := func(field string) Drop { return Drop{Server: server, Field: field, Reason: ReasonLiteralSecret} }
	whole := func(drops []Drop, field string) (string, []Drop) {
		drops = append(drops, drop(field))
		if mask {
			return redactMark, drops
		}
		return "", drops
	}
	if hit(raw, secret.HasSecretPrefix) {
		return whole(nil, "url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		// Unparseable (a stray '%', a bad port): judge each piece the way a parsed url
		// judges its parts. Host and path pieces get the path-segment rule (prefix, or
		// a long mixed letter/digit token); pieces after the first '?', '#' or '@'
		// (query, fragment, userinfo) get the full literal-secret check. Any hit drops
		// (or masks) the whole url, since its parts cannot be removed one by one.
		if unparsedURLSecret(raw, hit) {
			return whole(nil, "url")
		}
		return raw, nil
	}
	paramSecret := func(k, v string) (name string, keySecret, valSecret bool) {
		uk, uv := queryUnescape(k), queryUnescape(v)
		return uk, hit(uk, isSecretLiteral), hit(uv, isSecretLiteral)
	}
	var drops []Drop
	changed := false
	if u.User != nil {
		pw, _ := u.User.Password()
		if hit(u.User.Username(), isSecretLiteral) || hit(pw, isSecretLiteral) {
			u.User = nil
			if mask {
				u.User = url.User(redactMark)
			}
			changed = true
			drops = append(drops, drop("url.userinfo"))
		}
	}
	if strings.Trim(u.Path, "/") != "" {
		segs := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		masked := false
		for i, seg := range segs {
			if hit(pathUnescape(seg), pathSegmentSecret) {
				if !mask {
					return whole(drops, fmt.Sprintf("url.path[%d]", i))
				}
				segs[i] = redactMark
				masked = true
				drops = append(drops, drop(fmt.Sprintf("url.path[%d]", i)))
			}
		}
		if masked {
			u.Path = "/" + strings.Join(segs, "/")
			u.RawPath = ""
			changed = true
		}
	}
	if u.RawQuery != "" {
		parts := strings.Split(u.RawQuery, "&")
		kept := make([]string, 0, len(parts))
		for i, p := range parts {
			k, v, _ := strings.Cut(p, "=")
			uk, keySecret, valSecret := paramSecret(k, v)
			if !keySecret && !valSecret {
				kept = append(kept, p)
				continue
			}
			field := "url.query[" + uk + "]"
			if keySecret || uk == "" {
				field = fmt.Sprintf("url.query[#%d]", i)
			}
			drops = append(drops, drop(field))
			changed = true
			switch {
			case mask && keySecret:
				kept = append(kept, redactMark)
			case mask:
				kept = append(kept, k+"="+redactMark)
			}
		}
		u.RawQuery = strings.Join(kept, "&")
		u.ForceQuery = false
	}
	if frag := u.EscapedFragment(); frag != "" {
		for _, p := range strings.Split(frag, "&") {
			k, v, _ := strings.Cut(p, "=")
			if _, ks, vs := paramSecret(k, v); ks || vs {
				drops = append(drops, drop("url.fragment"))
				changed = true
				u.Fragment, u.RawFragment = "", ""
				if mask {
					u.Fragment = redactMark
				}
				break
			}
		}
	}
	if !changed {
		return raw, nil
	}
	if mask {
		// url.String escapes the marker in userinfo and path; show it plainly.
		return strings.ReplaceAll(u.String(), "%2A%2A%2A", redactMark), drops
	}
	return u.String(), drops
}

// queryUnescape unescapes a query or fragment parameter name or value (kept as is
// when it is not valid escaping).
func queryUnescape(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}

// pathUnescape unescapes one url path segment (kept as is when invalid).
func pathUnescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// pathSegmentSecret applies the FR-028 checks to one unescaped url path segment: the
// prefix check always; the length check (path allowance) only for a segment that
// mixes letters and digits, the shape of an opaque token, so a long readable slug
// ("some-rather-long-readable-endpoint-name") is kept.
// unparsedURLSecret splits an unparseable url after its scheme at / ? # & = @ : and
// checks each piece; hit applies the caller's piece filter (reference placeholders).
func unparsedURLSecret(raw string, hit func(string, func(string) bool) bool) bool {
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	tail := false // past the first '?', '#' or '@'
	start := 0
	check := func(piece string) bool {
		if piece == "" {
			return false
		}
		if tail {
			return hit(piece, isSecretLiteral)
		}
		return hit(piece, pathSegmentSecret)
	}
	for i, r := range rest {
		if !strings.ContainsRune("/?#&=@:", r) {
			continue
		}
		if check(rest[start:i]) {
			return true
		}
		if strings.ContainsRune("?#@", r) {
			tail = true
		}
		start = i + 1
	}
	return check(rest[start:])
}

func pathSegmentSecret(seg string) bool {
	if secret.HasSecretPrefix(seg) {
		return true
	}
	if secret.CheckLength(seg) == nil {
		return false
	}
	return strings.ContainsAny(seg, "0123456789") &&
		strings.IndexFunc(seg, func(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }) >= 0
}

// containsSecretLiteral reports whether any string inside a decoded native value (a
// string, or nested in arrays and objects) is a literal matching a secret pattern.
func containsSecretLiteral(v any) bool {
	switch x := v.(type) {
	case string:
		return isLiteralSecretValue(x)
	case []any:
		for _, e := range x {
			if containsSecretLiteral(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if containsSecretLiteral(e) {
				return true
			}
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
