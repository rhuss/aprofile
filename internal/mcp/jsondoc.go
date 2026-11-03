package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// jsonServerEncoder renders a neutral server into a flavor's native JSON object and
// reports what it had to drop (unrepresentable fields, literal secrets).
type jsonServerEncoder func(s Server) (map[string]any, []Drop)

// jsonServerDecoder parses a flavor's native JSON server object into neutral form.
type jsonServerDecoder func(name string, native map[string]any) Server

// decodeJSONDoc extracts neutral servers from a JSON config file's MCP object (the
// object under mcpKey). A missing object yields no servers.
func decodeJSONDoc(fileData []byte, mcpKey string, dec jsonServerDecoder) ([]Server, error) {
	top, err := decodeTopObject(fileData)
	if err != nil {
		return nil, err
	}
	raw, ok := top[mcpKey]
	if !ok {
		return nil, nil
	}
	mcpObj := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &mcpObj); err != nil {
			return nil, fmt.Errorf("decode %q object: %w", mcpKey, err)
		}
	}
	var servers []Server
	for _, name := range sortedRawKeys(mcpObj) {
		var native map[string]any
		if err := json.Unmarshal(mcpObj[name], &native); err != nil {
			return nil, fmt.Errorf("decode MCP server %q: %w", name, err)
		}
		servers = append(servers, dec(name, native))
	}
	return servers, nil
}

// mergeJSONDoc merges newServers into a JSON file's MCP object and re-encodes the
// whole file, preserving every other top-level key and every existing server not in
// newServers. Existing servers are copied through verbatim (their raw bytes are never
// re-encoded), so a literal secret a harness wrote into one is never dropped here;
// only the servers this merge writes are run through enc.
func mergeJSONDoc(fileData []byte, mcpKey string, newServers []Server, enc jsonServerEncoder, overwrite bool) ([]byte, ConversionReport, error) {
	var res ConversionReport
	top, err := decodeTopObject(fileData)
	if err != nil {
		return nil, res, err
	}
	mcpObj := map[string]json.RawMessage{}
	if raw, ok := top[mcpKey]; ok && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &mcpObj); err != nil {
			return nil, res, fmt.Errorf("decode existing %q object: %w", mcpKey, err)
		}
	}
	for _, s := range newServers {
		_, exists := mcpObj[s.Name]
		if exists && !overwrite {
			res.Skipped = append(res.Skipped, SkippedServer{Name: s.Name, Reason: "already in target (pass --overwrite)"})
			continue
		}
		native, drops := enc(s)
		res.Drops = append(res.Drops, drops...)
		b, err := json.Marshal(native)
		if err != nil {
			return nil, res, fmt.Errorf("encode MCP server %q: %w", s.Name, err)
		}
		mcpObj[s.Name] = b
		if exists {
			res.Replaced = append(res.Replaced, s.Name)
		} else {
			res.Converted = append(res.Converted, s.Name)
		}
	}
	mcpRaw, err := json.Marshal(mcpObj)
	if err != nil {
		return nil, res, err
	}
	top[mcpKey] = mcpRaw
	out, err := encodeTopObject(top)
	return out, res, err
}

// decodeTopObject parses a JSON object into a key->raw map, preserving each value's
// bytes. An empty document decodes to an empty map (so a fresh target works).
func decodeTopObject(data []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}

// encodeTopObject renders a key->raw map with two-space indent and a trailing
// newline (Go marshals map keys sorted, so output is deterministic).
func encodeTopObject(m map[string]json.RawMessage) ([]byte, error) {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode JSON object: %w", err)
	}
	return append(out, '\n'), nil
}

func sortedRawKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// asStringSlice converts a decoded JSON array (of strings) to []string.
func asStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// asStringMap converts a decoded JSON object (of strings) to map[string]string.
func asStringMap(v any) map[string]string {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for k, e := range obj {
		if s, ok := e.(string); ok {
			out[k] = s
		}
	}
	return out
}

// asString returns v as a string, or "".
func asString(v any) string {
	s, _ := v.(string)
	return s
}

// asInt returns v as an int from a decoded JSON number, or 0.
func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
