package mcp

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/secret"
)

// Codex MCP: the [mcp_servers.<name>] tables in config.toml. Codex expands no
// reference inside a string, so secrets are expressed structurally, never inline
// (research.md item 4): an env reference goes in env_vars (forwarded from the host env
// by name), a literal env goes in env; a whole-value header reference goes in
// env_http_headers (header -> env var name), a literal header in http_headers; a
// bearer token (including an `Authorization: Bearer <ref>` header, see foldAuth) goes
// in bearer_token_env_var. Because env_vars forwards by name, an env reference whose
// server-var name differs from the host-var name cannot be expressed and is reported;
// so is every reference embedded in a larger value and every default.
//
// Both transports also carry enabled, tool_timeout_sec (the neutral per-request
// timeout), enabled_tools and disabled_tools; stdio carries cwd (verified against
// codex-cli 0.167.0 `codex mcp get --json`). startup_timeout_sec and the other
// Codex-only keys stay in Server.Extra.

func encodeCodexServer(s Server) (map[string]any, []Drop) {
	native := map[string]any{}
	s, c, drops := prepareServer(FlavorCodex, s)
	if s.IsRemote() {
		u, ud := encodeURLValue(s.Name, s.URL, nil)
		drops = append(drops, ud...)
		if u != "" {
			native["url"] = u
		}
		litHeaders := map[string]string{}
		envHeaders := map[string]string{}
		for _, k := range sortedKeys(s.Headers) {
			v, field := s.Headers[k], "headers["+k+"]"
			if name, def, ok := singleRef(v); ok {
				envHeaders[k] = name
				if def != nil {
					drops = append(drops, Drop{s.Name, field + ".default", ReasonUnrepresentable})
				}
				continue
			}
			lit, ok, d := encodeValue(s.Name, field, v, nil, true, isSecretLiteral)
			drops = append(drops, d...)
			if ok {
				litHeaders[k] = lit
			}
		}
		if len(litHeaders) > 0 {
			native["http_headers"] = litHeaders
		}
		if len(envHeaders) > 0 {
			native["env_http_headers"] = envHeaders
		}
		if s.Auth != "" {
			if r, err := secret.ParseReference(s.Auth); err == nil && r.Kind == secret.KindEnv {
				native["bearer_token_env_var"] = r.Value
			} else {
				drops = append(drops, Drop{s.Name, "auth", ReasonUnrepresentable})
			}
		}
	} else {
		cmd, ok, cd := encodeCommand(s, c)
		drops = append(drops, cd...)
		if ok {
			native["command"] = cmd
		}
		args, ad := encodeArgs(s.Name, s.Args, nil)
		drops = append(drops, ad...)
		if len(args) > 0 {
			native["args"] = args
		}
		litEnv := map[string]string{}
		var envVars []string
		for _, k := range sortedKeys(s.Env) {
			v, field := s.Env[k], "env["+k+"]"
			// env_vars forwards by name only; a rename, file ref or embedded ref
			// cannot be expressed and encodeValue reports it.
			if name, def, ok := singleRef(v); ok && name == k {
				envVars = append(envVars, k)
				if def != nil {
					drops = append(drops, Drop{s.Name, field + ".default", ReasonUnrepresentable})
				}
				continue
			}
			lit, ok, d := encodeValue(s.Name, field, v, nil, true, isSecretLiteral)
			drops = append(drops, d...)
			if ok {
				litEnv[k] = lit
			}
		}
		if len(litEnv) > 0 {
			native["env"] = litEnv
		}
		if len(envVars) > 0 {
			sort.Strings(envVars)
			native["env_vars"] = envVars
		}
		cwd, wd := encodeWorkingDir(s, c)
		drops = append(drops, wd...)
		if cwd != "" {
			native["cwd"] = cwd
		}
	}
	if s.Enabled != nil {
		native["enabled"] = *s.Enabled
	}
	if s.Timeout != 0 {
		native["tool_timeout_sec"] = s.Timeout
	}
	if len(s.ToolAllow) > 0 {
		native["enabled_tools"] = s.ToolAllow
	}
	if len(s.ToolDeny) > 0 {
		native["disabled_tools"] = s.ToolDeny
	}
	drops = append(drops, encodeExtra(FlavorCodex, s, native)...)
	return native, drops
}

func decodeCodexServer(name string, n map[string]any) Server {
	s := Server{Name: name}
	known := []string{"enabled", "enabled_tools", "disabled_tools"}
	if b, ok := n["enabled"].(bool); ok {
		s.Enabled = boolPtr(b)
	}
	// A fractional tool_timeout_sec has no neutral form; it stays Codex-only.
	if t, ok := wholeInt(n["tool_timeout_sec"]); ok && t > 0 {
		s.Timeout = t
		known = append(known, "tool_timeout_sec")
	}
	s.ToolAllow = asStringSlice(n["enabled_tools"])
	s.ToolDeny = asStringSlice(n["disabled_tools"])
	url := asString(n["url"])
	if url != "" {
		s.Transport = TransportHTTP
		s.URL = neutralLiteral(url, false)
		headers := map[string]string{}
		for k, v := range asStringMap(n["http_headers"]) {
			headers[k] = neutralLiteral(v, true)
		}
		for k, v := range asStringMap(n["env_http_headers"]) {
			headers[k] = "env:" + v
		}
		if len(headers) > 0 {
			s.Headers = headers
		}
		if bt := asString(n["bearer_token_env_var"]); bt != "" {
			s.Auth = "env:" + bt
		}
		s.Extra = collectExtra(FlavorCodex, n, append(known, "url", "http_headers", "env_http_headers", "bearer_token_env_var")...)
	} else {
		s.Transport = TransportStdio
		s.Command = neutralLiteral(asString(n["command"]), false)
		s.Args = decodeArgs(asStringSlice(n["args"]), nil)
		env := map[string]string{}
		for k, v := range asStringMap(n["env"]) {
			env[k] = neutralLiteral(v, true)
		}
		for _, vr := range asStringSlice(n["env_vars"]) {
			env[vr] = "env:" + vr
		}
		if len(env) > 0 {
			s.Env = env
		}
		s.WorkingDir = neutralLiteral(asString(n["cwd"]), false)
		s.Extra = collectExtra(FlavorCodex, n, append(known, "command", "args", "env", "env_vars", "cwd")...)
	}
	return s
}

// decodeCodexDoc extracts neutral servers from config.toml's [mcp_servers] table.
func decodeCodexDoc(fileData []byte) ([]Server, error) {
	top := map[string]any{}
	if len(bytes.TrimSpace(fileData)) > 0 {
		if err := toml.Unmarshal(fileData, &top); err != nil {
			return nil, fmt.Errorf("decode config.toml: %w", err)
		}
	}
	servers, _ := top["mcp_servers"].(map[string]any)
	var out []Server
	for _, name := range sortedAnyKeys(servers) {
		native, _ := servers[name].(map[string]any)
		out = append(out, decodeCodexServer(name, native))
	}
	return out, nil
}

// mergeCodexDoc merges newServers into config.toml's [mcp_servers] table, preserving
// every other key of the file and every existing server not in newServers. Existing
// servers are kept as their decoded tables (never re-encoded), so a secret Codex wrote
// into one is never dropped here.
func mergeCodexDoc(fileData []byte, newServers []Server, overwrite bool) ([]byte, ConversionReport, error) {
	var res ConversionReport
	top := map[string]any{}
	if len(bytes.TrimSpace(fileData)) > 0 {
		if err := toml.Unmarshal(fileData, &top); err != nil {
			return nil, res, fmt.Errorf("decode config.toml: %w", err)
		}
	}
	servers, _ := top["mcp_servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	for _, s := range newServers {
		if why, skip := skipDisabled(FlavorCodex, s); skip {
			res.Skipped = append(res.Skipped, SkippedServer{Name: s.Name, Reason: why})
			continue
		}
		if why, skip := unwritableName(FlavorCodex, s); skip {
			res.Skipped = append(res.Skipped, SkippedServer{Name: s.Name, Reason: why})
			continue
		}
		_, exists := servers[s.Name]
		if exists && !overwrite {
			res.Skipped = append(res.Skipped, SkippedServer{Name: s.Name, Reason: "already in target (pass --overwrite)"})
			continue
		}
		native, drops := encodeCodexServer(s)
		res.Drops = append(res.Drops, drops...)
		if why, skip := unwritable(s, native, drops); skip {
			res.Skipped = append(res.Skipped, SkippedServer{Name: s.Name, Reason: why})
			continue
		}
		servers[s.Name] = native
		if exists {
			res.Replaced = append(res.Replaced, s.Name)
		} else {
			res.Converted = append(res.Converted, s.Name)
		}
	}
	top["mcp_servers"] = servers
	out, err := toml.Marshal(top)
	if err != nil {
		return nil, res, fmt.Errorf("encode config.toml: %w", err)
	}
	return out, res, nil
}

func sortedAnyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
