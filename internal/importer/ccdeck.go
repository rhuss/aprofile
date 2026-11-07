package importer

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// cc-deck on-disk layout, verified against the cc-deck source (internal/config/
// config.go, internal/config/profile.go, internal/profile/translator.go and
// sync.go):
//
//	<cc-deck config>/config.yaml                       profiles + default_profile
//	<cc-deck config>/profiles/<harness>/<name>/<field> cc-deck's copy of each
//	                                                   file-sourced credential
//	                                                   (api_key, oauth_token,
//	                                                   credentials)
//	<cc-deck data>/profiles/<name>/<harness>/          the live harness config dir
//
// config.yaml groups profiles by harness:
//
//	default_profile: claude/private        # "<harness>/<name>" or an unambiguous name
//	profiles:
//	  claude:
//	    work:
//	      backend: vertex                  # anthropic | vertex | openai (default per harness)
//	      project: ...                     # vertex only
//	      region: ...                      # vertex only
//	      model: ...
//	      auth:
//	        login: true                    # or one or more of:
//	        api_key: {env: VAR | file: PATH | secret: NAME}
//	        oauth_token: {env: VAR | file: PATH}
//	        credentials: {env: VAR | file: PATH | secret: NAME}
//	      env: {NAME: value}
//	      color: '#RRGGBB'
//	      icon: X
//
// Kubernetes-only fields (api_key_secret, credentials_secret, permissions,
// allowed_egress, git_*) have no aprofile equivalent.

// ccConfigFile is the cc-deck config file name inside its config dir.
const ccConfigFile = "config.yaml"

type ccConfig struct {
	DefaultProfile string                          `yaml:"default_profile"`
	Profiles       map[string]map[string]ccProfile `yaml:"profiles"`
}

// ccSource is cc-deck's CredentialSource: exactly one of Env, File or Secret.
type ccSource struct {
	Env    string `yaml:"env"`
	File   string `yaml:"file"`
	Secret string `yaml:"secret"`
}

type ccAuth struct {
	APIKey      *ccSource `yaml:"api_key"`
	OAuthToken  *ccSource `yaml:"oauth_token"`
	Credentials *ccSource `yaml:"credentials"`
	Login       bool      `yaml:"login"`
}

type ccProfile struct {
	Backend           string            `yaml:"backend"`
	Model             string            `yaml:"model"`
	Project           string            `yaml:"project"`
	Region            string            `yaml:"region"`
	APIKeySecret      string            `yaml:"api_key_secret"`
	CredentialsSecret string            `yaml:"credentials_secret"`
	Auth              *ccAuth           `yaml:"auth"`
	Env               map[string]string `yaml:"env"`
	Color             string            `yaml:"color"`
	Icon              string            `yaml:"icon"`
}

// ccEntry is one cc-deck profile with its identity and primary flag resolved.
type ccEntry struct {
	Harness string
	Name    string
	Primary bool
	Profile ccProfile
}

// readCCDeck reads <sourceDir>/config.yaml. A missing file yields no profiles (not
// an error). Global warnings (for example an ambiguous default_profile) are
// returned separately.
func readCCDeck(sourceDir string) ([]ccEntry, []string, error) {
	path := filepath.Join(sourceDir, ccConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read cc-deck config %s: %w", path, err)
	}
	var cfg ccConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parse cc-deck config %s: %w", path, err)
	}
	var out []ccEntry
	for h, group := range cfg.Profiles {
		for n, p := range group {
			out = append(out, ccEntry{Harness: h, Name: n, Profile: p})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Harness != out[j].Harness {
			return out[i].Harness < out[j].Harness
		}
		return out[i].Name < out[j].Name
	})

	var warnings []string
	if ref := cfg.DefaultProfile; ref != "" {
		var matches []int
		for i, e := range out {
			if e.Harness+"/"+e.Name == ref || (!strings.Contains(ref, "/") && e.Name == ref) {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 1:
			out[matches[0]].Primary = true
		case 0:
			warnings = append(warnings, fmt.Sprintf("cc-deck default_profile %q names no profile; no profile was marked primary", ref))
		default:
			warnings = append(warnings, fmt.Sprintf("cc-deck default_profile %q is ambiguous; no profile was marked primary (use aprofile edit --primary)", ref))
		}
	}
	return out, warnings, nil
}

// ccLiveDir is cc-deck's live harness config dir for one profile. Note the order:
// cc-deck keys it by profile name first, then harness.
func ccLiveDir(sourceDataDir, harnessName, name string) string {
	return filepath.Join(sourceDataDir, "profiles", name, harnessName)
}

// ccCredCopy is cc-deck's copy of a file-sourced credential field.
func ccCredCopy(sourceDir, harnessName, name, field string) string {
	return filepath.Join(sourceDir, "profiles", harnessName, name, field)
}

// --- mapping data (Constitution I: keyed by cc-deck's own field and backend
// names, never by harness name) ---

// Credential fields of cc-deck's auth block. A key-bearing field maps to the
// aprofile auth kind of the same meaning, which each harness definition declares
// with its own variable; cc-deck's login maps to the definition's auth kind
// without variables.
const (
	fieldAPIKey      = "api_key"
	fieldOAuthToken  = "oauth_token"
	fieldCredentials = "credentials"
)

// keyAuthKinds lists the key-bearing cc-deck auth fields in cc-deck's precedence
// order (internal/profile/translator.go Kind: oauth_token wins over api_key) with
// the aprofile auth kind name each maps to.
var keyAuthKinds = []struct{ field, kind string }{
	{fieldOAuthToken, "oauth-token"},
	{fieldAPIKey, "api-key"},
}

// ccBackend is the environment a cc-deck backend implies. Values may name the
// profile's {project} or {region}; an entry whose value expands to empty is left
// out, as cc-deck's renderer does.
type ccBackend struct {
	env [][2]string
	// credentialsVar receives auth.credentials: a file source becomes the path of
	// the imported copy, an env source a reference.
	credentialsVar string
}

// ccBackends maps cc-deck's backend names (empty = the harness's default) to the
// environment cc-deck's wrapper sets for them (internal/profile/claude.go Render).
// cc-deck offers vertex for Claude Code only, so its variables are Claude Code's.
var ccBackends = map[string]ccBackend{
	"":          {},
	"anthropic": {},
	"openai":    {},
	"vertex": {
		env: [][2]string{
			{"CLAUDE_CODE_USE_VERTEX", "1"},
			{"ANTHROPIC_VERTEX_PROJECT_ID", "{project}"},
			{"CLOUD_ML_REGION", "{region}"},
		},
		credentialsVar: "GOOGLE_APPLICATION_CREDENTIALS",
	},
}

func (b ccBackend) expand(p ccProfile) [][2]string {
	var out [][2]string
	for _, kv := range b.env {
		v := strings.NewReplacer("{project}", p.Project, "{region}", p.Region).Replace(kv[1])
		if v != "" {
			out = append(out, [2]string{kv[0], v})
		}
	}
	return out
}

// ccPalette and ccDeriveColor reproduce cc-deck's name-derived profile color
// (internal/profile/color.go Derive), which cc-deck shows when a profile declares
// no valid color, so an imported profile keeps the look it had.
var ccPalette = [8]string{
	"#E27C5A", "#E2E25A", "#7CE25A", "#5AE29E",
	"#5AC0E2", "#8686EA", "#C05AE2", "#E25A9E",
}

func ccDeriveColor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return ccPalette[h.Sum32()%uint32(len(ccPalette))]
}

// expandHome resolves a cc-deck "~/" path the way cc-deck does.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
