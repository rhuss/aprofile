package mcp

import "fmt"

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

// ParseDoc decodes a harness's native MCP document (the whole config file's bytes)
// into neutral servers, sorted by name. A missing MCP section yields no servers.
func ParseDoc(flavor Flavor, fileData []byte) ([]Server, error) {
	switch flavor {
	case FlavorClaude:
		return decodeJSONDoc(fileData, "mcpServers", decodeClaudeServer)
	case FlavorPi:
		return decodeJSONDoc(fileData, "mcpServers", decodePiServer)
	case FlavorOpenCode:
		return decodeJSONDoc(fileData, "mcp", decodeOpenCodeServer)
	case FlavorCodex:
		return decodeCodexDoc(fileData)
	default:
		return nil, fmt.Errorf("unknown MCP flavor %q", flavor)
	}
}

// MergeInto converts servers into flavor and merges them into fileData (the target
// config file, possibly empty), preserving every other key of the file and every
// existing target server not named in servers. Existing target servers are carried
// through verbatim, so a secret the harness wrote into one is never dropped; only the
// servers written here are run through the secret and representability checks
// (Constitution IV, FR-035). Returns the new file content and the report.
func MergeInto(flavor Flavor, fileData []byte, servers []Server, overwrite bool) ([]byte, ConversionReport, error) {
	switch flavor {
	case FlavorClaude:
		return mergeJSONDoc(fileData, "mcpServers", servers, encodeClaudeServer, overwrite)
	case FlavorPi:
		return mergeJSONDoc(fileData, "mcpServers", servers, encodePiServer, overwrite)
	case FlavorOpenCode:
		return mergeJSONDoc(fileData, "mcp", servers, encodeOpenCodeServer, overwrite)
	case FlavorCodex:
		return mergeCodexDoc(fileData, servers, overwrite)
	default:
		return nil, ConversionReport{}, fmt.Errorf("unknown MCP flavor %q", flavor)
	}
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

// SecretWarnings parses a harness's MCP document and reports every env or header value
// that is a literal matching a secret pattern. A same-harness copy carries these
// verbatim and warns about each (FR-033a); no value is included in the warning.
func SecretWarnings(flavor Flavor, fileData []byte) ([]SecretWarning, error) {
	servers, err := ParseDoc(flavor, fileData)
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
	}
	return ws, nil
}
