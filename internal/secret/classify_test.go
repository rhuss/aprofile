package secret_test

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/secret"
)

func TestLooksLikeLiteralSecret(t *testing.T) {
	long := "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	none := secret.Options{}
	paths := secret.Options{AllowPaths: true}
	refs := secret.Options{SkipReferences: true}
	prefix := secret.Options{PrefixOnly: true}
	cases := []struct {
		v    string
		o    secret.Options
		want bool
	}{
		{"", none, false},
		{"o3", none, false},
		{"sk-ant-abc", none, true},
		{"Bearer ghp_abcdef", none, true}, // token after a scheme word
		{long, none, true},
		{"Basic " + long, none, true},
		{"a long human readable sentence over thirty-two characters", none, false},
		{long, prefix, false},
		{"Bearer sk-abc", prefix, true},

		// Path/URL allowance: segments get the prefix check only.
		{"/Users/someone/projects/a/very/long/path/to/a/file.txt", none, true},
		{"/Users/someone/projects/a/very/long/path/to/a/file.txt", paths, false},
		{"https://llm.example.com/v1/some/very/long/path/segment", paths, false},
		{"@modelcontextprotocol/server-filesystem-with-a-long-name", paths, false},
		{"dist/some/very/long/relative/path/to/the/index.js", paths, false},
		{"/opt/tools/ghp_abcdef/run", paths, true},
		{"https://x.example.com/?key=sk-abc", paths, true},
		{long, paths, true}, // not a path: both checks

		// References are not literals; only their literal remainder is checked.
		{"env:OPENAI_API_KEY", refs, false},
		{"file:/run/secrets/token", refs, false},
		{"${TOKEN}", refs, false},
		{"$TOKEN", refs, false},
		{"Bearer ${TOKEN}", refs, false},
		{"${TOKEN:-sk-fallback}", refs, true}, // a literal default is still checked
		{"sk-${SUFFIX}", refs, true},
		{"env:" + long, none, true}, // without the option a reference-looking value is checked
		{"env:not a name " + long, refs, true},
	}
	for _, c := range cases {
		if got := secret.LooksLikeLiteralSecret(c.v, c.o); got != c.want {
			t.Errorf("LooksLikeLiteralSecret(%q, %+v) = %v, want %v", c.v, c.o, got, c.want)
		}
	}
}

func TestHasSecretPrefix(t *testing.T) {
	for v, want := range map[string]bool{
		"sk-abc":                         true,
		"token ghp_abc":                  true,
		strings.Repeat("a", 40):          false,
		"https://example.com/" + "aaaaa": false,
	} {
		if got := secret.HasSecretPrefix(v); got != want {
			t.Errorf("HasSecretPrefix(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestLooksLikePath(t *testing.T) {
	for v, want := range map[string]bool{
		"/abs":           true,
		"./rel":          true,
		"../up":          true,
		"~/home":         true,
		"@scope/pkg":     true,
		"https://x":      true,
		"dir/index.js":   true,
		"dir/noext":      false,
		"plain":          false,
		"a/b.toolongext": false,
		// A bare file name with a short extension reads as a path (FR-035), unless it
		// starts with a known token prefix; an extension without a letter does not count.
		"my-really-long-server-file-name-x.js":           true,
		"server.py":                                      true,
		"ghp_CANARYARGTOKEN0000000000000000000000abc.js": false,
		"AbCdEf0123456789AbCdEf0123456789XyZ":            false,
		"abcdef0123456789abcdef0123456789.12345":         false,
		"name.":                                          false,
	} {
		if got := secret.LooksLikePath(v); got != want {
			t.Errorf("LooksLikePath(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestLooksLikeSecretKey(t *testing.T) {
	for _, k := range []string{
		"primaryApiKey", "api_key", "api-key", "ANTHROPIC_API_KEY", "authToken", "oauthAccount",
		"clientSecret", "password", "passwd", "sessionKey", "userCredentials", "cookie", "private_key",
	} {
		if !secret.LooksLikeSecretKey(k) {
			t.Errorf("%q should look like a credential key", k)
		}
	}
	for _, k := range []string{"theme", "autoUpdates", "keybinds", "verbose", "editorMode", "provider", "PLAIN", "statusLine"} {
		if secret.LooksLikeSecretKey(k) {
			t.Errorf("%q should not look like a credential key", k)
		}
	}
}

func TestIsReferenceOnly(t *testing.T) {
	for v, want := range map[string]bool{
		"env:TOKEN":       true,
		"file:/x/y":       true,
		"${TOKEN}":        true,
		"$A ${B}":         true,
		"":                false,
		"abc":             false,
		"${TOKEN:-abc}":   false,
		"Bearer ${TOKEN}": false,
		"env:not valid":   false,
	} {
		if got := secret.IsReferenceOnly(v); got != want {
			t.Errorf("IsReferenceOnly(%q) = %v, want %v", v, got, want)
		}
	}
}
