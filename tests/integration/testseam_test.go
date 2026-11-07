package integration

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// T092: the running-process test seam exists only in the binary the tests build
// (testutil.BuildBinary, tag aprofile_testseam). A production build
// (`go build ./cmd/aprofile`) does not contain it and ignores APROFILE_TEST_RUNNING.
func TestProductionBuildIgnoresTestSeam(t *testing.T) {
	prod := testutil.BuildProductionBinary(t, "github.com/cc-deck/aprofile/cmd/aprofile", "aprofile")
	e := newEnv(t)

	const name = "APROFILE_TEST_RUNNING"
	if data, err := os.ReadFile(prod); err != nil {
		t.Fatal(err)
	} else if bytes.Contains(data, []byte(name)) {
		t.Errorf("the production binary must not reference %s", name)
	}
	if data, err := os.ReadFile(e.aprofile); err != nil {
		t.Fatal(err)
	} else if !bytes.Contains(data, []byte(name)) {
		t.Errorf("the test binary should carry the seam (sanity check of this test)")
	}

	e.run(nil, "add", "claude", "busy", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	// This test process is alive but does not use the profile: only the seam can
	// make it the running answer.
	forced := name + "=" + strconv.Itoa(os.Getpid())

	out, err := e.tryRun([]string{forced}, "edit", "claude/busy", "--share", "none", "--yes", "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	out, err = e.tryRun([]string{forced}, "edit", "claude/busy", "--share", "none", "--yes")
	if exitCodeOf(err) != 4 {
		t.Fatalf("the test binary should honor the seam: exit %d, want 4\n%s", exitCodeOf(err), out)
	}

	cmd := exec.Command(prod, "edit", "claude/busy", "--share", "none", "--yes")
	cmd.Env = append(append([]string{}, e.vars...), forced)
	pout, perr := cmd.CombinedOutput()
	if perr != nil || strings.Contains(string(pout), "in use by a running process") {
		t.Errorf("the production binary must ignore %s: %v\n%s", name, perr, pout)
	}
}
