package mcp

// repCaps is one flavor's representability matrix for the optional neutral fields.
// It drives unrepresentableDrops so each flavor states, as data, which fields it can
// express; conversion reports the rest (FR-035, SC-005). The always-representable
// fields (command, args, env, url, headers) are handled directly by each encoder.
type repCaps struct {
	workingDir  bool // stdio working directory / cwd
	enabled     bool // explicit enabled/disabled flag
	timeout     bool // startup/connect timeout
	auth        bool // bearer-token env reference
	description bool
	toolLists   bool // tool_allow / tool_deny
	sse         bool // can distinguish or at least carry an sse remote server
}

var (
	// Claude: stdio command/args/env and remote sse|http url/headers, env refs via
	// ${NAME}. No working dir, enabled, timeout, bearer auth, description or tools.
	claudeCaps = repCaps{sse: true}

	// pi: stdio command/args/env/cwd and http url/headers, timeout (seconds), refs
	// via ${NAME}. No enabled, bearer auth, description, tools; rejects sse.
	piCaps = repCaps{workingDir: true, timeout: true}

	// OpenCode: local command[]/environment/cwd/enabled/timeout and remote
	// url/headers/enabled, refs via {env:NAME}. A remote server carries any url, so
	// an sse server maps cleanly to "remote". No bearer auth, description, tools.
	opencodeCaps = repCaps{workingDir: true, enabled: true, timeout: true, sse: true}

	// Codex: stdio command/args/env (+env_vars refs) and streamable-http url with
	// bearer_token_env_var and env_http_headers. No working dir, enabled, timeout,
	// description, tools; rejects sse.
	codexCaps = repCaps{auth: true}
)

// unrepresentableDrops reports the optional fields a server carries that the flavor
// (per c) cannot express. working_dir is only meaningful for stdio servers.
func unrepresentableDrops(s Server, c repCaps) []Drop {
	var d []Drop
	add := func(field string) { d = append(d, Drop{Server: s.Name, Field: field, Reason: ReasonUnrepresentable}) }
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
