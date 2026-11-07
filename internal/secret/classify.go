package secret

import (
	"path/filepath"
	"regexp"
	"strings"
)

// This file holds the one rule for "a value matches a secret pattern" (FR-028
// prefix and length checks, as used by FR-033a and FR-035) and the one rule for "a
// key name looks like it holds a credential". Every package that warns about,
// drops or refuses to seed a secret-looking value calls these, so a value is judged
// the same way in every section of a report.

// Options select how LooksLikeLiteralSecret applies the FR-028 checks.
type Options struct {
	// PrefixOnly applies only the prefix check, for values that are routinely long
	// without being secret (hook env output, literal-marked values, settings files).
	PrefixOnly bool
	// AllowPaths gives a value that reads as a path, a package name or a URL
	// (LooksLikePath) the prefix check on each of its segments instead of the length
	// check, since such values are routinely 32+ characters (FR-035).
	AllowPaths bool
	// SkipReferences treats references as not literal: a well-formed env:/file:
	// reference is never a secret, and shell variable references ($NAME, ${NAME})
	// embedded in a value are removed before the checks. The literal default of
	// ${NAME:-default} is kept and checked.
	SkipReferences bool
}

// shellRefRE matches $NAME, ${NAME} and ${NAME:-default}; group 1 is the default.
var shellRefRE = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*(?::-([^}]*))?\}|\$[A-Za-z_][A-Za-z0-9_]*`)

// LooksLikeLiteralSecret reports whether v matches a secret pattern: the FR-028
// prefix check or length check, applied to the whole value and to each
// whitespace-separated token, so a secret after a scheme word ("Bearer sk-...",
// "Basic <long blob>") is caught. o tunes the checks (see Options).
func LooksLikeLiteralSecret(v string, o Options) bool {
	if o.SkipReferences {
		if _, err := ParseReference(v); err == nil {
			return false
		}
		v = shellRefRE.ReplaceAllString(v, " $1 ")
	}
	if strings.TrimSpace(v) == "" {
		return false
	}
	if o.AllowPaths && LooksLikePath(v) {
		return pathSegmentsHavePrefix(v)
	}
	check := func(tok string) bool {
		return CheckPrefix(tok) != nil || (!o.PrefixOnly && CheckLength(tok) != nil)
	}
	if check(v) {
		return true
	}
	for _, tok := range strings.Fields(v) {
		if check(tok) {
			return true
		}
	}
	return false
}

// IsReferenceOnly reports whether v holds no literal text: a well-formed env:/file:
// reference, or only shell variable references ($NAME, ${NAME}) and whitespace. A
// ${NAME:-default} default counts as literal text.
func IsReferenceOnly(v string) bool {
	if _, err := ParseReference(v); err == nil {
		return true
	}
	return strings.TrimSpace(v) != "" && strings.TrimSpace(shellRefRE.ReplaceAllString(v, " $1 ")) == ""
}

// HasSecretPrefix applies only the FR-028 prefix check, to the whole value and to
// each whitespace-separated token.
func HasSecretPrefix(v string) bool {
	return LooksLikeLiteralSecret(v, Options{PrefixOnly: true})
}

// pathSegmentsHavePrefix applies the prefix check to every segment of a path or URL
// (split at '/', '?', '&' and '=').
func pathSegmentsHavePrefix(v string) bool {
	for _, seg := range strings.FieldsFunc(v, func(r rune) bool { return r == '/' || r == '?' || r == '&' || r == '=' }) {
		if HasSecretPrefix(seg) {
			return true
		}
	}
	return false
}

// LooksLikePath reports whether v reads as a filesystem path, an npm scope, a URL or
// a relative file name with an extension (FR-035): it starts with "/", "./", "../",
// "~/" or "@", contains "://", contains "/" and ends in a 1-5 character
// alphanumeric extension, or is a bare file name ending in such an extension that
// holds a letter ("server-entry.js"). A bare name starting with a known token prefix
// is not a path, so "ghp_....js" keeps every secret check.
func LooksLikePath(v string) bool {
	for _, p := range []string{"/", "./", "../", "~/", "@"} {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	if strings.Contains(v, "://") {
		return true
	}
	ext := filepath.Ext(v)
	if len(ext) < 2 || len(ext) > 6 || len(ext) == len(v) {
		return false
	}
	letter := false
	for _, r := range ext[1:] {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isLetter && (r < '0' || r > '9') {
			return false
		}
		letter = letter || isLetter
	}
	if strings.Contains(v, "/") {
		return true
	}
	// A bare file name: the extension must hold a letter (a numeric suffix such as
	// ".12345" is no file type) and the name must not carry a known token prefix.
	return letter && CheckPrefix(v) == nil
}

// secretKeyHints are lower-case name fragments that mark a key as credential-like.
var secretKeyHints = []string{
	"token", "secret", "password", "passwd", "credential", "apikey", "api_key", "api-key",
	"oauth", "auth", "account", "cookie", "private_key", "privatekey",
}

// LooksLikeSecretKey reports whether a key name suggests it holds a credential (a
// token, secret, password, API key, account, cookie or auth data, or any name ending
// in "key"). False positives only cost a warning or a key left unseeded.
func LooksLikeSecretKey(key string) bool {
	k := strings.ToLower(key)
	if strings.HasSuffix(k, "key") {
		return true
	}
	for _, h := range secretKeyHints {
		if strings.Contains(k, h) {
			return true
		}
	}
	return false
}
