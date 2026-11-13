package launch

import (
	"fmt"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// Folder states (FR-020, contracts/cli.md).
const (
	FolderSelfContained    = "self-contained"
	FolderSelfContainedEnv = "self-contained-env"
	FolderNotSelfContained = "not-self-contained"
)

// FolderReport is the `folder` object of show --json (contracts/cli.md).
type FolderReport struct {
	Status       string        `json:"status"`
	EnvDependent []string      `json:"env_dependent"`
	WrapperOnly  []WrapperOnly `json:"wrapper_only"`
	Sync         string        `json:"sync"`
	Unplaced     []Entry       `json:"unplaced"`
}

// WrapperOnly is one launch setting only the wrapper supplies, with its reason, or
// the auth-clearing report item (KindAuthClearing, spec 005 FR-005), which is not a
// launch setting.
type WrapperOnly struct {
	Setting  string `json:"setting"`
	Variable string `json:"variable"`
	Reason   Reason `json:"reason"`
}

// Folder summarizes a status report: whether the folder is self-contained, which
// settings stay with the wrapper, whether it needs sync and what it holds that
// aprofile did not place. No value is ever part of it.
func (r StatusReport) Folder() FolderReport {
	f := FolderReport{Status: FolderSelfContained, EnvDependent: []string{}, WrapperOnly: []WrapperOnly{}, Sync: r.Sync, Unplaced: []Entry{}}
	for _, s := range r.Settings {
		switch {
		case s.Carrier == CarrierWrapper:
			f.WrapperOnly = append(f.WrapperOnly, WrapperOnly{Setting: s.Kind, Variable: s.Variable, Reason: s.Reason})
		case s.EnvDependent && s.Ref != nil:
			f.EnvDependent = append(f.EnvDependent, s.Ref.Value)
		}
	}
	// Spec 005 FR-005: auth clearing is the one wrapper-only item that is not a
	// launch setting.
	if r.AuthClearing != "" {
		f.WrapperOnly = append(f.WrapperOnly, WrapperOnly{Setting: KindAuthClearing, Reason: r.AuthClearing})
	}
	switch {
	case len(f.WrapperOnly) > 0:
		f.Status = FolderNotSelfContained
	case len(f.EnvDependent) > 0:
		f.Status = FolderSelfContainedEnv
	}
	for _, e := range r.Unplaced {
		f.Unplaced = append(f.Unplaced, Entry{File: e.File, Location: e.Location})
	}
	return f
}

// StatusText is the folder status line of show: "self-contained", "self-contained
// (needs NAME in the launching environment)" or "not self-contained".
func (f FolderReport) StatusText() string {
	switch f.Status {
	case FolderSelfContainedEnv:
		return fmt.Sprintf("self-contained (needs %s in the launching environment)", strings.Join(f.EnvDependent, ", "))
	case FolderNotSelfContained:
		return "not self-contained"
	}
	return "self-contained"
}

// SettingName names a setting for people: model, endpoint, key, auth-clearing, or
// the variable of an extra or hook variable.
func (w WrapperOnly) SettingName() string {
	if strings.HasPrefix(w.Setting, extraPrefix) || strings.HasPrefix(w.Setting, hookPrefix) {
		return w.Variable
	}
	return w.Setting
}

// ReasonText explains why a setting is wrapper-only (FR-020).
func ReasonText(r Reason, p *profile.Profile, def *harness.Definition) string {
	file := "the settings file"
	if def.FolderLaunch != nil {
		if f := def.FolderLaunch.EnvFile(); f != "" {
			file = f
		} else if def.FolderLaunch.KeyCommand != nil {
			file = def.FolderLaunch.KeyCommand.File()
		}
	}
	switch r {
	case ReasonReferenceValue:
		return "reference value"
	case ReasonAuthKind:
		return fmt.Sprintf("auth kind %s cannot be carried by the folder", p.AuthKind)
	case ReasonShareAll:
		return fmt.Sprintf("share level all: %s is linked", file)
	case ReasonNoProvider:
		if def.FolderLaunch != nil && def.FolderLaunch.KeyCommand != nil {
			return fmt.Sprintf("%s selects no provider defined in the file", def.FolderLaunch.KeyCommand.File())
		}
		return "no provider defined in the folder"
	case ReasonHarnessRewrites:
		return "passed by the wrapper because the harness rewrites it"
	case ReasonClearedSource:
		return "key referenced through a variable aprofile clears"
	case ReasonBootstrap:
		return "required before the harness starts"
	case ReasonHookEnv:
		return "set by the exec hook's env operation"
	case ReasonNoSupport:
		return fmt.Sprintf("harness %s cannot read it from its folder", def.Name)
	}
	return string(r)
}

// UnplacedText describes an unplaced entry (FR-020b): the doctor message, its fix,
// and the short note show prints. It names the entry, never its value.
func (r StatusReport) UnplacedText(e Entry) (msg, fix, short string) {
	what := e.Location
	if name, ok := strings.CutPrefix(e.Location, "env."); ok {
		what = "env " + name
	}
	supplied := false
	for _, s := range r.Settings {
		if s.Variable != "" && strings.HasSuffix(e.Location, "."+s.Variable) || s.Kind == KindKey && !strings.HasPrefix(e.Location, "env.") {
			supplied = true
		}
	}
	switch {
	case r.Linked[e.File]:
		return fmt.Sprintf("the linked %s sets %s, which overrides the value the wrapper sets", e.File, what),
			"remove the entry from the default setup or use share level content",
			"set in the linked " + e.File + "; overrides the value the wrapper sets"
	case supplied:
		return fmt.Sprintf("%s sets %s, which overrides the value the wrapper sets", e.File, what),
			"remove the entry or set it on the profile", "overrides the value the wrapper sets"
	}
	return fmt.Sprintf("%s sets %s, which this profile does not set", e.File, what),
		"remove the entry or set it on the profile", "not set by this profile; overrides it"
}
