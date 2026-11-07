package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// nativeServer returns one server's native object from a converted document.
func nativeServer(t *testing.T, flavor Flavor, data []byte, name string) map[string]any {
	t.Helper()
	top := map[string]any{}
	var err error
	if flavor == FlavorCodex {
		err = toml.Unmarshal(data, &top)
	} else {
		err = json.Unmarshal(data, &top)
	}
	if err != nil {
		t.Fatalf("%s: decode output: %v\n%s", flavor, err, data)
	}
	key := map[Flavor]string{FlavorClaude: "mcpServers", FlavorPi: "mcpServers", FlavorOpenCode: "mcp", FlavorCodex: "mcp_servers"}[flavor]
	servers, _ := top[key].(map[string]any)
	s, _ := servers[name].(map[string]any)
	return s
}

// noDeadRef fails when a converted document holds a reference token the target
// cannot expand: Codex expands nothing in strings, so neither `${` nor `{env:` may
// appear; a JSON flavor must not carry the other flavor's token.
func noDeadRef(t *testing.T, flavor Flavor, data []byte) {
	t.Helper()
	out := string(data)
	switch flavor {
	case FlavorCodex:
		if strings.Contains(out, "${") || strings.Contains(out, "{env:") {
			t.Errorf("codex output carries a dead reference literal:\n%s", out)
		}
	case FlavorOpenCode:
		if strings.Contains(out, "${") {
			t.Errorf("opencode output carries a dead ${} literal:\n%s", out)
		}
	default:
		if strings.Contains(out, "{env:") {
			t.Errorf("%s output carries a dead {env:} literal:\n%s", flavor, out)
		}
	}
}

// us4Source is the US4 independent-test profile in each source flavor: a stdio
// server with env references, a remote server with a header from an env variable,
// and a disabled server (where the flavor can express one).
var us4Source = map[Flavor]string{
	FlavorClaude: `{"mcpServers":{
		"local":{"command":"mcp-local","args":["--port","1234"],"env":{"API_KEY":"${API_KEY}","MODE":"fast"}},
		"remote":{"type":"http","url":"https://mcp.example.com/mcp","headers":{"Authorization":"Bearer ${HDR_TOKEN}"}}}}`,
	FlavorOpenCode: `{"mcp":{
		"local":{"type":"local","command":["mcp-local","--port","1234"],"environment":{"API_KEY":"{env:API_KEY}","MODE":"fast"}},
		"remote":{"type":"remote","url":"https://mcp.example.com/mcp","headers":{"Authorization":"Bearer {env:HDR_TOKEN}"}},
		"off":{"type":"local","command":["mcp-off"],"enabled":false}}}`,
	FlavorPi: `{"mcpServers":{
		"local":{"type":"stdio","command":"mcp-local","args":["--port","1234"],"env":{"API_KEY":"${API_KEY}","MODE":"fast"}},
		"remote":{"type":"http","url":"https://mcp.example.com/mcp","headers":{"Authorization":"Bearer ${HDR_TOKEN}"}},
		"off":{"type":"stdio","command":"mcp-off","enabled":false}}}`,
	FlavorCodex: `[mcp_servers.local]
command = "mcp-local"
args = ["--port", "1234"]
env_vars = ["API_KEY"]
env = { MODE = "fast" }

[mcp_servers.remote]
url = "https://mcp.example.com/mcp"
bearer_token_env_var = "HDR_TOKEN"

[mcp_servers.off]
command = "mcp-off"
enabled = false
`,
}

// wantAuthHeader is the projected Authorization header per JSON target flavor.
var wantAuthHeader = map[Flavor]string{
	FlavorClaude:   "Bearer ${HDR_TOKEN}",
	FlavorPi:       "Bearer ${HDR_TOKEN}",
	FlavorOpenCode: "Bearer {env:HDR_TOKEN}",
}

// TestUS4HeaderFromEnvVariable is the US4 independent test: a profile with a stdio
// server with env references, a remote server whose Authorization header comes from
// an env variable, and a disabled server, copied to every harness. The header
// reaches each target as a working reference (Codex: bearer_token_env_var), never as
// a dead literal, and the disabled server never becomes an enabled one.
func TestUS4HeaderFromEnvVariable(t *testing.T) {
	for _, src := range allFlavors {
		servers, err := ParseDoc(src, []byte(us4Source[src]))
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		for _, dst := range allFlavors {
			t.Run(string(src)+"->"+string(dst), func(t *testing.T) {
				data, rep, err := MergeInto(dst, nil, servers, false)
				if err != nil {
					t.Fatal(err)
				}
				if len(rep.Drops) != 0 {
					t.Errorf("unexpected drops: %+v", rep.Drops)
				}
				noDeadRef(t, dst, data)

				remote := nativeServer(t, dst, data, "remote")
				if dst == FlavorCodex {
					if remote["bearer_token_env_var"] != "HDR_TOKEN" {
						t.Errorf("codex: want bearer_token_env_var=HDR_TOKEN, got %v", remote)
					}
					if _, ok := remote["http_headers"]; ok {
						t.Errorf("codex: the bearer header must not also be written as http_headers: %v", remote)
					}
				} else {
					h, _ := remote["headers"].(map[string]any)
					if h["Authorization"] != wantAuthHeader[dst] {
						t.Errorf("%s: Authorization = %v, want %q", dst, h["Authorization"], wantAuthHeader[dst])
					}
				}

				// The stdio server's env reference is a reference in the target syntax.
				local := nativeServer(t, dst, data, "local")
				switch dst {
				case FlavorCodex:
					if vars := asStringSlice(local["env_vars"]); len(vars) != 1 || vars[0] != "API_KEY" {
						t.Errorf("codex: env_vars = %v", local["env_vars"])
					}
				case FlavorOpenCode:
					if env, _ := local["environment"].(map[string]any); env["API_KEY"] != "{env:API_KEY}" {
						t.Errorf("opencode: environment = %v", local["environment"])
					}
				default:
					if env, _ := local["env"].(map[string]any); env["API_KEY"] != "${API_KEY}" {
						t.Errorf("%s: env = %v", dst, local["env"])
					}
				}

				// The disabled server: written disabled where the target can say so,
				// skipped and reported otherwise. Never enabled.
				if src == FlavorClaude {
					return // Claude's mcpServers cannot hold a disabled server
				}
				off := nativeServer(t, dst, data, "off")
				if dst == FlavorClaude {
					if off != nil {
						t.Errorf("claude: a disabled server must not be written: %v", off)
					}
					if !hasSkippedReason(rep.Skipped, "off", "disabled in source") {
						t.Errorf("claude: disabled server not reported as skipped: %+v", rep.Skipped)
					}
					return
				}
				if off["enabled"] != false {
					t.Errorf("%s: disabled server written as %v", dst, off)
				}
			})
		}
	}
}

// TestEmbeddedReferenceTranslation proves a reference inside a larger value (env,
// header, args, url) is translated into the target's in-string syntax where the
// target expands references in that field, and dropped and reported as
// unrepresentable otherwise; no dead `${X}` literal is ever written.
func TestEmbeddedReferenceTranslation(t *testing.T) {
	src := `{"mcpServers":{
		"s":{"command":"run","args":["--token=${TOK}","plain"],"env":{"DSN":"postgres://u:${DB_PASS}@db/x"}},
		"r":{"type":"http","url":"https://${MCP_HOST}/mcp","headers":{"X-Api":"key=${API_KEY};v=1"}}}}`
	servers, err := ParseDoc(FlavorClaude, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		dst     Flavor
		args0   any // expected args[0] (nil = dropped)
		dsn     any
		url     any
		header  any
		dropped []string
	}{
		{FlavorClaude, "--token=${TOK}", "postgres://u:${DB_PASS}@db/x", "https://${MCP_HOST}/mcp", "key=${API_KEY};v=1", nil},
		{FlavorOpenCode, "--token={env:TOK}", "postgres://u:{env:DB_PASS}@db/x", "https://{env:MCP_HOST}/mcp", "key={env:API_KEY};v=1", nil},
		// pi expands references in env and headers only. The remote server's url
		// cannot be written, so the whole server is skipped (its drops still reported).
		{FlavorPi, nil, "postgres://u:${DB_PASS}@db/x", nil, nil, []string{"args[0]", "url"}},
		// Codex expands nothing inside a string.
		{FlavorCodex, nil, nil, nil, nil, []string{"args[0]", "env[DSN]", "url", "headers[X-Api]"}},
	}
	for _, tc := range cases {
		data, rep, err := MergeInto(tc.dst, nil, servers, false)
		if err != nil {
			t.Fatalf("%s: %v", tc.dst, err)
		}
		noDeadRef(t, tc.dst, data)
		for _, f := range tc.dropped {
			if !hasDropReason(rep.Drops, f, ReasonUnrepresentable) {
				t.Errorf("%s: %s not reported unrepresentable: %+v", tc.dst, f, rep.Drops)
			}
		}
		if len(rep.Drops) != len(tc.dropped) {
			t.Errorf("%s: drops = %+v, want exactly %v", tc.dst, rep.Drops, tc.dropped)
		}
		got, err := ParseDoc(tc.dst, data)
		if err != nil {
			t.Fatal(err)
		}
		// Re-encode to the target's native form to compare the literal text.
		s := nativeServer(t, tc.dst, data, "s")
		r := nativeServer(t, tc.dst, data, "r")
		var args []string
		var env map[string]any
		switch tc.dst {
		case FlavorOpenCode:
			cmd := asStringSlice(s["command"])
			args = cmd[1:]
			env, _ = s["environment"].(map[string]any)
		default:
			args = asStringSlice(s["args"])
			env, _ = s["env"].(map[string]any)
		}
		if tc.args0 == nil {
			if len(args) != 1 || args[0] != "plain" {
				t.Errorf("%s: args = %v, want only the plain arg", tc.dst, args)
			}
		} else if len(args) < 1 || args[0] != tc.args0 {
			t.Errorf("%s: args = %v, want [0]=%v", tc.dst, args, tc.args0)
		}
		if env["DSN"] != tc.dsn && !(tc.dsn == nil && env["DSN"] == nil) {
			t.Errorf("%s: env DSN = %v, want %v", tc.dst, env["DSN"], tc.dsn)
		}
		if tc.url == nil {
			// A remote server without a writable url is skipped, never written
			// without its url (Codex would read it back as an empty stdio server).
			if r != nil {
				t.Errorf("%s: server written without its url: %v", tc.dst, r)
			}
			if len(rep.Converted) != 1 || !hasSkippedReason(rep.Skipped, "r", "url not representable") {
				t.Errorf("%s: r not skipped: %+v", tc.dst, rep)
			}
		} else if r["url"] != tc.url {
			t.Errorf("%s: url = %v, want %v", tc.dst, r["url"], tc.url)
		}
		h, _ := r["headers"].(map[string]any)
		if tc.dst == FlavorCodex {
			h, _ = r["http_headers"].(map[string]any)
		}
		if h["X-Api"] != tc.header && !(tc.header == nil && h["X-Api"] == nil) {
			t.Errorf("%s: header = %v, want %v", tc.dst, h["X-Api"], tc.header)
		}
		// A same-flavor round trip of the target output is lossless.
		back, rep2, err := MergeInto(tc.dst, nil, got, false)
		if err != nil || len(rep2.Drops) != 0 {
			t.Errorf("%s: self round-trip: %v %+v", tc.dst, err, rep2.Drops)
		}
		if string(back) != string(data) {
			t.Errorf("%s: self round-trip changed the document:\n%s\nvs\n%s", tc.dst, back, data)
		}
	}
}

// TestReferenceDefault proves `${X:-default}` keeps its default where the target can
// express one (Claude) and otherwise keeps the reference and reports the default as
// unrepresentable; the default text never appears in such a target.
func TestReferenceDefault(t *testing.T) {
	src := `{"mcpServers":{
		"s":{"command":"run","env":{"MODE":"${MODE:-fastdefault}","LEVEL":"lvl-${LVL:-infodefault}"}},
		"r":{"type":"http","url":"https://x/mcp","headers":{"X-Key":"${XKEY:-keydefault}","Authorization":"Bearer ${TOK:-tokdefault}"}}}}`
	servers, err := ParseDoc(FlavorClaude, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// Claude -> Claude keeps every default.
	data, rep, err := MergeInto(FlavorClaude, nil, servers, false)
	if err != nil || len(rep.Drops) != 0 {
		t.Fatalf("claude self: %v %+v", err, rep.Drops)
	}
	for _, d := range []string{"${MODE:-fastdefault}", "lvl-${LVL:-infodefault}", "${XKEY:-keydefault}", "Bearer ${TOK:-tokdefault}"} {
		if !strings.Contains(string(data), d) {
			t.Errorf("claude self round-trip lost %q:\n%s", d, data)
		}
	}

	for _, dst := range []Flavor{FlavorOpenCode, FlavorPi, FlavorCodex} {
		data, rep, err := MergeInto(dst, nil, servers, false)
		if err != nil {
			t.Fatal(err)
		}
		noDeadRef(t, dst, data)
		for _, d := range []string{"fastdefault", "infodefault", "keydefault", "tokdefault"} {
			if strings.Contains(string(data), d) {
				t.Errorf("%s: default %q written to a target that cannot express it:\n%s", dst, d, data)
			}
		}
		wantDefaultDrops := []string{"env[MODE].default", "env[LEVEL].default", "headers[X-Key].default", "headers[Authorization].default"}
		if dst == FlavorCodex {
			// MODE forwards the same-named var (env_vars, default dropped); LEVEL embeds
			// its reference, which Codex cannot express.
			wantDefaultDrops = []string{"env[MODE].default", "headers[X-Key].default", "headers[Authorization].default"}
			if !hasDropReason(rep.Drops, "env[LEVEL]", ReasonUnrepresentable) {
				t.Errorf("codex: env[LEVEL] not reported: %+v", rep.Drops)
			}
			if vars := asStringSlice(nativeServer(t, dst, data, "s")["env_vars"]); len(vars) != 1 || vars[0] != "MODE" {
				t.Errorf("codex: env_vars = %v", vars)
			}
			r := nativeServer(t, dst, data, "r")
			if r["bearer_token_env_var"] != "TOK" {
				t.Errorf("codex: bearer default form not mapped: %v", r)
			}
			if eh, _ := r["env_http_headers"].(map[string]any); eh["X-Key"] != "XKEY" {
				t.Errorf("codex: env_http_headers = %v", r["env_http_headers"])
			}
		}
		for _, f := range wantDefaultDrops {
			if !hasDropReason(rep.Drops, f, ReasonUnrepresentable) {
				t.Errorf("%s: %s not reported: %+v", dst, f, rep.Drops)
			}
		}
	}
}

// TestLiteralThatTargetWouldExpand proves a literal value that the target would read
// as a reference (a Codex literal "${HOME}" going to Claude) is dropped and reported
// rather than silently changing meaning, and that pi's `$` is escaped.
func TestLiteralThatTargetWouldExpand(t *testing.T) {
	src := `[mcp_servers.s]
command = "run"
args = ["${HOME}", "{env:HOME}", "{file:~/.ssh/id_rsa}"]
env = { PRICE = "5$US", RAW = "${HOME}", KEY = "{file:~/.ssh/id_rsa}", "{file:~/.ssh/id_rsa}" = "k" }

[mcp_servers."{file:~/.ssh/id_rsa}"]
command = "run"

[mcp_servers.h]
url = "https://h.example.com/mcp"
http_headers = { "{env:HOME}" = "v", OK = "y" }
`
	servers, err := ParseDoc(FlavorCodex, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// Claude would expand ${HOME}: args[0] and env[RAW] are unrepresentable.
	data, rep, err := MergeInto(FlavorClaude, nil, servers, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"args[0]", "env[RAW]"} {
		if !hasDropReason(rep.Drops, f, ReasonUnrepresentable) {
			t.Errorf("claude: %s not reported: %+v", f, rep.Drops)
		}
	}
	if strings.Contains(string(data), "${HOME}") {
		t.Errorf("claude: literal written where Claude would expand it:\n%s", data)
	}
	// OpenCode would expand {env:HOME} and read the file named by {file:...}.
	data, rep, _ = MergeInto(FlavorOpenCode, nil, servers, false)
	// OpenCode substitutes in object keys too: an env or header name holding a
	// substitution is dropped, a server named with one is skipped.
	for _, f := range []string{"args[1]", "args[2]", "env[KEY]", "env[{file:~/.ssh/id_rsa}]", "headers[{env:HOME}]"} {
		if !hasDropReason(rep.Drops, f, ReasonUnrepresentable) {
			t.Errorf("opencode: %s not reported: %+v", f, rep.Drops)
		}
	}
	if !hasSkippedReason(rep.Skipped, "{file:~/.ssh/id_rsa}", "server name") || strings.Contains(string(data), "{env:HOME}") {
		t.Errorf("opencode: substituting server name or header name written: %+v\n%s", rep, data)
	}
	if hasDrop(rep.Drops, "s", "args[0]") || strings.Contains(string(data), "{file:") {
		t.Errorf("opencode drops = %+v\n%s", rep.Drops, data)
	}
	// pi expands $ in env: the literal $ is escaped as $$ and parses back unchanged.
	data, rep, _ = MergeInto(FlavorPi, nil, servers, false)
	if len(rep.Drops) != 0 {
		t.Errorf("pi drops = %+v", rep.Drops)
	}
	if env, _ := nativeServer(t, FlavorPi, data, "s")["env"].(map[string]any); env["PRICE"] != "5$$US" || env["RAW"] != "$${HOME}" {
		t.Errorf("pi env = %v", env)
	}
	back, _ := ParseDoc(FlavorPi, data)
	for _, b := range back {
		if b.Name == "s" && (b.Env["PRICE"] != "5$US" || b.Env["RAW"] != "${HOME}") {
			t.Errorf("pi round-trip env = %v", b.Env)
		}
	}
}

// TestCodexServerFields proves the fields installed Codex accepts in an
// [mcp_servers.<name>] table (verified against codex-cli 0.167.0: cwd, enabled,
// tool_timeout_sec, enabled_tools, disabled_tools) convert both ways, and that a
// Codex-only field (startup_timeout_sec) survives a Codex round trip and is reported
// when converted elsewhere.
func TestCodexServerFields(t *testing.T) {
	s := Server{
		Name: "s", Transport: TransportStdio, Command: "run", WorkingDir: "/w",
		Enabled: boolPtr(false), Timeout: 45, ToolAllow: []string{"a", "b"}, ToolDeny: []string{"c"},
	}
	data, rep, err := MergeInto(FlavorCodex, nil, []Server{s}, false)
	if err != nil || len(rep.Drops) != 0 {
		t.Fatalf("codex: %v %+v", err, rep.Drops)
	}
	n := nativeServer(t, FlavorCodex, data, "s")
	if n["cwd"] != "/w" || n["enabled"] != false || n["tool_timeout_sec"] != int64(45) {
		t.Errorf("codex native = %v", n)
	}
	if got := asStringSlice(n["enabled_tools"]); len(got) != 2 || asStringSlice(n["disabled_tools"])[0] != "c" {
		t.Errorf("codex tool lists = %v", n)
	}
	back, _ := ParseDoc(FlavorCodex, data)
	jsonEq(t, "codex round-trip", back[0], s)

	native := `[mcp_servers.s]
command = "run"
startup_timeout_sec = 20
tool_timeout_sec = 90.0
`
	servers, _ := ParseDoc(FlavorCodex, []byte(native))
	if servers[0].Timeout != 90 {
		t.Errorf("tool_timeout_sec 90.0 decoded as %d", servers[0].Timeout)
	}
	data, rep, _ = MergeInto(FlavorCodex, nil, servers, false)
	if len(rep.Drops) != 0 || !strings.Contains(string(data), "startup_timeout_sec = 20") {
		t.Errorf("codex self round-trip lost startup_timeout_sec: %+v\n%s", rep.Drops, data)
	}
	_, rep, _ = MergeInto(FlavorPi, nil, servers, false)
	if !hasDropReason(rep.Drops, "startup_timeout_sec", ReasonUnrepresentable) {
		t.Errorf("pi: startup_timeout_sec not reported: %+v", rep.Drops)
	}
}

// TestDisabledServerNeverEnabled proves a disabled source server is never written as
// an enabled one: a target that cannot express enabled:false skips it and reports
// it, and a target that can writes enabled:false.
func TestDisabledServerNeverEnabled(t *testing.T) {
	off := Server{Name: "off", Transport: TransportStdio, Command: "run", Enabled: boolPtr(false)}
	for _, dst := range allFlavors {
		data, rep, err := MergeInto(dst, nil, []Server{off}, false)
		if err != nil {
			t.Fatal(err)
		}
		n := nativeServer(t, dst, data, "off")
		if !flavorCaps[dst].enabled {
			if n != nil || len(rep.Converted) != 0 || !hasSkippedReason(rep.Skipped, "off", "disabled in source") {
				t.Errorf("%s: disabled server must be skipped and reported: %v %+v", dst, n, rep)
			}
			continue
		}
		if n["enabled"] != false {
			t.Errorf("%s: disabled server written as %v", dst, n)
		}
	}
}

// TestOpenCodeTimeoutMilliseconds proves OpenCode's timeout (milliseconds, installed
// opencode 2.1.4 schema) converts to and from the neutral seconds, and a value that
// is not whole seconds is kept verbatim for OpenCode and reported elsewhere.
func TestOpenCodeTimeoutMilliseconds(t *testing.T) {
	s := Server{Name: "s", Transport: TransportStdio, Command: "run", Timeout: 30}
	data, _, _ := MergeInto(FlavorOpenCode, nil, []Server{s}, false)
	if n := nativeServer(t, FlavorOpenCode, data, "s"); n["timeout"] != float64(30000) {
		t.Errorf("opencode timeout = %v, want 30000 (ms)", n["timeout"])
	}
	servers, _ := ParseDoc(FlavorOpenCode, []byte(`{"mcp":{"s":{"type":"local","command":["run"],"timeout":1500},"r":{"type":"remote","url":"https://x","timeout":5000}}}`))
	if servers[0].Timeout != 5 || servers[1].Timeout != 0 {
		t.Errorf("decoded timeouts = %d, %d", servers[0].Timeout, servers[1].Timeout)
	}
	data, rep, _ := MergeInto(FlavorOpenCode, nil, servers, false)
	if len(rep.Drops) != 0 || !strings.Contains(string(data), `"timeout": 1500`) {
		t.Errorf("opencode self round-trip: %+v\n%s", rep.Drops, data)
	}
	_, rep, _ = MergeInto(FlavorPi, nil, servers, false)
	if !hasDropReason(rep.Drops, "timeout", ReasonUnrepresentable) {
		t.Errorf("pi: sub-second opencode timeout not reported: %+v", rep.Drops)
	}
}

func hasSkippedReason(skipped []SkippedServer, name, reason string) bool {
	for _, s := range skipped {
		if s.Name == name && strings.Contains(s.Reason, reason) {
			return true
		}
	}
	return false
}
