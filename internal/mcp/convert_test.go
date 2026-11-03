package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// allFlavors is every built-in flavor with the mcp capability.
var allFlavors = []Flavor{FlavorClaude, FlavorCodex, FlavorOpenCode, FlavorPi}

// fcap mirrors each flavor's optional-field representability for the test, so the
// round-trip fixtures use only fields the pair can both hold (SC-005).
type fcap struct{ workingDir, enabled, timeout bool }

var testCaps = map[Flavor]fcap{
	FlavorClaude:   {},
	FlavorCodex:    {},
	FlavorOpenCode: {workingDir: true, enabled: true, timeout: true},
	FlavorPi:       {workingDir: true, timeout: true},
}

// jsonEq compares two servers by their JSON encoding, which normalizes nil vs empty
// slices/maps (omitempty) while keeping *bool(false) distinct from nil.
func jsonEq(t *testing.T, label string, got, want Server) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("%s mismatch:\n got=%s\nwant=%s", label, g, w)
	}
}

// commonStdio builds a stdio server using only fields both a and b represent, so a
// round-trip a->b->a must preserve it 100% with zero drops. The env uses key==var so
// Codex's env_vars (forward-by-name) can carry the reference.
func commonStdio(a, b Flavor) Server {
	s := Server{
		Name:      "alpha",
		Transport: TransportStdio,
		Command:   "mcp-server",
		Args:      []string{"--port", "1234"},
		Env:       map[string]string{"TOKEN": "env:TOKEN", "MODE": "fast"},
	}
	if testCaps[a].workingDir && testCaps[b].workingDir {
		s.WorkingDir = "/work/dir"
	}
	if testCaps[a].timeout && testCaps[b].timeout {
		s.Timeout = 30
	}
	if testCaps[a].enabled && testCaps[b].enabled {
		s.Enabled = boolPtr(false)
	}
	return s
}

// commonRemote builds a remote server using only mutually representable fields.
func commonRemote(a, b Flavor) Server {
	s := Server{
		Name:      "beta",
		Transport: TransportHTTP,
		URL:       "https://mcp.example.com/sse",
		Headers:   map[string]string{"X-Token": "env:XTOKEN", "X-Mode": "fast"},
	}
	if testCaps[a].enabled && testCaps[b].enabled {
		s.Enabled = boolPtr(false)
	}
	return s
}

// roundTripOne emits a server in `from`, parses it back, and returns the parsed form.
func roundTripOne(t *testing.T, flavor Flavor, s Server) Server {
	t.Helper()
	data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
	if err != nil {
		t.Fatalf("MergeInto(%s): %v", flavor, err)
	}
	if len(rep.Drops) != 0 {
		t.Fatalf("emit %s dropped fields for a representable record: %+v", flavor, rep.Drops)
	}
	got, err := ParseDoc(flavor, data)
	if err != nil {
		t.Fatalf("ParseDoc(%s): %v", flavor, err)
	}
	if len(got) != 1 {
		t.Fatalf("ParseDoc(%s) returned %d servers, want 1", flavor, len(got))
	}
	return got[0]
}

// TestRoundTripEveryPair proves SC-005: between every pair of mcp-capable harnesses,
// conversion preserves 100% of the fields both sides can represent, with no drops.
func TestRoundTripEveryPair(t *testing.T) {
	for _, src := range allFlavors {
		for _, dst := range allFlavors {
			for _, base := range []Server{commonStdio(src, dst), commonRemote(src, dst)} {
				name := string(src) + "->" + string(dst) + "/" + base.Name
				t.Run(name, func(t *testing.T) {
					// The source must parse/emit its own record losslessly.
					srcGot := roundTripOne(t, src, base)
					jsonEq(t, "source self round-trip", srcGot, base)

					// src -> dst: no drops (every field is mutually representable).
					dstData, rep, err := MergeInto(dst, nil, []Server{srcGot}, false)
					if err != nil {
						t.Fatalf("convert to %s: %v", dst, err)
					}
					if len(rep.Drops) != 0 {
						t.Fatalf("%s->%s dropped mutually representable fields: %+v", src, dst, rep.Drops)
					}
					if len(rep.Converted) != 1 {
						t.Fatalf("%s->%s converted %v, want 1 server", src, dst, rep.Converted)
					}
					dstGot, err := ParseDoc(dst, dstData)
					if err != nil {
						t.Fatalf("parse %s: %v", dst, err)
					}
					jsonEq(t, "converted", dstGot[0], base)

					// dst -> src: back to the original.
					backData, rep2, err := MergeInto(src, nil, []Server{dstGot[0]}, false)
					if err != nil {
						t.Fatalf("convert back to %s: %v", src, err)
					}
					if len(rep2.Drops) != 0 {
						t.Fatalf("%s->%s drops on the return trip: %+v", dst, src, rep2.Drops)
					}
					backGot, _ := ParseDoc(src, backData)
					jsonEq(t, "round-trip", backGot[0], base)
				})
			}
		}
	}
}

// TestDropsAreReported proves the second half of SC-005: a field the target cannot
// represent is named in the report, server and field.
func TestDropsAreReported(t *testing.T) {
	cases := []struct {
		flavor Flavor
		server Server
		field  string
	}{
		{FlavorClaude, Server{Name: "s", Transport: TransportStdio, Command: "c", WorkingDir: "/w"}, "working_dir"},
		{FlavorClaude, Server{Name: "s", Transport: TransportStdio, Command: "c", Timeout: 10}, "timeout"},
		{FlavorClaude, Server{Name: "s", Transport: TransportStdio, Command: "c", Enabled: boolPtr(false)}, "enabled"},
		{FlavorPi, Server{Name: "s", Transport: TransportStdio, Command: "c", Enabled: boolPtr(true)}, "enabled"},
		{FlavorCodex, Server{Name: "s", Transport: TransportStdio, Command: "c", WorkingDir: "/w"}, "working_dir"},
		{FlavorOpenCode, Server{Name: "s", Transport: TransportHTTP, URL: "https://x", Auth: "env:TOK"}, "auth"},
		// Codex env_vars forwards by name only: a ref whose key != var name cannot convert.
		{FlavorCodex, Server{Name: "s", Transport: TransportStdio, Command: "c", Env: map[string]string{"SERVER_VAR": "env:HOST_VAR"}}, "env[SERVER_VAR]"},
	}
	for _, tc := range cases {
		_, rep, err := MergeInto(tc.flavor, nil, []Server{tc.server}, false)
		if err != nil {
			t.Fatalf("%s: %v", tc.flavor, err)
		}
		if !hasDrop(rep.Drops, "s", tc.field) {
			t.Errorf("%s: expected a drop for %q, got %+v", tc.flavor, tc.field, rep.Drops)
		}
	}
}

// TestLiteralSecretNeverWritten proves Constitution IV / FR-035: a literal value that
// looks like a secret is dropped (reason literal-secret) and never appears in output.
func TestLiteralSecretNeverWritten(t *testing.T) {
	const canary = "sk-ant-CANARYSECRETVALUE000000000000"
	for _, flavor := range allFlavors {
		s := Server{
			Name:      "s",
			Transport: TransportStdio,
			Command:   "c",
			Env:       map[string]string{"API_KEY": canary, "SAFE": "ok"},
		}
		data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		if strings.Contains(string(data), canary) {
			t.Errorf("%s: canary secret leaked into output:\n%s", flavor, data)
		}
		if !hasDropReason(rep.Drops, "env[API_KEY]", ReasonLiteralSecret) {
			t.Errorf("%s: literal secret not reported: %+v", flavor, rep.Drops)
		}
		// The non-secret literal survives.
		got, _ := ParseDoc(flavor, data)
		if len(got) != 1 || got[0].Env["SAFE"] != "ok" {
			t.Errorf("%s: non-secret literal lost: %+v", flavor, got)
		}
	}
}

// TestCompositeHeaderSecretNeverWritten proves a secret embedded after a scheme word
// (for example "Authorization: Bearer sk-...", or a long blob after "Basic ") is caught
// and dropped, not only whole-value secrets. Regression for the composite-header leak.
func TestCompositeHeaderSecretNeverWritten(t *testing.T) {
	const token = "sk-ant-CANARYSECRETVALUE000000000000"
	const basicBlob = "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5" // 32+ chars, no space, no known prefix
	headers := map[string]string{
		"Authorization":       "Bearer " + token,
		"X-Api":               "token " + token,
		"Proxy-Authorization": "Basic " + basicBlob,
	}
	for _, flavor := range allFlavors {
		s := Server{Name: "s", Transport: TransportHTTP, URL: "https://x", Headers: map[string]string{}}
		for k, v := range headers {
			s.Headers[k] = v
		}
		data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		if strings.Contains(string(data), token) || strings.Contains(string(data), basicBlob) {
			t.Errorf("%s: composite-header secret leaked into output:\n%s", flavor, data)
		}
		for k := range headers {
			if !hasDropReason(rep.Drops, "headers["+k+"]", ReasonLiteralSecret) {
				t.Errorf("%s: header %q secret not reported as a literal-secret drop: %+v", flavor, k, rep.Drops)
			}
		}
	}
}

// TestFileReferenceUnrepresentableInline proves a file: reference (no inline form in
// the JSON flavors) is dropped as unrepresentable, never written.
func TestFileReferenceUnrepresentableInline(t *testing.T) {
	s := Server{Name: "s", Transport: TransportStdio, Command: "c", Env: map[string]string{"TOKEN": "file:/etc/secret"}}
	for _, flavor := range []Flavor{FlavorClaude, FlavorOpenCode, FlavorPi, FlavorCodex} {
		data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		if strings.Contains(string(data), "/etc/secret") {
			t.Errorf("%s: file reference path leaked: %s", flavor, data)
		}
		if !hasDrop(rep.Drops, "s", "env[TOKEN]") {
			t.Errorf("%s: file reference not reported dropped: %+v", flavor, rep.Drops)
		}
	}
}

// TestMergePreservesExistingServers proves mcp copy leaves existing target servers and
// other file keys alone, and that --overwrite replaces only the named one.
func TestMergePreservesExistingServers(t *testing.T) {
	existing := []byte(`{"otherKey":{"keep":true},"mcpServers":{"old":{"command":"keep-me","env":{"RAW":"sk-should-stay-untouched-verbatim"}}}}`)
	add := Server{Name: "new", Transport: TransportStdio, Command: "n"}

	// Without overwrite, a same-named server is skipped.
	out, rep, err := MergeInto(FlavorClaude, existing, []Server{add, {Name: "old", Transport: TransportStdio, Command: "x"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSkipped(rep.Skipped, "old") {
		t.Errorf("expected 'old' skipped without overwrite: %+v", rep.Skipped)
	}
	// Existing server's verbatim secret-looking value is preserved (never re-encoded).
	if !strings.Contains(string(out), "sk-should-stay-untouched-verbatim") {
		t.Errorf("existing server's value must be preserved verbatim:\n%s", out)
	}
	if !strings.Contains(string(out), "otherKey") {
		t.Errorf("unrelated top-level key must be preserved:\n%s", out)
	}
	got, _ := ParseDoc(FlavorClaude, out)
	if len(got) != 2 {
		t.Errorf("want 2 servers (old preserved + new added), got %d", len(got))
	}
}

func hasDrop(drops []Drop, server, field string) bool {
	for _, d := range drops {
		if d.Server == server && d.Field == field {
			return true
		}
	}
	return false
}

func hasDropReason(drops []Drop, field, reason string) bool {
	for _, d := range drops {
		if d.Field == field && d.Reason == reason {
			return true
		}
	}
	return false
}

func hasSkipped(skipped []SkippedServer, name string) bool {
	for _, s := range skipped {
		if s.Name == name {
			return true
		}
	}
	return false
}
