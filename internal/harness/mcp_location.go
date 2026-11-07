package harness

import "strings"

// File returns the MCP file of the location ("file#key" or "file"), relative to the
// profile config dir in slash form. A nil location or an empty one yields "".
func (m *MCPLocation) File() string {
	if m == nil {
		return ""
	}
	file, _, _ := strings.Cut(m.Location, "#")
	return file
}

// Key returns the key inside the MCP file that holds the servers (the part after
// '#'), or "" when the location names none; the flavor's default key applies then.
func (m *MCPLocation) Key() string {
	if m == nil {
		return ""
	}
	_, key, _ := strings.Cut(m.Location, "#")
	return key
}
