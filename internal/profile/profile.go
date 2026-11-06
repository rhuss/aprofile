// Package profile defines the user-facing profile model and its on-disk store:
// one human-editable YAML file per profile (FR-006). It applies the FR-028 secret
// checks on every load so a hand-edited plaintext secret is rejected with an error
// that names the profile and field.
package profile

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/cc-deck/aprofile/internal/secret"
)

// namePattern matches a valid profile name (FR-002).
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}[a-z0-9]$`)

// NameValid reports whether name is a valid profile name.
func NameValid(name string) bool { return namePattern.MatchString(name) }

// Profile is one account/configuration of one harness (FR-001). Identity is the
// pair (Harness, Name) (FR-002).
type Profile struct {
	Harness    string                      `yaml:"harness"`
	Name       string                      `yaml:"name"`
	Label      string                      `yaml:"label,omitempty"`
	Color      string                      `yaml:"color,omitempty"`
	Icon       string                      `yaml:"icon,omitempty"`
	Primary    bool                        `yaml:"primary,omitempty"`
	AuthKind   string                      `yaml:"auth_kind,omitempty"`
	AuthRefs   map[string]secret.Reference `yaml:"auth_refs,omitempty"`
	Model      string                      `yaml:"model,omitempty"`
	Endpoint   string                      `yaml:"endpoint,omitempty"`
	ExtraEnv   []ExtraEnvVar               `yaml:"extra_env,omitempty"`
	HerdrAgent string                      `yaml:"herdr_agent,omitempty"`
	// Share is how much of the default setup the profile uses (feature 002). A file
	// written before this feature has none; it loads as all (FR-033).
	Share Share `yaml:"share,omitempty"`
	// HookSets names the hook sets attached to the profile; ExcludeHookSets names
	// every-profile sets it opts out of (FR-025, FR-026). Both are stored sorted.
	HookSets        []string `yaml:"hook_sets,omitempty"`
	ExcludeHookSets []string `yaml:"exclude_hook_sets,omitempty"`

	// ShareImplicit is true when the file did not store a share level and the
	// profile was loaded as all (FR-033); show uses it for the upgrade hint.
	ShareImplicit bool `yaml:"-"`
}

// Share is a profile's share level.
type Share string

// The three share levels (FR-006).
const (
	ShareAll     Share = "all"
	ShareContent Share = "content"
	ShareNone    Share = "none"
)

// Shares lists the levels in display order.
var Shares = []Share{ShareAll, ShareContent, ShareNone}

// Valid reports whether s is one of the three levels.
func (s Share) Valid() bool {
	return s == ShareAll || s == ShareContent || s == ShareNone
}

// ParseShare validates a level given on the command line.
func ParseShare(s string) (Share, error) {
	if !Share(s).Valid() {
		return "", fmt.Errorf("share level %q must be all, content or none", s)
	}
	return Share(s), nil
}

// ShareLevel returns the effective level: the stored one, or all for a profile
// file written before share levels existed (FR-033).
func (p *Profile) ShareLevel() Share {
	if p.Share == "" {
		return ShareAll
	}
	return p.Share
}

// normalizeLists sorts and de-duplicates the hook-set lists so the stored file is
// deterministic.
func (p *Profile) normalizeLists() {
	p.HookSets = sortedUnique(p.HookSets)
	p.ExcludeHookSets = sortedUnique(p.ExcludeHookSets)
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// ExtraEnvVar is one extra environment variable the wrapper sets. Value is either
// a reference (env:/file:) or a literal (FR-027, FR-028).
type ExtraEnvVar struct {
	Name    string `yaml:"name"`
	Value   string `yaml:"value"`
	Literal bool   `yaml:"literal,omitempty"`
}

// ID returns the canonical "<harness>/<name>" identifier used in messages.
func (p *Profile) ID() string { return p.Harness + "/" + p.Name }

// DisplayLabel returns Label, falling back to Name.
func (p *Profile) DisplayLabel() string {
	if p.Label != "" {
		return p.Label
	}
	return p.Name
}

// Validate checks the profile's static invariants and applies the FR-028 secret
// checks. It is called on every load and before every save, so a plaintext secret
// never reaches a generated artifact. Errors name the profile and field.
func (p *Profile) Validate() error {
	if !NameValid(p.Name) {
		return fmt.Errorf("profile %s: name must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", p.ID())
	}
	if p.Harness == "" {
		return fmt.Errorf("profile %s: harness is required", p.ID())
	}
	if p.Share != "" && !p.Share.Valid() {
		return fmt.Errorf("profile %s: share level %q must be all, content or none", p.ID(), p.Share)
	}
	for _, list := range [][]string{p.HookSets, p.ExcludeHookSets} {
		for _, n := range list {
			if !NameValid(n) {
				return fmt.Errorf("profile %s: hook set name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", p.ID(), n)
			}
		}
	}
	// Auth references accept only env:/file: (already enforced by Reference's
	// UnmarshalYAML on load; re-check here for programmatically built profiles).
	for v, ref := range p.AuthRefs {
		// The variable name is interpolated into the wrapper as an unset/assignment
		// target, so it must be a safe shell identifier (FR-028b).
		if !secret.ValidEnvVarName(v) {
			return fmt.Errorf("profile %s: auth variable name %q must match [A-Za-z_][A-Za-z0-9_]*", p.ID(), v)
		}
		if ref.Raw == "" {
			return fmt.Errorf("profile %s: auth variable %q has an empty reference", p.ID(), v)
		}
		if _, err := secret.ParseReference(ref.Raw); err != nil {
			return fmt.Errorf("profile %s: auth variable %q: %w", p.ID(), v, err)
		}
	}
	// Extra env: references pass through; literals get the FR-028 checks (prefix
	// always; length unless marked literal).
	for _, e := range p.ExtraEnv {
		// The name becomes an assignment target / export / unset in the generated
		// wrapper; a name with shell metacharacters would inject code, so validate it
		// on every load, not just on the CLI path (FR-028b).
		if !secret.ValidEnvVarName(e.Name) {
			return fmt.Errorf("profile %s: extra env name %q must match [A-Za-z_][A-Za-z0-9_]*", p.ID(), e.Name)
		}
		if !secret.IsReference(e.Value) {
			if err := secret.ValidateValue(e.Value, e.Literal); err != nil {
				return fmt.Errorf("profile %s: extra env %q: %w", p.ID(), e.Name, err)
			}
			continue
		}
		if _, err := secret.ParseReference(e.Value); err != nil {
			return fmt.Errorf("profile %s: extra env %q: %w", p.ID(), e.Name, err)
		}
	}
	return nil
}
