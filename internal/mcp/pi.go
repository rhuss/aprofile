package mcp

// pi MCP: the mcpServers object in agent/mcp.json (Claude-compatible). stdio servers
// carry type/command/args/env/cwd; http servers carry type/url/headers. timeout is in
// seconds. Secret references use ${NAME} (research.md item 3). pi rejects sse, so an
// sse source is reported unrepresentable (caps) and emitted as http.

func encodePiServer(s Server) (map[string]any, []Drop) {
	native := map[string]any{}
	drops := unrepresentableDrops(s, piCaps)
	if s.IsRemote() {
		native["type"] = "http"
		native["url"] = s.URL
		headers, hd := encodeInlineMap(s.Name, "headers", s.Headers, encodeDollarRef)
		drops = append(drops, hd...)
		if len(headers) > 0 {
			native["headers"] = headers
		}
	} else {
		native["type"] = "stdio"
		native["command"] = s.Command
		if len(s.Args) > 0 {
			native["args"] = s.Args
		}
		env, ed := encodeInlineMap(s.Name, "env", s.Env, encodeDollarRef)
		drops = append(drops, ed...)
		if len(env) > 0 {
			native["env"] = env
		}
		if s.WorkingDir != "" {
			native["cwd"] = s.WorkingDir
		}
	}
	if s.Timeout != 0 {
		native["timeout"] = s.Timeout
	}
	return native, drops
}

func decodePiServer(name string, n map[string]any) Server {
	s := Server{Name: name}
	typ := asString(n["type"])
	url := asString(n["url"])
	if url != "" || typ == "http" || typ == "streamable-http" || typ == "sse" {
		s.Transport = TransportHTTP
		s.URL = url
		s.Headers = decodeInlineMap(asStringMap(n["headers"]), decodeDollarRef)
	} else {
		s.Transport = TransportStdio
		s.Command = asString(n["command"])
		s.Args = asStringSlice(n["args"])
		s.Env = decodeInlineMap(asStringMap(n["env"]), decodeDollarRef)
		s.WorkingDir = asString(n["cwd"])
	}
	if t := asInt(n["timeout"]); t != 0 {
		s.Timeout = t
	}
	return s
}
