package wrapper_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// PATH must be exported before executable lookup, even when the harness can
// otherwise carry literal environment variables in settings.json.
func TestLiteralPATHBootstrapsHarnessAndLogin(t *testing.T) {
	for _, share := range []profile.Share{profile.ShareContent, profile.ShareNone} {
		t.Run(string(share), func(t *testing.T) {
			def := builtinDef(t, "claude")
			customPath := stubBin(t, "claude")
			p := &profile.Profile{Harness: "claude", Name: "custom", AuthKind: "oauth-login", Share: share,
				ExtraEnv: []profile.ExtraEnvVar{{Name: "PATH", Value: customPath, Literal: true}}}
			cfg := folderCfg(t)
			script, err := wrapper.RenderWith(p, def, cfg, wrapper.RenderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "wrapper")
			if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/sh", path)
			cmd.Env = []string{"PATH=" + customPath + ":/usr/bin:/bin"}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("configured PATH did not start custom harness: %v\n%s", err, out)
			} else if !strings.Contains(string(out), "PATH="+customPath+"\n") {
				t.Errorf("harness PATH not configured: %s", out)
			}
			env, err := wrapper.Environ(p, def, cfg, wrapper.RenderOptions{}, []string{"PATH=/usr/bin:/bin"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains("\n"+strings.Join(env, "\n")+"\n", "\nPATH="+customPath+"\n") {
				t.Errorf("login PATH missing: %v", env)
			}
		})
	}
}
