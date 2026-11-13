package launch_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/profile"
)

// This helper uses pipes for barriers; a separate process is essential because
// the transaction lock must protect independent aprofile invocations.
func TestLaunchTransactionProcess(t *testing.T) {
	role := os.Getenv("APROFILE_TRANSACTION_TEST")
	if role == "" {
		return
	}
	cfg, rec := os.Getenv("APROFILE_TRANSACTION_CONFIG"), os.Getenv("APROFILE_TRANSACTION_RECORD")
	input := bufio.NewReader(os.Stdin)
	wait := func() {
		if _, err := input.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	if role == "second" {
		// Probe the real kernel lock to choose the pre-fix interleaving without a
		// timeout: when serialization is missing, hold B after its initial plan
		// until A has committed. With serialization, A must finish before B plans.
		f, err := os.OpenFile(rec+".lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
				t.Fatal(err)
			}
			fmt.Println("unserialized")
		case errors.Is(err, unix.EWOULDBLOCK):
			fmt.Println("serialized")
		default:
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		wait()
	}
	restore := launch.SetBeforeWrite(func() { fmt.Println("planned"); wait() })
	defer restore()
	p := loginProfile(profile.ShareContent)
	p.Features = []profile.FeatureState{{Feature: "automode-log"}}
	if _, err := launch.Apply(p, builtin(t, "claude"), cfg, rec, false); err != nil {
		t.Fatal(err)
	}
}

type transactionChild struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr bytes.Buffer
}

func startTransactionChild(t *testing.T, role string, e applyEnv) *transactionChild {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	c := &transactionChild{cmd: exec.CommandContext(ctx, binary, "-test.run=^TestLaunchTransactionProcess$")}
	c.cmd.Env = append(os.Environ(), "APROFILE_TRANSACTION_TEST="+role, "APROFILE_TRANSACTION_CONFIG="+e.cfg, "APROFILE_TRANSACTION_RECORD="+e.rec)
	c.cmd.Stderr = &c.stderr
	c.in, err = c.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.out = bufio.NewReader(out)
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.in.Close()
		if c.cmd.ProcessState == nil {
			_ = c.cmd.Process.Kill()
			_ = c.cmd.Wait()
		}
	})
	return c
}
func (c *transactionChild) line(t *testing.T) string {
	t.Helper()
	s, err := c.out.ReadString('\n')
	if err != nil {
		t.Fatalf("child barrier: %v; %s", err, c.stderr.String())
	}
	return s
}
func (c *transactionChild) release(t *testing.T) {
	t.Helper()
	if _, err := io.WriteString(c.in, "continue\n"); err != nil {
		t.Fatal(err)
	}
}
func (c *transactionChild) finish(t *testing.T) {
	t.Helper()
	if err := c.cmd.Wait(); err != nil {
		t.Fatalf("child failed: %v; %s", err, c.stderr.String())
	}
}

// Without locking before Load, B snapshots A's managed "1" as the recovery
// original. Disabling must instead restore the genuine user value.
func TestOverlappingProcessesPreserveFeatureRecovery(t *testing.T) {
	e := newApplyEnv(t)
	path := filepath.Join(e.cfg, "settings.json")
	if err := os.WriteFile(path, []byte(`{"env":{"AUTOMODE_DECISION_LOG":"original"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(e.rec), 0o700); err != nil {
		t.Fatal(err)
	}
	a := startTransactionChild(t, "first", e)
	if got := a.line(t); got != "planned\n" {
		t.Fatalf("first barrier %q", got)
	}
	b := startTransactionChild(t, "second", e)
	mode := b.line(t)
	switch mode {
	case "unserialized\n":
		b.release(t)
		if got := b.line(t); got != "planned\n" {
			t.Fatalf("second barrier %q", got)
		}
		a.release(t)
		a.finish(t)
		b.release(t)
		b.finish(t)
	case "serialized\n":
		b.release(t) // B now attempts Apply while A holds the transaction.
		a.release(t)
		a.finish(t)
		// B sees an already synced record and needs no write barrier.
		b.finish(t)
	default:
		t.Fatalf("unexpected lock state %q", mode)
	}
	p := loginProfile(profile.ShareContent)
	if _, err := launch.Apply(p, builtin(t, "claude"), e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	env := readJSON(t, path)["env"].(map[string]any)
	if env["AUTOMODE_DECISION_LOG"] != "original" {
		t.Fatalf("lost original value: got %q, want original", env["AUTOMODE_DECISION_LOG"])
	}
	if mode != "serialized\n" {
		t.Fatal("overlapping Apply did not hold a cross-process transaction lock")
	}
}
