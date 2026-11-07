package mcp

import (
	"encoding/json"
	"fmt"
)

// Flavor is the per-harness MCP schema flavor a harness definition declares
// (definition.MCP.Flavor). The mcp package's only per-harness knowledge is the switch
// on this value (Principle I).
type Flavor string

const (
	FlavorClaude   Flavor = "claude-json"
	FlavorCodex    Flavor = "codex-toml"
	FlavorOpenCode Flavor = "opencode-json"
	FlavorPi       Flavor = "pi-json"
)

// ConversionReport is the MCP portion of a copy / mcp copy report (FR-035, the
// data-model's ConversionReport). The CLI embeds it in the full copy report alongside
// skills, plugins and dropped-setting information.
type ConversionReport struct {
	Converted []string        `json:"converted"`          // servers newly written to the target
	Replaced  []string        `json:"replaced,omitempty"` // existing target servers overwritten
	Skipped   []SkippedServer `json:"skipped,omitempty"`  // servers not written
	Drops     []Drop          `json:"drops,omitempty"`    // per-field drops for written servers
}

// MarshalJSON writes converted as [] when no server was converted, so the field
// is always a list (FR-041 stable schema), never null.
func (r ConversionReport) MarshalJSON() ([]byte, error) {
	type plain ConversionReport
	if r.Converted == nil {
		r.Converted = []string{}
	}
	return json.Marshal(plain(r))
}

// Doc names where a harness keeps its MCP servers inside its config file: the schema
// flavor and the key holding the servers (the part after '#' of the definition's mcp
// location, FR-034). An empty Key is the flavor's default key (DefaultKey), so a
// definition that names its key is honored and one that names none still works
// (Principle I: the key is data, not code).
type Doc struct {
	Flavor Flavor
	Key    string
}

// DefaultKey is the key a flavor's servers live under when a definition's location
// names none.
func DefaultKey(f Flavor) string {
	switch f {
	case FlavorClaude, FlavorPi:
		return "mcpServers"
	case FlavorOpenCode:
		return "mcp"
	case FlavorCodex:
		return "mcp_servers"
	}
	return ""
}

// ResolvedKey is the key the servers live under: Key, or the flavor's default.
func (d Doc) ResolvedKey() string {
	if d.Key != "" {
		return d.Key
	}
	return DefaultKey(d.Flavor)
}

// Parse decodes a harness's native MCP document (the whole config file's bytes) into
// neutral servers, sorted by name. A missing MCP section yields no servers.
func (d Doc) Parse(fileData []byte) ([]Server, error) {
	key := d.ResolvedKey()
	switch d.Flavor {
	case FlavorClaude:
		return decodeJSONDoc(fileData, key, decodeClaudeServer)
	case FlavorPi:
		return decodeJSONDoc(fileData, key, decodePiServer)
	case FlavorOpenCode:
		return decodeJSONDoc(fileData, key, decodeOpenCodeServer)
	case FlavorCodex:
		return decodeCodexDoc(fileData, key)
	default:
		return nil, fmt.Errorf("unknown MCP flavor %q", d.Flavor)
	}
}

// Merge converts servers into the flavor and merges them into fileData (the target
// config file, possibly empty), preserving every other key of the file and every
// existing target server not named in servers. Existing target servers are carried
// through verbatim, so a secret the harness wrote into one is never dropped; only the
// servers written here are run through the secret and representability checks
// (Constitution IV, FR-035). Returns the new file content and the report.
func (d Doc) Merge(fileData []byte, servers []Server, overwrite bool) ([]byte, ConversionReport, error) {
	key := d.ResolvedKey()
	switch d.Flavor {
	case FlavorClaude:
		return mergeJSONDoc(FlavorClaude, fileData, key, servers, encodeClaudeServer, overwrite)
	case FlavorPi:
		return mergeJSONDoc(FlavorPi, fileData, key, servers, encodePiServer, overwrite)
	case FlavorOpenCode:
		return mergeJSONDoc(FlavorOpenCode, fileData, key, servers, encodeOpenCodeServer, overwrite)
	case FlavorCodex:
		return mergeCodexDoc(fileData, key, servers, overwrite)
	default:
		return nil, ConversionReport{}, fmt.Errorf("unknown MCP flavor %q", d.Flavor)
	}
}

// ParseDoc is Doc.Parse under the flavor's default key.
func ParseDoc(flavor Flavor, fileData []byte) ([]Server, error) {
	return Doc{Flavor: flavor}.Parse(fileData)
}

// MergeInto is Doc.Merge under the flavor's default key.
func MergeInto(flavor Flavor, fileData []byte, servers []Server, overwrite bool) ([]byte, ConversionReport, error) {
	return Doc{Flavor: flavor}.Merge(fileData, servers, overwrite)
}

// SkipAll builds a report that marks every given server skipped with reason, used when
// the target harness lacks the mcp capability (FR-035).
func SkipAll(servers []Server, reason string) ConversionReport {
	var r ConversionReport
	for _, s := range servers {
		r.Skipped = append(r.Skipped, SkippedServer{Name: s.Name, Reason: reason})
	}
	return r
}

// SecretWarnings parses a harness's MCP document and reports every env, header, args,
// url or unmodeled native value that is a literal matching a secret pattern. A same-harness copy carries these
// verbatim and warns about each (FR-033a); no value is included in the warning.
func SecretWarnings(flavor Flavor, fileData []byte) ([]SecretWarning, error) {
	return Doc{Flavor: flavor}.SecretWarnings(fileData)
}

// SecretWarnings is the package-level SecretWarnings for the servers under d's key.
func (d Doc) SecretWarnings(fileData []byte) ([]SecretWarning, error) {
	flavor := d.Flavor
	servers, err := d.Parse(fileData)
	if err != nil {
		return nil, err
	}
	var ws []SecretWarning
	for _, s := range servers {
		for _, k := range sortedKeys(s.Env) {
			if isLiteralSecretValue(s.Env[k]) {
				ws = append(ws, SecretWarning{Server: s.Name, Field: "env[" + k + "]"})
			}
		}
		for _, k := range sortedKeys(s.Headers) {
			if isLiteralSecretValue(s.Headers[k]) {
				ws = append(ws, SecretWarning{Server: s.Name, Field: "headers[" + k + "]"})
			}
		}
		// args, url and unmodeled native fields use the same checks a conversion
		// applies; their drop list names exactly the secret-looking values.
		_, ad := encodeArgs(s.Name, s.Args, nil)
		_, ud := encodeURLValue(s.Name, s.URL, nil)
		ed := encodeExtra(flavor, s, map[string]any{})
		for _, d := range append(append(ad, ud...), ed...) {
			if d.Reason == ReasonLiteralSecret {
				ws = append(ws, SecretWarning{Server: d.Server, Field: d.Field})
			}
		}
	}
	return ws, nil
}
