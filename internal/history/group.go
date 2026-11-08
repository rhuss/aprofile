// Package history manages history groups (spec 003): named sets of profiles of one
// harness that share one history. Every harness whose definition declares history
// entries has a built-in home group whose store is the default setup; further
// groups are created explicitly and keep their store in aprofile's data directory.
// Membership lives in the profile files; group records only hold the name and the
// creation time, so members are computed by scanning profiles.
package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// Home is the name of the built-in group of every harness that supports groups.
const Home = "home"

// Errors of the "operation not valid" class (exit 2, contracts/cli.md).
var (
	ErrUnknownGroup   = errors.New("unknown history group")
	ErrNotSupported   = errors.New("harness does not support history groups")
	ErrBuiltinGroup   = errors.New("home is a built-in history group")
	ErrGroupExists    = errors.New("history group exists")
	ErrHasMembers     = errors.New("history group has members")
	ErrInvalidName    = errors.New("invalid history group name")
	ErrKeptStore      = errors.New("a kept store exists at the group's store path")
	ErrUnsuitableHome = errors.New("the default location cannot serve as the home store")
	ErrUnknownHarness = errors.New("unknown harness")
	ErrStoreIsProfile = errors.New("the store entry resolves into the profile's config dir")
)

// IsInvalid reports whether err is an "operation not valid" error (exit 2).
func IsInvalid(err error) bool {
	for _, k := range []error{ErrUnknownGroup, ErrNotSupported, ErrBuiltinGroup, ErrGroupExists,
		ErrHasMembers, ErrInvalidName, ErrKeptStore, ErrUnsuitableHome, ErrUnknownHarness, ErrStoreIsProfile} {
		if errors.Is(err, k) {
			return true
		}
	}
	return false
}

// Group is one history group of a harness.
type Group struct {
	Harness string
	Name    string
	Builtin bool
	// Store is the store root: the default location for home, the data-dir store
	// otherwise.
	Store string
	// Members are the names of the profiles whose history_group is Name, sorted.
	Members []string
	// DefaultSetupMember is true for home: the default setup is its implicit member
	// and member of no other group (FR-017).
	DefaultSetupMember bool
	// Created is the record's creation time (zero for home).
	Created time.Time
}

// ID returns "<harness>/<group>".
func (g *Group) ID() string { return g.Harness + "/" + g.Name }

// record is the on-disk group record (research R5).
type record struct {
	Harness string    `yaml:"harness"`
	Name    string    `yaml:"name"`
	Created time.Time `yaml:"created"`
}

// Manager lists, reads, creates and deletes history groups.
type Manager struct {
	paths    *config.Paths
	defs     *harness.Set
	profiles *profile.Store
}

// NewManager returns a Manager over the given paths, definitions and profiles.
func NewManager(paths *config.Paths, defs *harness.Set, profiles *profile.Store) *Manager {
	return &Manager{paths: paths, defs: defs, profiles: profiles}
}

// definition returns the definition of a harness that supports groups.
func (m *Manager) definition(harnessName string) (*harness.Definition, error) {
	def, ok := m.defs.Get(harnessName)
	if !ok {
		return nil, invalidf(ErrUnknownHarness, "unknown harness %q", harnessName)
	}
	if !def.SupportsGroups() {
		return nil, invalidf(ErrNotSupported, "harness %s does not support history groups: its definition declares no history entries", harnessName)
	}
	return def, nil
}

// HomeStore returns the home store of a harness.
func (m *Manager) HomeStore(def *harness.Definition) *Store {
	return homeStore(def, m.paths.Home)
}

// StoreOf returns the store of a group by name, without checking that it exists.
func (m *Manager) StoreOf(def *harness.Definition, group string) *Store {
	if group == Home {
		return m.HomeStore(def)
	}
	return dataStore(def, m.paths, group)
}

// StoreFor returns the store of the profile's group: nil for an isolated profile,
// ErrUnknownGroup when the group does not exist, ErrNotSupported when the harness
// declares no history entries.
func (m *Manager) StoreFor(def *harness.Definition, p *profile.Profile) (*Store, error) {
	if p == nil || p.HistoryGroup == "" {
		return nil, nil
	}
	if !def.SupportsGroups() {
		return nil, invalidf(ErrNotSupported, "profile %s names history group %s, but harness %s does not support history groups", p.ID(), p.HistoryGroup, def.Name)
	}
	if !m.exists(def.Name, p.HistoryGroup) {
		return nil, m.unknown(def.Name, p.HistoryGroup)
	}
	return m.StoreOf(def, p.HistoryGroup), nil
}

// exists reports whether a group exists (home always does).
func (m *Manager) exists(harnessName, group string) bool {
	if group == Home {
		return true
	}
	_, err := os.Stat(m.paths.GroupRecordPath(harnessName, group))
	return err == nil
}

// Names returns the group names of a harness, home first, then sorted.
func (m *Manager) Names(harnessName string) []string {
	names := []string{Home}
	entries, err := os.ReadDir(filepath.Join(m.paths.GroupsConfigDir(), harnessName))
	if err != nil {
		return names
	}
	var own []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || filepath.Ext(n) != ".yaml" {
			continue
		}
		n = strings.TrimSuffix(n, ".yaml")
		if n != Home && profile.NameValid(n) {
			own = append(own, n)
		}
	}
	sort.Strings(own)
	return append(names, own...)
}

// unknown builds the ErrUnknownGroup error listing the harness's groups (FR-009).
func (m *Manager) unknown(harnessName, group string) error {
	return invalidf(ErrUnknownGroup, "history group %s/%s does not exist; groups of %s: %s (create one with: aprofile group create %s/<group>)",
		harnessName, group, harnessName, strings.Join(m.Names(harnessName), ", "), harnessName)
}

// Get returns one group. home is synthesized for every harness that supports
// groups; other groups need a record.
func (m *Manager) Get(harnessName, group string) (*Group, error) {
	def, err := m.definition(harnessName)
	if err != nil {
		return nil, err
	}
	var g *Group
	if group == Home {
		g = &Group{Harness: def.Name, Name: Home, Builtin: true, Store: m.HomeStore(def).Root(), DefaultSetupMember: true}
	} else {
		if !profile.NameValid(group) {
			return nil, m.unknown(harnessName, group)
		}
		data, err := os.ReadFile(m.paths.GroupRecordPath(harnessName, group))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, m.unknown(harnessName, group)
			}
			return nil, err
		}
		var rec record
		if err := yaml.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("history group %s/%s: %w", harnessName, group, err)
		}
		g = &Group{Harness: def.Name, Name: group, Store: m.StoreOf(def, group).Root(), Created: rec.Created}
	}
	members, err := m.members(harnessName, group)
	if err != nil {
		return nil, err
	}
	g.Members = members
	return g, nil
}

// members returns the sorted names of the profiles of a harness in group.
func (m *Manager) members(harnessName, group string) ([]string, error) {
	profiles, err := m.profiles.ListByHarness(harnessName)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range profiles {
		if p.HistoryGroup == group {
			out = append(out, p.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// List returns the groups of one harness (home first), or of every harness that
// supports groups when harnessName is empty.
func (m *Manager) List(harnessName string) ([]*Group, error) {
	var harnesses []string
	if harnessName != "" {
		if _, err := m.definition(harnessName); err != nil {
			return nil, err
		}
		harnesses = []string{harnessName}
	} else {
		for _, d := range m.defs.All() {
			if d.SupportsGroups() {
				harnesses = append(harnesses, d.Name)
			}
		}
	}
	var out []*Group
	for _, h := range harnesses {
		for _, n := range m.Names(h) {
			g, err := m.Get(h, n)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
	}
	return out, nil
}

// Create creates a group of a harness that supports groups: its record and its
// empty store in aprofile's data directory, with one empty directory per directory
// history entry (FR-006). It refuses (exit-2 class) home, an invalid name, an
// existing group, and a kept store of a deleted group at the store path, which is
// never adopted implicitly.
func (m *Manager) Create(harnessName, group string, now time.Time) (*Group, error) {
	def, err := m.definition(harnessName)
	if err != nil {
		return nil, err
	}
	if group == Home {
		return nil, invalidf(ErrBuiltinGroup, "%s/home is built in: it always exists and cannot be created, renamed or deleted", harnessName)
	}
	if !profile.NameValid(group) {
		return nil, invalidf(ErrInvalidName, "history group name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", group)
	}
	if m.exists(harnessName, group) {
		return nil, invalidf(ErrGroupExists, "history group %s/%s already exists", harnessName, group)
	}
	st := dataStore(def, m.paths, group)
	if _, err := os.Lstat(st.Root()); err == nil {
		return nil, invalidf(ErrKeptStore, "a kept store of a deleted group %s/%s exists at %s; move or remove it by hand first (aprofile never adopts a kept store)", harnessName, group, st.Root())
	}
	data, err := yaml.Marshal(record{Harness: harnessName, Name: group, Created: now.UTC().Truncate(time.Second)})
	if err != nil {
		return nil, err
	}
	// The store did not exist before this call (checked above): on any failure it
	// is removed again, so a failed create never leaves a "kept store" behind.
	fail := func(err error) (*Group, error) {
		_ = os.RemoveAll(st.Root())
		return nil, fmt.Errorf("create store of %s/%s: %w", harnessName, group, err)
	}
	for _, r := range def.HistoryRules() {
		dir := st.EntryPath(r.Path())
		if !r.IsDir() {
			// A history file comes into existence when the harness writes it; its
			// directory must exist for that write to succeed through the link.
			dir = filepath.Dir(dir)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail(err)
		}
	}
	if err := os.MkdirAll(st.Root(), 0o755); err != nil {
		return fail(err)
	}
	if err := config.WriteFileAtomic(m.paths.GroupRecordPath(harnessName, group), data, 0o644); err != nil {
		// The store was created by this call and is still empty: remove it, so a
		// failed create does not leave a "kept store" that blocks the next one.
		_ = os.RemoveAll(st.Root())
		return nil, fmt.Errorf("write group record: %w", err)
	}
	return m.Get(harnessName, group)
}

// CheckDelete reports why a group cannot be deleted (exit-2 class): home is built
// in, an unknown group, or a group that still has members (listed, FR-008).
func (m *Manager) CheckDelete(harnessName, group string) (*Group, error) {
	if _, err := m.definition(harnessName); err != nil {
		return nil, err
	}
	if group == Home {
		return nil, invalidf(ErrBuiltinGroup, "%s/home is built in: it always exists and cannot be created, renamed or deleted", harnessName)
	}
	g, err := m.Get(harnessName, group)
	if err != nil {
		return nil, err
	}
	if len(g.Members) > 0 {
		return nil, invalidf(ErrHasMembers, "history group %s still has members: %s; move them out first with: aprofile edit %s/<name> --no-group (or --group <other>)",
			g.ID(), strings.Join(g.Members, ", "), harnessName)
	}
	return g, nil
}

// Delete removes a group without members (FR-008). Without purge the store is kept
// and its path returned; with purge the store is deleted too, but only when it lies
// under aprofile's group stores directory (a home store is never deleted).
func (m *Manager) Delete(harnessName, group string, purge bool) (keptStore string, err error) {
	g, err := m.CheckDelete(harnessName, group)
	if err != nil {
		return "", err
	}
	if purge && (g.Builtin || !within(g.Store, m.paths.GroupStoresDir(harnessName)) || filepath.Clean(g.Store) == filepath.Clean(m.paths.GroupStoresDir(harnessName))) {
		return g.Store, fmt.Errorf("refusing to delete %s: it is not a group store under %s", g.Store, m.paths.GroupStoresDir(harnessName))
	}
	if err := os.Remove(m.paths.GroupRecordPath(harnessName, group)); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove group record: %w", err)
	}
	if !purge {
		return g.Store, nil
	}
	if err := os.RemoveAll(g.Store); err != nil {
		return g.Store, fmt.Errorf("delete store %s: %w", g.Store, err)
	}
	return "", nil
}

// KeptStores lists the store directories of a harness that belong to no group (kept
// stores of deleted groups, FR-021 information), sorted.
func (m *Manager) KeptStores(harnessName string) []string {
	entries, err := os.ReadDir(m.paths.GroupStoresDir(harnessName))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(m.paths.GroupRecordPath(harnessName, e.Name())); os.IsNotExist(err) {
			out = append(out, filepath.Join(m.paths.GroupStoresDir(harnessName), e.Name()))
		}
	}
	sort.Strings(out)
	return out
}
