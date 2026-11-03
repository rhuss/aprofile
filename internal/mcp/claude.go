package mcp

// Claude Code MCP: the mcpServers object in .claude.json. stdio servers carry
// command/args/env; remote servers carry type (sse|http)/url/headers. Secret
// references use Claude Code's ${NAME} env-expansion syntax (research.md item 1 is
// silent on the exact token; ${NAME} is Claude Code's documented MCP env expansion
// and, crucially, never embeds a literal secret, satisfying Constitution IV).

func encodeClaudeServer(s Server) (map[string]any, []Drop) {
	native := map[string]any{}
	drops := unrepresentableDrops(s, claudeCaps)
	if s.IsRemote() {
		if s.Transport == TransportSSE {
			native["type"] = "sse"
		} else {
			native["type"] = "http"
		}
		native["url"] = s.URL
		headers, hd := encodeInlineMap(s.Name, "headers", s.Headers, encodeDollarRef)
		drops = append(drops, hd...)
		if len(headers) > 0 {
			native["headers"] = headers
		}
	} else {
		native["command"] = s.Command
		if len(s.Args) > 0 {
			native["args"] = s.Args
		}
		env, ed := encodeInlineMap(s.Name, "env", s.Env, encodeDollarRef)
		drops = append(drops, ed...)
		if len(env) > 0 {
			native["env"] = env
		}
	}
	return native, drops
}

func decodeClaudeServer(name string, n map[string]any) Server {
	s := Server{Name: name}
	typ := asString(n["type"])
	url := asString(n["url"])
	if url != "" || typ == "sse" || typ == "http" {
		s.URL = url
		if typ == "sse" {
			s.Transport = TransportSSE
		} else {
			s.Transport = TransportHTTP
		}
		s.Headers = decodeInlineMap(asStringMap(n["headers"]), decodeDollarRef)
	} else {
		s.Transport = TransportStdio
		s.Command = asString(n["command"])
		s.Args = asStringSlice(n["args"])
		s.Env = decodeInlineMap(asStringMap(n["env"]), decodeDollarRef)
	}
	return s
}
