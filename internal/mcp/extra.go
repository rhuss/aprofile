package mcp

import "slices"

// Server.Extra holds the native fields a decoder found but the neutral record does not
// model, keyed by the source flavor: {"claude-json": {"cwd": "/w", "timeout": 30000}}.
// A same-flavor emit writes them back; any other flavor cannot interpret them, so each
// one is reported as an unrepresentable drop instead of vanishing silently (FR-035,
// SC-005).

// collectExtra returns the fields of native whose keys are not in known, keyed by
// flavor, or nil when every field is modeled.
func collectExtra(flavor Flavor, native map[string]any, known ...string) map[string]any {
	var fields map[string]any
	for k, v := range native {
		if slices.Contains(known, k) {
			continue
		}
		if fields == nil {
			fields = map[string]any{}
		}
		fields[k] = v
	}
	if fields == nil {
		return nil
	}
	return map[string]any{string(flavor): fields}
}

// encodeExtra writes s's unmodeled fields from flavor back into native and reports
// every field it cannot carry: fields from another flavor are unrepresentable, and a
// field holding a literal secret is dropped as literal-secret (Constitution IV). A
// field never overrides a key the encoder already set.
func encodeExtra(flavor Flavor, s Server, native map[string]any) []Drop {
	var drops []Drop
	for _, fl := range sortedAnyKeys(s.Extra) {
		fields, _ := s.Extra[fl].(map[string]any)
		for _, k := range sortedAnyKeys(fields) {
			v := fields[k]
			switch {
			case Flavor(fl) != flavor:
				drops = append(drops, Drop{Server: s.Name, Field: k, Reason: ReasonUnrepresentable})
			case containsSecretLiteral(v):
				drops = append(drops, Drop{Server: s.Name, Field: k, Reason: ReasonLiteralSecret})
			default:
				if _, taken := native[k]; !taken {
					native[k] = v
				}
			}
		}
	}
	return drops
}
