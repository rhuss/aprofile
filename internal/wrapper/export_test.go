package wrapper

import (
	"github.com/rhuss/aprofile/internal/harness"
	"github.com/rhuss/aprofile/internal/profile"
)

// Render produces the wrapper script for one profile with no exec-hook env output.
func Render(p *profile.Profile, def *harness.Definition, configDir string) (string, error) {
	return RenderWith(p, def, configDir, RenderOptions{})
}
