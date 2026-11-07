package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

const (
	pathBlockBegin = "# >>> aprofile (managed) >>>"
	pathBlockEnd   = "# <<< aprofile (managed) <<<"
)

// PathResult reports what PATH management did (or would do under dry-run).
type PathResult struct {
	Action  string // added | removed | unchanged | manual | disabled
	RCFile  string // the shell startup file edited, when applicable
	Manual  string // manual instructions when aprofile could not edit the file
	Reason  string // why aprofile could not edit the file (manual and disabled outcomes)
	Changed bool
	Err     error // non-nil when editing failed
}

// NeedsManual reports whether the user has to finish the PATH setup by hand: the
// shell is unsupported, the rc file is a symlink or read-only, editing failed, or
// shell edits are disabled. `sync --require-path` exits non-zero then (FR-024).
func (r PathResult) NeedsManual() bool {
	return r.Action == "manual" || r.Action == "disabled" || r.Err != nil
}

// EnsurePath adds (or, when remove is true, removes) a marked block that puts
// binDir on PATH in the user's shell startup file (FR-024). It is idempotent, never
// replaces a symlinked or read-only rc file, and prints manual instructions when
// it cannot edit automatically.
func EnsurePath(binDir string, settings *config.Settings, remove, dryRun bool) PathResult {
	exportLine := posixExportLine(binDir)
	manual := fmt.Sprintf("add %q to PATH, e.g. put this in your shell startup file:\n  %s", binDir, exportLine)

	if settings != nil && settings.DisableShellEdit {
		return PathResult{Action: "disabled", Manual: manual, Reason: "shell file edits are disabled in the settings"}
	}

	shell := filepath.Base(os.Getenv("SHELL"))
	rc, kind, ok := rcFileFor(shell)
	if !ok {
		return PathResult{Action: "manual", Manual: manual, Reason: fmt.Sprintf("shell %q is not supported (bash, zsh and fish are)", shell)}
	}

	if remove {
		return removeBlock(rc, kind, dryRun)
	}
	return addBlock(rc, kind, binDir, exportLine, dryRun, manual)
}

// notReplaceable reports why aprofile must not replace rc: it is a symlink or
// read-only (FR-024). The atomic write renames a new file over the old one, which
// would turn a symlink into a regular file and a read-only file into a writable one.
// A missing file is replaceable.
func notReplaceable(rc string) string {
	fi, err := os.Lstat(rc)
	if err != nil {
		return ""
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return rc + " is a symlink; aprofile does not replace it"
	}
	if fi.Mode().Perm()&0o200 == 0 || !writable(rc) {
		return rc + " is read-only; aprofile does not replace it"
	}
	return ""
}

// rcMode is the permission an rewritten rc file keeps: its current one, or 0644.
func rcMode(rc string) os.FileMode {
	if fi, err := os.Stat(rc); err == nil {
		return fi.Mode().Perm()
	}
	return 0o644
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
		cfg := config.XDGBase("XDG_CONFIG_HOME", home, ".config")
		return filepath.Join(cfg, "fish", "conf.d", "aprofile.fish"), "file", true
	default:
		return "", "", false
	}
}

func addBlock(rc, kind, binDir, exportLine string, dryRun bool, manual string) PathResult {
	var desired string
	if kind == "file" {
		desired = fmt.Sprintf("%s\nfish_add_path %s\n%s\n", pathBlockBegin, shellSingleQuote(binDir), pathBlockEnd)
	} else {
		desired = fmt.Sprintf("%s\n%s\n%s\n", pathBlockBegin, exportLine, pathBlockEnd)
	}

	existing, err := os.ReadFile(rc) // follows a symlink
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		// An rc file that exists but cannot be read is never replaced: its content
		// would be lost.
		return PathResult{Action: "manual", RCFile: rc, Manual: manual, Reason: rc + " cannot be read; aprofile does not replace it: " + err.Error()}
	}
	current := string(existing)
	updated, changed := upsertBlock(current, desired)
	if !changed {
		return PathResult{Action: "unchanged", RCFile: rc}
	}
	if why := notReplaceable(rc); why != "" {
		// The user may have added the line by hand, as the instructions say.
		if kind == "block" && (strings.Contains(current, exportLine) || hasLegacyExportLine(current, binDir)) {
			return PathResult{Action: "unchanged", RCFile: rc}
		}
		return PathResult{Action: "manual", RCFile: rc, Manual: manual, Reason: why}
	}
	if dryRun {
		return PathResult{Action: "added", RCFile: rc, Changed: true}
	}
	if err := writeRC(rc, updated); err != nil {
		return PathResult{Action: "manual", RCFile: rc, Manual: manual, Reason: err.Error(), Err: err}
	}
	return PathResult{Action: "added", RCFile: rc, Changed: true}
}

func removeBlock(rc, kind string, dryRun bool) PathResult {
	manual := fmt.Sprintf("remove the lines between %q and %q from %s", pathBlockBegin, pathBlockEnd, rc)
	existing, err := os.ReadFile(rc)
	if err != nil {
		return PathResult{Action: "unchanged", RCFile: rc}
	}
	stripped, changed := stripBlock(string(existing))
	if !changed {
		return PathResult{Action: "unchanged", RCFile: rc}
	}
	if why := notReplaceable(rc); why != "" {
		return PathResult{Action: "manual", RCFile: rc, Manual: manual, Reason: why}
	}
	if dryRun {
		return PathResult{Action: "removed", RCFile: rc, Changed: true}
	}
	// For a dedicated fish file, remove the whole file if it is now empty.
	if kind == "file" && strings.TrimSpace(stripped) == "" {
		if err := os.Remove(rc); err != nil && !os.IsNotExist(err) {
			return PathResult{Action: "manual", RCFile: rc, Manual: manual, Reason: err.Error(), Err: err}
		}
		return PathResult{Action: "removed", RCFile: rc, Changed: true}
	}
	if err := writeRC(rc, stripped); err != nil {
		return PathResult{Action: "manual", RCFile: rc, Manual: manual, Reason: err.Error(), Err: err}
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
	return current[:start] + current[end:], true
}

// writeRC writes the rc file atomically, keeping its current permission.
func writeRC(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return config.WriteFileAtomic(path, []byte(content), rcMode(path))
}

// posixExportLine is the bash/zsh line that puts binDir on PATH. binDir is a
// single-quoted literal, so `$`, backticks and quotes in it never expand; only
// "$PATH" does.
func posixExportLine(binDir string) string {
	return "export PATH=" + wrapper.ShellQuote(binDir) + `:"$PATH"`
}

// hasLegacyExportLine reports whether current contains the line older aprofile
// versions suggested for manual setup (export PATH="<bin>:$PATH"), so a user who
// added it by hand is not asked again. Only a binDir that needs no escaping inside
// double quotes can match it.
func hasLegacyExportLine(current, binDir string) bool {
	if strings.ContainsAny(binDir, "\\\"$`") {
		return false
	}
	return strings.Contains(current, `export PATH="`+binDir+`:$PATH"`)
}

// shellSingleQuote quotes a path for fish. Inside fish single quotes only \\ and
// \' are escapes (unlike POSIX sh, which has none), so backslashes are escaped
// before quotes; otherwise a trailing backslash would escape the closing quote.
// This helper is used only for the fish conf.d snippet.
func shellSingleQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}
