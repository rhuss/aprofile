package cli

import (
	"fmt"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// copyAuth is the auth a cross-harness copy gives its target, resolved and validated
// before anything is written (FR-033b, T067, T075).
type copyAuth struct {
	set  bool // the copy sets the target's auth (new target, --auth or --auth-key)
	kind string
	refs map[string]secret.Reference
}

// resolveCopyAuth decides the target's auth kind and references for a cross-harness
// copy. A new target gets --auth or the target's default kind; an overwritten target
// keeps its own auth unless --auth or --auth-key is given. --auth-key takes the same
// env:NAME or file:/abs/path reference as add's. A kind that carries its credential in
// a variable needs a reference for it, so a missing one fails naming --auth-key.
func resolveCopyAuth(dstDef *harness.Definition, existing *profile.Profile, auth, authKey string, dstID string) (copyAuth, error) {
	var out copyAuth
	switch {
	case auth != "":
		if _, ok := dstDef.AuthKind(auth); !ok {
			return out, fmt.Errorf("harness %q has no auth kind %q", dstDef.Name, auth)
		}
		out = copyAuth{set: true, kind: auth}
	case existing == nil:
		out = copyAuth{set: true, kind: dstDef.DefaultAuthKind}
	case authKey != "":
		out = copyAuth{set: true, kind: existing.AuthKind, refs: cloneRefs(existing.AuthRefs)}
	default:
		return copyAuth{}, nil // the overwritten target keeps its own auth
	}
	if out.kind == "" {
		if authKey != "" {
			return out, fmt.Errorf("--auth-key requires an auth kind, and harness %q declares none", dstDef.Name)
		}
		return out, nil
	}
	kind, ok := dstDef.AuthKind(out.kind)
	if !ok {
		return out, fmt.Errorf("harness %q has no auth kind %q", dstDef.Name, out.kind)
	}
	vars := kind.VarNames()
	if authKey != "" {
		ref, err := secret.ParseAuthReference(authKey)
		if err != nil {
			return out, fmt.Errorf("--auth-key: %w", err)
		}
		switch {
		case len(vars) == 0:
			return out, fmt.Errorf("auth kind %q takes no key (it uses an interactive login)", out.kind)
		case len(vars) > 1:
			return out, fmt.Errorf("auth kind %q sets multiple variables; set them with edit --env instead", out.kind)
		}
		out.refs = map[string]secret.Reference{vars[0]: ref}
	}
	for _, v := range vars {
		if _, ok := out.refs[v]; !ok {
			return out, fmt.Errorf("target %s uses auth kind %q, which needs a reference for %s: pass --auth-key env:NAME (or file:/abs/path), or choose an interactive kind with --auth",
				dstID, out.kind, v)
		}
	}
	return out, nil
}
