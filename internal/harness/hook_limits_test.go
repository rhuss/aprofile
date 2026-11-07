//go:build unix

package harness_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/harness"
)

// M12: a hook that times out is killed together with its children. A shell hook whose
// background grandchild holds the stdout pipe must neither keep Call waiting nor be
// left running as an orphan.
func TestHookTimeoutKillsGrandchildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	bin := filepath.Join(dir, "hook")
	writeExec(t, bin, "#!/bin/sh\nsleep 30 &\necho $! >"+pidFile+"\nsleep 30\n")

	// Under heavy load the shell may not get to start its child before a short
	// deadline; retry with a longer one until it has.
	var data []byte
	for _, timeout := range []time.Duration{600 * time.Millisecond, 2 * time.Second, 5 * time.Second} {
		h := harness.NewHookForTest("x", bin, timeout)
		start := time.Now()
		_, err := h.Call("env", map[string]string{"harness": "x"})
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("want a timeout error, got %v", err)
		}
		if d := time.Since(start); d > timeout+3*time.Second {
			t.Errorf("Call returned after %s; the process group should be killed at the deadline", d)
		}
		if b, rerr := os.ReadFile(pidFile); rerr == nil {
			data = b
			break
		}
		// The timeout is remembered per executable; a new copy is a new executable.
		bin = filepath.Join(dir, "hook-"+timeout.String())
		writeExec(t, bin, "#!/bin/sh\nsleep 30 &\necho $! >"+pidFile+"\nsleep 30\n")
	}
	if data == nil {
		t.Skip("the hook never started its child before the deadline (machine too loaded)")
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pid <= 0 {
		t.Fatalf("bad pid %q", data)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d still runs after the hook timed out (orphaned)", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// M13: a hook operation that timed out is not called again for the same profile in
// the same run, and after it timed out for two profiles it is not called again for
// any profile: the next call fails at once with the same reason. A different
// operation is still called.
func TestHookTimeoutIsRememberedPerOperation(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	installHook(t, "slowops", "#!/bin/sh\necho \"$1\" >>"+calls+"\ncat >/dev/null\nsleep 5\n")
	harness.SetHookTimeoutForTest(t, 500*time.Millisecond)
	def := &harness.Definition{Name: "slowops", ExecHook: &harness.ExecHook{Operations: []string{"env", "auth-status"}}}

	for _, profile := range []string{"a", "a", "b", "c", "d"} {
		start := time.Now()
		_, err := harness.RunEnvHook(def, harness.HookContext{Harness: "slowops", Profile: profile, ConfigDir: "/d"})
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("profile %s: want a timeout error, got %v", profile, err)
		}
		// a (second call), c and d are answered from the memo: a for its own
		// timeout, c and d because two profiles (a, b) timed out.
		fast := time.Since(start) < 100*time.Millisecond
		switch profile {
		case "c", "d":
			if !fast {
				t.Errorf("profile %s: two profiles timed out, the operation must fail fast (took %s)", profile, time.Since(start))
			}
			if !strings.Contains(err.Error(), "2 profiles") {
				t.Errorf("profile %s: error should say why it was not called: %v", profile, err)
			}
		case "b":
			if fast {
				t.Errorf("profile b: one other profile's timeout must not skip the call")
			}
		}
	}
	// A different operation is called (and times out on its own account).
	if _, err := harness.RunAuthStatusHook(def, harness.HookContext{Harness: "slowops", Profile: "a", ConfigDir: "/d"}); err == nil ||
		!strings.Contains(err.Error(), "auth-status") || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("auth-status should time out on its own, got %v", err)
	}
	// Under load the shell may be killed before it logs its call, so only an upper
	// bound is certain: env ran at most for a and b, auth-status at most once.
	data, _ := os.ReadFile(calls)
	got := strings.Fields(string(data))
	envCalls, authCalls := 0, 0
	for _, c := range got {
		switch c {
		case "env":
			envCalls++
		case "auth-status":
			authCalls++
		}
	}
	if envCalls > 2 || authCalls > 1 {
		t.Errorf("hook calls = %v, want at most two env and one auth-status call", got)
	}
}

// A remembered timeout expires after its TTL, so a long-lived process (the TUI)
// calls the hook again instead of failing the operation forever.
func TestHookTimeoutMemoExpires(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	installHook(t, "slowttl", "#!/bin/sh\necho \"$1\" >>"+calls+"\ncat >/dev/null\nsleep 5\n")
	harness.SetHookTimeoutForTest(t, 300*time.Millisecond)
	now := time.Now()
	harness.SetHookClockForTest(t, func() time.Time { return now })
	def := &harness.Definition{Name: "slowttl", ExecHook: &harness.ExecHook{Operations: []string{"env"}}}
	ctx := harness.HookContext{Harness: "slowttl", Profile: "p", ConfigDir: "/d"}

	if _, err := harness.RunEnvHook(def, ctx); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout, got %v", err)
	}
	start := time.Now()
	if _, err := harness.RunEnvHook(def, ctx); err == nil || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("within the TTL the timeout must be remembered (err %v, took %s)", err, time.Since(start))
	}
	now = now.Add(61 * time.Second)
	start = time.Now()
	if _, err := harness.RunEnvHook(def, ctx); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout, got %v", err)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Errorf("after the TTL the hook must be called again (took %s)", time.Since(start))
	}
}

// A non-zero exit is not remembered: it is fast, and may depend on the profile.
func TestHookExitFailureIsNotRemembered(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	installHook(t, "failops", "#!/bin/sh\necho x >>"+calls+"\nexit 3\n")
	def := &harness.Definition{Name: "failops", ExecHook: &harness.ExecHook{Operations: []string{"env"}}}
	for i := 0; i < 2; i++ {
		if _, err := harness.RunEnvHook(def, harness.HookContext{Harness: "failops", Profile: "p", ConfigDir: "/d"}); err == nil {
			t.Fatal("want an error")
		}
	}
	data, _ := os.ReadFile(calls)
	if n := len(strings.Fields(string(data))); n != 2 {
		t.Errorf("hook called %d times, want 2", n)
	}
}

// Prod7: hook output is bounded. Standard output above 1 MiB is an error, and a long
// standard error is truncated in the error message.
func TestHookOutputIsBounded(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	writeExec(t, big, "#!/bin/sh\ncat >/dev/null\nhead -c 2000000 /dev/zero | tr '\\0' 'a'\n")
	h := harness.NewHookForTest("x", big, 0)
	if _, err := h.Call("env", map[string]string{"harness": "x"}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("want an output-size error, got %v", err)
	}

	noisy := filepath.Join(dir, "noisy")
	writeExec(t, noisy, "#!/bin/sh\ncat >/dev/null\nhead -c 100000 /dev/zero | tr '\\0' 'e' >&2\nexit 2\n")
	h = harness.NewHookForTest("x", noisy, 0)
	_, err := h.Call("env", map[string]string{"harness": "x"})
	if err == nil {
		t.Fatal("want an exit error")
	}
	if len(err.Error()) > 5000 {
		t.Errorf("error message carries %d bytes of stderr; want it truncated", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("a truncated stderr should say so: %.200s", err.Error())
	}
}
