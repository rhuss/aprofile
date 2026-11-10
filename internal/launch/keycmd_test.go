package launch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// wrapperResolve returns what the wrapper's `IFS= read -r` resolution of a file:
// reference yields (internal/secret/render.go ResolveShell).
func wrapperResolve(t *testing.T, path string) string {
	t.Helper()
	snippet := secret.Reference{Kind: secret.KindFile, Value: path, Raw: "file:" + path}.
		ResolveShell("K", "test/x", secret.ShellQuote, "") + `printf '%s' "$K"`
	out, err := exec.Command("/bin/sh", "-c", snippet).Output()
	if err != nil {
		t.Fatalf("wrapper resolution failed: %v", err)
	}
	return string(out)
}

func runShell(t *testing.T, cmd string, env ...string) string {
	t.Helper()
	c := exec.Command("/bin/sh", "-c", cmd)
	c.Env = append([]string{"PATH=/usr/bin:/bin"}, env...)
	out, err := c.Output()
	if err != nil {
		t.Fatalf("key command %q failed: %v", cmd, err)
	}
	return string(out)
}

func TestShellKeyCommandMatchesWrapper(t *testing.T) {
	h := testutil.HermeticHome(t)
	dir := filepath.Join(h.Root, "keys dir")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"plain":       "sk-test-canary\n",
		"backslashes": `sk\test\\key` + "\n",
		"spaces":      "  sk-lead and trail  \n",
		"crlf":        "sk-crlf\r\n",
		"no newline":  "sk-no-newline",
		"two lines":   "first\nsecond\n",
	}
	names := map[string]string{
		"plain":       "plain",
		"backslashes": "back",
		"spaces":      "it's a key",
		"crlf":        "crlf",
		"no newline":  "nonl",
		"two lines":   "two",
	}
	for name, content := range cases {
		path := filepath.Join(dir, names[name])
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		r := secret.Reference{Kind: secret.KindFile, Value: path, Raw: "file:" + path}
		cmd, err := launch.ShellCommand(r)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := wrapperResolve(t, path)
		if got := runShell(t, cmd); got != want {
			t.Errorf("%s: shell command printed %q, wrapper resolves %q", name, got, want)
		}
		auth, err := launch.ProviderAuth(r)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		args, _ := auth["args"].([]any)
		var sargs []string
		for _, a := range args {
			sargs = append(sargs, a.(string))
		}
		c := exec.Command(auth["command"].(string), sargs...)
		c.Env = []string{"PATH=/usr/bin:/bin"}
		out, err := c.Output()
		if err != nil {
			t.Fatalf("%s: provider auth command failed: %v", name, err)
		}
		if string(out) != want {
			t.Errorf("%s: provider auth printed %q, wrapper resolves %q", name, out, want)
		}
	}
}

func TestShellKeyCommandNoInjection(t *testing.T) {
	h := testutil.HermeticHome(t)
	marker := filepath.Join(h.Root, "pwned")
	for _, base := range []string{"k$(touch " + "pwned)x", "k`touch pwned`x", "k;touch pwned;x", "k' ; touch pwned ; '"} {
		path := filepath.Join(h.Root, base)
		if err := os.WriteFile(path, []byte("sk-safe\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		r := secret.Reference{Kind: secret.KindFile, Value: path, Raw: "file:" + path}
		cmd, err := launch.ShellCommand(r)
		if err != nil {
			t.Fatal(err)
		}
		c := exec.Command("/bin/sh", "-c", cmd)
		c.Dir = h.Root
		out, err := c.Output()
		if err != nil || string(out) != "sk-safe" {
			t.Errorf("%q: out=%q err=%v", base, out, err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%q: key command executed injected code", base)
		}
	}
}

func TestEnvKeyCommand(t *testing.T) {
	r := secret.Reference{Kind: secret.KindEnv, Value: "MY_KEY", Raw: "env:MY_KEY"}
	cmd, err := launch.ShellCommand(r)
	if err != nil {
		t.Fatal(err)
	}
	if cmd != `printf '%s' "${MY_KEY:-}"` {
		t.Errorf("env command = %s", cmd)
	}
	if got := runShell(t, cmd, `MY_KEY=sk-env\x`); got != `sk-env\x` {
		t.Errorf("env command printed %q", got)
	}
	auth, err := launch.ProviderAuth(r)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"command": "sh", "args": []any{"-c", `printf "%s" "${MY_KEY:-}"`}}
	if !reflect.DeepEqual(auth, want) {
		t.Errorf("provider auth = %#v, want %#v", auth, want)
	}
	for _, bad := range []string{"BAD-NAME", "1X", "X;rm", ""} {
		r := secret.Reference{Kind: secret.KindEnv, Value: bad, Raw: "env:" + bad}
		if _, err := launch.ShellCommand(r); err == nil {
			t.Errorf("ShellCommand accepted variable %q", bad)
		}
		if _, err := launch.ProviderAuth(r); err == nil {
			t.Errorf("ProviderAuth accepted variable %q", bad)
		}
	}
}

func TestProviderAuthFileShape(t *testing.T) {
	r := secret.Reference{Kind: secret.KindFile, Value: "/keys/a b", Raw: "file:/keys/a b"}
	auth, err := launch.ProviderAuth(r)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"command": "sh", "args": []any{
		"-c", `k=; IFS= read -r k < "$1" || [ -n "$k" ]; printf "%s" "$k"`, "aprofile-key", "/keys/a b"}}
	if !reflect.DeepEqual(auth, want) {
		t.Errorf("provider auth = %#v, want %#v", auth, want)
	}
	cmd, _ := launch.ShellCommand(r)
	if cmd != `k=; IFS= read -r k < '/keys/a b' || [ -n "$k" ]; printf '%s' "$k"` {
		t.Errorf("shell command = %s", cmd)
	}
}

// Checkpoint 1 finding: with a missing key file the redirect fails and read never
// runs; an inherited k must not be printed as the key.
func TestKeyCommandMissingFileIgnoresInheritedK(t *testing.T) {
	h := testutil.HermeticHome(t)
	path := filepath.Join(h.Root, "missing-key")
	r := secret.Reference{Kind: secret.KindFile, Value: path, Raw: "file:" + path}
	cmd, err := launch.ShellCommand(r)
	if err != nil {
		t.Fatal(err)
	}
	c := exec.Command("/bin/sh", "-c", cmd)
	c.Env = []string{"PATH=/usr/bin:/bin", "k=LEAKED"}
	out, _ := c.Output()
	if string(out) != "" {
		t.Errorf("shell command printed %q for a missing file", out)
	}
	auth, _ := launch.ProviderAuth(r)
	var sargs []string
	for _, a := range auth["args"].([]any) {
		sargs = append(sargs, a.(string))
	}
	c = exec.Command("sh", sargs...)
	c.Env = []string{"PATH=/usr/bin:/bin", "k=LEAKED"}
	out, _ = c.Output()
	if string(out) != "" {
		t.Errorf("provider auth printed %q for a missing file", out)
	}
}
