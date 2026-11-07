package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// bin_dir ends up in shell startup files and wrapper paths, so settings loading
// rejects a value that is not an absolute path or that spans several lines.
func TestLoadSettingsRejectsUnsafeBinDir(t *testing.T) {
	cases := map[string]string{
		"relative":      "bin_dir: bin\n",
		"tilde":         "bin_dir: ~/bin\n",
		"newline":       "bin_dir: \"/opt/bin\\nrm -rf ~\"\n",
		"carriage ret":  "bin_dir: \"/opt/bin\\rx\"\n",
		"literal block": "bin_dir: |\n  /opt/bin\n  touch /tmp/pwned\n",
		"control char":  "bin_dir: \"/opt/b\\tin\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			h := testutil.HermeticHome(t)
			dir := filepath.Join(h.ConfigHome, "aprofile")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := config.LoadSettings(dir)
			if err == nil || !strings.Contains(err.Error(), "bin_dir") {
				t.Fatalf("LoadSettings error = %v, want a bin_dir error", err)
			}
			if _, err := config.ResolvePaths(); err == nil {
				t.Error("ResolvePaths accepted the unsafe bin_dir")
			}
		})
	}
}

// An absolute bin_dir with spaces or shell metacharacters is still a valid path:
// it is quoted where it is written, not rejected.
func TestLoadSettingsAcceptsAbsoluteBinDirWithSpecialChars(t *testing.T) {
	h := testutil.HermeticHome(t)
	dir := filepath.Join(h.ConfigHome, "aprofile")
	want := "/opt/my bin/$HOME/`x`/it's"
	if err := config.SaveSettings(dir, &config.Settings{BinDir: want}); err != nil {
		t.Fatal(err)
	}
	s, err := config.LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.BinDir != want {
		t.Errorf("BinDir = %q, want %q", s.BinDir, want)
	}
}
