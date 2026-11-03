package mcp

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/secret"
)

// Codex MCP: the [mcp_servers.<name>] tables in config.toml. Secrets are expressed
// structurally, never inline (research.md item 4): an env reference goes in env_vars
// (forwarded from the host env by name), a literal env goes in env; a header
// reference goes in env_http_headers (header -> env var name), a literal header in
// http_headers; a bearer token goes in bearer_token_env_var. Because env_vars
// forwards by name, an env reference whose server-var name differs from the host-var
// name cannot be expressed and is reported.

func encodeCodexServer(s Server) (map[string]any, []Drop) {
	native := map[string]any{}
	drops := unrepresentableDrops(s, codexCaps)
	if s.IsRemote() {
		native["url"] = s.URL
		litHeaders := map[string]string{}
		envHeaders := map[string]string{}
		for _, k := range sortedKeys(s.Headers) {
			v := s.Headers[k]
			if secret.IsReference(v) {
				if r, err := secret.ParseReference(v); err == nil && r.Kind == secret.KindEnv {
					envHeaders[k] = r.Value
				} else {
					drops = append(drops, Drop{s.Name, "headers[" + k + "]", ReasonUnrepresentable})
				}
			} else if isSecretLiteral(v) {
				drops = append(drops, Drop{s.Name, "headers[" + k + "]", ReasonLiteralSecret})
			} else {
				litHeaders[k] = v
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
		native["command"] = s.Command
		if len(s.Args) > 0 {
			native["args"] = s.Args
		}
		litEnv := map[string]string{}
		var envVars []string
		for _, k := range sortedKeys(s.Env) {
			v := s.Env[k]
			if secret.IsReference(v) {
				r, err := secret.ParseReference(v)
				switch {
				case err == nil && r.Kind == secret.KindEnv && r.Value == k:
					envVars = append(envVars, k)
				default:
					// env_vars forwards by name only; a rename or file ref cannot be expressed.
					drops = append(drops, Drop{s.Name, "env[" + k + "]", ReasonUnrepresentable})
				}
			} else if isSecretLiteral(v) {
				drops = append(drops, Drop{s.Name, "env[" + k + "]", ReasonLiteralSecret})
			} else {
				litEnv[k] = v
			}
		}
		if len(litEnv) > 0 {
			native["env"] = litEnv
		}
		if len(envVars) > 0 {
			sort.Strings(envVars)
			native["env_vars"] = envVars
		}
	}
	return native, drops
}

func decodeCodexServer(name string, n map[string]any) Server {
	s := Server{Name: name}
	url := asString(n["url"])
	if url != "" {
		s.Transport = TransportHTTP
		s.URL = url
		headers := map[string]string{}
		for k, v := range asStringMap(n["http_headers"]) {
			headers[k] = v
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
	} else {
		s.Transport = TransportStdio
		s.Command = asString(n["command"])
		s.Args = asStringSlice(n["args"])
		env := map[string]string{}
		for k, v := range asStringMap(n["env"]) {
			env[k] = v
		}
		for _, vr := range asStringSlice(n["env_vars"]) {
			env[vr] = "env:" + vr
		}
		if len(env) > 0 {
			s.Env = env
		}
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
		_, exists := servers[s.Name]
		if exists && !overwrite {
			res.Skipped = append(res.Skipped, SkippedServer{Name: s.Name, Reason: "already in target (pass --overwrite)"})
			continue
		}
		native, drops := encodeCodexServer(s)
		res.Drops = append(res.Drops, drops...)
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
