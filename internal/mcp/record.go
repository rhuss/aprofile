// Package mcp holds the harness-neutral MCP server record and the per-harness
// parse/emit/convert logic used by `copy` and `mcp copy` (FR-034, FR-035). It
// contains no `if harness == "..."` branching: the only per-harness knowledge is the
// flavor dispatch in flavor.go, driven by the flavor string a harness definition
// carries (Principle I).
//
// Secrets never become literals in a generated artifact (Constitution IV): a value
// held as an env:/file: reference is emitted in the target's native reference syntax,
// and a literal value that matches a secret pattern is dropped and reported rather
// than written.
package mcp

// Transport is the neutral transport kind. Only `stdio` is local; `sse` and `http`
// are remote (distinguished so Claude can preserve its sse/http type; harnesses that
// model only one remote kind coerce and report).
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportSSE   Transport = "sse"
	TransportHTTP  Transport = "http"
)

// Server is the harness-neutral MCP server record (the data-model's MCPServerRecord,
// FR-034). Env and Headers values are each either a secret reference (`env:NAME` or
// `file:/abs/path`, recognized with internal/secret) or a literal. Auth is a bearer
// token reference. Extra carries per-harness fields no neutral field models, so a
// same-flavor round-trip can preserve them.
type Server struct {
	Name        string            `json:"name"`
	Transport   Transport         `json:"transport"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Auth        string            `json:"auth,omitempty"` // bearer token reference (env:/file:)
	Enabled     *bool             `json:"enabled,omitempty"`
	Timeout     int               `json:"timeout,omitempty"` // seconds; 0 = unspecified
	ToolAllow   []string          `json:"tool_allow,omitempty"`
	ToolDeny    []string          `json:"tool_deny,omitempty"`
	Description string            `json:"description,omitempty"`
	Extra       map[string]any    `json:"extra,omitempty"`
}

// IsRemote reports whether the server is a remote (URL-based) server.
func (s *Server) IsRemote() bool {
	return s.URL != "" || (s.Transport != TransportStdio && s.Transport != "")
}

// Drop records one field or value an emit could not carry into a flavor (FR-035).
// Reason is one of the Reason* constants.
type Drop struct {
	Server string `json:"server"`
	Field  string `json:"field"`  // e.g. "env[FOO]", "working_dir", "headers[X-Key]", "enabled"
	Reason string `json:"reason"` // ReasonUnrepresentable | ReasonLiteralSecret
}

const (
	// ReasonUnrepresentable marks a field the target flavor cannot express.
	ReasonUnrepresentable = "unrepresentable"
	// ReasonLiteralSecret marks a literal value matching a secret pattern that MUST
	// NOT be written to the target (Constitution IV, FR-035).
	ReasonLiteralSecret = "literal-secret"
)

// SkippedServer records a whole server that was not written to the target.
type SkippedServer struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// SecretWarning names an owned MCP value that matches a secret pattern, reported by a
// same-harness copy which carries it verbatim (FR-033a).
type SecretWarning struct {
	Server string `json:"server"`
	Field  string `json:"field"`
}

// boolPtr is a small helper for building Server.Enabled in tests and emitters.
func boolPtr(b bool) *bool { return &b }
