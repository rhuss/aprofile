// Package launch decides which carrier supplies each launch setting of a profile
// (the profile folder or the wrapper) and places the folder-carried ones into the
// harness's own files (feature 004, specs/004-corgi-integration). sync, its dry
// run, show, doctor, list, edit and settings pull all use the one planner here, so
// they cannot drift; the wrapper renderer takes the same Decide result, so a
// setting is never carried twice or not at all.
//
// Every harness difference is definition data (Constitution Principle I): the
// folder locations come from folder_launch, the key command form is a named form
// (shell, provider-auth). No value of a key is ever written or printed
// (Principle IV): key commands hold a path or a variable name only.
package launch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
)

// Doc is the parsed content of the files the definition's folder_launch names,
// read once.
type Doc struct {
	Files map[string]FileDoc // keyed by profile-relative file name
}

// FileDoc is one parsed folder_launch file.
type FileDoc struct {
	// Exists is true when the file exists (through a link, too).
	Exists bool
	// Linked is true when the file, or a directory on its path below the config dir,
	// is a link (into the default setup): aprofile never writes through it.
	Linked bool
	JSON   map[string]json.RawMessage // settings.json-style files
	TOML   map[string]any             // config.toml-style files
	Err    error                      // parse error, reported with file and key
	// Mode is the file's permission bits (0 when absent).
	Mode os.FileMode
}

// isTOML reports whether a profile-relative file is TOML (by extension).
func isTOML(file string) bool { return strings.EqualFold(filepath.Ext(file), ".toml") }

// ParseFileDoc parses file content as the file's format (JSON object or TOML) into
// an existing, unlinked FileDoc. An empty document parses to an empty object.
func ParseFileDoc(file string, data []byte) FileDoc {
	fd := FileDoc{Exists: true}
	if isTOML(file) {
		fd.TOML = map[string]any{}
		if len(bytes.TrimSpace(data)) > 0 {
			if err := toml.Unmarshal(data, &fd.TOML); err != nil {
				fd.Err = fmt.Errorf("not valid TOML: %w", err)
				fd.TOML = nil
			}
		}
		return fd
	}
	m, err := layer.DecodeJSONObject(data)
	if err != nil {
		fd.Err = fmt.Errorf("not a valid JSON object: %w", err)
		return fd
	}
	fd.JSON = m
	return fd
}

// files returns the profile-relative files the definition's folder_launch names,
// de-duplicated, in a fixed order (env file first), plus the files its feature
// placement rules name (spec 006).
func files(def *harness.Definition) []string {
	var out []string
	seen := map[string]bool{}
	add := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	fl := def.FolderLaunch
	if fl != nil {
		if fl.Env != "" {
			add(fl.EnvFile())
		}
		if kc := fl.KeyCommand; kc != nil {
			add(kc.File())
		}
	}
	for _, f := range featureFiles(def) {
		add(f)
	}
	return out
}

// ReadDoc reads each folder_launch file of the definition once from configDir and
// records whether it exists, whether it is linked, and a parse error.
func ReadDoc(def *harness.Definition, configDir string) Doc {
	doc := Doc{Files: map[string]FileDoc{}}
	for _, f := range files(def) {
		doc.Files[f] = readFileDoc(configDir, f)
	}
	return doc
}

// ReadDocFor is ReadDoc plus every file the record lists, so entries placed at a
// location the definition no longer names (a harnesses.d override removed or moved
// it) are read and can be removed (FR-005). A recorded path that is not a plain
// relative path inside the profile dir is skipped (the record lives on disk).
func ReadDocFor(def *harness.Definition, configDir string, rec Record) Doc {
	doc := ReadDoc(def, configDir)
	for _, e := range rec.Entries {
		if _, ok := doc.Files[e.File]; ok || !relPathSafe(e.File) {
			continue
		}
		doc.Files[e.File] = readFileDoc(configDir, e.File)
	}
	return doc
}

// relPathSafe reports whether file is a relative path with no empty, "." or ".."
// segment.
func relPathSafe(file string) bool {
	if file == "" || filepath.IsAbs(file) {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(file), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func readFileDoc(configDir, file string) FileDoc {
	path := filepath.Join(configDir, filepath.FromSlash(file))
	linked := false
	cur := configDir
	for _, seg := range strings.Split(filepath.ToSlash(file), "/") {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			break
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			linked = true
			break
		}
	}
	fi, err := os.Stat(path)
	if err != nil {
		fd := FileDoc{Linked: linked}
		if !errors.Is(err, os.ErrNotExist) {
			fd.Err = err
		}
		return fd
	}
	if !fi.Mode().IsRegular() {
		return FileDoc{Exists: true, Linked: linked, Err: fmt.Errorf("not a regular file")}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FileDoc{Exists: true, Linked: linked, Err: err}
	}
	fd := ParseFileDoc(file, data)
	fd.Linked = linked
	fd.Mode = fi.Mode().Perm()
	return fd
}
