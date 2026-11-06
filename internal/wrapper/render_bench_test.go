package wrapper_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// BenchmarkWrapperStartup measures the start-up overhead a generated wrapper adds
// over exec'ing the no-op stub harness directly (T061, slice one SC-011; feature
// 002 adds APROFILE_SHARE to the contract). It reports the median overhead of 20
// runs and fails above the 50 ms budget. Run with: go test -bench WrapperStartup
// ./internal/wrapper/
func BenchmarkWrapperStartup(b *testing.B) {
	stub := testutil.BuildBinary(b, "github.com/cc-deck/aprofile/tests/fixtures/stub", "claude")
	dir := b.TempDir()
	p := &profile.Profile{Harness: "claude", Name: "bench", AuthKind: "oauth-login", Share: profile.ShareContent}
	content, err := wrapper.Render(p, claudeDef(b), filepath.Join(dir, "profile"))
	if err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "profile"), 0o755); err != nil {
		b.Fatal(err)
	}
	wpath := filepath.Join(dir, "claude-bench")
	if err := os.WriteFile(wpath, []byte(content), 0o755); err != nil {
		b.Fatal(err)
	}
	env := []string{"PATH=" + filepath.Dir(stub) + ":/usr/bin:/bin", "HOME=" + dir}
	run := func(bin string) time.Duration {
		cmd := exec.Command(bin, "--version")
		cmd.Env = env
		start := time.Now()
		if err := cmd.Run(); err != nil {
			b.Fatalf("run %s: %v", bin, err)
		}
		return time.Since(start)
	}
	median := func(bin string) time.Duration {
		var d []time.Duration
		for i := 0; i < 20; i++ {
			d = append(d, run(bin))
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[len(d)/2]
	}
	_, _ = run(stub), run(wpath)
	b.ResetTimer()
	var overhead time.Duration
	for i := 0; i < b.N; i++ {
		overhead = median(wpath) - median(stub)
	}
	b.StopTimer()
	if overhead < 0 {
		overhead = 0
	}
	b.ReportMetric(float64(overhead.Microseconds())/1000, "ms-overhead")
	if overhead > 50*time.Millisecond {
		b.Errorf("wrapper overhead %v exceeds the 50 ms budget", overhead)
	}
}
