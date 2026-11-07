package mcp

import (
	"strings"
	"testing"
)

// Adjacent references, references separated only by letters, many references and a
// reference in the port must neither panic nor lose a reference, and an ordinary long
// path after a port reference is not a secret (deep review round 3).
func TestTemplateURLShapes(t *testing.T) {
	cases := []struct{ in, claude string }{
		{"tmpl:https://${env:A}${env:B}/mcp", "https://${A}${B}/mcp"},
		{"tmpl:https://h.example.com/${env:A}abc${env:B}/mcp", "https://h.example.com/${A}abc${B}/mcp"},
		{"tmpl:https://${env:A}${env:B}${env:C}${env:D}${env:E}/mcp", "https://${A}${B}${C}${D}${E}/mcp"},
		{"tmpl:http://localhost:${env:PORT}/api/v1/model-context-protocol/endpoint", "http://localhost:${PORT}/api/v1/model-context-protocol/endpoint"},
		{"tmpl:http://localhost:${env:PORT}/mcp/servers/github?toolsets=repos,issues,pull_requests", "http://localhost:${PORT}/mcp/servers/github?toolsets=repos,issues,pull_requests"},
	}
	for _, tc := range cases {
		if got := RedactURL(tc.in); got != tc.claude {
			t.Errorf("RedactURL(%q) = %q, want %q", tc.in, got, tc.claude)
		}
		s := Server{Name: "r", Transport: TransportHTTP, URL: tc.in}
		data, rep, err := MergeInto(FlavorClaude, nil, []Server{s}, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Drops) != 0 {
			t.Errorf("%s: unexpected drops %+v", tc.in, rep.Drops)
		}
		if r := nativeServer(t, FlavorClaude, data, "r"); r == nil || r["url"] != tc.claude {
			t.Errorf("%s: claude url = %v, want %s", tc.in, r, tc.claude)
		}
		if _, err := SecretWarnings(FlavorClaude, data); err != nil {
			t.Errorf("%s: SecretWarnings: %v", tc.in, err)
		}
	}
}

// A secret next to adjacent references or behind a port reference is still removed,
// and every reference survives.
func TestTemplateURLShapeSecrets(t *testing.T) {
	cases := []struct{ in, field, claude string }{
		{"tmpl:https://${env:A}${env:B}/mcp?api_key=" + tok40, "url.query[api_key]", "https://${A}${B}/mcp"},
		{"tmpl:http://localhost:${env:PORT}/mcp?key=" + tok40 + "&m=x", "url.query[key]", "http://localhost:${PORT}/mcp?m=x"},
	}
	for _, tc := range cases {
		for _, flavor := range allFlavors {
			s := Server{Name: "r", Transport: TransportHTTP, URL: tc.in}
			data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), tok40) {
				t.Errorf("%s %s: secret leaked:\n%s", flavor, tc.in, data)
			}
			if !hasDropReason(rep.Drops, tc.field, ReasonLiteralSecret) {
				t.Errorf("%s %s: %s not reported: %+v", flavor, tc.in, tc.field, rep.Drops)
			}
			if flavor == FlavorClaude {
				if r := nativeServer(t, flavor, data, "r"); r == nil || r["url"] != tc.claude {
					t.Errorf("claude %s: url = %v, want %s", tc.in, r, tc.claude)
				}
			}
		}
		if got := RedactURL(tc.in); strings.Contains(got, tok40) {
			t.Errorf("RedactURL(%q) leaked the secret: %q", tc.in, got)
		}
	}
}

// An unparseable url (stray '%', bad port, two references in the port) must not let a
// literal secret through: it is dropped from every flavor and masked in RedactURL.
func TestUnparseableURLSecrets(t *testing.T) {
	urls := []string{
		"https://h.example.com/mcp?api_key=" + tok40 + "#50%",
		"https://h.example.com/100%/mcp?api_key=" + tok40,
		"https://h.example.com:abc/mcp?api_key=" + tok40,
		"https://user:" + tok40 + "@h.example.com/%zz",
		"https://h.example.com/%zz#" + tok40,
		"https://h.example.com/%zz/" + tok40,
		"tmpl:https://h.example.com/${env:A}/mcp?api_key=" + tok40 + "#50%",
		"tmpl:http://h:${env:A}${env:B}/mcp?key=" + tok40,
	}
	for _, in := range urls {
		for _, flavor := range allFlavors {
			s := Server{Name: "r", Transport: TransportHTTP, URL: in}
			data, _, err := MergeInto(flavor, nil, []Server{s}, false)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), tok40) {
				t.Errorf("%s %q: secret written:\n%s", flavor, in, data)
			}
		}
		if got := RedactURL(in); strings.Contains(got, tok40) {
			t.Errorf("RedactURL(%q) leaked the secret: %q", in, got)
		}
	}
	// An ordinary unparseable url without a secret is kept.
	keep := "https://h.example.com/100%/mcp/servers/github-integration"
	s := Server{Name: "r", Transport: TransportHTTP, URL: keep}
	data, rep, err := MergeInto(FlavorClaude, nil, []Server{s}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r := nativeServer(t, FlavorClaude, data, "r"); r == nil || r["url"] != keep || len(rep.Drops) != 0 {
		t.Errorf("ordinary unparseable url not kept: %v %+v", r, rep.Drops)
	}
}
