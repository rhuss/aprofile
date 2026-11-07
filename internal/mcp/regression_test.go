package mcp

import (
	"strings"
	"testing"
)

// piEnvHeaders returns every env and header value of every server in a pi document.
func piEnvHeaders(t *testing.T, data []byte, names ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, n := range names {
		srv := nativeServer(t, FlavorPi, data, n)
		for _, key := range []string{"env", "headers"} {
			m, _ := srv[key].(map[string]any)
			for k, v := range m {
				s, _ := v.(string)
				out[n+"/"+key+"["+k+"]"] = s
			}
		}
	}
	return out
}

// M1: a pi `!command` has its own neutral form, set only by the pi decoder. A literal
// that starts with `!` from any other source is written to pi escaped (`$!`), so it
// never becomes a command; a pi command never reaches another flavor as literal text.
func TestPiCommandNeutralForm(t *testing.T) {
	// A Claude literal starting with `!` is escaped for pi and round-trips.
	claudeDoc := `{"mcpServers":{
		"s":{"command":"run","env":{"X":"!echo pwned","Y":"cmd:echo pwned"}},
		"r":{"type":"http","url":"https://h/mcp","headers":{"H":"!curl evil"}}}}`
	servers, err := ParseDoc(FlavorClaude, []byte(claudeDoc))
	if err != nil {
		t.Fatal(err)
	}
	data, rep, err := MergeInto(FlavorPi, nil, servers, false)
	if err != nil || len(rep.Drops) != 0 {
		t.Fatalf("claude->pi: %v %+v", err, rep.Drops)
	}
	got := piEnvHeaders(t, data, "s", "r")
	if got["s/env[X]"] != "$!echo pwned" || got["r/headers[H]"] != "$!curl evil" || got["s/env[Y]"] != "cmd:echo pwned" {
		t.Errorf("claude->pi values = %v", got)
	}
	back, _ := ParseDoc(FlavorPi, data)
	data2, _, _ := MergeInto(FlavorClaude, nil, back, false)
	if s := nativeServer(t, FlavorClaude, data2, "s"); s["env"].(map[string]any)["X"] != "!echo pwned" {
		t.Errorf("claude->pi->claude env = %v", s["env"])
	}
	// pi->pi keeps the escape.
	data3, _, _ := MergeInto(FlavorPi, nil, back, false)
	if string(data3) != string(data) {
		t.Errorf("pi self round-trip changed the document:\n%s\nvs\n%s", data3, data)
	}

	// pi: `$!foo` stays a literal, `!cmd` stays a command, pi->pi.
	piDoc := `{"mcpServers":{"s":{"type":"stdio","command":"run","env":{"LIT":"$!foo","CMD":"!op read op://v/i/f"}},
		"r":{"type":"http","url":"https://h/mcp","headers":{"Authorization":"!op read op://v/i/tok"}}}}`
	servers, err = ParseDoc(FlavorPi, []byte(piDoc))
	if err != nil {
		t.Fatal(err)
	}
	data, rep, err = MergeInto(FlavorPi, nil, servers, false)
	if err != nil || len(rep.Drops) != 0 {
		t.Fatalf("pi->pi: %v %+v", err, rep.Drops)
	}
	got = piEnvHeaders(t, data, "s", "r")
	if got["s/env[LIT]"] != "$!foo" || got["s/env[CMD]"] != "!op read op://v/i/f" || got["r/headers[Authorization]"] != "!op read op://v/i/tok" {
		t.Errorf("pi->pi values = %v", got)
	}

	// A pi command reaches no other flavor: dropped and reported.
	for _, dst := range []Flavor{FlavorClaude, FlavorOpenCode, FlavorCodex} {
		data, rep, err := MergeInto(dst, nil, servers, false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "op read") {
			t.Errorf("%s: pi command written as literal text:\n%s", dst, data)
		}
		for _, f := range []string{"env[CMD]", "headers[Authorization]"} {
			if !hasDropReason(rep.Drops, f, ReasonUnrepresentable) {
				t.Errorf("%s: %s not reported: %+v", dst, f, rep.Drops)
			}
		}
		if !strings.Contains(string(data), "foo") {
			t.Errorf("%s: the escaped literal was lost:\n%s", dst, data)
		}
	}
}

// M1: no pi env or header value converted from a non-pi source starts with a bare `!`.
func TestNoBareBangFromNonPiSource(t *testing.T) {
	sources := map[Flavor]string{
		FlavorClaude: `{"mcpServers":{"s":{"command":"run","env":{"A":"!x","B":"!${HOME}"}},
			"r":{"type":"http","url":"https://h/mcp","headers":{"H":"!y"}}}}`,
		FlavorOpenCode: `{"mcp":{"s":{"type":"local","command":["run"],"environment":{"A":"!x","B":"!{env:HOME}"}},
			"r":{"type":"remote","url":"https://h/mcp","headers":{"H":"!y"}}}}`,
		FlavorCodex: "[mcp_servers.s]\ncommand = \"run\"\nenv = { A = \"!x\", B = \"!z\" }\n\n[mcp_servers.r]\nurl = \"https://h/mcp\"\nhttp_headers = { H = \"!y\" }\n",
	}
	for src, doc := range sources {
		servers, err := ParseDoc(src, []byte(doc))
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		data, _, err := MergeInto(FlavorPi, nil, servers, false)
		if err != nil {
			t.Fatal(err)
		}
		vals := piEnvHeaders(t, data, "s", "r")
		if len(vals) != 3 {
			t.Errorf("%s->pi: values lost: %v", src, vals)
		}
		for k, v := range vals {
			if strings.HasPrefix(v, "!") {
				t.Errorf("%s->pi: %s = %q starts with a bare !", src, k, v)
			}
		}
	}
}

// M2: a remote server whose url cannot be written is skipped and reported, never
// written without a url (Codex would read it back as an empty stdio server).
func TestRemoteServerWithoutURLSkipped(t *testing.T) {
	cases := []struct {
		name string
		url  string
		dsts []Flavor
	}{
		{"secret", "sk-ant-api03-CANARYWHOLEURL0000000000000", allFlavors},
		{"tmpl", "tmpl:https://${env:MCP_HOST}/mcp", []Flavor{FlavorPi, FlavorCodex}},
	}
	for _, tc := range cases {
		s := Server{Name: "r", Transport: TransportHTTP, URL: tc.url, Headers: map[string]string{"X-Mode": "fast"}}
		for _, dst := range tc.dsts {
			data, rep, err := MergeInto(dst, nil, []Server{s}, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Converted) != 0 || !hasSkippedReason(rep.Skipped, "r", "url not representable") {
				t.Errorf("%s/%s: want skipped, got %+v", tc.name, dst, rep)
			}
			if !hasDrop(rep.Drops, "r", "url") {
				t.Errorf("%s/%s: url drop not reported: %+v", tc.name, dst, rep.Drops)
			}
			if n := nativeServer(t, dst, data, "r"); n != nil {
				t.Errorf("%s/%s: server written without url: %v", tc.name, dst, n)
			}
		}
	}
}

// M4: the url fragment and each path segment are checked for literal secrets.
func TestURLFragmentAndPathSecrets(t *testing.T) {
	const tok = "ghp_CANARYFRAGMENT00000000000000000000000"
	const blob = "Zx9Qw8Er7Ty6Ui5Op4As3Df2Gh1Jk0LmNbVcXz12"
	for _, flavor := range allFlavors {
		for _, frag := range []string{"#tok=" + tok, "#access_token=" + blob, "#" + tok} {
			s := Server{Name: "r", Transport: TransportHTTP, URL: "https://h.example.com/mcp" + frag}
			data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), tok) || strings.Contains(string(data), blob) {
				t.Errorf("%s: fragment secret leaked:\n%s", flavor, data)
			}
			if !hasDropReason(rep.Drops, "url.fragment", ReasonLiteralSecret) {
				t.Errorf("%s: fragment not reported: %+v", flavor, rep.Drops)
			}
			got, _ := ParseDoc(flavor, data)
			if len(got) != 1 || got[0].URL != "https://h.example.com/mcp" {
				t.Errorf("%s: url = %+v", flavor, got)
			}
		}
		for _, u := range []string{"https://h.example.com/" + tok + "/mcp", "https://h.example.com/mcp/" + blob} {
			s := Server{Name: "r", Transport: TransportHTTP, URL: u}
			data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), tok) || strings.Contains(string(data), blob) {
				t.Errorf("%s: path secret leaked:\n%s", flavor, data)
			}
			want := "url.path[0]"
			if strings.HasSuffix(u, blob) {
				want = "url.path[1]"
			}
			if !hasDropReason(rep.Drops, want, ReasonLiteralSecret) {
				t.Errorf("%s: %s not reported: %+v", flavor, want, rep.Drops)
			}
			if !hasSkippedReason(rep.Skipped, "r", "url not representable") {
				t.Errorf("%s: server with a secret path not skipped: %+v", flavor, rep)
			}
		}
		// A templated url's literal pieces get the per-segment prefix check too.
		s := Server{Name: "r", Transport: TransportHTTP, URL: "tmpl:https://${env:H}/" + tok + "/mcp"}
		data, _, _ := MergeInto(flavor, nil, []Server{s}, false)
		if strings.Contains(string(data), tok) {
			t.Errorf("%s: templated path secret leaked:\n%s", flavor, data)
		}
	}
	// A readable long path segment and a short fragment stay.
	ok := "https://h.example.com/some-rather-long-readable-endpoint-name/mcp#section"
	_, rep, _ := MergeInto(FlavorClaude, nil, []Server{{Name: "r", Transport: TransportHTTP, URL: ok}}, false)
	if len(rep.Drops) != 0 {
		t.Errorf("plain url reported: %+v", rep.Drops)
	}
}

// Sec4: RedactURL masks the parts a conversion would drop and keeps the rest.
func TestRedactURL(t *testing.T) {
	const tok = "ghp_CANARYREDACT0000000000000000000000000"
	for in, want := range map[string]string{
		"https://h/mcp":                         "https://h/mcp",
		"https://bot:" + tok + "@h/mcp?mode=x":  "https://***@h/mcp?mode=x",
		"https://h/mcp?key=" + tok + "&mode=x":  "https://h/mcp?key=***&mode=x",
		"https://h/mcp#tok=" + tok:              "https://h/mcp#***",
		"https://h/" + tok + "/mcp":             "https://h/***/mcp",
		tok:                                     "***",
		"tmpl:https://${env:H}/" + tok + "/mcp": "https://${H}/***/mcp",
		"tmpl:https://${env:H}/mcp":             "https://${H}/mcp",
	} {
		got := RedactURL(in)
		if strings.Contains(got, tok) {
			t.Errorf("RedactURL(%q) leaks the secret: %q", in, got)
		}
		if got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// M3: OpenCode substitutes {file:path} (and any {word:...} form) in the whole config
// text. A literal holding one is never written to OpenCode (it would read a file),
// in any field including command and cwd; an OpenCode {file:} value survives an
// OpenCode round trip and is reported elsewhere.
func TestOpenCodeFileSubstitution(t *testing.T) {
	src := `[mcp_servers.s]
command = "run"
args = ["--key", "{file:~/.ssh/id_rsa}"]
cwd = "{file:/etc/cwd}"
env = { F = "{file:/etc/secret}", OK = "plain" }

[mcp_servers.c]
command = "{env:HOME}/bin/srv"

[mcp_servers.r]
url = "https://h/mcp"
http_headers = { X = "{file:~/.tok}" }
`
	servers, err := ParseDoc(FlavorCodex, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	data, rep, err := MergeInto(FlavorOpenCode, nil, servers, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "{file:") || strings.Contains(string(data), "{env:HOME}") {
		t.Errorf("opencode: substitution literal written:\n%s", data)
	}
	for _, f := range []string{"args[1]", "env[F]", "working_dir", "headers[X]"} {
		if !hasDropReason(rep.Drops, f, ReasonUnrepresentable) {
			t.Errorf("opencode: %s not reported: %+v", f, rep.Drops)
		}
	}
	if !hasSkippedReason(rep.Skipped, "c", "command not representable") {
		t.Errorf("opencode: server with a substituting command not skipped: %+v", rep.Skipped)
	}

	// OpenCode's own {file:} values round-trip and are reported for other targets.
	oc := `{"mcp":{"s":{"type":"local","command":["run","--key={file:~/.k}"],"environment":{"F":"{file:/etc/x}"}}}}`
	servers, err = ParseDoc(FlavorOpenCode, []byte(oc))
	if err != nil {
		t.Fatal(err)
	}
	data, rep, err = MergeInto(FlavorOpenCode, nil, servers, false)
	if err != nil || len(rep.Drops) != 0 {
		t.Fatalf("opencode self: %v %+v", err, rep.Drops)
	}
	if !strings.Contains(string(data), `"--key={file:~/.k}"`) || !strings.Contains(string(data), `"{file:/etc/x}"`) {
		t.Errorf("opencode self round-trip lost {file:}:\n%s", data)
	}
	for _, dst := range []Flavor{FlavorClaude, FlavorPi, FlavorCodex} {
		data, rep, _ := MergeInto(dst, nil, servers, false)
		if strings.Contains(string(data), "{file:") {
			t.Errorf("%s: {file:} written as literal text:\n%s", dst, data)
		}
		if !hasDropReason(rep.Drops, "env[F]", ReasonUnrepresentable) || !hasDropReason(rep.Drops, "args[0]", ReasonUnrepresentable) {
			t.Errorf("%s: {file:} values not reported: %+v", dst, rep.Drops)
		}
	}
}

// A literal that collides with a neutral marker (tmpl:, cmd:, env:, file:) stays a
// literal through a conversion.
func TestNeutralMarkerLiteralsStayLiteral(t *testing.T) {
	doc := `{"mcpServers":{"s":{"command":"run","args":["tmpl:${env:X}"],"env":{"A":"env:HOME","B":"file:/etc/x","C":"tmpl:plain","D":"cmd:ls"}}}}`
	servers, err := ParseDoc(FlavorClaude, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, dst := range []Flavor{FlavorClaude, FlavorCodex, FlavorPi} {
		data, rep, err := MergeInto(dst, nil, servers, false)
		if err != nil {
			t.Fatal(err)
		}
		n := nativeServer(t, dst, data, "s")
		env, _ := n["env"].(map[string]any)
		for k, v := range map[string]string{"A": "env:HOME", "B": "file:/etc/x", "C": "tmpl:plain", "D": "cmd:ls"} {
			if env[k] != v {
				t.Errorf("%s: env[%s] = %v, want %q (drops %+v)", dst, k, env[k], v, rep.Drops)
			}
		}
	}
}
