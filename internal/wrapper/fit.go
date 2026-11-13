package wrapper

import (
	"fmt"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// This file checks a profile against its harness definition where the two meet
// (sync, doctor, show). Profile validation on load cannot: the definition is not
// available there (FR-006). Every decision comes from definition data (Principle I).

// FitIssue is one way a profile does not fit its harness definition. Fatal issues
// stop sync from rendering the profile's wrapper; the others are reported only.
type FitIssue struct {
	Fatal   bool
	Field   string // auth_kind | model | endpoint
	Message string // without the profile id, e.g. `auth kind "nope" is not declared ...`
	Detail  string // the message without the field and value, for show's field line
	Fix     string
}

// AuthKindIssue reports a non-empty auth kind the definition does not declare. Such
// a wrapper would clear every auth variable and set none, so the profile would
// silently start without credentials; sync refuses it instead.
func AuthKindIssue(p *profile.Profile, def *harness.Definition) *FitIssue {
	if p.AuthKind == "" {
		return nil
	}
	if _, ok := def.AuthKind(p.AuthKind); ok {
		return nil
	}
	valid := "none"
	if len(def.AuthKinds) > 0 {
		names := make([]string, len(def.AuthKinds))
		for i, k := range def.AuthKinds {
			names[i] = k.Name
		}
		valid = strings.Join(names, ", ")
	}
	return &FitIssue{
		Fatal:   true,
		Field:   "auth_kind",
		Message: fmt.Sprintf("auth kind %q is not declared by harness %s (valid kinds: %s)", p.AuthKind, def.Name, valid),
		Detail:  fmt.Sprintf("not declared by harness %s; valid kinds: %s", def.Name, valid),
		Fix:     fmt.Sprintf("aprofile edit %s --auth-kind <kind>", p.ID()),
	}
}

// IgnoredSettingIssues reports a model or endpoint the profile sets although the
// definition declares no variable (or, for the model, no launch argument) for it,
// so neither the folder nor the wrapper can carry it.
func IgnoredSettingIssues(p *profile.Profile, def *harness.Definition) []FitIssue {
	var out []FitIssue
	add := func(field, value, varName, key string) {
		if value == "" || varName != "" {
			return
		}
		detail := fmt.Sprintf("ignored: harness %s declares no %s variable (%s)", def.Name, field, key)
		out = append(out, FitIssue{
			Field:   field,
			Message: fmt.Sprintf("%s %q is %s", field, value, detail),
			Detail:  detail,
			Fix:     fmt.Sprintf("aprofile edit %s --%s \"\"", p.ID(), field),
		})
	}
	modelCarrier := def.ModelVar
	if modelCarrier == "" && len(def.ModelArg) > 0 {
		modelCarrier = "model_arg" // passed as a launch argument (spec 004 FR-016)
	}
	add("model", p.Model, modelCarrier, "model_var")
	add("endpoint", p.Endpoint, def.EndpointVar, "endpoint_var")
	return out
}

// FitIssues returns every way p does not fit def: the auth kind first, then any
// ignored model or endpoint.
func FitIssues(p *profile.Profile, def *harness.Definition) []FitIssue {
	var out []FitIssue
	if i := AuthKindIssue(p, def); i != nil {
		out = append(out, *i)
	}
	return append(out, IgnoredSettingIssues(p, def)...)
}
