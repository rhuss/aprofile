package mcp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cc-deck/aprofile/internal/secret"
)

// repCaps is one flavor's representability matrix for the optional neutral fields
// and its in-string reference syntax per field. It drives unrepresentableDrops and
// the reference translation (template.go) so each flavor states, as data, what it can
// express; conversion reports the rest (FR-035, SC-005).
type repCaps struct {
	workingDir  bool // stdio working directory / cwd
	enabled     bool // explicit enabled/disabled flag
	timeout     bool // per-request (tool call) timeout, neutral seconds
	auth        bool // bearer-token env reference field
	description bool
	toolLists   bool // tool_allow / tool_deny
	sse         bool // can distinguish or at least carry an sse remote server
	refs        fieldRefs
}

// fieldRefs names, per field, the syntax a flavor expands env references with inside
// a string value; nil means the field is taken literally. command and cwd hold
// literals in every source but a flavor that substitutes there must still never
// receive a literal it would read as a reference or substitution. keys is the syntax
// a flavor substitutes in object keys (env and header names, the server name); a
// key is only ever a literal, so one that would not read back as itself is dropped
// (env/header name) or its server skipped (server name), see literalKey.
type fieldRefs struct {
	env, headers, args, url, command, cwd, keys *refSyntax
}

var (
	// Claude Code expands ${NAME} and ${NAME:-default} in command, args, env, url and
	// headers of an MCP server.
	claudeSyntax = &refSyntax{defaults: true}
	// pi (1.3.0, core/resolve-config-value.js) expands ${NAME} and $NAME in env and
	// header values; $$ is a literal $, $! a literal !, a leading ! runs a command.
	// No defaults; args and url are taken literally.
	piSyntax = &refSyntax{bare: true, dollarEscape: true, commandBang: true}
	// OpenCode substitutes {env:NAME} and {file:path} in the whole config text
	// (opencode 2.1.4, ConfigVariable.substitute), so every field expands them, object
	// keys included; any other `{word:...}` is treated as a substitution too. No
	// defaults, no escape.
	opencodeSyntax = &refSyntax{braceEnv: true}
)

// flavorCaps is every flavor's capability record.
var flavorCaps = map[Flavor]repCaps{
	// Claude: stdio command/args/env and remote sse|http url/headers. No working dir,
	// enabled flag (a disabled server is skipped), timeout, bearer field (a bearer
	// token becomes an Authorization header), description or tool lists.
	FlavorClaude: {sse: true,
		refs: fieldRefs{env: claudeSyntax, headers: claudeSyntax, args: claudeSyntax, url: claudeSyntax, command: claudeSyntax}},

	// pi (1.3.0 docs/mcp.md and its config validator): stdio command/args/env/cwd and
	// http url/headers, enabled, description, timeout (per request, seconds). No bearer
	// field, tool lists; rejects sse.
	FlavorPi: {workingDir: true, enabled: true, timeout: true, description: true,
		refs: fieldRefs{env: piSyntax, headers: piSyntax}},

	// OpenCode (2.1.4 schema): local command[]/environment/cwd and remote url/headers,
	// both with enabled and timeout (milliseconds). A remote server carries any url,
	// so an sse server maps cleanly to "remote". No bearer field, description, tools.
	FlavorOpenCode: {workingDir: true, enabled: true, timeout: true, sse: true,
		refs: fieldRefs{env: opencodeSyntax, headers: opencodeSyntax, args: opencodeSyntax, url: opencodeSyntax,
			command: opencodeSyntax, cwd: opencodeSyntax, keys: opencodeSyntax}},

	// Codex (codex-cli 0.167.0, RawMcpServerConfig): stdio command/args/env/env_vars/
	// cwd and streamable-http url/http_headers/env_http_headers/bearer_token_env_var;
	// both with enabled, tool_timeout_sec, enabled_tools and disabled_tools. Codex
	// expands no reference inside a string. No description; rejects sse.
	FlavorCodex: {workingDir: true, enabled: true, timeout: true, auth: true, toolLists: true},
}

// unrepresentableDrops reports the optional fields a server carries that the flavor
// (per c) cannot express. working_dir is only meaningful for stdio servers.
func unrepresentableDrops(s Server, c repCaps) []Drop {
	var d []Drop
	add := func(field string) {
		d = append(d, Drop{Server: s.Name, Field: field, Reason: ReasonUnrepresentable})
	}
	if s.WorkingDir != "" && !c.workingDir {
		add("working_dir")
	}
	if s.Enabled != nil && !c.enabled {
		add("enabled")
	}
	if s.Timeout != 0 && !c.timeout {
		add("timeout")
	}
	if s.Auth != "" && !c.auth {
		add("auth")
	}
	if s.Description != "" && !c.description {
		add("description")
	}
	if (len(s.ToolAllow) > 0 || len(s.ToolDeny) > 0) && !c.toolLists {
		add("tool_allow/tool_deny")
	}
	if s.Transport == TransportSSE && !c.sse {
		add("transport:sse")
	}
	return d
}

// prepareServer is every encoder's first step: it folds a bearer token into the form
// the flavor holds (foldAuth) and reports the optional fields the flavor cannot
// express.
func prepareServer(flavor Flavor, s Server) (Server, repCaps, []Drop) {
	c := flavorCaps[flavor]
	s, drops := foldAuth(s, c)
	return s, c, append(drops, unrepresentableDrops(s, c)...)
}

// skipDisabled reports why a server must not be written to flavor: a server disabled
// in the source is never written as an enabled one, so a flavor that cannot express
// enabled:false skips it.
func skipDisabled(flavor Flavor, s Server) (string, bool) {
	if s.Enabled != nil && !*s.Enabled && !flavorCaps[flavor].enabled {
		return "disabled in source; the target cannot express a disabled server", true
	}
	return "", false
}

// encodeCommand renders a stdio server's command in the target's syntax for that
// field. ok=false (reported under "command") means the target would read the
// literal as something else; the server is then skipped (see unwritable).
func encodeCommand(s Server, c repCaps) (string, bool, []Drop) {
	return encodeValue(s.Name, "command", s.Command, c.refs.command, false, nil)
}

// encodeWorkingDir renders a stdio server's working directory in the target's syntax
// for that field; "" with a drop when it cannot be written.
func encodeWorkingDir(s Server, c repCaps) (string, []Drop) {
	if s.WorkingDir == "" {
		return "", nil
	}
	v, ok, d := encodeValue(s.Name, "working_dir", s.WorkingDir, c.refs.cwd, false, nil)
	if !ok {
		return "", d
	}
	return v, d
}

// literalKey reports whether an object key reads back as exactly itself in a target
// that substitutes keys with syn (nil: keys are taken literally).
func literalKey(k string, syn *refSyntax) bool {
	if syn == nil {
		return true
	}
	parts := syn.parse(k)
	return allLit(parts) && joinLits(parts) == k
}

// unwritableName reports why a server cannot be written under its name: a target that
// substitutes object keys would read a name holding a substitution form as something
// else (OpenCode would read a file into the key).
func unwritableName(flavor Flavor, s Server) (string, bool) {
	if !literalKey(s.Name, flavorCaps[flavor].refs.keys) {
		return "server name holds a form the target would substitute", true
	}
	return "", false
}

// unwritable reports why an encoded server must not be written at all: a remote
// server whose url was dropped (Codex would read the table back as an empty stdio
// server, pi as a server without an endpoint), a stdio server whose command was
// dropped or is empty (the harness would get `command: ""`, FR-035), or a stdio
// server that lost a positional args element (see
// argPositionLost). drops are the encoder's drops for s. The field drops stay in the
// report; the server is listed as skipped.
func unwritable(s Server, native map[string]any, drops []Drop) (string, bool) {
	if s.IsRemote() {
		if u, _ := native["url"].(string); u == "" {
			return "url not representable in target", true
		}
		return "", false
	}
	cmd, ok := native["command"]
	if !ok {
		return "command not representable in target", true
	}
	if commandEmpty(cmd) {
		return "command empty", true
	}
	if i, lost := argPositionLost(s, drops); lost {
		return fmt.Sprintf("args[%d] dropped; writing the rest would shift the arguments after it", i), true
	}
	return "", false
}

// argPositionLost reports the first args element of s that drops removed and that is
// not a whole `--flag=value` option. Args are positional: removing "--prefix"'s value
// from ["--prefix", "<v>", "x"] turns "x" into the prefix, and removing any other
// element moves every later one into another role, so such a server is skipped
// rather than written with changed meaning. A `--flag=value` (or `-f=value`) element
// carries its own value, so removing it removes exactly one option and shifts the
// meaning of nothing else; it may be dropped on its own. A lost default
// ("args[i].default") keeps the element and does not count.
func argPositionLost(s Server, drops []Drop) (int, bool) {
	for i, a := range s.Args {
		field := fmt.Sprintf("args[%d]", i)
		for _, d := range drops {
			if d.Server == s.Name && d.Field == field && !flagValueArg(a) {
				return i, true
			}
		}
	}
	return 0, false
}

// flagValueRE matches the literal start of a `--flag=value` / `-f=value` element.
var flagValueRE = regexp.MustCompile(`^--?[A-Za-z0-9][A-Za-z0-9_.-]*=`)

// flagValueArg reports whether a neutral args element is a whole `--flag=value`
// option: its leading literal text names the flag and holds the `=`.
func flagValueArg(a string) bool {
	parts, ok := neutralParts(a, false)
	return ok && len(parts) > 0 && parts[0].isLit() && flagValueRE.MatchString(parts[0].lit)
}

// commandEmpty reports whether an encoded native command is empty: a blank string, or
// a command array (OpenCode) whose executable is blank.
func commandEmpty(cmd any) bool {
	switch c := cmd.(type) {
	case string:
		return strings.TrimSpace(c) == ""
	case []string:
		return len(c) == 0 || strings.TrimSpace(c[0]) == ""
	}
	return false
}

// optionWithheld stands in for a dropped option's flag name that itself looks like a
// secret, so the report never prints it.
const optionWithheld = "(name withheld)"

// markDroppedOptions sets Drop.Option on every drop of a written server that removed
// a whole `--flag=value` args element, so the report can say the server was written
// without that option (FR-035). Only the flag name is recorded, never the value.
func markDroppedOptions(s Server, drops []Drop) {
	for i, a := range s.Args {
		if !flagValueArg(a) {
			continue
		}
		field := fmt.Sprintf("args[%d]", i)
		for j := range drops {
			if drops[j].Server == s.Name && drops[j].Field == field {
				drops[j].Option = flagName(a)
			}
		}
	}
}

// flagName returns the flag of a `--flag=value` args element, or optionWithheld when
// the flag name itself looks like a secret.
func flagName(a string) string {
	parts, _ := neutralParts(a, false)
	name, _, _ := strings.Cut(parts[0].lit, "=")
	if secret.LooksLikeLiteralSecret(name, secret.Options{AllowPaths: true}) {
		return optionWithheld
	}
	return name
}
