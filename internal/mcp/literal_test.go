package mcp

import (
	"strings"
	"testing"
)

// Canaries for args/url literal secrets (FR-035, T061). Each matches the FR-028
// prefix check; argBlob matches only the length check.
const (
	argToken   = "ghp_CANARYARGTOKEN0000000000000000000000"
	argEqToken = "sk-ant-CANARYARGEQ000000000000000000"
	argBlob    = "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5"
	urlKey     = "sk-ant-api03-CANARYURLQUERY000000000000"
	urlPass    = "ghp_CANARYURLPASSWORD00000000000000000000"
)

// TestArgsLiteralSecretNeverWritten proves every args element is checked (prefix and
// length, also after a `--flag=` prefix): a hit is dropped as literal-secret naming
// the element, and non-secret args survive.
func TestArgsLiteralSecretNeverWritten(t *testing.T) {
	for _, flavor := range allFlavors {
		s := Server{
			Name:      "s",
			Transport: TransportStdio,
			Command:   "c",
			Args:      []string{"--token", argToken, "--api-key=" + argEqToken, argBlob, "--port", "1234"},
		}
		data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		for _, c := range []string{argToken, argEqToken, argBlob} {
			if strings.Contains(string(data), c) {
				t.Errorf("%s: args secret %q leaked into output:\n%s", flavor, c[:8], data)
			}
		}
		for _, f := range []string{"args[1]", "args[2]", "args[3]"} {
			if !hasDropReason(rep.Drops, f, ReasonLiteralSecret) {
				t.Errorf("%s: %s not reported as literal-secret: %+v", flavor, f, rep.Drops)
			}
		}
		// args[1] and args[3] are positional: dropping them would shift the rest, so
		// the server is skipped (see TestDroppedArgSkipsServer).
		if !hasSkippedReason(rep.Skipped, "s", "args[1]") {
			t.Errorf("%s: server with dropped positional args not skipped: %+v", flavor, rep)
		}
		// With only the `--flag=value` secret, the remaining args are preserved.
		s.Args = []string{"--api-key=" + argEqToken, "--port", "1234"}
		data, rep, err = MergeInto(flavor, nil, []Server{s}, false)
		if err != nil || !hasDropReason(rep.Drops, "args[0]", ReasonLiteralSecret) {
			t.Fatalf("%s: %v %+v", flavor, err, rep)
		}
		got, _ := ParseDoc(flavor, data)
		if len(got) != 1 || strings.Join(got[0].Args, " ") != "--port 1234" {
			t.Errorf("%s: non-secret args not preserved: %+v", flavor, got)
		}
	}
}

// TestURLLiteralSecretNeverWritten proves the url is checked in its userinfo and per
// query parameter: offending parts are removed and reported, the rest of the URL kept.
func TestURLLiteralSecretNeverWritten(t *testing.T) {
	for _, flavor := range allFlavors {
		s := Server{
			Name:      "s",
			Transport: TransportHTTP,
			URL:       "https://bot:" + urlPass + "@mcp.example.com/v1/mcp?key=" + urlKey + "&mode=fast",
		}
		data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		for _, c := range []string{urlKey, urlPass} {
			if strings.Contains(string(data), c) {
				t.Errorf("%s: url secret leaked into output:\n%s", flavor, data)
			}
		}
		if !hasDropReason(rep.Drops, "url.query[key]", ReasonLiteralSecret) {
			t.Errorf("%s: query secret not reported: %+v", flavor, rep.Drops)
		}
		if !hasDropReason(rep.Drops, "url.userinfo", ReasonLiteralSecret) {
			t.Errorf("%s: userinfo secret not reported: %+v", flavor, rep.Drops)
		}
		got, _ := ParseDoc(flavor, data)
		if len(got) != 1 || got[0].URL != "https://mcp.example.com/v1/mcp?mode=fast" {
			t.Errorf("%s: url not reduced to its non-secret parts: %+v", flavor, got)
		}
	}
}

// TestURLSecretKeyNotNamedInReport proves a query parameter whose name is itself the
// secret is reported by position, so the report never contains the secret.
func TestURLSecretKeyNotNamedInReport(t *testing.T) {
	s := Server{Name: "s", Transport: TransportHTTP, URL: "https://mcp.example.com/?a=1&" + urlKey}
	_, rep, err := MergeInto(FlavorClaude, nil, []Server{s}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDropReason(rep.Drops, "url.query[#1]", ReasonLiteralSecret) {
		t.Errorf("secret query key not reported by position: %+v", rep.Drops)
	}
	for _, d := range rep.Drops {
		if strings.Contains(d.Field, urlKey) {
			t.Errorf("report names the secret: %+v", d)
		}
	}
}

// TestLongURLWithoutSecretKept guards against applying the length check to the whole
// URL: an ordinary long URL is not a secret and must convert without drops.
func TestLongURLWithoutSecretKept(t *testing.T) {
	const u = "https://mcp.example.com/api/v1/some/rather/long/endpoint/path?mode=fast"
	for _, flavor := range allFlavors {
		s := Server{Name: "s", Transport: TransportHTTP, URL: u}
		data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		if len(rep.Drops) != 0 {
			t.Errorf("%s: unexpected drops for a plain URL: %+v", flavor, rep.Drops)
		}
		got, _ := ParseDoc(flavor, data)
		if len(got) != 1 || got[0].URL != u {
			t.Errorf("%s: url changed: %+v", flavor, got)
		}
	}
}

// TestUnmodeledNativeFieldsReported proves every decoder collects native fields the
// neutral record does not model and a cross-flavor emit reports each as an
// unrepresentable drop instead of discarding it silently.
func TestUnmodeledNativeFieldsReported(t *testing.T) {
	cases := []struct {
		flavor Flavor
		doc    string
		fields []string
	}{
		{FlavorClaude, `{"mcpServers":{"s":{"command":"c","cwd":"/w","timeout":30000}}}`, []string{"cwd", "timeout"}},
		{FlavorClaude, `{"mcpServers":{"s":{"type":"http","url":"https://x","oauth":{"clientId":"abc"}}}}`, []string{"oauth"}},
		// enabled_tools is modeled (tool_allow), which these targets cannot hold.
		{FlavorCodex, "[mcp_servers.s]\ncommand = \"c\"\nstartup_timeout_sec = 20\nrequired = true\nenabled_tools = [\"a\"]\n", []string{"startup_timeout_sec", "required", "tool_allow/tool_deny"}},
		{FlavorOpenCode, `{"mcp":{"s":{"type":"remote","url":"https://x","oauth":false}}}`, []string{"oauth"}},
		{FlavorPi, `{"mcpServers":{"s":{"type":"stdio","command":"c","lifecycle":"lazy"}}}`, []string{"lifecycle"}},
	}
	for _, tc := range cases {
		servers, err := ParseDoc(tc.flavor, []byte(tc.doc))
		if err != nil {
			t.Fatalf("%s: parse: %v", tc.flavor, err)
		}
		for _, dst := range allFlavors {
			if dst == tc.flavor {
				continue
			}
			_, rep, err := MergeInto(dst, nil, servers, false)
			if err != nil {
				t.Fatalf("%s->%s: %v", tc.flavor, dst, err)
			}
			for _, f := range tc.fields {
				if !hasDropReason(rep.Drops, f, ReasonUnrepresentable) {
					t.Errorf("%s->%s: unmodeled field %q not reported: %+v", tc.flavor, dst, f, rep.Drops)
				}
			}
		}
	}
}

// TestUnmodeledFieldsSurviveSameFlavor proves a same-flavor emit (mcp copy between
// two profiles of one harness) carries unmodeled native fields instead of dropping
// them, while still dropping one that holds a literal secret.
func TestUnmodeledFieldsSurviveSameFlavor(t *testing.T) {
	doc := `{"mcpServers":{"s":{"command":"c","cwd":"/w","oauth":{"clientSecret":"` + argEqToken + `"}}}}`
	servers, err := ParseDoc(FlavorClaude, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	out, rep, err := MergeInto(FlavorClaude, nil, servers, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"cwd":"/w"`) && !strings.Contains(string(out), `"cwd": "/w"`) {
		t.Errorf("same-flavor emit lost cwd:\n%s", out)
	}
	if strings.Contains(string(out), argEqToken) {
		t.Errorf("secret in an unmodeled field leaked:\n%s", out)
	}
	if !hasDropReason(rep.Drops, "oauth", ReasonLiteralSecret) {
		t.Errorf("secret-bearing unmodeled field not reported: %+v", rep.Drops)
	}
	if hasDrop(rep.Drops, "s", "cwd") {
		t.Errorf("cwd must not be dropped on a same-flavor emit: %+v", rep.Drops)
	}
}

// TestSecretWarningsCoverArgsAndURL proves a same-harness copy warns about literal
// secrets in args and url, not only env and headers (FR-033a).
func TestSecretWarningsCoverArgsAndURL(t *testing.T) {
	doc := `{"mcpServers":{
	  "a":{"command":"c","args":["--token","` + argToken + `"]},
	  "r":{"type":"http","url":"https://mcp.example.com/?key=` + urlKey + `"}}}`
	ws, err := SecretWarnings(FlavorClaude, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"a/args[1]": false, "r/url.query[key]": false}
	for _, w := range ws {
		if _, ok := want[w.Server+"/"+w.Field]; ok {
			want[w.Server+"/"+w.Field] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing secret warning %s: %+v", k, ws)
		}
	}
}

// Long paths, scoped packages and relative file names in args are not secrets: they
// get the prefix check only, so common server commands survive a conversion.
func TestArgsPathsAreNotSecrets(t *testing.T) {
	keep := []string{
		"/Users/someone/projects/mcp/server/dist/index.js",
		"@modelcontextprotocol/server-filesystem",
		"packages/server-everything/dist/index.mjs",
		"--config=/Users/someone/.config/some-server/settings.json",
	}
	out, drops := encodeArgs("s", keep, nil)
	if len(drops) != 0 || len(out) != len(keep) {
		t.Errorf("paths must be kept: out=%v drops=%v", out, drops)
	}
	_, drops = encodeArgs("s", []string{"/tmp/ghp_abcdefghijklmnopqrstuvwxyz0123456789", "Zx9Qw8Er7Ty6Ui5Op4As3Df2Gh1Jk0LmNbVcXz"}, nil)
	if len(drops) != 2 {
		t.Errorf("a token prefix in a path and a long opaque value are still secrets: %v", drops)
	}
}
