package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cc-deck/aprofile/internal/config"
)

const (
	pathBlockBegin = "# >>> aprofile (managed) >>>"
	pathBlockEnd   = "# <<< aprofile (managed) <<<"
)

// PathResult reports what PATH management did (or would do under dry-run).
type PathResult struct {
	Action  string // added | removed | unchanged | manual | disabled | skipped
	RCFile  string // the shell startup file edited, when applicable
	Manual  string // manual instructions when aprofile could not edit the file
	Changed bool
	Err     error // non-nil when editing failed (surfaced only with --require-path)
}

// EnsurePath adds (or, when remove is true, removes) a marked block that puts
// binDir on PATH in the user's shell startup file (FR-024). It is idempotent, never
// replaces a symlinked or read-only rc file, and prints manual instructions when
// it cannot edit automatically.
func EnsurePath(binDir string, settings *config.Settings, remove, dryRun bool) PathResult {
	exportLine := fmt.Sprintf("export PATH=%q", binDir+":$PATH")
	manual := fmt.Sprintf("add %q to PATH, e.g. put this in your shell startup file:\n  %s", binDir, exportLine)

	if settings != nil && settings.DisableShellEdit {
		return PathResult{Action: "disabled", Manual: manual}
	}

	shell := filepath.Base(os.Getenv("SHELL"))
	rc, kind, ok := rcFileFor(shell)
	if !ok {
		return PathResult{Action: "manual", Manual: manual}
	}

	if remove {
		return removeBlock(rc, kind, dryRun, manual)
	}
	return addBlock(rc, kind, binDir, exportLine, dryRun, manual)
}

// rcFileFor returns the startup file for the named shell and a "kind" describing
// how it is edited: "block" for a marked block appended to a shared rc file, or
// "file" for a dedicated conf.d file aprofile fully owns.
func rcFileFor(shell string) (path, kind string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}
	switch shell {
	case "zsh":
		dir := os.Getenv("ZDOTDIR")
		if dir == "" {
			dir = home
		}
		return filepath.Join(dir, ".zshrc"), "block", true
	case "bash":
		if isDarwin() {
			return filepath.Join(home, ".bash_profile"), "block", true
		}
		return filepath.Join(home, ".bashrc"), "block", true
	case "fish":
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		return filepath.Join(cfg, "fish", "conf.d", "aprofile.fish"), "file", true
	default:
		return "", "", false
	}
}

func addBlock(rc, kind, binDir, exportLine string, dryRun bool, manual string) PathResult {
	if fi, err := os.Lstat(rc); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		// Never replace a symlinked rc file (FR-024).
		return PathResult{Action: "manual", RCFile: rc, Manual: manual}
	}

	var desired string
	if kind == "file" {
		desired = fmt.Sprintf("%s\nfish_add_path %s\n%s\n", pathBlockBegin, shellSingleQuote(binDir), pathBlockEnd)
	} else {
		desired = fmt.Sprintf("%s\n%s\n%s\n", pathBlockBegin, exportLine, pathBlockEnd)
	}

	existing, _ := os.ReadFile(rc)
	current := string(existing)
	updated, changed := upsertBlock(current, desired)
	if !changed {
		return PathResult{Action: "unchanged", RCFile: rc}
	}
	if dryRun {
		return PathResult{Action: "added", RCFile: rc, Changed: true}
	}
	if err := writeRC(rc, updated); err != nil {
		return PathResult{Action: "manual", RCFile: rc, Manual: manual, Err: err}
	}
	return PathResult{Action: "added", RCFile: rc, Changed: true}
}

func removeBlock(rc, kind string, dryRun bool, manual string) PathResult {
	existing, err := os.ReadFile(rc)
	if err != nil {
		return PathResult{Action: "unchanged", RCFile: rc}
	}
	stripped, changed := stripBlock(string(existing))
	if !changed {
		return PathResult{Action: "unchanged", RCFile: rc}
	}
	if dryRun {
		return PathResult{Action: "removed", RCFile: rc, Changed: true}
	}
	// For a dedicated fish file, remove the whole file if it is now empty.
	if kind == "file" && strings.TrimSpace(stripped) == "" {
		if err := os.Remove(rc); err != nil && !os.IsNotExist(err) {
			return PathResult{Action: "manual", RCFile: rc, Manual: manual, Err: err}
		}
		return PathResult{Action: "removed", RCFile: rc, Changed: true}
	}
	if err := writeRC(rc, stripped); err != nil {
		return PathResult{Action: "manual", RCFile: rc, Manual: manual, Err: err}
	}
	return PathResult{Action: "removed", RCFile: rc, Changed: true}
}

// upsertBlock replaces an existing managed block with desired, or appends it when
// absent. It reports whether anything changed.
func upsertBlock(current, desired string) (string, bool) {
	start := strings.Index(current, pathBlockBegin)
	if start >= 0 {
		end := strings.Index(current[start:], pathBlockEnd)
		if end >= 0 {
			end = start + end + len(pathBlockEnd)
			// consume a trailing newline after the end marker if present
			if end < len(current) && current[end] == '\n' {
				end++
			}
			block := current[start:end]
			if block == desired {
				return current, false
			}
			return current[:start] + desired + current[end:], true
		}
	}
	if current != "" && !strings.HasSuffix(current, "\n") {
		current += "\n"
	}
	return current + desired, true
}

// stripBlock removes a managed block if present, reporting whether it changed.
func stripBlock(current string) (string, bool) {
	start := strings.Index(current, pathBlockBegin)
	if start < 0 {
		return current, false
	}
	end := strings.Index(current[start:], pathBlockEnd)
	if end < 0 {
		return current, false
	}
	end = start + end + len(pathBlockEnd)
	if end < len(current) && current[end] == '\n' {
		end++
	}
	// trim a lone preceding newline so we don't leave a blank gap
	trimStart := start
	if trimStart > 0 && current[trimStart-1] == '\n' {
		// keep one newline boundary tidy
	}
	return current[:trimStart] + current[end:], true
}

func writeRC(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return config.WriteFileAtomic(path, []byte(content), 0o644)
}

// shellSingleQuote quotes a path for fish. Fish single-quoted strings escape an
// embedded quote as \' (unlike POSIX sh, which cannot escape inside single quotes);
// this helper is used only for the fish conf.d snippet.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}
