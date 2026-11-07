package mcp

// Claude Code MCP: the mcpServers object in .claude.json. stdio servers carry
// command/args/env; remote servers carry type (sse|http)/url/headers. Secret
// references use Claude Code's ${NAME} env-expansion syntax (and ${NAME:-default}),
// which it applies inside command, args, env, url and headers values; a reference is
// never a literal secret, satisfying Constitution IV.

func encodeClaudeServer(s Server) (map[string]any, []Drop) {
	native := map[string]any{}
	s, c, drops := prepareServer(FlavorClaude, s)
	if s.IsRemote() {
		if s.Transport == TransportSSE {
			native["type"] = "sse"
		} else {
			native["type"] = "http"
		}
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
	}
	drops = append(drops, encodeExtra(FlavorClaude, s, native)...)
	return native, drops
}

func decodeClaudeServer(name string, n map[string]any) Server {
	s := Server{Name: name}
	r := flavorCaps[FlavorClaude].refs
	typ := asString(n["type"])
	url := asString(n["url"])
	if url != "" || typ == "sse" || typ == "http" {
		s.URL = decodeValue(url, r.url, false)
		if typ == "sse" {
			s.Transport = TransportSSE
		} else {
			s.Transport = TransportHTTP
		}
		s.Headers = decodeInlineMap(asStringMap(n["headers"]), r.headers)
		s.Extra = collectExtra(FlavorClaude, n, "type", "url", "headers")
	} else {
		s.Transport = TransportStdio
		s.Command = decodeValue(asString(n["command"]), r.command, false)
		s.Args = decodeArgs(asStringSlice(n["args"]), r.args)
		s.Env = decodeInlineMap(asStringMap(n["env"]), r.env)
		s.Extra = collectExtra(FlavorClaude, n, "type", "command", "args", "env")
	}
	return s
}
