package mcp

// OpenCode MCP: the mcp object in opencode.json. A local server has
// type:"local"/command[]/environment/cwd/enabled/timeout; a remote server has
// type:"remote"/url/headers/enabled. command is an array combining the executable and
// its arguments. Secret references use {env:NAME} (research.md item 2).

func encodeOpenCodeServer(s Server) (map[string]any, []Drop) {
	native := map[string]any{}
	drops := unrepresentableDrops(s, opencodeCaps)
	if s.IsRemote() {
		native["type"] = "remote"
		native["url"] = s.URL
		headers, hd := encodeInlineMap(s.Name, "headers", s.Headers, encodeBraceEnvRef)
		drops = append(drops, hd...)
		if len(headers) > 0 {
			native["headers"] = headers
		}
	} else {
		native["type"] = "local"
		cmd := append([]string{s.Command}, s.Args...)
		native["command"] = cmd
		env, ed := encodeInlineMap(s.Name, "env", s.Env, encodeBraceEnvRef)
		drops = append(drops, ed...)
		if len(env) > 0 {
			native["environment"] = env
		}
		if s.WorkingDir != "" {
			native["cwd"] = s.WorkingDir
		}
		if s.Timeout != 0 {
			native["timeout"] = s.Timeout
		}
	}
	if s.Enabled != nil {
		native["enabled"] = *s.Enabled
	}
	return native, drops
}

func decodeOpenCodeServer(name string, n map[string]any) Server {
	s := Server{Name: name}
	typ := asString(n["type"])
	if typ == "remote" || asString(n["url"]) != "" {
		s.Transport = TransportHTTP
		s.URL = asString(n["url"])
		s.Headers = decodeInlineMap(asStringMap(n["headers"]), decodeBraceEnvRef)
	} else {
		s.Transport = TransportStdio
		cmd := asStringSlice(n["command"])
		if len(cmd) > 0 {
			s.Command = cmd[0]
		}
		if len(cmd) > 1 {
			s.Args = cmd[1:]
		}
		s.Env = decodeInlineMap(asStringMap(n["environment"]), decodeBraceEnvRef)
		s.WorkingDir = asString(n["cwd"])
		if t := asInt(n["timeout"]); t != 0 {
			s.Timeout = t
		}
	}
	if b, ok := n["enabled"].(bool); ok {
		s.Enabled = boolPtr(b)
	}
	return s
}
