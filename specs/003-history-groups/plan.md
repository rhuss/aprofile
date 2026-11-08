# Implementation Plan: History Groups

**Branch**: `003-history-groups` | **Date**: 2026-11-07 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-history-groups/spec.md`

## Summary

Harness definitions mark some state entries as history (`history: true`, `merge: per-file|none`).
A profile may belong to one history group of its harness; its history entries are then linked to
the group store, while credentials and all other state stay private. Every harness with history
entries has a built-in `home` group whose store is the default setup; further groups are created
explicitly and store their history under aprofile's data directory. Joining moves mergeable files
into the store without overwriting and sets aside the rest; leaving gives fresh private entries.
The planner, `show`, `doctor` and `sync` learn the profile's expected history store so a member's
links are correct rather than leaks. A `group` command manages groups; `add`, `edit` and `copy`
gain `--group`/`--no-group`; the registry and wrapper publish the group. The constitution is
amended (1.2.0) for the opt-in exception.

## Technical Context

**Language/Version**: Go 1.24 (module `github.com/cc-deck/aprofile`)

**Primary Dependencies**: cobra, lipgloss, huh, bubbletea, `gopkg.in/yaml.v3` (all existing). No
new modules.

**Storage**: files only. Profile YAML gains `history_group`; new
`<config>/aprofile/groups/<harness>/<group>.yaml` records; stores in
`<data>/aprofile/groups/<harness>/<group>/`; set-aside history in
`<data>/aprofile/set-aside/<harness>/<name>/<ts>/` with `manifest.json` (research R5).

**Testing**: `go test ./...`; table-driven unit tests next to code; integration tests in
`tests/integration` against the built binary with `testutil.HermeticHome` and the
`aprofile_testseam` build for the running check (Constitution VII).

**Target Platform**: Linux and macOS.

**Project Type**: CLI (single Go module, `cmd/` + `internal/`).

**Performance Goals**: wrapper start-up unchanged except one exported variable (50 ms budget);
`sync` of 20 profiles stays under 1 s; a join of 2,000 session files completes in under 5 s on a
laptop (renames within one file system).

**Constraints**: never replace or rewrite an existing default-setup entry; `sync` never moves data
and never writes into the default setup; nothing in a store is ever overwritten; no harness-name
branches in core code; idempotent `sync`.

**Scale/Scope**: tens of profiles, a handful of groups per harness, thousands of session files
per store.

## Global Constraints

Every task inherits these, copied from the spec and constitution:

- A profile is in at most one group; new profiles are isolated unless `--group` is given; the
  default setup is always the implicit member of `home` and of no other group (FR-011, FR-017).
- Unknown group, `home` create/delete, group with members, harness without history entries,
  unsuitable default location, kept store at the create path: exit 2, nothing changed (FR-006,
  FR-008, FR-009, Edge Cases).
- Membership changes: refused while running (exit 4), plan printed, `--dry-run` exact, confirm
  once (or `--yes`; exit 3 off a terminal) when anything moves or is set aside (FR-013, FR-014).
- Join never overwrites a store file (`os.Link` fails on `EEXIST`; on `EXDEV`, copy to an
  `O_EXCL` target); per-file merge is
  recursive by relative path; partial moves allowed and reported (FR-014).
- Leaving or moving never changes the old store (FR-015); `remove --purge` never follows a
  history link (FR-019); `group delete --purge` only deletes stores under the data dir, never
  `home` (FR-008).
- `sync` never moves data or writes into the default setup (FR-022a); it links only where nothing
  is in the way and reports the rest with `edit --group <g>`.
- Credentials and non-history state stay private for members (FR-004); isolated profiles behave
  exactly as before (SC-002).
- No harness-name branches (Constitution I); Linux and macOS.

## Interfaces

Signatures later tasks rely on (owning area in brackets):

```go
// internal/harness [definition]
type EntryRule struct { /* existing */ History bool `yaml:"history,omitempty"`; Merge MergeMode `yaml:"merge,omitempty"` }
type MergeMode string // "per-file" | "none" ("" means none)
func (d *Definition) HistoryRules() []EntryRule
func (d *Definition) SupportsGroups() bool

// internal/config [paths]
func (p Paths) GroupsConfigDir() string                       // <config>/aprofile/groups
func (p Paths) GroupRecordPath(harness, group string) string  // <config>/aprofile/groups/h/g.yaml
func (p Paths) GroupStoreDir(harness, group string) string    // <data>/aprofile/groups/h/g
func (p Paths) SetAsideDir(harness, name string) string       // <data>/aprofile/set-aside/h/n

// internal/history [new package: groups, stores, join/leave planning]
const Home = "home"
type Group struct { Harness, Name string; Builtin bool; Store string; Members []string }
type Store interface{ EntryPath(rel string) string; IsHome() bool; Name() string; Root() string } // satisfies layer.HistoryStore
var ErrUnknownGroup, ErrNotSupported error // both map to exit 2
func NewManager(paths config.Paths, defs map[string]*harness.Definition, profiles *profile.Store) *Manager
func (m *Manager) Get(harnessName, group string) (*Group, error)      // ErrUnknownGroup (exit 2)
func (m *Manager) List(harnessName string) ([]*Group, error)
func (m *Manager) Create(harnessName, group string, now time.Time) (*Group, error)
func (m *Manager) Delete(harnessName, group string, purge bool) (keptStore string, err error)
func (m *Manager) StoreFor(def *harness.Definition, p *profile.Profile) (Store, error) // nil for isolated
func PlanJoin(def *harness.Definition, st Store, configDir, setAside string) ([]layer.Step, error)
func PlanLeave(def *harness.Definition, configDir string) ([]layer.Step, error)
func PlanMove(def *harness.Definition, to Store, configDir string) ([]layer.Step, error)
func CheckHomeLocation(def *harness.Definition, home string, paths config.Paths, profiles []string) error

// internal/layer [planner and apply]
const StepMoveToStore, StepSetAside, StepLinkHistory, StepRelinkHistory StepKind
type HistoryStore interface{ EntryPath(rel string) string; IsHome() bool; Name() string }
type PlanOptions struct { /* existing */ HistoryStore HistoryStore; ExplicitJoin bool } // nil store = isolated; ExplicitJoin only from add/edit/copy
type SetAsideItem struct{ Path, Reason string } // reason: exists-in-store | not-mergeable
type ApplyOptions struct { /* existing */ SetAsideManifest func(item SetAsideItem) error }
func moveNoOverwrite(src, dst string) error // os.Link(src,dst) (fails on EEXIST) + os.Remove(src); EXDEV -> copy to O_EXCL target + fsync + remove

// internal/cli
func newGroupCmd() *cobra.Command
func changeGroup(cmd *cobra.Command, p *profile.Profile, to string, leave, dryRun, yes bool) error
```

## Constitution Check

*GATE: checked against Constitution 1.1.0; this feature amends it to 1.2.0 (FR-025).*

| Principle | Status | How the design complies |
|-----------|--------|-------------------------|
| I. Harness Knowledge Is Data | Pass | History entries and merge modes are rule fields in the definitions; OpenCode's lack of support is data (no history rules), not a branch. |
| II. Never Fight the Harness | Pass after amendment | New writes (history links, move-to-store and set-aside on a confirmed join, fresh entries on leave, the `home` join's missing-directory creation and file moves into the default location) are added to the allowed list in 1.2.0. `sync` stays non-destructive and never writes into the default setup. |
| III. Profile Isolation by Construction | Pass after amendment | 1.2.0 adds the opt-in history exception; credentials and all other state remain private; isolated profiles unchanged. |
| IV. No Secrets in Generated Artifacts | Pass | No secret values handled; OpenCode excluded because its session database holds tokens; account identifiers in transcripts are not secrets (R2). |
| V. Transparent Wrappers, Pull-Only | Pass | `APROFILE_HISTORY_GROUP` and the registry field are additive at format_version 1. |
| VI. Scriptable First | Pass | Every capability is a CLI flag or command with `--json`; the TUI adds a group select through the form-parity path. |
| VII. Tests Never Touch the Real Home | Pass | All tests use HermeticHome; research evidence was read-only and is not read at test time. |

Re-check after Phase 1: no unjustified violations; the amendment is part of the feature.

## Project Structure

### Documentation (this feature)

```text
specs/003-history-groups/
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
.specify/memory/constitution.md     # 1.2.0: Principles II and III amended (FR-025)
internal/harness/
├── definition.go        # EntryRule.History/Merge, MergeMode, HistoryRules, SupportsGroups
├── loader.go            # validation; expandCompanions copies history fields (R8)
└── builtin/{claude,codex,pi}.yaml   # history flags (R1); codex gains archived_sessions/
internal/config/config.go          # group/store/set-aside path builders
internal/profile/profile.go        # HistoryGroup field + name validation
internal/history/                  # NEW
├── group.go             # Group, Manager: list/get/create/delete, home synthesis, members
├── store.go             # Store (home vs data-dir), entry paths, CheckHomeLocation
├── join.go              # PlanJoin / PlanLeave / PlanMove, set-aside layout and manifest
└── *_test.go
internal/layer/
├── plan.go              # new step kinds; history-aware planRule (R7)
├── apply.go             # move-to-store, set-aside, link-history, relink-history; moveNoOverwrite (R6)
└── treatment.go         # history links reported as linked with the group
internal/sync/sync.go              # passes HistoryStore; reports joins it cannot do (FR-022a/c)
internal/doctor/doctor.go          # group checks; member history links are not leaks (FR-021)
internal/registry/registry.go      # Entry.HistoryGroup
internal/wrapper/{wrapper.go,env.go}   # APROFILE_HISTORY_GROUP
internal/cli/
├── group.go             # NEW: group list/show/create/delete
├── add.go, edit.go, copy.go       # --group/--no-group; changeGroup; rename/remove set-aside
├── list_show.go         # group column and JSON; set-aside listing
├── harness.go           # harness show history entries; classify keeps history fields
├── help_sharing.go, levels.go     # "History groups" section; always-private wording
└── ui.go                # group select glue
internal/tui/forms.go              # group select
README.md                          # history groups section; layer model update
specs/002-profile-isolation-levels/spec.md   # note on the FR-008/level-table exception
tests/integration/history_groups_test.go     # NEW: US1 to US5, SC-001..SC-005
```

**Structure Decision**: a new `internal/history` package owns groups, stores and the join/leave
planning; it emits `layer.Step` values so dry runs, confirmation and apply reuse the spec 002
plan/apply path (SC-005). The layer planner only needs the expected store to treat links
correctly.

## Design Decisions

1. **Store abstraction**: `home` resolves each entry with `def.DefaultPathUnder(home, rel)`
   (honors `default_locations`); other groups join the entry path to the store root. Callers
   never special-case `home` beyond `IsHome()` (FR-016 writes, purge refusal).
2. **Join plan** (`PlanJoin`): for each history rule: absent entry -> `link-history`; a link ->
   relink if wrong; a real entry -> for `merge: per-file`, walk files, `move-to-store` when the
   store path is absent, else `set-aside` (reason `exists-in-store`); then remove the emptied
   directory tree and `link-history`; for `merge: none`, `set-aside` the whole entry (reason
   `not-mergeable`), then link. For `home`, a missing directory in the default location becomes
   a `mkdir` step marked as a default-setup write (`Note: "default setup"`), emitted only when
   `PlanOptions.ExplicitJoin` is set (add, edit, copy), never by `sync`; for other stores, `mkdir`
   in the store. A missing history *file* in any store is linked anyway (dangling until the
   harness writes it). Store-side `mkdir` happens before moves.
3. **Apply order and safety**: moves use `moveNoOverwrite`, which never overwrites even when a
   running member creates the destination between planning and apply (hard link fails with
   `EEXIST`; the item is then set aside instead); a failure stops the apply, leaving
   the profile entry partly moved but nothing lost (moved files are in the store, the rest still
   in the profile); rerunning `edit --group` completes it (FR-022b). The set-aside manifest is
   written before the first set-aside move and appended per item.
4. **Leave plan**: `unlink` each history link (never the target) then `create` an empty private
   entry; move = `relink-history` to the new store (no data moves; the profile's entries are links
   already), plus a `default setup` `mkdir` for missing history directories when the target is
   `home`.
5. **Planner awareness (R7)**: `PlanOptions.HistoryStore` lets `planRule` accept a history link
   whose target equals the store path, emit `relink-history` for a wrong target, emit
   `link-history` for a missing file entry, and for a missing directory entry when the store
   side exists, is a non-`home` store (where it may create the directory), or `ExplicitJoin` is
   set, and `report` a real entry with "run `aprofile edit <p>
   --group <g>`". Entries no longer declared as history but linked into a store become
   `fix-link` (empty private entry). Isolated profiles: unchanged.
6. **Groups are records plus scanning**: membership lives in profile files; `Members` is computed
   by scanning profiles, so there is no second source of truth to drift.
7. **Text**: the always-private sentence changes from "logins, history, sessions and memory are
   always private" to "logins are always private; history and memory are private unless you put
   profiles in a history group"; README and `levels.go` change together (readme_test).
8. **Version notes**: a definition-level `history_note` (free text, for example pi's session
   format migration) is shown by `harness show` and `help sharing`, not per profile.
9. **TUI**: a group select (isolated, `home`, existing groups) on add and edit; parity test
   extended.

## Migration and compatibility

- Existing profiles have no `history_group` and stay isolated; the planner path for them is
  unchanged (SC-002, upgrade test).
- Registry and wrapper gain one additive field/variable; `render_test.go` and `registry_test.go`
  pins are updated.
- User `harnesses.d` overrides of history entries without `history: true` turn the entry back into
  private state for that user (documented in the contract).
- The user guide on the `docs/profile-guide` branch is out of this branch; README and in-tool help
  are updated here.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Constitution amendment (Principles II, III) | The feature is an opt-in exception to "state never symlinked" | A separate mechanism outside config dirs (a native session-dir variable) exists only for pi (R4) |
