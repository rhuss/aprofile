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

// T093: a value that is an environment variable name (Codex env_key =
// "OPENAI_API_KEY") names where the secret lives; it is not the secret. The value
// rule is strict: an upper case identifier holding an underscore, at most 64
// characters, without a known secret prefix.
func TestLooksLikeEnvVarName(t *testing.T) {
	for v, want := range map[string]bool{
		"OPENAI_API_KEY":               true,
		"_X":                           true,
		"A1_B2":                        true,
		"openai_api_key":               false, // lower case: not the env name convention
		"OpenAI":                       false,
		"1ABC":                         false,
		"":                             false,
		"sk-abc":                       false,
		"ABC DEF":                      false,
		"ABC-DEF":                      false,
		"HUNTER2":                      false, // no underscore
		"JBSWY3DPEHPK3PXP":             false, // base32 TOTP seed: no underscore
		"AKIA_IOSFODNN7_EXAMP":         false, // known secret prefix
		"A_" + strings.Repeat("B", 63): false, // longer than 64
	} {
		if got := secret.LooksLikeEnvVarName(v); got != want {
			t.Errorf("LooksLikeEnvVarName(%q) = %v, want %v", v, got, want)
		}
	}
}

// T093 follow-up: the exemption needs a key that names a variable.
func TestKeyNamesEnvVar(t *testing.T) {
	for k, want := range map[string]bool{
		"env_key":              true,
		"api_key_env":          true,
		"token_env_var":        true,
		"api_key_var":          true,
		"envKey":               true,
		"bearer_token_env_var": true,
		"apiKeyVar":            true,
		"api_key":              false,
		"token":                false,
		"password":             false,
		"secret":               false,
		"apiKey":               false,
	} {
		if got := secret.KeyNamesEnvVar(k); got != want {
			t.Errorf("KeyNamesEnvVar(%q) = %v, want %v", k, got, want)
		}
	}
}

// T093: the credential-key rule skips env-var-name values only under a key that
// names a variable, keeps every other literal, and the prefix check still applies
// to name-shaped values. All-caps secrets under plain credential keys still count.
func TestSecretUnderCredentialKey(t *testing.T) {
	cases := []struct {
		key, value string
		want       bool
	}{
		{"env_key", "OPENAI_API_KEY", false},
		{"api_key_env", "MY_TOKEN_VAR", false},
		{"apiKey", "MY_TOKEN_VAR", true}, // the key does not name a variable
		{"env_key", "abc123", true},
		{"env_key", "HUNTER2", true}, // no underscore: not a variable name
		{"token", "s3cr3t-value", true},
		{"apiKey", "env:OPENAI_API_KEY", false},
		{"apiKey", "${OPENAI_API_KEY}", false},
		{"theme", "abc123", false},
		{"env_key", "", false},
		// All-caps secrets under credential keys warn again.
		{"password", "HUNTER2", true},
		{"api_key", "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP", true},
		{"token", "ABCDEF0123456789ABCDEF0123456789ABCDEF01", true},
		{"secret", "X9K2_PROD_KEY_2024", true},
	}
	for _, c := range cases {
		if got := secret.SecretUnderCredentialKey(c.key, c.value); got != c.want {
			t.Errorf("SecretUnderCredentialKey(%q, %q) = %v, want %v", c.key, c.value, got, c.want)
		}
	}
}
