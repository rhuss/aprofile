package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
)

// pathHome points HOME, ZDOTDIR and SHELL into a temp dir so EnsurePath never sees
// the real home (Constitution VII).
func pathHome(t *testing.T, shell string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ZDOTDIR", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("SHELL", shell)
	return root
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return uint64(fi.Sys().(*syscall.Stat_t).Ino)
}

// T073/FR-024: every outcome that leaves the user with manual instructions is
// reported by NeedsManual, so --require-path can fail on it.
func TestEnsurePathManualOutcomes(t *testing.T) {
	bin := "/opt/aprofile/bin"

	t.Run("unsupported shell", func(t *testing.T) {
		pathHome(t, "/bin/tcsh")
		r := EnsurePath(bin, nil, false, false)
		if r.Action != "manual" || !r.NeedsManual() || r.Reason == "" || !strings.Contains(r.Manual, bin) {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		pathHome(t, "/bin/zsh")
		r := EnsurePath(bin, &config.Settings{DisableShellEdit: true}, false, false)
		if r.Action != "disabled" || !r.NeedsManual() {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("read-only rc is not replaced", func(t *testing.T) {
		root := pathHome(t, "/bin/zsh")
		rc := filepath.Join(root, ".zshrc")
		if err := os.WriteFile(rc, []byte("# mine\n"), 0o444); err != nil {
			t.Fatal(err)
		}
		ino := inode(t, rc)
		for _, dry := range []bool{true, false} {
			r := EnsurePath(bin, nil, false, dry)
			if r.Action != "manual" || !r.NeedsManual() || !strings.Contains(r.Reason, "read-only") {
				t.Errorf("dry=%v: got %+v", dry, r)
			}
		}
		fi, _ := os.Stat(rc)
		if fi.Mode().Perm() != 0o444 || inode(t, rc) != ino {
			t.Errorf("read-only rc replaced: mode %v", fi.Mode().Perm())
		}
		if data, _ := os.ReadFile(rc); string(data) != "# mine\n" {
			t.Errorf("content changed: %q", data)
		}
	})

	t.Run("unreadable rc is not replaced", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads files regardless of their mode")
		}
		root := pathHome(t, "/bin/zsh")
		rc := filepath.Join(root, ".zshrc")
		if err := os.WriteFile(rc, []byte("# mine\n"), 0o200); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(rc, 0o600) })
		r := EnsurePath(bin, nil, false, false)
		if r.Action != "manual" || !r.NeedsManual() || r.Reason == "" {
			t.Errorf("got %+v", r)
		}
		_ = os.Chmod(rc, 0o600)
		if data, _ := os.ReadFile(rc); string(data) != "# mine\n" {
			t.Errorf("unreadable rc replaced: %q", data)
		}
	})

	t.Run("symlinked rc is not replaced", func(t *testing.T) {
		root := pathHome(t, "/bin/zsh")
		target := filepath.Join(root, "dotfiles", "zshrc")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("# dotfiles\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		rc := filepath.Join(root, ".zshrc")
		if err := os.Symlink(target, rc); err != nil {
			t.Fatal(err)
		}
		r := EnsurePath(bin, nil, false, false)
		if r.Action != "manual" || !r.NeedsManual() || !strings.Contains(r.Reason, "symlink") {
			t.Errorf("got %+v", r)
		}
		if fi, _ := os.Lstat(rc); fi.Mode()&os.ModeSymlink == 0 {
			t.Error("symlink replaced")
		}
		// The line added by hand completes the setup.
		if err := os.WriteFile(target, []byte("# dotfiles\nexport PATH=\""+bin+":$PATH\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if r := EnsurePath(bin, nil, false, false); r.Action != "unchanged" || r.NeedsManual() {
			t.Errorf("hand-added line: got %+v", r)
		}
	})

	t.Run("remove-path leaves a read-only rc alone", func(t *testing.T) {
		root := pathHome(t, "/bin/zsh")
		rc := filepath.Join(root, ".zshrc")
		if r := EnsurePath(bin, nil, false, false); r.Action != "added" {
			t.Fatalf("add: %+v", r)
		}
		if err := os.Chmod(rc, 0o444); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(rc)
		r := EnsurePath(bin, nil, true, false)
		if r.Action != "manual" || !r.NeedsManual() {
			t.Errorf("got %+v", r)
		}
		if after, _ := os.ReadFile(rc); string(after) != string(before) {
			t.Error("read-only rc edited")
		}
	})
}

// A rewritten rc keeps its permission instead of becoming 0644.
func TestEnsurePathKeepsRCMode(t *testing.T) {
	root := pathHome(t, "/bin/zsh")
	rc := filepath.Join(root, ".zshrc")
	if err := os.WriteFile(rc, []byte("# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := EnsurePath("/opt/aprofile/bin", nil, false, false)
	if r.Action != "added" || r.NeedsManual() {
		t.Fatalf("got %+v", r)
	}
	fi, _ := os.Stat(rc)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("rc mode = %v, want 0600", fi.Mode().Perm())
	}
	if r := EnsurePath("/opt/aprofile/bin", nil, false, false); r.Action != "unchanged" {
		t.Errorf("second run: %+v", r)
	}
}

// Sec5: the bash/zsh block single-quotes bin_dir, so `$`, backticks, double quotes
// and spaces in it stay literal when the shell reads the rc file; only "$PATH"
// expands.
func TestEnsurePathQuotesBinDirForPOSIXShells(t *testing.T) {
	root := pathHome(t, "/bin/zsh")
	bin := filepath.Join(root, "my bin", "$HOME", "`touch pwned`", `a"b`, "it's")
	r := EnsurePath(bin, nil, false, false)
	if r.Action != "added" {
		t.Fatalf("got %+v", r)
	}
	rc := filepath.Join(root, ".zshrc")
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	wantLine := `export PATH='` + strings.ReplaceAll(bin, "'", `'\''`) + `':"$PATH"`
	if !strings.Contains(string(data), "\n"+wantLine+"\n") {
		t.Errorf("rc block:\n%s\nwant line %s", data, wantLine)
	}
	if !strings.Contains(r.Manual+manualFor(bin), wantLine) {
		t.Errorf("manual instructions do not show the quoted line: %q", manualFor(bin))
	}

	// Source the block with a POSIX shell: PATH must start with bin_dir verbatim
	// and nothing in it may run.
	cmd := exec.Command("/bin/sh", "-c", `. "$1"; printf %s "$PATH"`, "sh", rc)
	cmd.Dir = root
	cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sourcing the rc block: %v\n%s", err, out)
	}
	if want := bin + ":/usr/bin:/bin"; string(out) != want {
		t.Errorf("PATH after sourcing = %q, want %q", out, want)
	}
	if _, err := os.Stat(filepath.Join(root, "pwned")); err == nil {
		t.Error("a command substitution in bin_dir ran")
	}
}

// manualFor returns the manual instructions EnsurePath prints for bin.
func manualFor(bin string) string {
	return EnsurePath(bin, &config.Settings{DisableShellEdit: true}, false, false).Manual
}

// Sec5: the fish snippet escapes backslashes before quotes, so a bin_dir ending in
// a backslash cannot escape the closing quote.
func TestEnsurePathQuotesBinDirForFish(t *testing.T) {
	root := pathHome(t, "/usr/bin/fish")
	bin := `/opt/a\b'c\`
	if r := EnsurePath(bin, nil, false, false); r.Action != "added" {
		t.Fatalf("got %+v", r)
	}
	data, err := os.ReadFile(filepath.Join(root, ".config", "fish", "conf.d", "aprofile.fish"))
	if err != nil {
		t.Fatal(err)
	}
	want := `fish_add_path '/opt/a\\b\'c\\'`
	if !strings.Contains(string(data), "\n"+want+"\n") {
		t.Errorf("fish snippet:\n%s\nwant line %s", data, want)
	}
}

// A line added by hand in the form older aprofile versions suggested still
// completes the setup for a non-replaceable rc file.
func TestEnsurePathAcceptsHandAddedLineInBothForms(t *testing.T) {
	bin := "/opt/aprofile/bin"
	for name, line := range map[string]string{
		"current": `export PATH='` + bin + `':"$PATH"`,
		"legacy":  `export PATH="` + bin + `:$PATH"`,
	} {
		t.Run(name, func(t *testing.T) {
			root := pathHome(t, "/bin/zsh")
			rc := filepath.Join(root, ".zshrc")
			if err := os.WriteFile(rc, []byte("# mine\n"+line+"\n"), 0o444); err != nil {
				t.Fatal(err)
			}
			if r := EnsurePath(bin, nil, false, false); r.Action != "unchanged" || r.NeedsManual() {
				t.Errorf("got %+v", r)
			}
		})
	}
}
