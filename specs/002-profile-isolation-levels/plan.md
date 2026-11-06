# Implementation Plan: Profile Share Levels and Hook Sets

**Branch**: `002-profile-isolation-levels` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/002-profile-isolation-levels/spec.md`

## Summary

Every profile gets a share level (`all`, `content`, `none`) that decides how much of the default
setup it borrows; credentials and state stay private at every level. Harness definitions replace
the `shared` layer and the `*: shared` catch-all with four borrowable categories (content,
settings, plugin store, hook location), and the Claude Code and Codex lists are corrected from
on-disk evidence (research R1). Layer preparation becomes level-aware and produces one change plan
used by `sync`, `edit --share`, `settings pull` and `--dry-run`. Hook sets add a central library
whose entries `sync` places into profiles, tracked by fingerprints so user hooks are never touched
(R4). `show`, `doctor`, `harness show` and a new `help sharing` topic make the levels
understandable. The registry and wrappers publish `share` / `APROFILE_SHARE`.

## Technical Context

**Language/Version**: Go 1.24 (module `github.com/cc-deck/aprofile`)

**Primary Dependencies**: cobra, lipgloss, huh, bubbletea, `gopkg.in/yaml.v3`,
`pelletier/go-toml/v2` (all existing). `golang.org/x/sys` moves from indirect to direct for the
macOS running-profile check (R11). No other new modules.

**Storage**: files only. Profile YAML gains `share`, `hook_sets`, `exclude_hook_sets`; global
`settings.yaml` gains `default_share`; new `<config>/aprofile/hooks.d/*.yaml` library and
`hooks.d/files/<set>/`; new `<data>/aprofile/managed-hooks/<harness>/<name>.json` records;
backups reuse `<data>/aprofile/backups/<harness>/<name>/<ts>/`.

**Testing**: `go test ./...`; table-driven unit tests next to code; integration tests in
`tests/integration` against the built binary with `testutil.HermeticHome` (Constitution VII). The
R1 entry lists become fixtures that seed a fake default setup per harness.

**Target Platform**: Linux and macOS.

**Project Type**: CLI (single Go module, `cmd/` + `internal/`).

**Performance Goals**: wrapper start-time budget from slice one (50 ms) unchanged: the wrapper
gains one exported variable. `sync` of 20 profiles stays under 1 s on a laptop.

**Constraints**: never modify the default setup; never delete profile data without backup and
confirmation; idempotent `sync`; no harness-name branches in core code.

**Scale/Scope**: up to tens of profiles, four built-in harnesses, tens of hook sets.

## Global Constraints

Every task inherits these, copied from the spec and constitution:

- Share levels are exactly `all`, `content`, `none`; built-in default for new profiles `content`;
  profiles without a stored level are `all` (FR-006, FR-007, FR-033).
- Level descriptions are at most 80 characters and identical wherever shown; the `all`
  description says logins and history stay private (FR-014).
- Credentials and state are private at every level; built-in definitions contain no catch-all rule
  that links unclassified entries (FR-001, Constitution III).
- aprofile never modifies the default setup (Constitution II); `hook-sets import` only reads it.
- Any step that replaces profile data backs it up first and asks once (TTY) or requires `--yes`
  (exit 3 otherwise) (FR-017, FR-018, FR-021, FR-028).
- `sync` stays idempotent: a second run writes zero files (slice one FR-025).
- A level change is refused while the profile runs (exit 4) (FR-017).
- No harness-name branches in core code (Constitution I); Linux and macOS (Constitution,
  Technical Constraints).
- Wrapper start-up overhead stays at most 50 ms without an exec hook (slice one); `sync` of 20
  profiles under 1 s on a developer laptop.

## Interfaces

Signatures later tasks rely on (owning task in brackets):

```go
// internal/cli/exit.go [T002]
type ExitError struct{ Code int; Err error }

// internal/cli/confirm.go [T003]
func confirmOrYes(cmd *cobra.Command, prompt string, yes bool) error           // ExitError{3} off TTY without yes
func backupPaths(p config.Paths, harness, name string, paths []string, now time.Time) (dir string, err error)

// internal/layer/plan.go [T012, T013]
type StepKind string // link, unlink, mkdir, copy-once, seed-mixed, replace-with-link,
                     // merge-settings, replace-settings, place-hook, remove-hook, report
type Step struct{ Kind StepKind; Path, Source, Backup, Note string; Confirm bool }
func Plan(def *harness.Definition, p *profile.Profile, level profile.Share,
          defaultSetup, configDir string) ([]Step, error)
func Treatments(def *harness.Definition, p *profile.Profile, defaultSetup, configDir string) ([]EntryTreatment, error) // report mode for show [T035]

// internal/layer/apply.go [T014]
type ApplyOptions struct{ DryRun, Safe, Yes bool; Backup func(paths []string) (string, error) }
func Apply(steps []Step, opts ApplyOptions) (applied, skipped []Step, err error)

// internal/layer/settings.go [T038] (produces merge-settings / replace-settings steps)
func MergeSettings(profileDoc, defaultDoc []byte, keys []string, exclude []string) (out []byte, removed, changed []string, err error)
func ReplaceSettings(profileDoc, defaultDoc []byte, keys []string, exclude []string) (out []byte, removed, changed []string, err error)
func SecretWarnings(doc []byte) []string // key paths only

// internal/hooksets [T043 to T047] (produces place-hook / remove-hook steps)
type Set struct{ Name, Description string; EveryProfile bool; Bodies map[string]Body }
func LoadLibrary(dir string) (map[string]*Set, []error)
func Applies(s *Set, p *profile.Profile) bool
func PlanHooks(def *harness.Definition, p *profile.Profile, lib map[string]*Set,
               rec *Record, configDir string) ([]layer.Step, []Divergence, error)
func LoadRecord(path string) (*Record, error); func (r *Record) Save(path string) error
```

## Constitution Check

*GATE: checked against Constitution 1.1.0 (amended for this feature, uncommitted in this branch).*

| Principle | Status | How the design complies |
|-----------|--------|-------------------------|
| I. Harness Knowledge Is Data | Pass | Categories, level treatment, plugin store, hook location and form, `$HOME` escapes are definition fields (contracts/harness-definition.md). Core code switches on category and form, never on harness name. Hook forms are flavors like MCP flavors. |
| II. Never Fight the Harness | Pass | Every write is in the 1.1.0 list: links per level, one-time settings copy, managed hook groups identified by fingerprint (R4), confirmed and backed-up `settings pull`, level change, relink. `sync` safe mode never replaces data (R9). |
| III. Profile Isolation by Construction | Pass | Credentials/state private at all levels; catch-all removed from built-ins; unclassified reported; `none` seeds nothing, including mixed-file owned keys (fix to `seedMixedFile`). |
| IV. No Secrets in Generated Artifacts | Pass | Settings copy carries user values verbatim with per-key warnings (1.1.0 exception, R6). Hook-set bodies are user files validated with the existing secret-prefix check; aprofile never authors a secret. |
| V. Transparent Wrappers, Pull-Only | Pass | `APROFILE_SHARE` added to the contract, not caller-overridable; registry field additive at `format_version` 1 (additive per slice one contract); hook sets give cc-deck an option without aprofile writing into cc-deck. |
| VI. Scriptable First | Pass | Every new capability is a flag or command; TUI adds the same fields via the form-parity path; `--json` on all read commands; doctor gains severity. |
| VII. Tests Never Touch the Real Home | Pass | All fixtures under temporary home; research evidence is copied into fixtures, never read at test time. |

Re-check after Phase 1: no new violations. Complexity Tracking lists one justified deviation.

## Project Structure

### Documentation (this feature)

```text
specs/002-profile-isolation-levels/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md
│   └── harness-definition.md
├── checklists/requirements.md
└── tasks.md            # /speckit-tasks
```

### Source Code (repository root)

```text
cmd/aprofile/main.go                 # exit code from typed errors (new ExitError)
internal/harness/
├── definition.go        # Category enum replaces Layer for borrowables; LevelPolicy; HookLocation; HomeEscapes
├── loader.go            # validation for new fields; allFieldNames; companion expansion (R2)
├── override.go          # classification merges by entry (FR-001a)
└── builtin/*.yaml       # R1 lists; no catch-all; skills -> content; hooks/plugin-store/escapes
internal/layer/
├── classify.go          # categories; companions; unclassified-in-default-setup listing
├── plan.go              # NEW: change planner (entries x level -> []Step), safe vs confirmed steps
├── apply.go             # NEW: executes a plan (backup, link, unlink, copy-once, merge)
├── prepare.go           # becomes a thin wrapper over plan+apply; pass 2 removed
├── mixed.go             # level-aware seeding (none seeds nothing); shared-key drift kept
└── settings.go          # NEW: settings copy, merge and replace rules (FR-018)
internal/hooksets/       # NEW: library load/validate, body per form, apply/detach, fingerprints
├── library.go
├── eventgroups.go       # form event-groups (Claude settings.json#hooks, Codex hooks.json#hooks)
├── files.go             # form files (OpenCode plugins/, pi extensions/)
├── record.go            # managed-hooks/<harness>/<name>.json
└── importer.go          # hook-sets import
internal/profile/
├── profile.go           # Share, HookSets, ExcludeHookSets fields; default handling (FR-033)
└── running_darwin.go    # NEW: KERN_PROCARGS2 env scan (R11); running_other.go keeps fallback
internal/config/config.go          # Settings.DefaultShare; HooksDir, ManagedHooksDir paths
internal/sync/sync.go              # passes profile to planner (safe mode); places hook sets
internal/wrapper/wrapper.go        # APROFILE_SHARE in renderContract
internal/registry/registry.go      # Entry.Share
internal/doctor/doctor.go          # Severity; new checks (FR-020, FR-001a)
internal/cli/
├── add.go, edit.go, copy.go, import.go   # --share, --hook-sets, --exclude-hook-sets, --dry-run/--yes
├── list_show.go         # per-entry treatment view and JSON
├── harness.go           # per-level summary; `harness classify`
├── settings.go          # NEW: settings pull
├── hooksets.go          # NEW: hook-sets list/show/import/reapply
├── help_sharing.go      # NEW: help topic, shared text constants (levels.go)
├── levels.go            # NEW: level names, one-line descriptions, use-case table (single source)
├── confirm.go           # NEW: shared confirm/--yes/backup helper (extracted from remove --purge)
├── login.go             # APROFILE_SHARE in loginEnv; level-aware prepare
└── ui.go                # form fields via FormValues
internal/tui/forms.go              # share select with descriptions; hook-set multi-select
internal/importer/importer.go      # share level; borrowables replace LayerShared cases
README.md                          # decision guide (FR-012), hook-set alternatives (FR-032)
tests/integration/                 # isolation per level, upgrade, hook sets, settings pull, show/doctor
tests/fixtures/defaults/<harness>/ # NEW: R1 entry lists as fake default setups
```

**Structure Decision**: keep the single-module layout. The planner/apply split in `internal/layer`
gives one code path for `sync`, `edit --share`, `settings pull` and dry runs (SC-006). Hook sets
get their own package because they span the library, two forms and the record store.

## Design Decisions

1. **Categories replace `shared`** (contracts/harness-definition.md). `Layer` keeps
   `credentials`, `owned`, `state`, `mixed`; `shared` is removed from built-ins and accepted in
   user definitions as a deprecated alias for `content` with a doctor warning (keeps slice one user
   overrides loading).
2. **Level policy is data**: each built-in carries the default policy table (content and
   plugin-store link at `all`/`content`; settings link at `all`, copy-once at `content`, minimal at
   `none`; hook location link at `all`, managed otherwise). A definition may override per category,
   e.g. FR-009a falls out of the settings file being declared `mixed` or `owned`.
3. **One planner** (`layer.Plan(def, profile, level, defaultSetup, current) []Step`). Step kinds:
   `link`, `unlink`, `mkdir`, `copy-once`, `seed-mixed`, `replace-with-link` (confirmed),
   `merge-settings` / `replace-settings` (confirmed), `report`. `Step.Confirm` marks the data
   replacing kinds. `sync` applies non-confirm steps and reports the rest (R9).
4. **Hook sets** follow R3 to R5. Placement happens in `sync` after the planner. A set applies to a
   profile when named, or when `every_profile` and level is `content` and not excluded; `all`
   rejects sets at add/edit time.
5. **Entry treatment for `show`** comes from the same planner run in report mode, so `show`, dry
   runs and `sync` cannot disagree (SC-005, SC-006).
6. **Text lives once**: level names, descriptions and use-case table are constants in
   `cli/levels.go`, rendered by flag help, TUI, `show`, `help sharing`; a test checks the README
   tables against them (SC-003).
7. **Exit codes**: a `cli.ExitError{Code}` type; `Execute` returns its code (contracts/cli.md).
8. **Running detection on macOS** (R11): `sysctl kern.procargs2` per same-user pid via
   `golang.org/x/sys/unix`, scanning for `APROFILE_CONFIG_DIR=`; failure to read returns
   "unknown", which `edit --share` treats as "ask the user to confirm the profile is not running"
   rather than silently proceeding.

## Research addendum

- **R11. Running-profile detection on macOS**: slice one returns `false` on non-Linux, so
  `rename` and the new level change would never refuse on macOS. Decision: implement the
  `kern.procargs2` scan behind a build tag. Alternatives: `ps -E` parsing (fragile output);
  pid files written by wrappers (would add a write at launch, Constitution V keeps wrappers
  self-contained).

## Migration and compatibility

- Profiles without `share` load as `all` (FR-033); built-in `all` no longer links unclassified
  entries, and existing catch-all links are reported, not removed (R10).
- User `harnesses.d` files that use `layer: shared` keep working through the alias (Decision 1).
- `registry_test.go` pins nine fields; it becomes ten, and the slice one registry contract gets a
  note that `share` was added at `format_version` 1 as an additive field.
- Tests that pin the catch-all (`layer_test.go:40-41`, `isolation_test.go`) are rewritten to the
  explicit lists.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Deprecated `shared` alias kept in the loader | Slice one user definitions in `harnesses.d` may use it | Hard failure would break user overrides on upgrade with no migration path |
