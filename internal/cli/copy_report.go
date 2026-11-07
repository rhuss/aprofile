package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// This file builds the parts of the copy report that look beyond the MCP servers
// (FR-033a, FR-033b, FR-021, FR-036). Everything is derived from the harness
// definitions' data (classification, mcp location, plugin store, skills location,
// home escapes); nothing branches on a harness name (Principle I).

// mcpKeyOf returns the MCP file (relative, slash form) and the key inside it.
func mcpKeyOf(def *harness.Definition) (file, key string) {
	if def.MCP == nil || def.MCP.Location == "" {
		return "", ""
	}
	loc := def.MCP.Location
	if i := strings.IndexByte(loc, '#'); i >= 0 {
		return loc[:i], loc[i+1:]
	}
	return loc, ""
}

// decodeStructured decodes a JSON or TOML document into generic values. It reports
// false for any other or unparsable content.
func decodeStructured(rel string, data []byte) (map[string]any, bool) {
	var m map[string]any
	if strings.EqualFold(filepath.Ext(rel), ".toml") {
		if err := toml.Unmarshal(data, &m); err != nil {
			return nil, false
		}
		return m, true
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	return m, true
}

// ownedSecretWarnings scans every owned-layer value outside the MCP servers (whole
// owned files and the owned keys of mixed files) for values matching a secret
// pattern (the FR-028 prefix or length check) and returns "<file>: <key path>" for
// each. The MCP servers are reported per server and field by mcp.SecretWarnings, so
// their key is skipped here. No value is ever included (FR-033a).
func ownedSecretWarnings(def *harness.Definition, dir string) []string {
	mcpFile, mcpKey := mcpKeyOf(def)
	var out []string
	scanFile := func(rel string, only []string) {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return
		}
		doc, ok := decodeStructured(rel, data)
		if !ok {
			return
		}
		skip := ""
		if rel == mcpFile {
			skip = mcpKey
		}
		var hits []string
		if only == nil {
			walkSecretValues("", doc, skip, &hits)
		} else {
			for _, k := range only {
				if v, ok := doc[k]; ok && k != skip {
					walkSecretValues(k, v, skip, &hits)
				}
			}
		}
		for _, h := range hits {
			out = append(out, rel+": "+h)
		}
	}
	for _, rule := range def.Classification {
		switch rule.Layer {
		case harness.LayerOwned:
			for _, rel := range ownedMatches(rule, dir) {
				p := filepath.Join(dir, filepath.FromSlash(rel))
				fi, err := os.Lstat(p)
				if err != nil || fi.Mode()&os.ModeSymlink != 0 {
					continue
				}
				if !fi.IsDir() {
					scanFile(rel, nil)
					continue
				}
				_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
						return nil
					}
					r, _ := filepath.Rel(dir, path)
					scanFile(filepath.ToSlash(r), nil)
					return nil
				})
			}
		case harness.LayerMixed:
			var keys []string
			for k, l := range rule.Keys {
				if l == harness.LayerOwned {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			if len(keys) > 0 {
				scanFile(rule.Path(), keys)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ownedMatches returns the profile-relative paths (slash form) an owned rule names in
// dir: the entry itself, or every match of a glob entry.
func ownedMatches(rule harness.EntryRule, dir string) []string {
	if !rule.IsGlob() {
		if layer.PathExists(filepath.Join(dir, filepath.FromSlash(rule.Path()))) {
			return []string{rule.Path()}
		}
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(rule.Path())))
	var out []string
	for _, m := range matches {
		if r, err := filepath.Rel(dir, m); err == nil {
			out = append(out, filepath.ToSlash(r))
		}
	}
	sort.Strings(out)
	return out
}

// walkSecretValues appends the key path of every string leaf under v that looks like
// a literal secret. skip names a top-level key path left out of the walk.
func walkSecretValues(path string, v any, skip string, out *[]string) {
	if skip != "" && path == skip {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkSecretValues(joinKey(path, k), t[k], skip, out)
		}
	case []any:
		for i, c := range t {
			walkSecretValues(fmt.Sprintf("%s[%d]", path, i), c, skip, out)
		}
	case string:
		if secret.LooksLikeLiteralSecret(t, ownedSecretOptions) {
			*out = append(*out, path)
		}
	}
}

func joinKey(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

// ownedSecretOptions is how an owned value is judged (FR-033a): both FR-028 checks,
// references are not literals, and a path or URL gets the prefix check on its
// segments only (it is long without being secret), the same rule MCP args get.
var ownedSecretOptions = secret.Options{AllowPaths: true, SkipReferences: true}

// ownedNotConverted lists every owned entry or owned key of the source that a
// cross-harness copy does not convert: only the MCP servers are converted, so every
// other key of the MCP file ("<file>#<key>"), every other owned entry and every other
// owned key of a mixed file is reported (FR-033b).
func ownedNotConverted(def *harness.Definition, dir string) []string {
	mcpFile, mcpKey := mcpKeyOf(def)
	var out []string
	topKeys := func(rel string) ([]string, bool) {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, false
		}
		doc, ok := decodeStructured(rel, data)
		if !ok {
			return nil, false
		}
		keys := make([]string, 0, len(doc))
		for k := range doc {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys, true
	}
	for _, rule := range def.Classification {
		switch rule.Layer {
		case harness.LayerOwned:
			for _, rel := range ownedMatches(rule, dir) {
				if rel != mcpFile || mcpKey == "" {
					out = append(out, rel)
					continue
				}
				keys, ok := topKeys(rel)
				if !ok {
					out = append(out, rel+" (unreadable)")
					continue
				}
				for _, k := range keys {
					if k != mcpKey {
						out = append(out, rel+"#"+k)
					}
				}
			}
		case harness.LayerMixed:
			keys, ok := topKeys(rule.Path())
			if !ok {
				continue
			}
			for _, k := range keys {
				if rule.Keys[k] != harness.LayerOwned || (rule.Path() == mcpFile && k == mcpKey) {
					continue
				}
				out = append(out, rule.Path()+"#"+k)
			}
		}
	}
	return out
}

// skippedPlugin is one plugin a cross-harness copy does not carry, with the skills and
// MCP servers inside it the user can copy separately (FR-036).
type skippedPlugin struct {
	Name       string   `json:"name"`
	Skills     []string `json:"skills,omitempty"`
	MCPServers []string `json:"mcp_servers,omitempty"`
}

// findPlugins lists the source's installed plugins from its plugin store (the
// definition's plugin-store entries, followed through a link to the default setup).
// An install record (a JSON file at the top of a store directory with a "plugins"
// object) names the plugins and their install paths; without one, directories holding
// a plugin manifest (plugin.json, directly or in a ".<x>-plugin" directory) are the
// plugins.
func findPlugins(def *harness.Definition, dir string) []skippedPlugin {
	var stores []string
	for _, rule := range def.Classification {
		if rule.Layer != harness.LayerPluginStore || rule.IsGlob() || !rule.IsDir() {
			continue
		}
		p, err := filepath.EvalSymlinks(filepath.Join(dir, filepath.FromSlash(rule.Path())))
		if err != nil {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			stores = append(stores, p)
		}
	}
	byName := map[string]*skippedPlugin{}
	add := func(name string, roots []string) {
		sp := byName[name]
		if sp == nil {
			sp = &skippedPlugin{Name: name}
			byName[name] = sp
		}
		for _, r := range roots {
			sp.Skills = mergeNames(sp.Skills, entryNames(filepath.Join(r, "skills")))
			sp.MCPServers = mergeNames(sp.MCPServers, pluginMCPServers(r))
		}
	}
	for _, s := range stores {
		for name, roots := range installRecords(s) {
			add(name, roots)
		}
	}
	if len(byName) == 0 {
		for _, s := range stores {
			for name, root := range manifestPlugins(s) {
				add(name, []string{root})
			}
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]skippedPlugin, 0, len(names))
	for _, n := range names {
		out = append(out, *byName[n])
	}
	return out
}

// installRecords reads the install records at the top of a plugin store directory.
func installRecords(store string) map[string][]string {
	out := map[string][]string{}
	entries, _ := os.ReadDir(store)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(store, e.Name()))
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		plugins, ok := doc["plugins"].(map[string]any)
		if !ok {
			continue
		}
		for name, v := range plugins {
			out[name] = append(out[name], installPaths(v)...)
		}
	}
	return out
}

// installPaths collects every "installPath" string in an install record value.
func installPaths(v any) []string {
	var out []string
	switch t := v.(type) {
	case map[string]any:
		if p, ok := t["installPath"].(string); ok && p != "" {
			out = append(out, harness.ExpandHome(p))
		}
	case []any:
		for _, c := range t {
			out = append(out, installPaths(c)...)
		}
	}
	return out
}

// manifestPlugins finds directories holding a plugin manifest under a store, at most
// a few levels deep, keyed by the manifest's name (or the directory name).
func manifestPlugins(store string) map[string]string {
	out := map[string]string{}
	base := strings.Count(store, string(filepath.Separator))
	_ = filepath.WalkDir(store, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if strings.Count(path, string(filepath.Separator))-base > 5 {
			return filepath.SkipDir
		}
		manifest := ""
		if layer.PathExists(filepath.Join(path, "plugin.json")) {
			manifest = filepath.Join(path, "plugin.json")
		} else if m, _ := filepath.Glob(filepath.Join(path, ".*-plugin", "plugin.json")); len(m) > 0 {
			manifest = m[0]
		}
		if manifest == "" {
			return nil
		}
		name := filepath.Base(path)
		if data, err := os.ReadFile(manifest); err == nil {
			var doc struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(data, &doc) == nil && doc.Name != "" {
				name = doc.Name
			}
		}
		if _, seen := out[name]; !seen {
			out[name] = path
		}
		return filepath.SkipDir
	})
	return out
}

// pluginMCPServers lists the MCP servers a plugin declares in its .mcp.json or
// mcp.json: the keys of an "mcpServers" object, or the top-level object entries.
func pluginMCPServers(root string) []string {
	for _, f := range []string{".mcp.json", "mcp.json"} {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		servers := doc
		if m, ok := doc["mcpServers"].(map[string]any); ok {
			servers = m
		}
		var names []string
		for k, v := range servers {
			if _, ok := v.(map[string]any); ok {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		return names
	}
	return nil
}

// entryNames lists the non-hidden entries of a directory (following a link), sorted.
func entryNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func mergeNames(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func describePlugin(p skippedPlugin) string {
	var parts []string
	if len(p.Skills) > 0 {
		parts = append(parts, "skills: "+strings.Join(p.Skills, ", "))
	}
	if len(p.MCPServers) > 0 {
		parts = append(parts, "MCP servers: "+strings.Join(p.MCPServers, ", "))
	}
	s := "plugin " + p.Name + " not carried across harnesses"
	if len(parts) > 0 {
		s += " (contains " + strings.Join(parts, "; ") + "; copy them separately)"
	}
	return s
}

// skillsHomeEscapes returns the definition's $HOME escapes that hold skills.
func skillsHomeEscapes(def *harness.Definition) []string {
	var out []string
	for _, he := range def.HomeEscapes {
		if he.What == "skills" {
			out = append(out, he.Path)
		}
	}
	return out
}

// skillsLinked reports whether the definition links the skills entry to the default
// setup at level (the entry's category treatment from the share policy).
func skillsLinked(def *harness.Definition, level profile.Share) bool {
	if def.SkillsLocation == nil {
		return false
	}
	for _, rule := range def.Classification {
		if rule.Path() == strings.TrimSuffix(def.SkillsLocation.Path, "/") {
			return rule.Layer.Borrowable() && def.Treatment(rule.Layer, string(level)) == harness.TreatLink
		}
	}
	return false
}

// effectiveSkills lists the skills a profile sees: its skills location (followed
// through a link, or the default setup's when it links skills and the link is not in
// place) plus its $HOME escapes.
func effectiveSkills(def *harness.Definition, level profile.Share, dir string) []string {
	var names []string
	if def.SkillsLocation != nil {
		p := skillsPath(def, dir)
		if !layer.PathExists(p) && skillsLinked(def, level) {
			p = def.DefaultPath(def.SkillsLocation.Path)
		}
		names = entryNames(p)
	}
	for _, esc := range skillsHomeEscapes(def) {
		names = mergeNames(names, entryNames(harness.ExpandHome(esc)))
	}
	return names
}

// skillsAvailability reports, for a cross-harness target whose skills are linked or
// read from $HOME (FR-021, FR-036), which of the source's skills the target already
// sees and which are missing, and names the target's escapes. It reports nothing when
// the source has no skills or the target's skills are only its own.
func skillsAvailability(srcDef, dstDef *harness.Definition, srcLevel, dstLevel profile.Share, srcDir, dstDir string) (in, missing, escapes []string) {
	escapes = skillsHomeEscapes(dstDef)
	if !skillsLinked(dstDef, dstLevel) && len(escapes) == 0 {
		return nil, nil, nil
	}
	src := effectiveSkills(srcDef, srcLevel, srcDir)
	if len(src) == 0 {
		return nil, nil, nil
	}
	have := map[string]bool{}
	for _, s := range effectiveSkills(dstDef, dstLevel, dstDir) {
		have[s] = true
	}
	for _, s := range src {
		if have[s] {
			in = append(in, s)
		} else {
			missing = append(missing, s)
		}
	}
	return in, missing, escapes
}
