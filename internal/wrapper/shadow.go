package wrapper

import (
	"os/exec"
	"path/filepath"
)

// ShadowedBy reports whether a different executable named wrapperName resolves on
// the current PATH before aprofile's bin dir (FR-044). It returns the shadowing
// executable's path, or "" and false when the name resolves to binDir or not at all.
// doctor and sync both use it so the two report shadowing identically.
func ShadowedBy(binDir, wrapperName string) (string, bool) {
	resolved, err := exec.LookPath(wrapperName)
	if err != nil || SameDir(filepath.Dir(resolved), binDir) {
		return "", false
	}
	return resolved, true
}

// SameDir reports whether two paths denote the same directory after cleaning and
// resolving to absolute form.
func SameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

// ShadowMessage is the shared wording for a shadowed wrapper.
func ShadowMessage(wrapperName, shadow string) string {
	return "wrapper \"" + wrapperName + "\" is shadowed by " + shadow + " earlier on PATH"
}

// ShadowFix is the shared remedy for a shadowed wrapper.
const ShadowFix = "remove the shadowing executable or put aprofile's bin dir first on PATH"
