package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestDoctorAuthStatusHook covers the FR-044 doctor check for the exec-hook auth-status
// result: a harness whose hook answers status=missing produces a doctor finding naming
// the profile and a fix, exercised end-to-end through the built binary.
func TestDoctorAuthStatusHook(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, ".config")
	dataHome := filepath.Join(root, ".local", "share")
	stateHome := filepath.Join(root, ".local", "state")
	scripts := filepath.Join(root, "scripts")
	for _, d := range []string{configHome, dataHome, stateHome, scripts} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	aprofileBin := testutil.BuildBinary(t, "github.com/cc-deck/aprofile/cmd/aprofile", "aprofile")

	// Hook: answers auth-status with "missing"; every other operation is unsupported.
	writeExecFile(t, filepath.Join(scripts, "aprofile-harness-hooked"),
		"#!/bin/sh\ncase \"$1\" in\n  auth-status) cat >/dev/null; echo 'status=missing'; echo 'detail=no token' ;;\n  *) exit 64 ;;\nesac\n")

	// A data-only harness whose binary exists (sh) so the missing-binary check is quiet,
	// and whose exec hook implements auth-status.
	harnessesDir := filepath.Join(configHome, "aprofile", "harnesses.d")
	testutil.WriteFile(t, filepath.Join(harnessesDir, "hooked.yaml"),
		"name: hooked\nbinary: sh\nwrapper_prefix: hooked\nconfig_env:\n  HOOKED_DIR: \".\"\nexec_hook:\n  operations: [auth-status]\n")

	binDir := filepath.Join(dataHome, "aprofile", "bin")
	path := strings.Join([]string{scripts, binDir, "/usr/bin", "/bin"}, string(os.PathListSeparator))
	vars := []string{
		"HOME=" + root,
		"XDG_CONFIG_HOME=" + configHome,
		"XDG_DATA_HOME=" + dataHome,
		"XDG_STATE_HOME=" + stateHome,
		"PATH=" + path,
		"SHELL=/bin/sh",
		"ZDOTDIR=" + root,
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(aprofileBin, args...)
		cmd.Env = vars
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("add", "hooked", "h1"); err != nil {
		t.Fatalf("add hooked/h1: %v\n%s", err, out)
	}
	if out, err := run("sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	// Capture stdout separately so the JSON is clean (the non-zero exit prints a summary
	// to stderr).
	doctorCmd := exec.Command(aprofileBin, "doctor", "--json")
	doctorCmd.Env = vars
	var stdout strings.Builder
	doctorCmd.Stdout = &stdout
	err := doctorCmd.Run()
	out := stdout.String()
	if err == nil {
		t.Fatalf("doctor should exit non-zero when auth-status reports missing:\n%s", out)
	}
	var report struct {
		Findings []struct {
			Profile string `json:"profile"`
			Message string `json:"message"`
			Fix     string `json:"fix"`
		} `json:"findings"`
	}
	if jerr := json.Unmarshal([]byte(out), &report); jerr != nil {
		t.Fatalf("doctor --json not parseable: %v\n%s", jerr, out)
	}
	found := false
	for _, f := range report.Findings {
		if f.Profile == "hooked/h1" && strings.Contains(f.Message, "auth-status") && strings.Contains(f.Message, "missing") {
			found = true
			if !strings.Contains(f.Fix, "login") {
				t.Errorf("auth-status finding should suggest login, got fix %q", f.Fix)
			}
		}
	}
	if !found {
		t.Errorf("doctor did not report the auth-status missing finding:\n%s", out)
	}
}
