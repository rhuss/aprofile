# Tasks: Neutralize Provider Switches for Every Profile Folder

**Input**: Design documents from `specs/005-neutralize-provider-switches/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/cli.md, quickstart.md

**Tests**: Included. The constitution (Principle VII) and spec 004 practice require tests with
every behavior change; tests are written first in each story and must fail before the change.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1 to US4 from spec.md

## Phase 1: Setup

- [X] T001 Run `make test` on the branch base and record the baseline (all green) before any change; note the existing tests that assert login folders hold no entries (search `internal/launch/plan_test.go`, `internal/launch/apply_test.go`, `tests/integration/launch_*_test.go`, `tests/integration/login_test.go` for login/oauth profiles with empty `env` expectations)

## Phase 2: Foundational

- [X] T002 Expose the "folder can carry env entries" decision for the planner and status in `internal/launch/carrier.go`: a small helper (for example `canCarryEnv(p, def, doc) bool` returning `envCarrier(p, def, doc) == ""`) so both `PlanWith` and `Status` use the same linked-file rule (research R3); no behavior change yet

**Checkpoint**: `make test` still green.

## Phase 3: User Story 1 - Login profile folders neutralize exported switches (P1) MVP

**Goal**: every Claude profile with its own settings file carries empty entries for the whole auth-clearing list, whatever its auth kind (FR-001, FR-003, FR-004, FR-005a).

**Independent test**: an `oauth-login` profile at `content` and at `none`, after `sync`, holds an empty `env` entry for every name of `def.UnsetAuthVars()`, recorded as placed; a second plan is empty; deleting one entry makes the folder stale and the next plan restores it; the folder status stays `self-contained`.

- [X] T003 [P] [US1] Add failing planner tests in `internal/launch/plan_test.go`: `oauth-login` profile at `content` and at `none` (desired env = every `UnsetAuthVars()` name with value ""), second plan empty (idempotent), hand-deleted entry yields a restore step, user's own non-empty `CLAUDE_CODE_USE_VERTEX` is replaced and named (spec 004 FR-004), share level `all` (linked env file) yields no neutralizing entries and `unplaced` still reports a hand-added auth-clearing entry in the linked file, a profile changed from `all` to `content` gets the entries placed on the next plan, and a definition override that drops a name from the auth-clearing list removes that placed entry
- [X] T004 [P] [US1] Add a failing status test in `internal/launch/apply_test.go`: a login profile at `content` whose folder holds only neutralizing entries reports `Folder().Status == "self-contained"` and no wrapper-only items (FR-005a)
- [X] T005 [US1] Change `desiredState` in `internal/launch/plan.go`: take the `canCarryEnv` result from `PlanWith`; place the empty entries when `canCarryEnv` is true (it already implies `def.FolderLaunch != nil` with an `env` location; never dereference `def.FolderLaunch` without that, because `PlanWith` runs with a nil `def.FolderLaunch` when an override removed the locations but the record still holds entries), independent of a folder-carried key; move the `key == nil` return below the neutralizing block; keep the comment pointing at FR-006a and this spec's FR-001; add a planner test for a definition without `folder_launch` and a non-empty record (entries removed, no panic)
- [X] T006 [US1] Run `make test` after T005 and update every failure caused by the new entries, wherever it is (beyond T001's list, for example `internal/cli/sync_summary_test.go`, `sync_shadow_test.go`, `list_show_test.go`, `internal/doctor/doctor_share_test.go`, and integration tests `share_levels`, `sync_idempotent`, `level_change`, `upgrade`, `launch_report`), so they expect the neutralizing entries; never weaken assertions about wrapper output. Known behavior change: `TestPlanErrors` expects a note for an `oauth-login` profile with an unparseable `settings.json`; that profile now wants entries and gets an error (spec edge case "settings file does not parse"), so point the note case at a profile that wants nothing in the file (for example a definition override with an empty auth-clearing list) and add the error case for the login profile
- [ ] T007 [US1] Add an integration test in `tests/integration/launch_claude_test.go`: `aprofile add claude <name> --auth-kind oauth-login --share content`, `aprofile sync`, read the folder's `settings.json` and assert empty entries for `CLAUDE_CODE_USE_VERTEX`, `ANTHROPIC_API_KEY` and the rest of the list; second `sync` reports no changes; `sync --dry-run` before the first `sync` lists the additions without values

**Checkpoint**: US1 tests green; `make test` green.

## Phase 4: User Story 2 - Wrapper-supplied names are never blanked (P1)

**Goal**: names the profile supplies through the wrapper keep working (FR-002).

**Independent test**: `oauth-token` profile folder has no entry for `CLAUDE_CODE_OAUTH_TOKEN`; an `api-key` profile with a wrapper-only key (reason `cleared-source` or `auth-kind`) has no entry for `ANTHROPIC_API_KEY`; an extra variable `CLAUDE_CODE_USE_VERTEX=1` is placed with its value, not blanked; a folder-carried `api-key` key gives exactly today's entries; wrapper rendering is byte-identical to before.

- [ ] T008 [P] [US2] Add failing planner tests in `internal/launch/plan_test.go`: `oauth-token` (all names except `CLAUDE_CODE_OAUTH_TOKEN`), `api-key` with a key referenced through a cleared variable (`env:ANTHROPIC_API_KEY`, reason `cleared-source`: no empty `ANTHROPIC_API_KEY`), login profile with extra `CLAUDE_CODE_USE_VERTEX=1` (placed with value "1", not ""), folder-carried `api-key` key (entries unchanged from `TestPlanClaudeDesiredEntries`), auth kind change from folder-carried `api-key` to `oauth-token` (next plan removes the empty `CLAUDE_CODE_OAUTH_TOKEN` entry)
- [ ] T009 [US2] Extend the supplied-name set in `desiredState` (`internal/launch/plan.go`) with the variable of every `KindKey` setting whose `Carrier == CarrierWrapper` (data-model "Excluded from the empty entries")
- [ ] T010 [P] [US2] Add a regression test in `internal/wrapper/render_test.go` (or the closest existing wrapper test) asserting that wrapper output for an `oauth-token` and an `oauth-login` profile is unchanged by this feature (still sets the token, still clears the auth-clearing list)

**Checkpoint**: US1 and US2 tests green.

## Phase 5: User Story 3 - Login profiles at `all` report the gap (P2)

**Goal**: `show` and `doctor` report `auth-clearing` as wrapper-only for interactive-login Claude profiles at `all` (FR-005, FR-006, FR-007; contracts/cli.md).

**Independent test**: `oauth-login` at `all`: `show --json` has `folder.status == "not-self-contained"` and a wrapper-only item `{"setting":"auth-clearing","variable":"","reason":"share-all"}`; text shows `wrapper-only: auth-clearing (share level all: settings.json is linked)`; `doctor` has an info finding with the fix naming the wrapper and `aprofile edit <id> --share content`, exit status unaffected. `api-key` and `oauth-token` at `all`, and Codex `chatgpt-login` at any level: no `auth-clearing` item.

- [ ] T011 [P] [US3] Add failing tests in `internal/launch/apply_test.go` for the report item: present for `oauth-login` at `all`; absent for `api-key` and `oauth-token` at `all`, for `oauth-login` at `content`, and for a Codex `chatgpt-login` profile; status `not-self-contained` when present
- [ ] T012 [P] [US3] Add failing tests in `internal/doctor/doctor_share_test.go`: info finding `auth-clearing is wrapper-only: share level all: settings.json is linked` with fix `start the profile through its wrapper <wrapper>, or give it its own settings: aprofile edit <id> --share content`; severity `info`; no such finding for an `api-key` profile at `all`
- [ ] T013 [US3] Implement the report item: in `internal/launch/apply.go` `Status`, set a new `StatusReport` field (for example `AuthClearing Reason`) to `ReasonShareAll` when `def.FolderLaunch != nil && def.FolderLaunch.Env != ""`, the env file is linked (helper from T002 is false), the profile's auth kind is defined by the definition (`def.AuthKind(p.AuthKind)` found; an unknown kind gets no item) and its `VarNames()` is empty and `def.UnsetAuthVars()` is not empty; in `internal/launch/report.go` `Folder()`, append `WrapperOnly{Setting: "auth-clearing", Reason: r.AuthClearing}` when set; add a `KindAuthClearing = "auth-clearing"` constant; `SettingName()` returns it unchanged; never touch `DecideWith`
- [ ] T014 [US3] In `internal/doctor/doctor.go` `launchFindings`, use the special fix text for the `auth-clearing` item (T012), keeping severity info
- [ ] T015 [US3] Add an integration test in `tests/integration/launch_report_test.go`: `oauth-login` profile at `all` → `show` text and `--json` per contracts/cli.md, `doctor --json` info finding, `sync` writes nothing into the linked `settings.json`

**Checkpoint**: US1 to US3 green.

## Phase 6: User Story 4 - Codex verified, unchanged (P3)

**Goal**: record research R2 (FR-008); FR-009's "unchanged" branch applies.

- [ ] T016 [P] [US4] Add a comment above `launch_clear` (or the auth kinds) in `internal/harness/builtin/codex.yaml`: verified with Codex 0.167.0 on 2026-11-10 that a login profile launched from `CODEX_HOME` alone ignores exported `OPENAI_API_KEY` and `OPENAI_BASE_URL` (specs/005-neutralize-provider-switches/research.md R2), so the folder needs no neutralization; check that definition-loading tests still pass

## Phase 7: Polish & Cross-Cutting

- [ ] T017 [P] Add amendment notes to `specs/004-corgi-integration/spec.md` at FR-001, FR-006a and FR-020 pointing to spec 005 (neutralization for every own settings file; `auth-clearing` as the one wrapper-only item that is not a launch setting)
- [ ] T018 [P] Update `docs/guide/docs/user/corgi.md`: what the folder neutralizes for every profile with its own settings, and that a login profile at share level `all` inherits exported provider switches when started from the folder alone (fix: wrapper or `--share content`); add a short note to `README.md` only if it describes folder launches
- [ ] T019 Run `make test` and `go vet ./...`; run quickstart.md steps 2 and 3 in a throwaway `HOME`; confirm no value appears in any output (FR-007); list quickstart step 4 (real-login folder launch, FR-004a) as a manual check before merge in the RUNBOOK

## Dependencies & Execution Order

- T001 → T002 → US1 (T003, T004 in parallel → T005 → T006 → T007)
- US2 depends on T005 (same function): T008 → T009; T010 independent
- US3 depends on T002 only: T011, T012 in parallel → T013 → T014 → T015
- US4 (T016) independent
- Polish after all stories; T017 and T018 in parallel

## Parallel Examples

- US1: T003 (`plan_test.go`) with T004 (`apply_test.go`)
- US3: T011 (`apply_test.go`) with T012 (`doctor_share_test.go`), once US1's `apply_test.go` edits are in
- Any time after T002: T016, T017, T018

## Implementation Strategy

MVP is US1 + US2 together (both P1: widening without the supplied-name exclusion would break wrapper launches of `oauth-token` profiles). Then US3 reporting, US4 comment, docs.
