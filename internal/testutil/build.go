package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// origEnv is a snapshot of the process environment captured at package load,
// before any test calls t.Setenv to point HOME/XDG at a hermetic sandbox. The
// `go build` subprocess runs with this snapshot so it keeps reaching the real
// module and build caches (GOPATH, GOMODCACHE, GOCACHE) even while a test has
// relocated HOME.
var origEnv = os.Environ()

// buildCache memoises built binaries by import path for the lifetime of the test
// process, so the (slow) `go build` runs at most once per binary across the whole
// package test run.
var (
	buildMu    sync.Mutex
	buildCache = map[string]string{}
)

// TestSeamTag is the build tag of the test seams compiled into test binaries only:
// a binary built with it answers profile.RunningProcess from APROFILE_TEST_RUNNING
// (none, unknown, a pid, or real for the real process scan; unset means none), so
// integration tests do not depend on whether the sandbox can read other processes'
// environments (T092). Production builds never carry it.
const TestSeamTag = "aprofile_testseam"

// BuildBinary builds the Go package at importPath, with the TestSeamTag test seams,
// into a temp directory and returns the absolute path to the resulting executable.
// Results are cached per import path + output name. It fails the test on build
// error.
//
// outName is the executable's file name (e.g. "claude" so a wrapper that execs
// the "claude" binary finds the stub, or "aprofile" for the CLI).
func BuildBinary(t testing.TB, importPath, outName string) string {
	t.Helper()
	return build(t, importPath, outName, TestSeamTag)
}

// BuildProductionBinary builds the Go package at importPath exactly as a release
// does, without the test seams, for tests that check the seams are absent.
func BuildProductionBinary(t testing.TB, importPath, outName string) string {
	t.Helper()
	return build(t, importPath, outName, "")
}

func build(t testing.TB, importPath, outName, tags string) string {
	t.Helper()
	key := importPath + "\x00" + outName + "\x00" + tags
	buildMu.Lock()
	defer buildMu.Unlock()
	if p, ok := buildCache[key]; ok {
		return p
	}

	dir, err := os.MkdirTemp("", "aprofile-bin-*")
	if err != nil {
		t.Fatalf("testutil: make build dir: %v", err)
	}
	out := filepath.Join(dir, outName)

	args := []string{"build", "-o", out}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	cmd := exec.Command("go", append(args, importPath)...)
	cmd.Dir = moduleRoot(t)
	cmd.Env = origEnv
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("testutil: go build %s failed: %v\n%s", importPath, err, combined)
	}
	buildCache[key] = out
	return out
}

// moduleRoot walks up from this source file's directory to the module root (the
// directory containing go.mod).
func moduleRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("testutil: cannot determine caller for module root")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("testutil: go.mod not found walking up from " + file)
		}
		dir = parent
	}
}
