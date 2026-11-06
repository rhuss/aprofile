---

description: "Task list for profile share levels and hook sets"
---

# Tasks: Profile Share Levels and Hook Sets

**Input**: Design documents from `specs/002-profile-isolation-levels/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/cli.md,
contracts/harness-definition.md, quickstart.md

**Tests**: Required. Constitution VII mandates test-first development and table-driven tests for
harness definitions, layer classification and config merging. Each test task precedes its
implementation and must fail before the implementation starts. All tests use
`testutil.HermeticHome` or `tests/integration` `newEnv`; none read the real home.

**Organization**: Tasks are grouped by user story (spec.md US1 to US6) so each story can be built
and verified on its own after the foundational phase.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete tasks)
- **[Story]**: User story the task serves (US1 to US6)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: fixtures and small shared helpers every later phase uses.

- [X] T001 [P] Create fake default setups from research.md R1 under `tests/fixtures/defaults/{claude,codex,opencode,pi}/` (empty files and dirs with the exact names, SQLite databases with `-wal`/`-shm` companions, a `settings.json` with `permissions`, `statusLine`, `enabledPlugins` and a `hooks` key, a Codex `hooks.json`, OpenCode `config/opencode/plugins/x.ts`, pi `agent/extensions/y.ts`, plus user extras `RTK.md` and `cpr.sh` for Claude) and a helper `testutil.SeedDefaults(t, home, harness)` in `internal/testutil/defaults.go`
- [X] T002 [P] Add `ExitError{Code int; Err error}` in `internal/cli/exit.go` and make `Execute` in `internal/cli/root.go` return its code (2 invalid for level, 3 confirmation needed without TTY, 4 profile running; everything else 1), with a unit test in `internal/cli/exit_test.go`
- [X] T003 [P] Extract the confirmation and backup helper from `remove --purge` (`internal/cli/edit.go:330-370`) into `internal/cli/confirm.go`: `confirmOrYes(cmd, prompt, yes bool) error` returning `ExitError{3}` off a TTY without `--yes`, and `backupPaths(paths, harness, name, now)` writing to `config.Paths.BackupDir(harness, name, ts)`; keep `remove --purge` behavior identical; tests in `internal/cli/confirm_test.go`
- [X] T004 [P] Create `internal/cli/levels.go` holding the single source of level text: names `all`, `content`, `none`; one-line descriptions verbatim from contracts/cli.md ("Uses your whole setup live; logins, history and memory stay private.", "Uses your instructions and skills; keeps its own settings and hooks.", "Shares nothing with your setup; starts clean, with only named hook sets."); the use-case table UC1 to UC7 and the level table from spec.md; a test asserting every description is at most 80 characters and the `all` description mentions logins and history (FR-014)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the definition schema, profile fields and the level-aware planner that every story
depends on. No user story work starts before this phase is complete.

- [X] T005 [P] Write failing table tests in `internal/harness/harness_test.go` and `internal/harness/override_hook_test.go` for: layers `content`, `settings`, `plugin-store` valid; `shared` accepted only from `harnesses.d` and mapped to `content` with a deprecation flag; built-ins rejected if they contain a glob rule with a borrowable layer; `share_policy` cells limited to `link|own|copy-once|minimal|managed`; `hooks.form` limited to `event-groups|files` with `location` in `file#key` form for `event-groups`; `home_escapes` entries; classification overrides merged by `entry` (replace, append, `layer: null` removes) while other lists still replace; database companions (`-wal`, `-shm`, `-journal`, `.lock`) implied for `*.sqlite`, `*.db` and `database: true`
- [X] T006 Extend `internal/harness/definition.go`: new `Layer` values `content`, `settings`, `plugin-store`; `shared` kept as deprecated alias; types `SharePolicy` (category to level to treatment, default table from contracts/harness-definition.md), `HookLocation{Location, Form}`, `HomeEscape{Path, What}`; fields `SharePolicy`, `Hooks`, `HomeEscapes` on `Definition`
- [X] T007 Update `internal/harness/loader.go` validation (`:184-255`) for the new fields and rules from T005, add `share_policy`, `hooks`, `home_escapes` to `allFieldNames` (`:268-273`), and expand database companions at load
- [X] T008 Change `internal/harness/override.go` so `classification` merges by `entry` (FR-001a) inside `overlayHarnessesDir` (`:25`), recording the origin of overridden rules for `harness show`
- [X] T009 [P] Write failing tests then add `type Share string` with constants `ShareAll`, `ShareContent`, `ShareNone`, and fields `Share` (`all|content|none`), `HookSets`, `ExcludeHookSets` (stored sorted) to `profile.Profile` in `internal/profile/profile.go:22`; a file without `share` loads as `all` and `Load` never rewrites it (FR-033); `Validate` rejects unknown level values; tests in `internal/profile/profile_test.go`
- [X] T010 [P] Add `DefaultShare` (default `content`) to `config.Settings` (`internal/config/config.go:29`), and paths `HooksDir` (`<config>/aprofile/hooks.d`), `HookFilesDir(set)` (`hooks.d/files/<set>`), `ManagedHooksPath(harness, name)` (`<data>/aprofile/managed-hooks/<harness>/<name>.json`) with tests in `internal/config/config_test.go`
- [X] T011 Write failing tests then update `internal/layer/classify.go`: classify into categories including companions; `Unclassified()` (`prepare.go:178`) treats glob matches as classified; new `UnborrowedDefaultEntries(def, defaultSetup)` lists default-setup entries that no rule covers (FR-001a); tests in `internal/layer/layer_test.go`
- [X] T012 Write failing table tests in `internal/layer/plan_test.go` for `Plan(def, profile, level, defaultSetup, current)` covering every category times every level times current state (absent, correct link, wrong link, real entry, leaking state link), producing steps `link`, `unlink`, `mkdir`, `copy-once`, `seed-mixed`, `replace-with-link` (confirm), `report`, with the level-transition table from data-model.md and FR-009a (a `mixed` or `owned` settings file is never linked); plus: default setup directory missing entirely (no links, settings created minimal, one `report` step "nothing to copy"), and profile config dir not yet created (plan starts with `mkdir`); signatures per plan.md Interfaces
- [X] T013 Implement the planner in `internal/layer/plan.go` (`Step{Kind, Path, Source, Confirm, Backup, Note}`) to pass T012
- [X] T014 Write failing tests then implement `internal/layer/apply.go`: `Apply(steps, opts{DryRun, Safe, Yes, Backup})` executes steps, backs up before every `Confirm` step, skips and reports `Confirm` steps in safe mode, never deletes a link target, and is idempotent (second run zero writes)
- [X] T015 Make `internal/layer/prepare.go` a wrapper over `Plan` plus `Apply`: remove pass 2 (`:136-156`) and `hasSharedGlob` (`:354`); make `seedMixedFile`/`stripNonSeedKeys` (`:296-342`) level-aware so `none` seeds no key from the default setup (FR-011); change `PrepareConfigDirWith` to take the profile, and update callers `internal/sync/sync.go:105`, `internal/cli/login.go:55`, `internal/cli/copy.go:125`
- [X] T016 Rewrite tests that pin the catch-all to the explicit lists: `internal/layer/layer_test.go:40-41`, `internal/layer/mixed_test.go:27,58,77-85`, `tests/integration/isolation_test.go:15-52`
- [X] T017 [P] Implement running detection on macOS in `internal/profile/running_darwin.go` (build tag `darwin`) by reading `kern.procargs2` for same-user pids via `golang.org/x/sys/unix` and matching `APROFILE_CONFIG_DIR=<dir>`; return an "unknown" result on read errors; make `golang.org/x/sys` a direct dependency in `go.mod`; restrict `running_other.go` to non-Linux, non-darwin; test by starting a child process with the variable set in `internal/profile/running_test.go`

**Checkpoint**: definitions load with categories, profiles carry a level, and one planner drives
preparation. Existing slice one tests pass after T016.

---

## Phase 3: User Story 1 - Runtime state never shared (Priority: P1) MVP

**Goal**: at every level, credentials and state of all four built-ins are private; unknown entries
are reported, never linked.

**Independent Test**: seed T001 fixtures, create one profile per harness per level, `sync`, and
assert no state or credentials entry is a link and every link is content, settings, plugin store or
hook location (spec US1, SC-001).

- [X] T018 [US1] Write failing integration tests in `tests/integration/isolation_levels_test.go`: per harness and level, no link among state and credentials (including `history.jsonl`, `sessions/`, `todos/`, `memories_1.sqlite` and its `-wal`/`-shm`); an unknown `foo.json` is untouched, not linked, and reported by `sync` and `doctor`; a slice one leaking `history.jsonl` link is reported and replaced by `sync --fix-links` with an empty entry while its target stays unchanged
- [X] T019 [P] [US1] Rewrite `internal/harness/builtin/claude.yaml` from research.md R1 Claude table: no catch-all; `settings.json` and `settings.local.json` as `settings`; `skills/` as `content`; `plugins/` as `plugin-store`; `hooks: {location: "settings.json#hooks", form: event-groups}`
- [X] T020 [P] [US1] Rewrite `internal/harness/builtin/codex.yaml` from R1 Codex table: no catch-all; `log/` (not `logs/`); `*.sqlite` state; `rules/` as `settings`; `config.toml` owned; `plugins/`, `packages/` as `plugin-store`; `hooks: {location: "hooks.json#hooks", form: event-groups}`; `home_escapes: [{path: "~/.agents/skills", what: skills}]`
- [X] T021 [P] [US1] Rewrite `internal/harness/builtin/opencode.yaml` from R1 OpenCode table: credentials `auth.json`, `mcp-auth.json`, `account.json`; `state/opencode/` and `cache/opencode/` state; skills, agent, command, themes as `content`; `tui.jsonc`, `cli.json` as `settings`; `node_modules/`, `package.json`, `bun.lock`, `package-lock.json` as `plugin-store`; `hooks: {location: "config/opencode/plugins", form: files}`; `home_escapes` for `~/.agents/skills` and `~/.claude/skills`
- [X] T022 [P] [US1] Rewrite `internal/harness/builtin/pi.yaml` from R1 pi table: `agent/settings.json` as `settings`; `agent/trust.json`, `agent/models-store.json` state; skills, prompts, themes as `content`; `hooks: {location: "agent/extensions", form: files}`
- [X] T023 [US1] Replace `LayerShared` handling in `internal/importer/importer.go` (`:20-21`, `:277-278`, `:331`) with the new categories (content, settings, plugin store and hook location are never imported; globs are classified, not "shared catch-all")
- [X] T024 [US1] Make T018 pass end to end (sync reporting in `internal/sync/sync.go`, doctor unclassified reporting unchanged at all levels) and run `go test ./...`

**Checkpoint**: the leak is closed for every existing and new profile; this alone is shippable.

---

## Phase 4: User Story 2 - Pick a share level from a use case (Priority: P1)

**Goal**: `--share` everywhere a profile is created, with plain descriptions and `help sharing`.

**Independent Test**: spec US2 scenarios 1 to 5; `help sharing` prints the tables; a `none`
profile has no content links and minimal settings; a `content` profile's settings copy has no
`hooks` key.

- [X] T025 [US2] Write failing tests in `tests/integration/share_levels_test.go` for US2 scenarios 1 to 5, including `add` output naming the level and its description, the default from `settings.yaml` `default_share`, and `help sharing` containing "logins, history, sessions and memory are always private"
- [X] T026 [US2] Add `--share` to `profileFlags`/`bind()` in `internal/cli/add.go:18-33`, to `anyProfileFlagChanged` and `editValueFlags` in `internal/cli/edit.go:73-79`, and to `applyEditFields` (`:136`); `add` uses `settings.default_share` when absent; flag help uses `levels.go` descriptions
- [X] T027 [US2] Add the share choice to the TUI: `FormValues.Share` in `internal/tui/forms.go:13-23`, a select in `RunProfileForm` (`:70`) showing each description and its use cases, and the mappings `formValuesFromFlags`, `applyFormValues` (`internal/cli/add.go:184/:200`), `formEditDefaults`, `applyEditForm` (`internal/cli/edit.go:93/:115`); extend `internal/cli/form_parity_test.go` and `tests/integration/ui_parity_test.go`
- [X] T028 [P] [US2] Implement `aprofile help sharing` in `internal/cli/help_sharing.go` (registered in `internal/cli/root.go`), rendering the level table, use-case table and always-private sentence from `levels.go`, plus per-harness differences computed from each definition's effective `share_policy` and FR-009a (FR-013, FR-022)
- [X] T029 [P] [US2] Export `APROFILE_SHARE` in `renderContract` (`internal/wrapper/wrapper.go:111-122`, both the `set` line and the export line) and in `loginEnv` (`internal/cli/login.go:91-96`); update `internal/wrapper/render_test.go:45` and `:135-151`
- [X] T030 [P] [US2] Add `Share` to `registry.Entry` (`internal/registry/registry.go:27`), fill it in `internal/sync/sync.go:147-157`, update `registry_test.go:100-127` from 9 to 10 fields, and add a note to `specs/001-aprofile-standalone-profile-cli/contracts/registry-and-env.md` that `share` was added additively at `format_version` 1
- [X] T031 [P] [US2] Add a share column to `list` and `share` to `listJSON`/`profileJSON` in `internal/cli/list_show.go:26-43`
- [X] T032 [US2] Add `--share` to `import cc-deck` in `internal/cli/import.go` and use it (or `default_share`) in `buildProfile` (`internal/importer/importer.go:221`)
- [X] T033 [US2] Add `--share` to `copy` (`internal/cli/copy.go:35-62`) and implement FR-007a: level defaults to the source's; when source and target both have own settings, the target's settings start as a copy of the source's without the hook location; skills travel only when they are the source's own (at `none`); replace the owned-skills assumption in `carryOwnedLayer` (`:235`) and `carrySkills` (`:325`); tests in `internal/cli/copy_test.go`

**Checkpoint**: users can choose and see a level; US1 and US2 together are the MVP.

---

## Phase 5: User Story 3 - See, preview and refresh what a profile shares (Priority: P2)

**Goal**: per-entry treatment in `show`, exact dry runs, safe level changes, `settings pull`,
`harness classify`.

**Independent Test**: spec US3 scenarios 1 to 6; SC-006 property: for every level pair and
harness, applying after `--dry-run` performs exactly the listed steps.

- [X] T034 [US3] Write failing tests: `internal/cli/list_show_test.go` for per-entry treatment and reason per level; `tests/integration/level_change_test.go` for `edit --share --dry-run` output, the SC-006 property over all 6 level pairs and 4 harnesses, backup plus confirmation when moving to `all`, exit 3 without TTY and `--yes`, exit 4 while running, a level change on a profile that was never synced, and the `all` to `content` dry run listing the default setup's hooks as not copied with the hint `aprofile hook-sets import`
- [X] T035 [US3] Extend `show` (`internal/cli/list_show.go:119`, `profileToJSON` `:155`): `share_description`, `entries[]` `{entry, category, treatment, reason, status}` from `layer.Treatments` (planner report mode, plan.md Interfaces), `hook_sets[]`, `settings_differ`, `home_escapes[]`, `unborrowed_default_entries[]` (data-model.md Entry treatment); text view groups entries by treatment
- [X] T036 [US3] Implement the share change in `edit` (`internal/cli/edit.go`): plan, print, refuse with `ExitError{4}` when `profile.Running` is true and ask when it is unknown, confirm and back up `Confirm` steps via `confirm.go`, apply, then print "run: aprofile sync" for wrapper and registry (FR-016, FR-017, FR-017a); `--dry-run` and `--yes` flags
- [X] T037 [US3] Make `sync` run the planner in safe mode (`internal/sync/sync.go:105`): apply non-confirm steps, report each confirm step with the `aprofile edit <h>/<n> --share <level>` command that completes it (FR-017a)
- [X] T038 [P] [US3] Write failing table tests then implement `internal/layer/settings.go`: merge (objects by key, lists gain missing default entries in order, scalars take the default's value, profile-only keys kept) and replace (exact copy of pulled keys), both excluding the hook location key and owned keys; secret-pattern warnings per key without values (research R6); JSON only, other formats copied as whole files under `--replace` and reported otherwise; an invalid JSON file on either side fails with an error naming the file and writes nothing (also for the one-time copy in T013/T014); signatures per plan.md Interfaces
- [X] T039 [US3] Implement `aprofile settings pull <h>/<n> [key...]` in `internal/cli/settings.go` with `--replace`, `--dry-run`, `--yes`, `--json`; refuse `all` and `none` with `ExitError{2}` and an explanation; back up before writing (FR-018)
- [X] T040 [P] [US3] Extend `harness show` (`internal/cli/harness.go:111`) with the effective treatment per category per level, plugin store, hook location and form, home escapes, and origin per classification rule
- [X] T041 [US3] Implement `aprofile harness classify <harness> <entry> --as <category>` in `internal/cli/harness.go`, writing or updating `<config>/aprofile/harnesses.d/<harness>.yaml` with one keyed rule via `config.WriteFileAtomic`, `--dry-run`; tests in `internal/cli/harness_classify_test.go`

**Checkpoint**: every claim about a profile is visible and every change is previewable.

---

## Phase 6: User Story 4 - Hook sets (Priority: P2)

**Goal**: central hook library, per-profile selection, user hooks preserved.

**Independent Test**: spec US4 scenarios 1 to 6 and quickstart.md section 4; SC-008.

- [X] T042 [US4] Write failing table tests in `internal/hooksets/*_test.go`: library load and validation (body shape per form, secret prefix check, unknown harness); `applies(S, P)` truth table from data-model.md; event-groups place, update, detach preserving user groups and recording fingerprints; diverged groups left alone and not re-added; files form links into `hooks.d/files/<set>/`, never touching real files or foreign links; deterministic order (user groups first, then sets alphabetically)
- [X] T043 [P] [US4] Implement the library in `internal/hooksets/library.go` (`hooks.d/<name>.yaml`: `name`, `description`, `every_profile`, `bodies`)
- [X] T044 [P] [US4] Implement form `event-groups` in `internal/hooksets/eventgroups.go` (key path inside a JSON file; creates the file with `{"hooks": {}}` when the location is a separate own file such as Codex `hooks.json` at `content`/`none`)
- [X] T045 [P] [US4] Implement form `files` in `internal/hooksets/files.go`
- [X] T046 [P] [US4] Implement the record store in `internal/hooksets/record.go` (`managed-hooks/<harness>/<name>.json`, `format_version: 1`, SHA-256 of key-sorted groups)
- [X] T047 [US4] Call hook placement from `internal/sync/sync.go` after layer preparation; report diverged entries and missing sets (exit non-zero for a missing set, spec Edge Cases); clear the record when a profile moves to `all`
- [X] T048 [US4] Add `--hook-sets` and `--exclude-hook-sets` to `add`, `edit`, `copy` (`internal/cli/add.go`, `edit.go`, `copy.go`) with validation: unknown set, no body for the harness, harness without `hooks` (FR-024), and `all` level (`ExitError{2}`, FR-027); `copy` carries the source's `hook_sets` and `exclude_hook_sets` when the target harness has a body for each set and reports the ones left out (FR-007a); add a multi-select to `RunProfileForm` and `RunCopyForm` in `internal/tui/forms.go`; extend the form-parity tests
- [X] T049 [US4] Implement `internal/hooksets/importer.go` and `aprofile hook-sets list|show|import|reapply` in `internal/cli/hooksets.go`: `import` copies `files`-form files into the library and fails without changes if the set exists (FR-031); `reapply` plans, confirms, backs up and re-places (FR-028)
- [X] T050 [US4] Write and pass `tests/integration/hooksets_test.go` following quickstart.md section 4, including a hand-added user group that survives attach and detach, and a `none` profile not receiving an `every_profile` set

**Checkpoint**: hooks are selectable per profile across all four harnesses.

---

## Phase 7: User Story 5 - Keep the setup honest (Priority: P3)

**Goal**: doctor reports drift with severity and fixes; `sync --relink`.

**Independent Test**: spec US5 scenarios 1 to 4 on a temporary home.

- [X] T051 [US5] Write failing tests in `internal/doctor/doctor_full_test.go` for each new check and severity, and for the exit status counting only problems
- [X] T052 [US5] Add `Severity` (`problem|info`) to `doctor.Finding` (`internal/doctor/doctor.go:27-31`), emit it in `doctorJSON`, and make `internal/cli/doctor.go:61` exit non-zero only for problems
- [X] T053 [US5] Implement the checks in `internal/doctor/doctor.go`: problems for link replaced by a real file, diverged managed hook, missing referenced set, missing hook script, deprecated `shared` alias, user catch-all rule; info for `content` settings differing from the default setup, `$HOME` escapes, unborrowed unclassified default entries (FR-020, FR-001a)
- [X] T054 [US5] Implement `sync --relink` with `--yes` in `internal/cli/sync.go` and `internal/sync/sync.go`: back up the real file, show the change, confirm, relink (FR-021)

---

## Phase 8: User Story 6 - Existing profiles keep working (Priority: P3)

**Goal**: slice one profiles upgrade without surprises.

**Independent Test**: spec US6 scenarios 1 and 2; SC-009.

- [X] T055 [US6] Write and pass `tests/integration/upgrade_test.go`: a profile file without `share` plus a config dir laid out as slice one's catch-all produced it (links for `settings.json`, `CLAUDE.md`, `history.jsonl`, `RTK.md`); after `sync`, settings and content links are unchanged, `history.jsonl` is reported as leaking, `RTK.md` stays linked and is reported as unclassified, nothing is deleted, the profile file is unchanged, and `show` reports `share: all` with a hint about `--share content`
- [X] T056 [US6] Make the `sync --fix-links` message state that earlier data stays in the default setup (`internal/layer/prepare.go` `handleLeakingLink`, FR-034) and add the `show` hint for profiles without a stored level

---

## Phase 9: Polish & Cross-Cutting Concerns

- [X] T057 Write the README decision guide (use-case table first, level table, always-private sentence, per-harness differences, hook sets with the `APROFILE_NAME`/`APROFILE_SHARE` guard and per-profile plugins as alternatives, `harness classify` for user files) in `README.md`, and a test in `internal/cli/readme_test.go` that the README tables match `levels.go` (FR-012, FR-032, SC-003)
- [X] T058 Run the manual checks from research.md R7 (`@` imports in a linked `CLAUDE.md`), R8 (`/ide` from a profile) and R11 (`edit --share` refuses while running on macOS and Linux) and record results in `specs/002-profile-isolation-levels/research.md`
- [X] T059 Run quickstart.md sections 1 to 7 against a throwaway home and fix any deviation
- [X] T060 Run `make check` (gofmt, vet, full test suite) and fix findings
- [X] T061 [P] Add performance checks: `BenchmarkWrapperStartup` in `internal/wrapper/render_bench_test.go` (wrapper overhead over a no-op stub binary, assert at most 50 ms median over 20 runs) and `TestSyncTwentyProfilesUnderOneSecond` in `tests/integration/perf_test.go` (20 profiles across all levels on fixture defaults, `sync` wall time under 1 s; skipped with `-short`)
- [X] T062 Prepare the SC-004 walkthrough script in `specs/002-profile-isolation-levels/walkthrough.md` (the seven use cases, `aprofile help sharing` as the only reference, a results table) for the maintainer to run with at least three people who have not read the README; record results there (manual, not part of the test suite)

---

## Dependencies & Execution Order

### Phase dependencies

- Setup (T001 to T004): no dependencies; all four in parallel.
- Foundational (T005 to T017): after Setup; blocks every story.
  - T005 before T006 to T008; T006 before T007 and T008.
  - T009, T010, T017 in parallel with T005 to T008.
  - T011 after T006; T012 after T006 and T009; T013 after T012; T014 after T013; T015 after T014;
    T016 after T015.
- US1 (T018 to T024): after Foundational. MVP part 1.
- US2 (T025 to T033): after Foundational; independent of US1 except that its tests assume the
  corrected built-ins (T019 to T022).
- US3 (T034 to T041): after US2 T026 (the `--share` flag).
- US4 (T042 to T050): after Foundational and US2 T026; independent of US3 except `reapply` reusing
  `confirm.go` (T003).
- US5 (T051 to T054): after US4 T046 (diverged-hook checks read the record).
- US6 (T055 to T056): after US1.
- Polish (T057 to T062): after the stories they document; T061 after US2; T062 after T028.

### Within each story

Tests first (they must fail), then data and helpers, then commands, then integration.

## Parallel Examples

```text
Setup:        T001, T002, T003, T004
Foundational: T005 | T009 | T010 | T017   (then the T006..T016 chain)
US1:          T019, T020, T021, T022       (four definition files)
US2:          T028, T029, T030, T031       (help, wrapper, registry, list)
US3:          T038, T040                   (settings merge library, harness show)
US4:          T043, T044, T045, T046       (library, two forms, record)
```

## Implementation Strategy

### MVP (US1 + US2)

1. Phases 1 and 2.
2. US1: the leak is closed for every profile, including slice one ones (they default to `all`).
3. US2: users can choose `content` or `none` and read what each means.
4. Stop and validate with quickstart.md sections 1 and 2, then ship.

### Incremental delivery

5. US3: `show`, dry runs, `settings pull`, `harness classify` (understandability complete).
6. US4: hook sets.
7. US5 and US6: doctor and upgrade polish.
8. Polish: README guide, manual checks, quickstart, `make check`.
