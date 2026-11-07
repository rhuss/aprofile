package mcp

import (
	"strings"
	"testing"
)

// T084: a definition whose location names a non-default key converts from and into
// that key; an empty key keeps the flavor's default.
func TestDocNonDefaultKey(t *testing.T) {
	cases := []struct {
		flavor Flavor
		doc    string
	}{
		{FlavorClaude, `{"mcpServers":{"other":{"command":"no"}},"servers":{"gh":{"command":"mcp-gh"}}}`},
		{FlavorPi, `{"mcpServers":{"other":{"command":"no"}},"servers":{"gh":{"type":"stdio","command":"mcp-gh"}}}`},
		{FlavorOpenCode, `{"mcp":{"other":{"type":"local","command":["no"]}},"servers":{"gh":{"type":"local","command":["mcp-gh"]}}}`},
		{FlavorCodex, "[mcp_servers.other]\ncommand = \"no\"\n\n[servers.gh]\ncommand = \"mcp-gh\"\n"},
	}
	for _, c := range cases {
		d := Doc{Flavor: c.flavor, Key: "servers"}
		got, err := d.Parse([]byte(c.doc))
		if err != nil {
			t.Fatalf("%s: parse: %v", c.flavor, err)
		}
		if len(got) != 1 || got[0].Name != "gh" || got[0].Command != "mcp-gh" {
			t.Errorf("%s: parse from key servers = %+v", c.flavor, got)
		}
		out, rep, err := d.Merge(nil, []Server{{Name: "fs", Transport: TransportStdio, Command: "mcp-fs"}}, false)
		if err != nil {
			t.Fatalf("%s: merge: %v", c.flavor, err)
		}
		if len(rep.Converted) != 1 {
			t.Errorf("%s: merge report = %+v", c.flavor, rep)
		}
		back, err := d.Parse(out)
		if err != nil || len(back) != 1 || back[0].Name != "fs" {
			t.Errorf("%s: merged server not under key servers: %v %+v\n%s", c.flavor, err, back, out)
		}
		if def, _ := (Doc{Flavor: c.flavor}).Parse(out); len(def) != 0 {
			t.Errorf("%s: merged server landed under the default key:\n%s", c.flavor, out)
		}
		// An empty key is the flavor's default.
		if def, err := (Doc{Flavor: c.flavor}).Parse([]byte(c.doc)); err != nil || len(def) != 1 || def[0].Name != "other" {
			t.Errorf("%s: default key parse = %+v (%v)", c.flavor, def, err)
		}
	}
}

// T094: a stdio server whose command is empty (or empty once encoded) is skipped and
// reported, never written with an empty command.
func TestEmptyCommandSkipped(t *testing.T) {
	for _, f := range allFlavors {
		for _, cmd := range []string{"", "   "} {
			servers := []Server{
				{Name: "empty", Transport: TransportStdio, Command: cmd, Args: []string{"x"}},
				{Name: "ok", Transport: TransportStdio, Command: "mcp-ok"},
			}
			out, rep, err := MergeInto(f, nil, servers, false)
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			if len(rep.Converted) != 1 || rep.Converted[0] != "ok" {
				t.Errorf("%s %q: converted = %v", f, cmd, rep.Converted)
			}
			if len(rep.Skipped) != 1 || rep.Skipped[0].Name != "empty" || !strings.Contains(rep.Skipped[0].Reason, "command empty") {
				t.Errorf("%s %q: skipped = %+v", f, cmd, rep.Skipped)
			}
			back, err := ParseDoc(f, out)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range back {
				if s.Name == "empty" {
					t.Errorf("%s %q: server with an empty command written:\n%s", f, cmd, out)
				}
			}
		}
	}
}

// T095: a dropped whole `--flag=value` element of a written server names its option
// (the flag, never the value).
func TestDroppedOptionNamed(t *testing.T) {
	const tok = "sk-ant-OPTIONCANARY000000000000000000AAAA"
	for _, f := range allFlavors {
		s := Server{Name: "files", Transport: TransportStdio, Command: "mcp-files", Args: []string{"--root", "/tmp", "--token=" + tok}}
		out, rep, err := MergeInto(f, nil, []Server{s}, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Converted) != 1 {
			t.Fatalf("%s: server not written: %+v", f, rep)
		}
		var found bool
		for _, d := range rep.Drops {
			if d.Field == "args[2]" {
				found = true
				if d.Option != "--token" {
					t.Errorf("%s: drop option = %q, want --token", f, d.Option)
				}
			} else if d.Option != "" {
				t.Errorf("%s: option set on %+v", f, d)
			}
		}
		if !found {
			t.Errorf("%s: args[2] drop missing: %+v", f, rep.Drops)
		}
		if strings.Contains(string(out), tok) {
			t.Errorf("%s: secret written", f)
		}
	}
	// A skipped server (a positional value lost) carries no option mark.
	s := Server{Name: "pos", Transport: TransportStdio, Command: "x", Args: []string{"--token", tok, "--a=" + tok}}
	_, rep, _ := MergeInto(FlavorClaude, nil, []Server{s}, false)
	for _, d := range rep.Drops {
		if d.Option != "" {
			t.Errorf("skipped server's drop marked as option: %+v", d)
		}
	}
}

// Review follow-up: a merge refuses a value at the MCP key that is not a map
// (object or table) instead of replacing it, for every flavor; the target file is
// left as it was. A JSON null counts as no servers.
func TestMergeRefusesNonMapAtKey(t *testing.T) {
	fs := []Server{{Name: "fs", Transport: TransportStdio, Command: "mcp-fs"}}
	cases := []struct {
		flavor Flavor
		doc    string
	}{
		{FlavorClaude, `{"mcpServers":"a string"}`},
		{FlavorClaude, `{"mcpServers":["x"]}`},
		{FlavorClaude, `{"mcpServers":42}`},
		{FlavorPi, `{"mcpServers":true}`},
		{FlavorOpenCode, `{"mcp":"x"}`},
		{FlavorCodex, "mcp_servers = \"x\"\n"},
		{FlavorCodex, "mcp_servers = [1, 2]\n"},
		{FlavorCodex, "mcp_servers = 3\n"},
	}
	for _, c := range cases {
		out, _, err := (Doc{Flavor: c.flavor}).Merge([]byte(c.doc), fs, false)
		if err == nil {
			t.Errorf("%s %q: merge should refuse a non-map value, got\n%s", c.flavor, c.doc, out)
		} else if !strings.Contains(err.Error(), "not") {
			t.Errorf("%s %q: error should say what is wrong: %v", c.flavor, c.doc, err)
		}
	}
	for _, f := range []Flavor{FlavorClaude, FlavorPi, FlavorOpenCode} {
		key := DefaultKey(f)
		out, rep, err := (Doc{Flavor: f}).Merge([]byte(`{"`+key+`":null,"keep":1}`), fs, false)
		if err != nil || len(rep.Converted) != 1 || !strings.Contains(string(out), `"keep"`) {
			t.Errorf("%s: a null value is no servers: %v %+v\n%s", f, err, rep, out)
		}
	}
}
