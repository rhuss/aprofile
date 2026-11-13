package secret

import (
	"fmt"
	"strings"
)

// ResolveShell renders POSIX-sh lines that assign the reference's resolved value
// to the environment variable targetVar and export it, failing closed when the
// reference cannot be resolved: an unset/empty variable, or a missing, unreadable
// or empty file, prints a message naming the profile and reference and exits
// non-zero before the harness starts (FR-028a).
//
// quote must shell-quote an arbitrary literal safely (the caller owns the single
// correct quoting implementation). indent is prepended to every line.
func (r Reference) ResolveShell(targetVar, profileID string, quote func(string) string, indent string) string {
	return r.resolveShell(targetVar, profileID, quote, indent, true)
}

// CheckShell is ResolveShell without the export: it resolves the reference into the
// private shell variable targetVar, failing closed with the same message, so a
// wrapper can check a reference the profile folder carries without exporting its
// value (feature 004, FR-015). The caller unsets targetVar afterwards.
func (r Reference) CheckShell(targetVar, profileID string, quote func(string) string, indent string) string {
	return r.resolveShell(targetVar, profileID, quote, indent, false)
}

func (r Reference) resolveShell(targetVar, profileID string, quote func(string) string, indent string, export bool) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(indent + s + "\n") }

	switch r.Kind {
	case KindEnv:
		msg := quote(fmt.Sprintf("aprofile: profile %s: reference %s is unset or empty", profileID, r.Raw))
		line(fmt.Sprintf("if [ -z \"${%s:-}\" ]; then", r.Value))
		line(fmt.Sprintf("  printf '%%s\\n' %s >&2", msg))
		line("  exit 1")
		line("fi")
		line(fmt.Sprintf("%s=\"${%s}\"", targetVar, r.Value))
		if export {
			line(fmt.Sprintf("export %s", targetVar))
		}
	case KindFile:
		path := quote(r.Value)
		msg := quote(fmt.Sprintf("aprofile: profile %s: reference %s is missing, unreadable or empty", profileID, r.Raw))
		line(fmt.Sprintf("%s=''", targetVar))
		line(fmt.Sprintf("if [ -r %s ]; then", path))
		line(fmt.Sprintf("  IFS= read -r %s < %s", targetVar, path))
		line("fi")
		line(fmt.Sprintf("if [ -z \"$%s\" ]; then", targetVar))
		line(fmt.Sprintf("  printf '%%s\\n' %s >&2", msg))
		line("  exit 1")
		line("fi")
		if export {
			line(fmt.Sprintf("export %s", targetVar))
		}
	}
	return b.String()
}

// ShellQuote renders s as a single POSIX-sh single-quoted literal, so no value can
// inject shell code (FR-028b). It lives here so packages that build shell text
// (wrapper, launch) share one implementation without importing each other.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
