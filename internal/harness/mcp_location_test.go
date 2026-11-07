package harness

import "testing"

// T084: the MCP location ("file#key") is parsed in one place.
func TestMCPLocationFileKey(t *testing.T) {
	cases := []struct {
		loc       *MCPLocation
		file, key string
	}{
		{&MCPLocation{Location: ".claude.json#mcpServers"}, ".claude.json", "mcpServers"},
		{&MCPLocation{Location: "config/opencode/opencode.json#mcp"}, "config/opencode/opencode.json", "mcp"},
		{&MCPLocation{Location: "mcp.json"}, "mcp.json", ""},
		{&MCPLocation{}, "", ""},
		{nil, "", ""},
	}
	for _, c := range cases {
		if got := c.loc.File(); got != c.file {
			t.Errorf("%+v.File() = %q, want %q", c.loc, got, c.file)
		}
		if got := c.loc.Key(); got != c.key {
			t.Errorf("%+v.Key() = %q, want %q", c.loc, got, c.key)
		}
	}
}

// Review follow-up: the MCP location is validated at load. The file must be a
// relative path inside the profile dir (sync, mcp copy and import join it onto the
// config dir and write it), and a key inside a mixed file must be an owned key or
// an undeclared one, never a credentials, state or settings key.
func TestValidateMCPLocation(t *testing.T) {
	const base = "name: toy\nbinary: toy\nwrapper_prefix: toy\nconfig_env: { TOY_DIR: \".\" }\n" +
		"classification:\n" +
		"  - entry: app.json\n    layer: mixed\n    keys: { servers: owned, account: credentials, history: state, theme: settings }\n" +
		"  - { entry: auth.json, layer: credentials }\n" +
		"  - { entry: tools.json, layer: owned }\n"
	cases := []struct {
		loc string
		ok  bool
	}{
		{"app.json#servers", true},
		{"app.json#undeclaredKey", true},
		{"tools.json#mcpServers", true},
		{"other/mcp.json", true},
		{"/etc/mcp.json#servers", false},
		{"../outside.json#servers", false},
		{"sub/../../outside.json", false},
		{"#servers", false},
		{"app.json#account", false},
		{"app.json#history", false},
		{"app.json#theme", false},
		{"auth.json#servers", false},
	}
	for _, c := range cases {
		data := base + "mcp:\n  location: \"" + c.loc + "\"\n  flavor: claude-json\n"
		_, lerr := parseDefinition([]byte(data), "toy.yaml")
		if c.ok && lerr != nil {
			t.Errorf("location %q should load: %v", c.loc, lerr)
		}
		if !c.ok {
			if lerr == nil {
				t.Errorf("location %q should be rejected", c.loc)
			} else if lerr.Field != "mcp" {
				t.Errorf("location %q: field = %q, want mcp (%v)", c.loc, lerr.Field, lerr)
			}
		}
	}
}
