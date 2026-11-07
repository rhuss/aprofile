package mcp

import (
	"strings"
	"testing"
)

// Opaque tokens that only the FR-028 length check flags (no known prefix).
const (
	tok40 = "Zx9Qw8Er7Ty6Ui5Op4As3Df2Gh1Jk0LmNbVcXz12"
	tok36 = "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8"
)

// R2-1: an empty env value, header value or args element is a legitimate literal in
// every flavor; it is written as "" and reads back as "", never dropped (dropping an
// empty argument would shift the arguments after it).
func TestEmptyValuesSurvive(t *testing.T) {
	servers := []Server{
		{Name: "s", Transport: TransportStdio, Command: "run", Args: []string{"--prefix", "", "x"}, Env: map[string]string{"DEBUG": ""}},
		{Name: "r", Transport: TransportHTTP, URL: "https://h.example.com/mcp", Headers: map[string]string{"E": ""}},
	}
	for _, flavor := range allFlavors {
		data, rep, err := MergeInto(flavor, nil, servers, false)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		if len(rep.Drops) != 0 || len(rep.Skipped) != 0 {
			t.Errorf("%s: empty values reported: %+v", flavor, rep)
		}
		got, err := ParseDoc(flavor, data)
		if err != nil || len(got) != 2 {
			t.Fatalf("%s: parse back: %v %+v", flavor, err, got)
		}
		r, s := got[0], got[1]
		if strings.Join(s.Args, "|") != "--prefix||x" || len(s.Args) != 3 {
			t.Errorf("%s: args = %q", flavor, s.Args)
		}
		if v, ok := s.Env["DEBUG"]; !ok || v != "" {
			t.Errorf("%s: env = %v", flavor, s.Env)
		}
		if v, ok := r.Headers["E"]; !ok || v != "" {
			t.Errorf("%s: headers = %v", flavor, r.Headers)
		}
	}
	// A Claude -> Claude mcp copy keeps them too.
	doc := `{"mcpServers":{"s":{"command":"run","args":["--prefix","","x"],"env":{"DEBUG":""}}}}`
	src, err := ParseDoc(FlavorClaude, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	data, rep, err := MergeInto(FlavorClaude, nil, src, false)
	if err != nil || len(rep.Drops) != 0 || len(rep.Skipped) != 0 {
		t.Fatalf("claude self: %v %+v", err, rep)
	}
	if !strings.Contains(string(data), `"--prefix",`) || !strings.Contains(string(data), `"DEBUG": ""`) {
		t.Errorf("claude self lost an empty value:\n%s", data)
	}
	n := nativeServer(t, FlavorClaude, data, "s")
	if a := asStringSlice(n["args"]); len(a) != 3 || a[1] != "" {
		t.Errorf("claude self args = %q", a)
	}
}

// R2-1: args are positional, so a stdio server that loses an args element is not
// written (it would run with its arguments shifted); the drop is still reported. A
// whole `--flag=value` element carries its own value and may be dropped on its own.
func TestDroppedArgSkipsServer(t *testing.T) {
	for _, flavor := range allFlavors {
		s := Server{Name: "s", Transport: TransportStdio, Command: "run", Args: []string{"--token", argToken, "x"}}
		data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), argToken) || nativeServer(t, flavor, data, "s") != nil {
			t.Errorf("%s: server with a dropped positional arg written:\n%s", flavor, data)
		}
		if !hasDropReason(rep.Drops, "args[1]", ReasonLiteralSecret) {
			t.Errorf("%s: args[1] not reported: %+v", flavor, rep.Drops)
		}
		if !hasSkippedReason(rep.Skipped, "s", "args[1]") || len(rep.Converted) != 0 {
			t.Errorf("%s: server not skipped: %+v", flavor, rep)
		}

		s = Server{Name: "s", Transport: TransportStdio, Command: "run", Args: []string{"--api-key=" + argEqToken, "-k=" + argToken, "x"}}
		data, rep, err = MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Skipped) != 0 || !hasDropReason(rep.Drops, "args[0]", ReasonLiteralSecret) || !hasDropReason(rep.Drops, "args[1]", ReasonLiteralSecret) {
			t.Errorf("%s: --flag=value drop: %+v", flavor, rep)
		}
		got, _ := ParseDoc(flavor, data)
		if len(got) != 1 || strings.Join(got[0].Args, " ") != "x" {
			t.Errorf("%s: args = %+v", flavor, got)
		}
	}
}

// R2-2: a url holding a reference gets the same query, fragment and path checks as a
// literal url; the hits are removed (or the server skipped) and reported by position,
// and the references survive.
func TestTemplateURLSecrets(t *testing.T) {
	cases := []struct {
		url    string
		field  string
		claude string // expected Claude url ("" = server skipped)
	}{
		{"tmpl:https://h.example.com/${env:TOK}/mcp?key=" + tok40 + "&mode=x", "url.query[key]", "https://h.example.com/${TOK}/mcp?mode=x"},
		{"tmpl:https://h.example.com/${env:X}/mcp#access_token=" + tok36, "url.fragment", "https://h.example.com/${X}/mcp"},
		{"tmpl:https://h.example.com/${env:X}/" + tok36 + "#access_token=" + tok36, "url.path[1]", ""},
		{"tmpl:https://h.example.com/mcp?a=${env:A}&b=x" + tok36, "url.query[b]", "https://h.example.com/mcp?a=${A}"},
		{"tmpl:https://${env:U}:" + tok40 + "@h.example.com/${env:P}", "url.userinfo", "https://h.example.com/${P}"},
	}
	for _, tc := range cases {
		for _, flavor := range allFlavors {
			s := Server{Name: "r", Transport: TransportHTTP, URL: tc.url}
			data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), tok36) || strings.Contains(string(data), tok40) {
				t.Errorf("%s %s: template url secret leaked:\n%s", flavor, tc.field, data)
			}
			if !hasDropReason(rep.Drops, tc.field, ReasonLiteralSecret) {
				t.Errorf("%s: %s not reported: %+v", flavor, tc.field, rep.Drops)
			}
			r := nativeServer(t, flavor, data, "r")
			switch {
			case tc.claude == "" || flavor == FlavorPi || flavor == FlavorCodex:
				if r != nil {
					t.Errorf("%s %s: server written: %v", flavor, tc.field, r)
				}
			case flavor == FlavorClaude:
				if r["url"] != tc.claude {
					t.Errorf("claude %s: url = %v, want %s", tc.field, r["url"], tc.claude)
				}
			case flavor == FlavorOpenCode:
				want := strings.NewReplacer("${TOK}", "{env:TOK}", "${X}", "{env:X}", "${A}", "{env:A}", "${P}", "{env:P}").Replace(tc.claude)
				if r["url"] != want {
					t.Errorf("opencode %s: url = %v, want %s", tc.field, r["url"], want)
				}
			}
		}
	}
	// An unchanged template url is kept byte-for-byte.
	const plain = "tmpl:https://${env:H}/a$$b/mcp?k=${env:K:-x}"
	if got, d := encodeURLValue("r", plain, claudeSyntax); got != "https://${H}/a$b/mcp?k=${K:-x}" || len(d) != 0 {
		t.Errorf("plain template url: %q %+v", got, d)
	}
}

// R2-2: RedactURL masks a template url's secret query, fragment and path parts.
func TestRedactTemplateURL(t *testing.T) {
	for in, want := range map[string]string{
		"tmpl:https://h/${env:TOK}/mcp?key=" + tok40 + "&mode=x":      "https://h/${TOK}/mcp?key=***&mode=x",
		"tmpl:https://h/${env:X}/" + tok36 + "#access_token=" + tok36: "https://h/${X}/***#***",
		"tmpl:https://h/${env:X}/mcp?k=${env:K:-d}":                   "https://h/${X}/mcp?k=${K:-d}",
	} {
		got := RedactURL(in)
		if strings.Contains(got, tok36) || strings.Contains(got, tok40) || got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// R2-3: OpenCode substitutes {file:...} and {env:...} anywhere in its config text, keys
// included. An env or header name holding one is dropped and reported; a server whose
// name holds one is skipped. Other targets take such names literally.
func TestOpenCodeSubstitutingKeys(t *testing.T) {
	const bad = "{file:~/.ssh/id_rsa}"
	servers := []Server{
		{Name: "h", Transport: TransportHTTP, URL: "https://h.example.com/mcp", Headers: map[string]string{bad: "x", "OK": "y"}},
		{Name: "e", Transport: TransportStdio, Command: "node", Env: map[string]string{bad: "x", "{env:HOME}": "z", "OK": "y"}},
		{Name: "n" + bad, Transport: TransportStdio, Command: "node"},
	}
	data, rep, err := MergeInto(FlavorOpenCode, nil, servers, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "{file:") || strings.Contains(string(data), "{env:") {
		t.Errorf("opencode: substituting key written:\n%s", data)
	}
	for _, f := range []string{"headers[" + bad + "]", "env[" + bad + "]", "env[{env:HOME}]"} {
		if !hasDropReason(rep.Drops, f, ReasonUnrepresentable) {
			t.Errorf("opencode: %s not reported: %+v", f, rep.Drops)
		}
	}
	if !hasSkippedReason(rep.Skipped, "n"+bad, "name") {
		t.Errorf("opencode: server with a substituting name not skipped: %+v", rep)
	}
	for _, n := range rep.Converted {
		if strings.Contains(n, "{") {
			t.Errorf("opencode: converted %q", n)
		}
	}
	if env, _ := nativeServer(t, FlavorOpenCode, data, "e")["environment"].(map[string]any); env["OK"] != "y" || len(env) != 1 {
		t.Errorf("opencode: env = %v", env)
	}
	// Claude takes keys literally.
	_, rep, _ = MergeInto(FlavorClaude, nil, servers, false)
	if len(rep.Drops) != 0 || len(rep.Skipped) != 0 {
		t.Errorf("claude: literal keys reported: %+v", rep)
	}
}

// R2-4: a command default OpenCode cannot hold is reported even when the command is
// written.
func TestOpenCodeCommandDefaultReported(t *testing.T) {
	s := Server{Name: "c", Transport: TransportStdio, Command: "tmpl:${env:BIN:-node}"}
	data, rep, err := MergeInto(FlavorOpenCode, nil, []Server{s}, false)
	if err != nil {
		t.Fatal(err)
	}
	if cmd := asStringSlice(nativeServer(t, FlavorOpenCode, data, "c")["command"]); len(cmd) != 1 || cmd[0] != "{env:BIN}" {
		t.Errorf("command = %v", cmd)
	}
	if !hasDropReason(rep.Drops, "command.default", ReasonUnrepresentable) {
		t.Errorf("command default not reported: %+v", rep.Drops)
	}
}
