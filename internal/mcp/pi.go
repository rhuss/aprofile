package mcp

// pi MCP: the mcpServers object in agent/mcp.json (Claude-compatible). stdio servers
// carry type/command/args/env/cwd; http servers carry type/url/headers. Both carry
// enabled, description and timeout (per request, seconds). env and header values
// expand ${NAME} and $NAME (research.md item 3; pi 1.3.0 docs/mcp.md). pi rejects
// sse, so an sse source is reported unrepresentable (caps) and emitted as http. An
// env or header value starting with `!` is a shell command pi runs; it decodes to the
// neutral command form (template.go cmdPrefix) and a literal `!` is written `$!`.

func encodePiServer(s Server) (map[string]any, []Drop) {
	native := map[string]any{}
	s, c, drops := prepareServer(FlavorPi, s)
	if s.IsRemote() {
		native["type"] = "http"
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
		native["type"] = "stdio"
		cmd, ok, cd := encodeCommand(s, c)
		drops = append(drops, cd...)
		if ok {
			native["command"] = cmd
		}
		args, ad := encodeArgs(s.Name, s.Args, c.refs.args)
		drops = append(drops, ad...)
		if len(args) > 0 {
			native["args"] = args
		}
		env, ed := encodeInlineMap(s.Name, "env", s.Env, c.refs.env, c.refs.keys)
		drops = append(drops, ed...)
		if len(env) > 0 {
			native["env"] = env
		}
		cwd, wd := encodeWorkingDir(s, c)
		drops = append(drops, wd...)
		if cwd != "" {
			native["cwd"] = cwd
		}
	}
	if s.Timeout != 0 {
		native["timeout"] = s.Timeout
	}
	if s.Enabled != nil {
		native["enabled"] = *s.Enabled
	}
	if s.Description != "" {
		native["description"] = s.Description
	}
	drops = append(drops, encodeExtra(FlavorPi, s, native)...)
	return native, drops
}

func decodePiServer(name string, n map[string]any) Server {
	s := Server{Name: name}
	r := flavorCaps[FlavorPi].refs
	typ := asString(n["type"])
	url := asString(n["url"])
	if url != "" || typ == "http" || typ == "streamable-http" || typ == "sse" {
		s.Transport = TransportHTTP
		s.URL = decodeValue(url, r.url, false)
		s.Headers = decodeInlineMap(asStringMap(n["headers"]), r.headers)
		s.Extra = collectExtra(FlavorPi, n, "type", "url", "headers", "timeout", "enabled", "description")
	} else {
		s.Transport = TransportStdio
		s.Command = decodeValue(asString(n["command"]), r.command, false)
		s.Args = decodeArgs(asStringSlice(n["args"]), r.args)
		s.Env = decodeInlineMap(asStringMap(n["env"]), r.env)
		s.WorkingDir = decodeValue(asString(n["cwd"]), r.cwd, false)
		s.Extra = collectExtra(FlavorPi, n, "type", "command", "args", "env", "cwd", "timeout", "enabled", "description")
	}
	if t := asInt(n["timeout"]); t != 0 {
		s.Timeout = t
	}
	if b, ok := n["enabled"].(bool); ok {
		s.Enabled = boolPtr(b)
	}
	s.Description = asString(n["description"])
	return s
}
