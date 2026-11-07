package mcp

// OpenCode MCP: the mcp object in opencode.json. A local server has
// type:"local"/command[]/environment/cwd; a remote server has type:"remote"/url/
// headers; both carry enabled and timeout (milliseconds, opencode 2.1.4 schema).
// command is an array combining the executable and its arguments. OpenCode
// substitutes {env:NAME} and {file:path} anywhere in the config text (research.md
// item 2), so a reference embedded in any value is expressible, and a literal holding
// a `{word:...}` form (which OpenCode would substitute) is never written.

func encodeOpenCodeServer(s Server) (map[string]any, []Drop) {
	native := map[string]any{}
	s, c, drops := prepareServer(FlavorOpenCode, s)
	if s.IsRemote() {
		native["type"] = "remote"
		u, ud := encodeURLValue(s.Name, s.URL, c.refs.url)
		drops = append(drops, ud...)
		if u != "" {
			native["url"] = u
		}
		headers, hd := encodeInlineMap(s.Name, "headers", s.Headers, c.refs.headers, c.refs.keys)
		drops = append(drops, hd...)
		if len(headers) > 0 {
			native["headers"] = headers
		}
	} else {
		native["type"] = "local"
		args, ad := encodeArgs(s.Name, s.Args, c.refs.args)
		drops = append(drops, ad...)
		cmd, ok, cd := encodeCommand(s, c)
		drops = append(drops, cd...)
		if ok {
			native["command"] = append([]string{cmd}, args...)
		}
		env, ed := encodeInlineMap(s.Name, "env", s.Env, c.refs.env, c.refs.keys)
		drops = append(drops, ed...)
		if len(env) > 0 {
			native["environment"] = env
		}
		cwd, wd := encodeWorkingDir(s, c)
		drops = append(drops, wd...)
		if cwd != "" {
			native["cwd"] = cwd
		}
	}
	if s.Timeout != 0 {
		native["timeout"] = s.Timeout * 1000
	}
	if s.Enabled != nil {
		native["enabled"] = *s.Enabled
	}
	drops = append(drops, encodeExtra(FlavorOpenCode, s, native)...)
	return native, drops
}

func decodeOpenCodeServer(name string, n map[string]any) Server {
	s := Server{Name: name}
	r := flavorCaps[FlavorOpenCode].refs
	known := []string{"type", "enabled"}
	// timeout is milliseconds; a value that is not whole seconds stays an
	// OpenCode-only field (kept for OpenCode, reported elsewhere).
	if ms, ok := wholeInt(n["timeout"]); ok && ms > 0 && ms%1000 == 0 {
		s.Timeout = ms / 1000
		known = append(known, "timeout")
	}
	typ := asString(n["type"])
	if typ == "remote" || asString(n["url"]) != "" {
		s.Transport = TransportHTTP
		s.URL = decodeValue(asString(n["url"]), r.url, false)
		s.Headers = decodeInlineMap(asStringMap(n["headers"]), r.headers)
		s.Extra = collectExtra(FlavorOpenCode, n, append(known, "url", "headers")...)
	} else {
		s.Transport = TransportStdio
		cmd := asStringSlice(n["command"])
		if len(cmd) > 0 {
			s.Command = decodeValue(cmd[0], r.command, false)
		}
		if len(cmd) > 1 {
			s.Args = decodeArgs(cmd[1:], r.args)
		}
		s.Env = decodeInlineMap(asStringMap(n["environment"]), r.env)
		s.WorkingDir = decodeValue(asString(n["cwd"]), r.cwd, false)
		s.Extra = collectExtra(FlavorOpenCode, n, append(known, "command", "environment", "cwd")...)
	}
	if b, ok := n["enabled"].(bool); ok {
		s.Enabled = boolPtr(b)
	}
	return s
}
