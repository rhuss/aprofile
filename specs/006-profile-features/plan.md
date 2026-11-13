# Implementation Plan: Profile Features (Curated Harness Capability Toggles)

**Branch**: `006-profile-features` | **Date**: 2026-11-13 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/006-profile-features/spec.md`

## Summary

Add a curated feature catalog (telemetry, automode-log) with a dedicated `aprofile features`
command, per-harness capability mappings in the harness definition YAML, feature state in the
profile record, and placement/removal of feature entries through the existing launch plan and
managed-launch record (`internal/launch`). The raw `--env`/`--env-literal` flags remain the
escape hatch; `add`/`edit` gain no feature flags.

The branch already merges `005-neutralize-provider-switches` (commit 1e2cf35), because feature
placement builds on that spec's placed-entry widening (spec assumption, review finding 3).

## Technical Context

**Language/Version**: Go (module `github.com/cc-deck/aprofile`), single static binary.

**Primary Dependencies**: cobra (CLI), lipgloss/huh/bubbletea (output, forms), stdlib
`encoding/json` / YAML for records and definitions. No new third-party modules.

**Storage**: profile records (YAML, profile store), managed-launch records (JSON,
`<data>/aprofile/managed-launch/<harness>/<name>.json`), harness definition YAML
(`internal/harness/builtin/*.yaml`, user `harnesses.d`).

**Testing**: `go test ./...` (table-driven unit tests per package), `tests/integration`
(against temporary `HOME`/XDG, Principle VII).

**Target Platform**: Linux and macOS.

**Project Type**: CLI tool.

**Performance Goals**: `features` bare status under a second (SC-003); `sync` unchanged in
order of magnitude.

**Constraints**: never write into the linked default setup at share level `all`; idempotent
placement; no secret values in the launch record (Principle IV).

**Scale/Scope**: two catalog features, two harnesses (claude, codex) in v1; other harnesses
(opencode, pi) declare no features.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Notes |
|-----------|---------|-------|
| I. Harness Knowledge Is Data | Pass | Per-harness capability mappings (supported features, param defaults and enums, placement rules) live in the definition YAML, overridable via `harnesses.d`. The cross-harness catalog (feature ids, param names, types, groups) is Go code per FR-005: it is harness-independent, like the share-level vocabulary. No `if harness == "..."` in core logic. |
| II. Never Fight the Harness | Pass with recorded extension | Feature-placed entries extend Principle II's launch-entry write category beyond launch variables, key command and auth blocks. Spec FR-018 requires recording this. Decision: PATCH constitution amendment (see Complexity Tracking) extending the Principle II parenthetical to name feature-placed entries, mirroring spec 005's precedent for neutralizing entries. All other rules hold: re-read before write, preserve the rest of the file, idempotent, recorded in aprofile's data directory. |
| III. Profile Isolation by Construction | Pass | Mutations refused at share level `all` (FR-013); `edit --share all` clears feature state with a warning (FR-014); bare status read-only. |
| IV. No Secrets in Generated Artifacts | Pass | v1 params carry a URL endpoint and enum/bool values, none secret-patterned. The launch record keeps locations only, never values (existing `Entry` invariant). Endpoint values in `settings.json`/`config.toml` are user-chosen config, not aprofile-derived secrets. |
| V. Transparent Wrappers, Pull-Only Integration | Pass | Features write into the profile's own settings files, not wrappers. The registry is not extended in v1. |
| VI. Scriptable First, Colorful Second | Pass | Plain CLI flags; `--json` for status and mutation (FR-017); color degrades off-TTY. |
| VII. Tests Never Touch the Real Home | Pass | All new tests use temp `HOME`/XDG; placement tests build on the existing launch test harness. Test-first for new behavior. |

## Project Structure

### Documentation (this feature)

```text
specs/006-profile-features/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── features-cli.md  # CLI contract: command, flags, JSON schema
└── tasks.md             # Phase 2 output (/speckit-tasks)
```

### Source Code (repository root)

```text
internal/
├── feature/             # NEW: cross-harness catalog
│   ├── catalog.go       #   Feature, Param, Group types; built-in catalog
│   └── catalog_test.go
├── harness/
│   ├── definition.go    # extended: Features map (capability mapping schema)
│   └── builtin/
│       ├── claude.yaml  # extended: features.telemetry, features.automode-log
│       └── codex.yaml   # extended: features.telemetry
├── profile/
│   └── profile.go       # extended: Features []FeatureState in the record
├── launch/
│   ├── plan.go          # extended: feature-derived desired state, nested TOML placement
│   ├── record.go        # unchanged schema (nested TOML needs no new fields: locations are
│   │                    #   already dotted paths; Replaced covers user-written values)
│   └── carrier.go       # extended: sync problems for feature drift
├── cli/
│   ├── features.go      # NEW: features command (status + mutation, --json)
│   ├── edit.go          # extended: --share all clears features; env collision check
│   ├── copy.go          # extended: carry feature state, cross-harness report
│   └── list_show.go     # extended: show features section + drift
tests/
└── integration/
    └── features_test.go # NEW: user-story scenarios end to end
```

**Structure Decision**: single Go module, existing package layout. The new `internal/feature`
package holds only the harness-independent catalog; everything per-harness stays in
`internal/harness` definitions, everything placement-related in `internal/launch`, matching the
existing separation (Principle I test: adding opencode telemetry is a YAML change).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Principle II write-category extension (feature-placed entries) | Enabled features must materialize in the harness's own settings files; the wrapper cannot carry them (folder-only launches, spec 004's lesson) | A warning-only or wrapper-only approach leaves folder launches without the feature, and silently diverges from the recorded state; spec 005 already established that placed entries beyond launch variables are the sanctioned mechanism. Recorded as a PATCH constitution amendment in the tasks (extend Principle II's parenthetical: "the launch variables, key command, provider `auth` block and feature-placed entries aprofile placed"), not a silent exception. |
