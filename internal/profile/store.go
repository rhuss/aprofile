package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
)

// ErrNotFound is returned when a profile does not exist in the store.
var ErrNotFound = errors.New("profile not found")

// ErrExists is returned when creating a profile whose (harness, name) is taken.
var ErrExists = errors.New("profile already exists")

// Store reads and writes profile files under <config dir>/profiles/<harness>/.
type Store struct {
	paths *config.Paths
	// Warn, when set, receives each load warning (Profile.LoadWarnings) once per
	// store, however often the profile is loaded.
	Warn func(msg string)

	mu     sync.Mutex
	warned map[string]bool
}

// NewStore creates a store rooted at the resolved aprofile paths.
func NewStore(paths *config.Paths) *Store { return &Store{paths: paths} }

// Path returns the store file path for (harness, name).
func (s *Store) Path(harness, name string) string {
	return s.paths.ProfileStorePath(harness, name)
}

// Exists reports whether a profile file exists for (harness, name).
func (s *Store) Exists(harness, name string) bool {
	_, err := os.Stat(s.Path(harness, name))
	return err == nil
}

// Load reads one profile, validates it, and returns it. A hand-edited file that
// fails validation (including FR-028 secret checks) returns an error naming the
// profile and field.
func (s *Store) Load(harness, name string) (*Profile, error) {
	path := s.Path(harness, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, harness, name)
		}
		return nil, fmt.Errorf("read profile %s/%s: %w", harness, name, err)
	}
	p, err := decodeProfile(data, harness, name)
	if err != nil {
		return nil, err
	}
	s.warn(p.LoadWarnings)
	return p, nil
}

func (s *Store) warn(msgs []string) {
	if s.Warn == nil || len(msgs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warned == nil {
		s.warned = map[string]bool{}
	}
	for _, m := range msgs {
		if !s.warned[m] {
			s.warned[m] = true
			s.Warn(m)
		}
	}
}

// decodeProfile parses and validates a profile, back-filling identity from the
// file path so a file whose body omits harness/name still loads correctly.
func decodeProfile(data []byte, harness, name string) (*Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile %s/%s: %w", harness, name, err)
	}
	if p.Harness == "" {
		p.Harness = harness
	}
	if p.Name == "" {
		p.Name = name
	}
	if p.Harness != harness || p.Name != name {
		return nil, fmt.Errorf("profile %s/%s: file identity (%s/%s) does not match its location", harness, name, p.Harness, p.Name)
	}
	p.repairAppearance()
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.Share == "" {
		// FR-033: profiles written before share levels existed behave as all. The
		// level is written only when the profile is next saved by an edit.
		p.Share = ShareAll
		p.ShareImplicit = true
	}
	p.normalizeLists()
	return &p, nil
}

// repairAppearance clears a color or icon that fails the FR-001 checks and records
// a warning instead of failing the load: a profile file written before the checks
// existed (or hand edited) must still list, sync and be repairable with edit. Save,
// the CLI and import keep rejecting such values.
func (p *Profile) repairAppearance() {
	if err := ValidateColor(p.Color); err != nil {
		p.LoadWarnings = append(p.LoadWarnings, fmt.Sprintf("profile %s: %v; ignored (set one with: aprofile edit %s --color <color>)", p.ID(), err, p.ID()))
		p.Color = ""
	}
	if err := ValidateIcon(p.Icon); err != nil {
		p.LoadWarnings = append(p.LoadWarnings, fmt.Sprintf("profile %s: %v; ignored (set one with: aprofile edit %s --icon <icon>)", p.ID(), err, p.ID()))
		p.Icon = ""
	}
}

// Save writes a profile atomically after validating it.
func (s *Store) Save(p *Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	p.normalizeLists()
	out := *p
	if out.ShareImplicit && out.Share == ShareAll {
		// FR-033: a level the file never stored stays unstored until the user sets
		// one (edit --share clears ShareImplicit); other edits keep it that way.
		out.Share = ""
	}
	data, err := yaml.Marshal(&out)
	if err != nil {
		return fmt.Errorf("encode profile %s: %w", p.ID(), err)
	}
	return config.WriteFileAtomic(s.Path(p.Harness, p.Name), data, 0o644)
}

// Remove deletes a profile store file. It does not touch the config dir (FR-004).
func (s *Store) Remove(harness, name string) error {
	err := os.Remove(s.Path(harness, name))
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: %s/%s", ErrNotFound, harness, name)
	}
	return err
}

// Keys returns the (harness, name) of every profile file in the store, sorted by
// harness and name, without loading the files: a file that fails to load still
// counts. The wrapper-name clash checks of the CLI and of import use it (FR-002).
func (s *Store) Keys() ([]harness.ProfileKey, error) {
	root := filepath.Join(s.paths.ConfigDir, "profiles")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir %s: %w", root, err)
	}
	var keys []harness.ProfileKey
	for _, he := range entries {
		if !he.IsDir() {
			continue
		}
		hdir := filepath.Join(root, he.Name())
		files, err := os.ReadDir(hdir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdir, err)
		}
		for _, fe := range files {
			if fe.IsDir() || filepath.Ext(fe.Name()) != ".yaml" {
				continue
			}
			keys = append(keys, harness.ProfileKey{Harness: he.Name(), Name: strings.TrimSuffix(fe.Name(), ".yaml")})
		}
	}
	// os.ReadDir sorts by file name; sort by (harness, name) explicitly so a name
	// that sorts differently with its ".yaml" suffix keeps the documented order.
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Harness != keys[j].Harness {
			return keys[i].Harness < keys[j].Harness
		}
		return keys[i].Name < keys[j].Name
	})
	return keys, nil
}

// List returns every profile in the store, sorted by (harness, name). Files that
// fail to load are returned as LoadFailures so a single bad file does not hide the
// rest (FR-006, FR-022).
func (s *Store) List() ([]*Profile, []LoadFailure, error) {
	keys, err := s.Keys()
	if err != nil {
		return nil, nil, err
	}
	var profiles []*Profile
	var failures []LoadFailure
	for _, k := range keys {
		p, err := s.Load(k.Harness, k.Name)
		if err != nil {
			failures = append(failures, LoadFailure{Harness: k.Harness, Name: k.Name, Err: err})
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, failures, nil
}

// ListByHarness returns the profiles for a single harness.
func (s *Store) ListByHarness(harness string) ([]*Profile, error) {
	hdir := s.paths.ProfileStoreDir(harness)
	files, err := os.ReadDir(hdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Profile
	for _, fe := range files {
		if fe.IsDir() || filepath.Ext(fe.Name()) != ".yaml" {
			continue
		}
		name := fe.Name()[:len(fe.Name())-len(".yaml")]
		p, err := s.Load(harness, name)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// LoadFailure records a profile file that could not be loaded.
type LoadFailure struct {
	Harness string
	Name    string
	Err     error
}

func (f LoadFailure) ID() string { return f.Harness + "/" + f.Name }
