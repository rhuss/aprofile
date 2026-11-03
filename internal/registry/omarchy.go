package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cc-deck/aprofile/internal/config"
)

// OmarchyProfile is the minimal per-profile information the Omarchy export needs
// (FR-031). Provider groups profiles into per-provider accounts files.
type OmarchyProfile struct {
	Harness  string
	Name     string
	Provider string // from the harness definition's omarchy_provider
	Label    string
	Home     string // the profile config dir
	Primary  bool
}

// OmarchyAccount is one entry of an Omarchy accounts file.
type OmarchyAccount struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Home    string `json:"home"`
	Primary bool   `json:"primary"`
}

type omarchyFile struct {
	Accounts []OmarchyAccount `json:"accounts"`
}

// omarchyIDPrefix marks accounts aprofile manages. Entries without it are preserved
// unchanged; aprofile entries for profiles that no longer exist are removed (FR-031).
const omarchyIDPrefix = "aprofile:"

// OmarchyID is the stable account id aprofile writes for a profile: aprofile:<h>/<n>.
func OmarchyID(harness, name string) string {
	return omarchyIDPrefix + harness + "/" + name
}

// ExportOmarchy writes one accounts file per provider under outputDir, merging with
// any existing file: entries whose id lacks the "aprofile:" prefix are preserved as
// they are, and aprofile entries are replaced by the current profile set (so stale
// ones are dropped). It returns the paths written, sorted. Writes are atomic and skip
// files whose content is unchanged (FR-031). Profiles with an empty provider are
// ignored (the harness opts out of Omarchy).
func ExportOmarchy(outputDir string, profiles []OmarchyProfile) ([]string, error) {
	byProvider := map[string][]OmarchyAccount{}
	providers := map[string]bool{}
	for _, p := range profiles {
		if p.Provider == "" {
			continue
		}
		providers[p.Provider] = true
		byProvider[p.Provider] = append(byProvider[p.Provider], OmarchyAccount{
			ID:      OmarchyID(p.Harness, p.Name),
			Label:   p.Label,
			Home:    p.Home,
			Primary: p.Primary,
		})
	}

	var written []string
	names := make([]string, 0, len(providers))
	for prov := range providers {
		names = append(names, prov)
	}
	sort.Strings(names)

	for _, prov := range names {
		path := filepath.Join(outputDir, prov+".json")
		preserved, err := preservedAccounts(path)
		if err != nil {
			return nil, err
		}
		accounts := append(preserved, byProvider[prov]...)
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })

		data, err := marshalOmarchy(accounts)
		if err != nil {
			return nil, err
		}
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
			continue // unchanged
		}
		if err := config.WriteFileAtomic(path, data, 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

// preservedAccounts reads the existing accounts file and returns the entries whose id
// is not aprofile-managed. A missing file yields no preserved entries; an unparseable
// file returns an error so the export aborts rather than silently discarding entries it
// could not read (the caller surfaces it; no file is overwritten).
func preservedAccounts(path string) ([]OmarchyAccount, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read omarchy accounts %s: %w", path, err)
	}
	var f omarchyFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse omarchy accounts %s: %w", path, err)
	}
	var out []OmarchyAccount
	for _, a := range f.Accounts {
		if len(a.ID) >= len(omarchyIDPrefix) && a.ID[:len(omarchyIDPrefix)] == omarchyIDPrefix {
			continue // aprofile-managed: rebuilt from the current profile set
		}
		out = append(out, a)
	}
	return out, nil
}

func marshalOmarchy(accounts []OmarchyAccount) ([]byte, error) {
	if accounts == nil {
		accounts = []OmarchyAccount{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(omarchyFile{Accounts: accounts}); err != nil {
		return nil, fmt.Errorf("encode omarchy accounts: %w", err)
	}
	return buf.Bytes(), nil
}
