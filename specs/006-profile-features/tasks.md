---
description: "Task list for profile features (curated harness capability toggles)"
---

# Tasks: Profile Features (Curated Harness Capability Toggles)

**Input**: Design documents from `/specs/006-profile-features/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/features-cli.md, quickstart.md

**Tests**: Included. Constitution Principle VII mandates test-first development: each user story's integration tests (T009, T015, T018, T021, T026, T030) are written first and failing before that story's implementation. Phase 2 foundational tasks and story unit tests pair each implementation task with its named test task (T004/T005, T002/T006, T007/T008, T012/T014, T019/T020, T024/T025); write the test in the same commit as the code it pins, test written first.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Single Go module at repository root: `internal/` packages, `tests/integration/` end-to-end.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project-level changes that precede all feature work

- [ ] T001 Amend the constitution (`.specify/memory/constitution.md`): extend Principle II's launch-entry parenthetical to name feature-placed entries ("the launch variables, key command, provider `auth` block and feature-placed entries aprofile placed"), version bump 2.0.0 → 2.0.1 (PATCH, spec FR-018, plan Complexity Tracking), following the file's Sync Impact Report format.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The catalog, the capability-mapping schema and the profile-record extension every user story builds on

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [ ] T002 [P] Create the cross-harness feature catalog in `internal/feature/catalog.go`: `Feature{ID, Group, Params}`, `Param{Name, Type, Values}` with types `bool`, `string`, `enum`; built-in v1 entries telemetry (group telemetry; params traces:bool, logs:bool, endpoint:string, protocol:enum) and automode-log (group logging; no params); lookup helpers by id and group.
- [ ] T003 [P] Add `FeatureState{Feature string, Params map[string]string}` to the profile record in `internal/profile/profile.go` (YAML key `features:`, stored sorted by feature id, beside `ExtraEnv`); unknown feature ids at load add a `LoadWarning` and place nothing (data-model "Feature state"); carry it through `Load`/`Save`/`Move` and the copy path's record handling. Unit tests in `internal/profile/profile_test.go`: features round-trip sorted by feature id through `Save`/`Load`; unknown feature id and out-of-enum stored param each add a `LoadWarning`.
- [ ] T004 Add the capability-mapping schema to `internal/harness/definition.go`: per feature `params` (each with optional `default` and `values` enum) and `place` rules (`file`, `location`, `value` literal or `{param: <name>}`, optional `when: <bool param>`), loader validation in `internal/harness/loader.go` (feature ids must exist in the catalog, param names must match, default ∈ values, `file` must be a classified settings path), and merge semantics for `harnesses.d` overrides in `internal/harness/override.go` (same section-replace rule as other definition keys).
- [ ] T005 Table-driven loader tests for T004 in `internal/harness/features_mapping_test.go`: valid mapping loads; unknown feature id, unknown param name, default outside values, and unclassified `file` each fail with an error naming the problem; a `harnesses.d` override replaces a feature mapping.
- [ ] T006 Catalog tests in `internal/feature/catalog_test.go`: lookups by id and group; the v1 catalog contains exactly telemetry and automode-log with the spec's params (FR-003).
- [ ] T007 Implement feature-state-to-desired-entries derivation in `internal/launch/plan.go`: `featureDesired(def *harness.Definition, p *profile.Profile) ([]Entry, error)` resolves a profile's `Features` against its harness capability mapping into concrete desired entries (file, location, value) honoring `when` rules and per-harness defaults (research R3); a signal that is off places its exporter entry as `none` (FR-011, R5). Also expose `FeatureEnvNames(def *harness.Definition, feature string) []string`, the single record-independent env-name surface derived from the mapping's `place` rules (R6), which mutation-time collision checks (T031) and drift reporting (T032) call.
- [ ] T008 Derivation tests in `internal/launch/plan_test.go`: telemetry with both signals on, off signals placing `none`, defaults applied per harness, `when`-gated rules skipped, param references resolved, automode-log's literal entry.

**Checkpoint**: Foundation ready; user story implementation can begin in parallel

---

## Phase 3: User Story 1 - Turn telemetry on for one profile (Priority: P1) 🎯 MVP

**Goal**: `aprofile features claude/work --enable telemetry ...` + `sync` places and removes Claude Code's OTel env entries

**Independent Test**: Enable telemetry on a `content`-level Claude profile, `sync`, inspect `settings.json` for the six mapped entries as placed entries; disable, re-sync, verify exact removal (quickstart Scenario 1)

### Tests for User Story 1 (test-first)

- [X] T009 [P] [US1] Integration tests in `tests/integration/features_test.go` for quickstart Scenario 1: enable → sync places `CLAUDE_CODE_ENABLE_TELEMETRY`, `OTEL_TRACES_EXPORTER`/`OTEL_LOGS_EXPORTER` (otlp for on, none for off), `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` (traces on) as recorded placed entries; disable → sync removes exactly those entries (SC-002); second sync is a no-op.

### Implementation for User Story 1

- [X] T010 [US1] Add `features.telemetry` to `internal/harness/builtin/claude.yaml` with the placement rules from data-model.md (master switch, both exporter signals with `when`, endpoint and protocol param references, beta flag `when: traces`), protocol default `http/protobuf`, enum values `grpc`, `http/json`, `http/protobuf`.
- [X] T011 [US1] Extend `internal/launch/plan.go`/`apply.go`: place feature-derived env entries through the existing `planJSON` env path (record entry `env.<NAME>`, `Replaced` for hand-written values, re-read before write, idempotent) and remove entries whose feature was disabled, restoring `Replaced` values.
- [X] T012 [US1] Create the `features` command in `internal/cli/features.go`: bare status grouped (telemetry, logging) with state, params and effective defaults; mutation flags `--enable`/`--disable`, `--traces`, `--logs`, `--endpoint`, `--protocol` (per-parameter flags imply enable, contract FR-002); validation errors for unsupported flags (FR-007), neither-signal-on, missing endpoint, bad enum values (FR-004); `--dry-run`; sync reminder matching `edit`'s contract; `--json` per `contracts/features-cli.md`.
- [X] T013 [US1] Add the features section to `show` in `internal/cli/list_show.go`: one line per enabled feature with params, drift warning between recorded state and placed entries (FR-016), `features` array in `show --json`.
- [X] T014 [US1] Unit tests in `internal/cli/features_test.go`: status output grouped and with defaults; mutation stores state and prints the reminder; each validation error message names the harness/flag/param (FR-007, FR-004).

**Checkpoint**: User Story 1 fully functional and independently testable

---

## Phase 4: User Story 2 - Enable the Auto-mode decision log (Priority: P1)

**Goal**: `--enable automode-log` places and removes `AUTOMODE_DECISION_LOG=1`

**Independent Test**: Quickstart Scenario 2: enable, sync, entry present as placed entry; disable, sync, gone

### Tests for User Story 2 (test-first)

- [ ] T015 [P] [US2] Integration tests in `tests/integration/features_test.go` for quickstart Scenario 2 (enable/disable lifecycle of the automode-log entry).

### Implementation for User Story 2

- [ ] T016 [US2] Add `features.automode-log` to `internal/harness/builtin/claude.yaml`: single literal placement rule `env.AUTOMODE_DECISION_LOG=1`, no params; no entry in `internal/harness/builtin/codex.yaml` (FR-003, User Story 3's gating depends on the asymmetry).
- [ ] T017 [US2] Status and mutation for a parameterless feature in `internal/cli/features.go` (covered by T012's machinery): bare status line, `--enable`/`--disable` without param flags.

**Checkpoint**: User Stories 1 and 2 both work independently

---

## Phase 5: User Story 3 - Discover what a harness supports (Priority: P2)

**Goal**: Bare status as the catalog's discovery surface; harness gating errors; per-harness defaults visible

**Independent Test**: Quickstart Scenario 3: codex status lists telemetry (default `otlp-http json`) and not automode-log; `--enable automode-log` on codex fails naming harness and flag

### Tests for User Story 3 (test-first)

- [ ] T018 [P] [US3] Integration tests in `tests/integration/features_test.go` for quickstart Scenario 3 (per-harness status asymmetry, default protocol display, unsupported-flag hard error changing nothing).

### Implementation for User Story 3

- [ ] T019 [US3] Status rendering details in `internal/cli/features.go`: only harness-supported features listed, per-harness default values shown for enabled features with unset params (contract "Status output"), `--json` groups/features schema exactly per `contracts/features-cli.md` (stable, public interface).
- [ ] T020 [US3] Cross-harness comparison test in `internal/cli/features_test.go`: claude vs codex status fixtures (feature sets, defaults) match the definitions.

**Checkpoint**: Stories 1-3 independently functional

---

## Phase 6: User Story 4 - Codex telemetry through the same command (Priority: P2)

**Goal**: Same command writes `[otel]` into Codex's `config.toml`; hand-written keys survive

**Independent Test**: Quickstart Scenario 4: enable, sync, `[otel]` exporter keys in the inline-table encoding; hand-write `environment = "prod"`, disable, sync, only aprofile keys removed

### Tests for User Story 4 (test-first)

- [ ] T021 [P] [US4] Integration tests in `tests/integration/features_test.go` for quickstart Scenario 4 (inline-table encoding per research R1, hand-written key preservation, removal of placed keys only).

### Implementation for User Story 4

- [ ] T022 [US4] Verify the Codex `[otel]` exporter encoding against a real codex release (research R1's live confirmation): write a profile `config.toml` with `exporter = { otlp-http = { endpoint = "...", protocol = "json" } }` and confirm the installed codex accepts it (or confirm via the upstream serde shape in a source-level round-trip); record the result in `specs/006-profile-features/research.md` R1.
- [ ] T023 [US4] Add `features.telemetry` to `internal/harness/builtin/codex.yaml`: `otel.exporter` and `otel.trace_exporter` placement rules (inline-table values, `none` for off signals), protocol default `otlp-http json`, enum values `otlp-http json`, `otlp-http binary`, `otlp-grpc`.
- [ ] T024 [US4] Extend `internal/launch/plan.go`'s `planTOML` for generic nested key placement: dotted-path locations (`otel.exporter`) with `Replaced` subtree capture and restore, reusing the record's existing `Entry`; placement replaces a hand-written non-empty value and sync reports the key path, never the value (FR-012, spec 005's replaced-user semantics).
- [ ] T025 [US4] TOML placement tests in `internal/launch/plan_test.go`: place, replace user value with `Replaced` restore on removal, preserve sibling hand-written `[otel]` keys, idempotency.

**Checkpoint**: Stories 1-4 independently functional

---

## Phase 7: User Story 5 - Features respect isolation and copy (Priority: P2)

**Goal**: Refusal at share level `all`, clearing on `edit --share all`, feature state carried by `copy`

**Independent Test**: Quickstart Scenario 5: mutation refused at `all` with hint, bare status still works, copy carries state, `edit --share all` warns and clears

### Tests for User Story 5 (test-first)

- [ ] T026 [P] [US5] Integration tests in `tests/integration/features_test.go` for quickstart Scenario 5 (refusal, bare status at `all`, copy carry, edit clearing with warning, cross-harness copy dropping the unsupported feature with a report line).

### Implementation for User Story 5

- [ ] T027 [US5] Share-level guard in `internal/cli/features.go`: mutation refused at `all` with the `--share content` hint, bare status allowed (FR-013).
- [ ] T028 [US5] Extend `edit --share all` in `internal/cli/edit.go` (`launchLevelEffects` path, research R7): clear `Features` from the record, print a warning naming each cleared feature, report in the edit output (FR-014); sync removes the orphaned placed entries through normal record maintenance.
- [ ] T029 [US5] Carry feature state in `internal/cli/copy.go`: same-harness copy carries all features; cross-harness copy drops features the target harness does not support and names them in the copy report (hook-set precedent, FR-009).

**Checkpoint**: Stories 1-5 independently functional

---

## Phase 8: User Story 6 - Conflicts with hand-set variables are caught (Priority: P3)

**Goal**: Hard errors at mutation time in both directions; sync problems and show warnings for drift

**Independent Test**: Quickstart Scenario 6: both collision directions fail naming the variable; hand-edited placed entry surfaces as sync problem and show warning

### Tests for User Story 6 (test-first)

- [ ] T030 [P] [US6] Integration tests in `tests/integration/features_test.go` for quickstart Scenario 6 (both mutation-time directions, sync problem, show warning for an externally changed placed entry).

### Implementation for User Story 6

- [ ] T031 [US6] Collision checks on both mutation paths (R6) using `FeatureEnvNames` from T007 (`internal/launch/plan.go`): `internal/cli/features.go` checks it against the profile's extra env names on mutation, `internal/cli/edit.go`'s `--env`/`--env-literal` path checks it against enabled features (FR-015, both directions, error names the variable).
- [ ] T032 [US6] Drift reporting: `internal/launch/carrier.go` sync problem and `internal/cli/list_show.go` warning when a placed feature entry differs from the derived want or collides with an extra env name (FR-015, FR-016).

**Checkpoint**: All user stories independently functional

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Consistency across the tool and final verification

- [ ] T033 [P] Update `README.md` and `internal/cli/help_sharing.go`: the features command in the command overview, one sentence on the capability layer and the raw `--env` escape hatch, the share-level `all` refusal in the sharing guide's `all` row.
- [ ] T034 Update `internal/cli/output.go` and any shared output helpers for the features section's plain/`--json`/NO_COLOR rendering (Principle VI).
- [ ] T035 Run the full check suite: `gofmt`, `go vet ./...`, `go test ./...` green (constitution Development Workflow).
- [ ] T036 Run `quickstart.md` scenarios 1-6 by hand against a scratch `HOME` and record the pass in `specs/006-profile-features/quickstart.md` (checkboxes or a dated note), including timing the bare `features` status call (SC-003: under a second).
- [ ] T037 Cross-check FR coverage: every FR-001..FR-018 in `specs/006-profile-features/spec.md` maps to at least one passing test or doc task; record gaps as deviations, not silent omissions.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies, can start immediately
- **Foundational (Phase 2)**: Depends on Phase 1; BLOCKS all user stories
- **User Stories (Phases 3-8)**: Depend on Phase 2. US1 before US2 (status/mutation machinery); US3-US8 can proceed after US1 in parallel or in order
- **Polish (Phase 9)**: Depends on all completed user stories

### User Story Dependencies

- **US1 (P1)**: After Foundational; no story dependencies (MVP)
- **US2 (P1)**: After US1's command machinery (T012), shares the placement path
- **US3 (P2)**: After US1; tests the asymmetry created by T016
- **US4 (P2)**: After US1's derivation (T007); independent of US2/US3
- **US5 (P2)**: After US1/US2 (needs features to refuse/clear/carry)
- **US6 (P3)**: After US1/US2; touches edit's env path independently of US4/US5

### Within Each User Story

- Tests written first and failing before implementation (Principle VII)
- Definition/record changes before launch placement before CLI surface
- Story complete and verified before moving on

### Parallel Opportunities

- T002, T003 in Phase 2 (different packages)
- T005, T006 in Phase 2 (different test files)
- US3, US4, US5 phases can run in parallel after US1+US2 (different files: features.go status paths, codex.yaml + planTOML, edit.go/copy.go)
- T033, T034 in Phase 9 (different files)

---

## Parallel Example: User Story 3 and User Story 4

```text
After US1+US2 complete, launch together:
Task T019: "Status rendering details in internal/cli/features.go" (US3)
Task T023: "Add features.telemetry to internal/harness/builtin/codex.yaml" (US4)
# Different files, no shared incomplete dependencies
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 (constitution amendment) and Phase 2 (catalog, mapping schema, record, derivation)
2. Complete Phase 3 (US1: Claude telemetry end to end)
3. **STOP and VALIDATE**: quickstart Scenario 1 passes
4. The MVP already delivers the primary value (telemetry on one profile)

### Incremental Delivery

1. Foundation → US1 (MVP) → validate
2. US2 (automode-log) → validate
3. US3 (discovery) → US4 (Codex) → US5 (isolation/copy) → validate each
4. US6 (collisions) → Phase 9 polish → full gate run

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks
- [Story] label maps each task to its user story for traceability
- The branch already merges `005-neutralize-provider-switches` (commit 1e2cf35); do not re-merge
- Commit after each task or logical group; `Assisted-By: 🤖 Claude Code` tagline on every commit
- Stop at any checkpoint to validate a story independently
- Avoid: vague tasks, same-file conflicts, cross-story dependencies that break independence
