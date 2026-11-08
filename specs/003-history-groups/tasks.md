---

description: "Task list for history groups"
---

# Tasks: History Groups

**Input**: Design documents from `specs/003-history-groups/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/cli.md,
contracts/harness-definition.md, quickstart.md

**Tests**: Required. Constitution VII mandates test-first development and table-driven tests for
harness definitions and layer planning. Each test task precedes its implementation and must fail
before the implementation starts. All tests use `testutil.HermeticHome` or the
`tests/integration` `newEnv` helper; none read the real home.

**Organization**: grouped by user story (spec.md US1 to US5) after a foundational phase.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on incomplete tasks)
- **[Story]**: user story the task serves (US1 to US5)

---

## Phase 1: Setup

**Purpose**: rules and fixtures every later phase relies on.

- [X] T001 Amend `.specify/memory/constitution.md` to 1.2.0 (FR-025): Principle III State bullet gains the opt-in history-group exception (history entries of a member of an explicitly chosen group are linked to the group store; credentials and all other state stay private; isolated profiles unchanged); Principle II's allowed-writes list gains adding, replacing and removing history links of members, creating fresh empty history entries on leave or when an entry stops being history, and, on an explicit confirmed join, moving mergeable history into the store and moving the rest to set-aside history; the default-setup rule gains the FR-016 writes on an explicit `home` join, never by `sync`; update the Sync Impact Report header and the version line (`**Version**: 1.2.0 ... **Last Amended**: 2026-11-07`)
- [X] T002 [P] Extend `internal/testutil/defaults.go` with `SeedHistory(t, dir, harness)` that writes history fixtures from research.md R1 (Claude: `projects/-tmp-demo/<uuid>.jsonl`, `projects/-tmp-demo/memory/MEMORY.md`, `history.jsonl`, `paste-cache/h1.txt`, `file-history/<uuid>/a@v1`, `todos/<uuid>-agent-x.json`, `plans/slug.md`, `tasks/<uuid>/1.json`; Codex: `sessions/2026/11/07/rollout-x-<id>.jsonl`, `session_index.jsonl`, `session_index.jsonl.lock`, `history.jsonl`; pi: `agent/sessions/--tmp--/ts_<id>.jsonl`), with a test in `internal/testutil/defaults_test.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: definition fields, paths, profile field, the history package core and a
history-aware planner. No story work starts before this phase is complete.

- [X] T003 [P] Write failing table tests in `internal/harness/harness_test.go` for the rule fields `history` and `merge` (contracts/harness-definition.md): `history` valid only on `layer: state`, rejected on globs, mixed entries and unsafe paths; `merge` only `per-file|none`, only with `history: true`, `per-file` only on directory entries (trailing `/`); database companions inherit both fields; `HistoryRules()` and `SupportsGroups()`; optional top-level `history_note` string accepted and listed in `allFieldNames`
- [X] T004 Implement in `internal/harness/definition.go` (`Definition.HistoryNote string \`yaml:"history_note,omitempty"\``, `EntryRule.History bool \`yaml:"history,omitempty" json:"history,omitempty"\``, `EntryRule.Merge MergeMode \`yaml:"merge,omitempty" json:"merge,omitempty"\``, `MergeMode` constants `per-file` and `none`, `HistoryRules()`, `SupportsGroups()`) and `internal/harness/loader.go` (validation in the classification loop near `:326-350`; `expandCompanions` at `:228-249` copies `History` and `Merge`) to pass T003
- [X] T005 Mark history entries in the built-in definitions per data-model.md "Built-in values": `internal/harness/builtin/claude.yaml` (`projects/` per-file, `history.jsonl` none, `paste-cache/` per-file, `file-history/` per-file, `todos/` per-file, `plans/` per-file, `tasks/` per-file; `sessions/`, `session-env/`, `shell-snapshots/`, `teams/`, `transcripts/` stay private), `internal/harness/builtin/codex.yaml` (`sessions/` per-file, new `archived_sessions/` state+history per-file, `session_index.jsonl` none, `session_index.jsonl.lock` none, `history.jsonl` none), `internal/harness/builtin/pi.yaml` (`agent/sessions/` per-file; `history_note` about the v1/v2 to v3 session migration from research R1); OpenCode unchanged; cite research.md R1 in comments; extend `internal/harness/builtin_test.go` (or the existing built-in test file) to assert these lists and that `opencode` has `SupportsGroups() == false`
- [X] T006 [P] Write failing tests then add path builders to `internal/config/config.go`: `GroupsConfigDir()` = `<config>/aprofile/groups`, `GroupRecordPath(h, g)` = `<config>/aprofile/groups/<h>/<g>.yaml`, `GroupStoreDir(h, g)` = `<data>/aprofile/groups/<h>/<g>`, `SetAsideDir(h, n)` = `<data>/aprofile/set-aside/<h>/<n>`; tests in `internal/config/config_test.go`
- [X] T007 [P] Write failing tests then add `HistoryGroup string \`yaml:"history_group,omitempty"\`` to `profile.Profile` (`internal/profile/profile.go:23-50`); `Validate()` checks the name with `NameValid` (`^[a-z][a-z0-9-]{0,62}[a-z0-9]$`) when set; round-trip and validation tests in `internal/profile/profile_test.go`
- [X] T008 Write failing tests in `internal/history/group_test.go` then implement `internal/history/group.go` and `internal/history/store.go` per plan.md Interfaces: `Manager` with `List`, `Get` (synthesizes `home` for harnesses with `SupportsGroups()`; `ErrUnknownGroup`, `ErrNotSupported`), members computed by scanning `profile.Store.ListByHarness` for `HistoryGroup`, the default setup listed as implicit `home` member; `Store` with `EntryPath(rel)` (`home`: `def.DefaultPathUnder(home, rel)`; others: `filepath.Join(GroupStoreDir, rel)`), `IsHome()`, `Root()`; `Store` also has `Name()` (group name) so it satisfies `layer.HistoryStore`; errors `ErrUnknownGroup` and `ErrNotSupported` are exported; `Manager.StoreFor(def, p)` returns nil for isolated profiles and `ErrUnknownGroup` for a missing group; `CheckHomeLocation` refuses when the default location is missing, a symlink, inside `<data>/aprofile`, or equal to a profile config dir (exit-2 class error naming the condition)
- [X] T009 Write failing table tests in `internal/layer/plan_test.go` for history awareness (research R7): with `PlanOptions.HistoryStore` set, a history entry that is a link to the store path yields no step; a link elsewhere yields `relink-history`; an absent *file* entry always yields `link-history` (dangling until written); an absent *directory* entry yields `link-history` when the store path exists, or when the store is not `home` (with a store-side `mkdir`), or, for `home` with a missing default-location directory, a `mkdir` noted `default setup` plus `link-history` only when `PlanOptions.ExplicitJoin` is set, otherwise a `report` naming `aprofile edit <p> --group home` (assert `sync`-mode plans never contain a `default setup` mkdir); a real entry yields `report` naming `aprofile edit <p> --group <g>`; a link into a store for an entry not declared as history yields `fix-link`; with `HistoryStore` nil every result is identical to today's (golden comparison over existing cases); non-history state is unaffected in both modes
- [X] T010 Implement T009 in `internal/layer/plan.go`: new step kinds `move-to-store`, `set-aside`, `link-history`, `relink-history` (constants next to `:20-34`, `String()` text used by dry runs), `PlanOptions.HistoryStore` (interface declared in `layer` to avoid an import cycle: `type HistoryStore interface{ EntryPath(rel string) string; IsHome() bool; Name() string }`) and `PlanOptions.ExplicitJoin bool`, `type SetAsideItem struct{ Path, Reason string }`, history handling in `planRule` (`:198-227`) and `leakingLink` (`:230`)
- [X] T011 Write failing tests in `internal/layer/apply_test.go` then implement the new kinds in `internal/layer/apply.go`: `moveNoOverwrite(src, dst)` (create parent dirs; `os.Link(src, dst)`, which fails with `EEXIST` and never overwrites, then `os.Remove(src)`; on `EXDEV` copy to an `O_EXCL` target with mode and mtime, fsync, then remove the source; when the destination appears between planning and apply, the item is set aside instead, with a unit test that creates the destination after planning); `set-aside` moves into the set-aside dir and calls `ApplyOptions.SetAsideManifest(item SetAsideItem)` per item before moving; `link-history` only creates the link (store directories come from separate `mkdir` steps); `relink-history` removes only the link and creates the new link; a failure stops apply and returns what was applied; second run is a no-op
- [X] T012 Write failing tests then update `internal/layer/treatment.go` (`treatmentFor` `:115`, `:138-140`): with a history store, a correct history link is `TreatmentLinked` with reason `history group <g>` and status OK; a wrong target is a drift status; isolated profiles unchanged; tests in `internal/layer/treatment_test.go`
- [X] T013 Update `internal/doctor/doctor.go` `isolationFindings` (`:467-517`) so a member's history link to its store is not a leak; leave all other findings unchanged; test in `internal/doctor/doctor_test.go`
- [X] T014 Wire the store into preparation: `internal/layer/prepare.go` `PrepareOptions` gains `HistoryStore` and `ExplicitJoin` (passed through to `PlanOptions`), and `internal/sync/sync.go` (per-profile `PrepareConfigDirWith` at `:283-311`), `internal/cli/login.go` and `internal/cli/copy.go` pass `Manager.StoreFor(def, p)` (nil when isolated; an unknown group is reported by sync and the profile is skipped for history handling only, FR-022c)

**Checkpoint**: `go test ./...` passes; isolated profiles behave exactly as before (existing tests
unchanged); the planner accepts and maintains a member's history links.

---

## Phase 3: User Story 1 - Continue a conversation under another profile of the same group (Priority: P1) MVP

**Goal**: profiles created with `--group home` share the default setup's history.

**Independent Test**: two Claude profiles added with `--group home` see one session file written
through either config dir, with exactly one copy on disk (SC-001).

- [X] T015 [P] [US1] Write failing integration tests in `tests/integration/history_groups_test.go`: `add claude personal --group home` and `add claude lab --group home` on a seeded default setup; every Claude history entry of both profiles is a link to `~/.claude/<entry>` (missing history dirs created empty in `~/.claude`, missing history files linked as dangling links that resolve once written); a session file written through `personal` exists once under `~/.claude/projects` and is visible through `lab`; credentials and non-history state entries are private real entries; `show claude/lab` lists `projects/` as `linked` with reason `history group home`; the wrapper still sets the profile's own endpoint and model variables; same check for `codex` and `pi` (SC-001); `opencode` with `--group home` exits 2 with "does not support history groups"; `add claude n --share none --group home` shows "shares history with your default setup" in `show` text and `show --json`; with `~/.claude` replaced by a symlink, and with `~/.claude` missing, `add claude x --group home` exits 2 and creates nothing
- [X] T016 [US1] Implement `--group <g>` on `add` in `internal/cli/add.go` (`profileFlags` `:19-39`, `bind()` `:41-57`): validate through `history.Manager.Get` before creating anything (exit 2 listing the harness's groups, FR-009); for `home` run `CheckHomeLocation` first; store `HistoryGroup`; build the profile's config dir with `HistoryStore` and, plan with `PlanOptions.ExplicitJoin = true` so missing history directories are created in the default location (FR-016); print the concurrent-use notice once (FR-023) and the group line
- [X] T017 [US1] Show history membership in `internal/cli/list_show.go`: group line in `show`, `history_group` in `profileJSON` (`:30-51`), entries from T012; for a `none` profile in `home`, a line "shares history with your default setup" (FR-020)

**Checkpoint**: US1 works end to end for Claude, Codex and pi.

---

## Phase 4: User Story 2 - Separate groups for separate worlds (Priority: P1)

**Goal**: own groups with stores in the data dir; groups do not see each other.

**Independent Test**: `claude/work` with two members, one `home` member, one isolated profile;
each session is visible only within its group (or its own profile).

- [X] T018 [P] [US2] Write failing integration tests in `tests/integration/history_groups_test.go`: `group create claude/work` creates `<data>/aprofile/groups/claude/work/` with empty history directories and `<config>/aprofile/groups/claude/work.yaml` (`harness`, `name`, `created`); `add claude acme --group work` and `add claude corp --group work` share sessions; `home` and `work` members do not see each other's sessions; an isolated `claude/client` sees neither and its sessions appear nowhere else; `add claude test --group wrok` exits 2, lists `home` and `work`, and leaves zero new files or directories under config and data (SC-004, snapshot compare); `group create claude/home`, `group create claude/work` (again), `group create opencode/x`, `group create claude/Bad_Name` exit 2
- [X] T019 [US2] Implement `Manager.Create` in `internal/history/group.go` (record via `config.WriteFileAtomic`, store with empty directory entries; refuse `home`, existing group, unsupported harness, invalid name, and an existing directory at `GroupStoreDir` naming the path) with unit tests in `internal/history/group_test.go`
- [X] T020 [US2] Add `internal/cli/group.go` with `newGroupCmd()` registered in `internal/cli/root.go:58-77`, and the `group create <h>/<g>` subcommand (`--json`), parsing refs with `parseProfileRef` (`internal/cli/output.go:97`), mapping `ErrUnknownGroup`, `ErrNotSupported` and validation errors to exit 2

**Checkpoint**: US1 and US2 pass independently.

---

## Phase 5: User Story 3 - Join and leave a group without losing history (Priority: P2)

**Goal**: membership changes on existing profiles are planned, confirmed and lossless.

**Independent Test**: an isolated profile with history joins, leaves and moves; every file is in
the store or the set-aside dir; stores are never changed by leave or move.

- [X] T021 [P] [US3] Write failing unit tests in `internal/history/join_test.go` for `PlanJoin`, `PlanLeave`, `PlanMove` (data-model.md transitions; plan.md Design Decisions 2 and 4): per-file merge is recursive by relative path, files absent in the store become `move-to-store`, files present become `set-aside` with reason `exists-in-store` (including `projects/<slug>/memory/MEMORY.md`), `merge: none` entries become one `set-aside` with reason `not-mergeable`; emptied directories are removed before `link-history`; already-linked entries produce no move steps; leave produces `unlink` plus `create` per history entry and never a step touching the store; move produces `relink-history` only, plus a `default setup` `mkdir` for each missing history directory when the target is `home`; joining the current group again plans only missing entries (FR-022b) or nothing
- [X] T022 [US3] Implement `internal/history/join.go` (`PlanJoin`, `PlanLeave`, `PlanMove`, set-aside dir `<SetAsideDir>/<UTC ts>/` and `manifest.json` writer per data-model.md: `profile`, `group`, `created`, `items[{path, reason}]`) to pass T021
- [X] T023 [P] [US3] Write failing integration tests in `tests/integration/history_groups_test.go`: `edit claude/solo --group home --dry-run` lists exactly the steps the real run performs (SC-005, compare with `entrySnapshot`/`diffSnapshots` from `level_change_test.go`) for join with merge, join with set-aside, move between groups and leave; `edit claude/solo --group ghost` exits 2 and creates zero files (SC-004 snapshot); `edit claude/solo --share none` keeps `history_group`; without a TTY and without `--yes` exits 3 with no change; with `--yes` moves `s-solo.jsonl` into `~/.claude/projects/-tmp-demo/`, sets aside `history.jsonl` and the colliding `MEMORY.md` with a manifest; no store file is overwritten (byte compare) and total files and bytes across store plus set-aside plus profile are unchanged (SC-003); `--no-group` leaves the store byte-identical and creates empty private entries; refused with exit 4 when `APROFILE_TEST_RUNNING=pid` (testseam build); a Codex profile with existing history sets aside every history entry it cannot merge (`history.jsonl`, `session_index.jsonl`) and moves rollouts; `--group` with `--no-group` is a usage error
- [X] T024 [US3] Implement `changeGroup` in `internal/cli/edit.go` modeled on `changeShare` (`:291-391`): flags `--group`, `--no-group` (mutually exclusive) added to `editValueFlags` (`:79`) and `anyProfileFlagChanged` (`:86-97`); validate the target group; `CheckHomeLocation` whenever the target is `home` (join or move); plan via `internal/history`; print the plan; `--dry-run` returns; running check via `profileRunning` (`:407`) with exit 4 (unknown asks via `confirmOrYes`); confirm once when any `move-to-store` or `set-aside` step exists (exit 3 off a TTY without `--yes`); apply with the manifest writer; save `HistoryGroup`; mark wrapper and registry stale as other edits do; print the concurrent-use notice on join; when `--share` is also given, run the share change first
- [X] T025 [US3] First write a failing integration test in `tests/integration/history_groups_test.go` that `remove --purge` of a member leaves every store file and the default setup byte-identical and removes its set-aside dir (FR-019), and that `rename` keeps `history_group`, moves the set-aside dir and leaves the store byte-identical; then carry set-aside history in `rename` (`internal/cli/edit.go:494-587`, alongside the credentials move at `:566-573`, with the same pre-check pattern as `:541-549`) and delete it in `remove --purge` (`:658-709`)

**Checkpoint**: US3 passes; SC-003 and SC-005 covered.

---

## Phase 6: User Story 4 - Manage groups explicitly (Priority: P2)

**Goal**: list, show and delete groups; `home` is permanent.

**Independent Test**: on a fresh home, `group list` shows `home` per supported harness; create,
show, delete; `home` cannot be deleted.

- [X] T026 [P] [US4] Write failing integration tests in `tests/integration/history_groups_test.go`: `group list` and `group list --json` show `claude/home`, `codex/home`, `pi/home` with the default setup as implicit member and no `opencode` row; `group show claude/work --json` lists store, members and history entries with `merge`, `exists`, `size_bytes`; `group delete claude/work` with members exits 2 and lists them; after members leave, delete removes the record, keeps the store and prints its path; `group create claude/work` then exits 2 naming the kept store; `group delete claude/home` exits 2; `group delete claude/x --purge --yes` on an empty group deletes its store; `--purge` without `--yes` off a TTY exits 3; `group show claude/ghost` and `group delete claude/ghost` exit 2 and create nothing; `group delete` without `--purge` leaves the store byte-identical; the default setup appears only as a member of `home` and `group list --json` never lists it under another group (FR-017)
- [X] T027 [US4] Implement `Manager.Delete` (refuse `home` and groups with members; `purge` deletes the store only when it lies under `<data>/aprofile/groups`) in `internal/history/group.go` with unit tests, and `group list`, `group show`, `group delete` subcommands in `internal/cli/group.go` with text and `--json` output per contracts/cli.md and data-model.md "JSON output"

**Checkpoint**: US4 passes.

---

## Phase 7: User Story 5 - See and keep group membership honest (Priority: P3)

**Goal**: drift detection, non-destructive `sync`, and the group in registry and wrapper.

**Independent Test**: a member's history link replaced by a real directory is reported by
`doctor`, left by `sync`, and restored by `sync --relink`.

- [X] T028 [P] [US5] Write failing integration tests in `tests/integration/history_groups_test.go`: (a) a member's `projects/` replaced by a real directory, and a member's `history.jsonl` link replaced by a real file (temp-file-and-rename save): `doctor` reports a problem with the `sync --relink` fix, `sync` leaves it and reports `edit --group`, `sync --relink --yes` backs it up and restores the link; (b) a member's history link deleted: `sync` recreates it; (c) a link pointing at another store: `sync` relinks it (FR-022c); (d) a profile file with `history_group: ghost`: `doctor` reports it, `sync` changes nothing for its history entries (covers T014's unknown-group path); (e) a non-`home` store directory removed by hand: `doctor` reports it, `sync` recreates empty store entries; (f) a kept store of a deleted group and set-aside history appear in `doctor` as info; (g) a harnesses.d override that adds `history: true` to `todos/` for a member with a real `todos/`: `sync` reports and names `edit --group`, never moves; `edit --group home` completes it (FR-022a, FR-022b); an override that removes `history` from `plans/`: `sync` replaces the link with an empty private entry; (h) `sync` twice writes nothing the second time; (i) `sync` never writes into `~/.claude` (snapshot compare)
- [X] T029 [US5] Implement the doctor checks in `internal/doctor/doctor.go` (problems: member history entry not a link, wrong target, missing non-`home` store, unknown group; info: kept stores of deleted groups by scanning `<data>/aprofile/groups/<h>/` for directories without a record, set-aside history) and the sync reporting in `internal/sync/sync.go` (`Result` gains reported history items) to pass T028
- [X] T030 [P] [US5] Write failing tests then add `HistoryGroup string \`json:"history_group"\`` to `registry.Entry` (`internal/registry/registry.go:27-40`, additive at `FormatVersion` 1) filled in `internal/sync/sync.go:363-374`, and export `APROFILE_HISTORY_GROUP` in `internal/wrapper/wrapper.go` `renderContract` (`:109-121`, always set, empty when isolated) and `internal/wrapper/env.go` `Environ` (`:155-161`); update `internal/wrapper/render_test.go:140,147` and `internal/registry/registry_test.go` pins
- [X] T031 [US5] Add a `GROUP` column to `list` text output (`internal/cli/list_show.go:115`, `-` when isolated) and `set_aside` (list of `{dir, items}`) to `show --json` (`showJSON` `:168-180`); test in `internal/cli/list_show_test.go`

**Checkpoint**: all user stories pass independently.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T032 [P] Implement `--group` on `copy` in `internal/cli/copy.go` (`copyFlags` `:57-73`, flags `:107-119`): validate in the target's harness before writing (exit 2), join the new target with `ExplicitJoin` set (and `CheckHomeLocation` for `home`), otherwise leave it isolated; when the source is a member and a group of that name exists in the target's harness, add the hint to the report (`copyReportJSON` `:25`); integration tests in `tests/integration/history_groups_test.go` that copy never transfers history files and leaves the source's store byte-identical, that `copy --group ghost` exits 2 and creates nothing, that `copy claude/a codex/b --group work` looks `work` up in `codex` (exit 2 when only `claude/work` exists), and that a member source prints the hint only when the target's harness has a group of that name
- [X] T033 [P] Update `internal/cli/harness.go`: `harness show` lists history entries with merge mode and the `history_note`, or "history groups not supported"; `classifyOverride` (`:320-380`) keeps `history`/`merge` when the new category is `state` and drops them with a note otherwise (research R8); tests in `internal/cli/harness_classify_test.go` and `internal/cli/harness_show_test.go`
- [X] T034 [P] Add a group select to the TUI: `internal/tui/forms.go` (`FormValues` `:14-28` gains `HistoryGroup`; `ProfileFormConfig` gains `GroupOptions` with "none (isolated)", `home` and existing groups; select modeled on `shareSelect` `:137`), glue in `internal/cli/add.go` (`profileFormConfig` `:208`, `formValuesFromFlags` `:254`, `applyFormValues` `:274`), `internal/cli/edit.go` (`formEditDefaults` `:101`, `applyEditForm` `:136-153`) and `internal/cli/ui.go`; extend `internal/cli/form_parity_test.go` and `tests/integration/ui_parity_test.go`, and assert the options never include the default setup as a choice (FR-027, FR-017)
- [X] T035 Update user-facing text together (FR-026, plan.md Design Decision 7): in `internal/cli/levels.go` change `alwaysPrivate` to "logins are always private; history and memory are private unless you put profiles in a history group", update the level table row for runtime state, add the use cases UC1 to UC4 of spec 003; add a "History groups" section to `internal/cli/help_sharing.go` (`home`, own groups, isolated profiles, the concurrent-use warning, that shared conversations show which account wrote them, OpenCode not supported, and each harness's `history_note`); update `README.md` ("Choosing a share level" `:42`, a new "History groups" section, "The layer model" `:186`, wrapper contract `:199` with `APROFILE_HISTORY_GROUP`) so `internal/cli/readme_test.go` passes
- [X] T036 [P] Add a note to `specs/002-profile-isolation-levels/spec.md` (level table row "Runtime state" and FR-008) and `specs/002-profile-isolation-levels/contracts/cli.md` (wrapper and registry section) pointing to the spec 003 exception
- [X] T037 Add a performance test in `tests/integration/perf_test.go`: joining a profile with 2,000 session files completes in under 5 s, `sync` of 20 profiles (half of them members) stays under 1 s, and a member's wrapper start-up stays within the existing 50 ms overhead check (`tests/integration/overhead_test.go`)
- [X] T038 Run `go vet ./...`, `go test ./...` and the automated part of `specs/003-history-groups/quickstart.md`; fix any failures; update the quickstart if a command or output differs; record the manual walkthrough (SC-006 timing) and the research R3 cross-account resume result, if a real harness is available, in `specs/003-history-groups/quickstart.md` under a "Results" heading

---

## Dependencies & Execution Order

- **Setup (T001-T002)**: no dependencies; T001 is documentation and can run any time before
  Phase 8.
- **Foundational (T003-T014)**: T003 -> T004 -> T005; T006 and T007 parallel; T008 needs T004,
  T006, T007; T009 -> T010 -> T011; T012 and T013 need T010; T014 needs T008 and T010.
- **US1 (T015-T017)**: needs Phase 2. **MVP.**
- **US2 (T018-T020)**: needs Phase 2; independent of US1 except shared test file (add tests in
  separate functions).
- **US3 (T021-T025)**: needs Phase 2; T024 benefits from T016's add path but does not depend on it.
- **US4 (T026-T027)**: needs T019-T020 (create), T016 (`add --group`) and T024 (`edit --no-group`) for its fixtures.
- **US5 (T028-T031)**: needs Phase 2 and T024 for `edit --group` completion checks.
- **Polish (T032-T038)**: after the stories they touch; T035 after T017 and T031.

### Parallel opportunities

- T006, T007 alongside T003-T005.
- Test-writing tasks marked [P] (T015, T018, T021, T023, T026, T028, T030) can be drafted in
  parallel once Phase 2 is done; they live in separate test functions.
- T032, T033, T034, T036 touch different files.

## Implementation Strategy

1. Phase 1 and 2, then US1: profiles in `home` share history (MVP, covers the main use case).
2. US2: own groups.
3. US3: safe membership changes for existing profiles.
4. US4 and US5: management and honesty.
5. Polish: copy, harness commands, TUI, text, performance, quickstart.
