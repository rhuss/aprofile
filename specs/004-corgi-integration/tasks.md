# Tasks: Self-Contained Profile Folders

**Input**: Design documents from `specs/004-corgi-integration/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required. Constitution Principle VII mandates test-first development: each test task
precedes its implementation and must fail before the implementation lands. All tests use
`testutil.HermeticHome`; none touch the real home.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: User story label (US1 to US5) for story phases only

---

## Phase 1: Setup

- [X] T001 Amend `.specify/memory/constitution.md` to version 2.0.0 per spec FR-024: Principle I lists as definition data the folder locations of launch settings, how the wrapper passes a setting as a launch argument, the per-auth-kind `folder_key` flag and the `launch_clear` names; Principle II allows aprofile-placed launch entries in a harness's settings files (recorded, rest preserved, re-read before write); Principle III states launch entries are owned only where the folder carries them and a settings file linked at `all` carries none, so it stays linkable; Principle V replaces the caller-override bullet with the spec's quoted single-source rule. Add a Sync Impact Report entry in the file's existing format (MAJOR, reason, modified principles, follow-ups) and update the version line.
- [X] T002 [P] Add superseding notes (one line each, pointing to spec 004 FR numbers) in `specs/001-aprofile-standalone-profile-cli/spec.md` (FR-006: model for harnesses with `model_arg`, FR-026 and FR-027: FR-012 to FR-016, FR-030: FR-018), `specs/001-aprofile-standalone-profile-cli/contracts/registry-and-env.md` (`model` field note in the existing "Addition" style; `APROFILE_SET_VARS` row and wrapper paragraph per `specs/004-corgi-integration/contracts/registry-and-env.md`), `specs/001-aprofile-standalone-profile-cli/data-model.md`, and `specs/002-profile-isolation-levels/spec.md` (FR-009a, FR-010, FR-017, FR-018).

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: definition data, the `internal/launch` package (carrier decision, key commands, planner, record, writer) and the carrier-aware wrapper. Every story builds on these.

### Definition data

- [X] T003 Write failing tests in `internal/harness/launch_fields_test.go`: built-in Claude and Codex definitions load the values in `specs/004-corgi-integration/contracts/harness-definition.md`; `harnesses.d` overrides merge (`folder_launch` by key, `launch_clear` and `model_arg` replace, `null` removes); validation errors name file and field for: location not `file#key` or file not classified, unknown `form`, `provider-auth` without `selector`, invalid or `APROFILE_*` or config-folder name in `launch_clear`, `model_arg` with the placeholder missing or repeated, `folder_key: true` without `folder_launch.key_command`; `auth_clears` may now name the model and endpoint variables; `CarriesModel()` is true for `model_var` or `model_arg`.
- [X] T004 Add to `internal/harness/definition.go`: `FolderLaunch{Env string; KeyCommand *KeyCommand}`, `KeyCommand{Location, Form ("shell"|"provider-auth"), Selector string; Replaces []string}` with `File()`/`Key()` helpers like `HookLocation`, `LaunchClear []string`, `ModelArg []string`, `AuthKind.FolderKey bool`, and `CarriesModel()`, `FolderKeyFor(kind) bool`, `ExpandModelArg(model) []string` (`{model:toml}` → TOML basic string with `\` and `"` escaped; `{model}` → raw).
- [X] T005 Extend `internal/harness/loader.go`: validate the new fields per T003, add them to `allFieldNames`, and drop the `auth_clears` rule that rejected the model and endpoint variables as caller-overridable (keep the other rules).
- [X] T006 Set built-in data in `internal/harness/builtin/claude.yaml` (`folder_launch.env: settings.json#env`, `key_command {location: settings.json#apiKeyHelper, form: shell}`, `launch_clear` with `ANTHROPIC_MODEL`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU,FABLE}_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL`, `folder_key: true` on `api-key` and `auth-token`; update the `settings.json` classification comment per Principle III as amended) and `internal/harness/builtin/codex.yaml` (`key_command {location: config.toml#model_providers, form: provider-auth, selector: model_provider, replaces: [env_key]}`, `model_arg: ["-c", "model={model:toml}"]`, `launch_clear: [OPENAI_BASE_URL]`, `folder_key: true` on `api-key`; update the model comment). Cite research.md R1 to R4 in comments.
- [X] T007 [P] Print `folder_launch`, `launch_clear`, `model_arg` and per-kind `folder_key` with origins in `internal/cli/harness.go` (`harness show`), with a test in `internal/cli/harness_show_test.go`.

### Launch package

- [X] T008 [P] Add `ManagedLaunchPath(harness, name)` = `<data>/managed-launch/<harness>/<name>.json` to `internal/config/config.go` next to `ManagedHooksPath`. Move `ShellQuote` from `internal/wrapper/wrapper.go` to `internal/secret` (keep `wrapper.ShellQuote` as an alias) so `launch` never imports `wrapper`; export the JSON helpers `launch` needs from `internal/layer/mixed.go` (`decodeJSONObject` → `DecodeJSONObject`).
- [X] T009 [P] Write failing tests in `internal/launch/record_test.go`: missing file is an empty record; save writes only on change, deletes when empty, writes atomically; round-trip of entries `{file, location, replaced}` per `data-model.md`; no value of an `env` entry is ever stored.
- [X] T010 Implement `internal/launch/record.go` (`Record{FormatVersion int; Entries []Entry}`, `Entry{File, Location string; Replaced map[string]string}`, `Load`, `Save`, `Move`, `Delete`), modeled on `internal/hooksets/record.go`.
- [X] T011 [P] Write failing table-driven tests in `internal/launch/carrier_test.go` for the carrier decision (data-model.md "Launch setting"): every kind (`model`, `endpoint`, `extra:<NAME>`, `key`) × reasons `no-support`, `reference-value`, `auth-kind`, `share-all`, `no-provider`, `harness-rewrites`; `env:` key is folder-carried with `env_dependent`; login auth kinds have no key setting; OpenCode/pi definitions keep everything in the wrapper; a Codex API-key profile at share level `all` still has a folder-carried key (`config.toml` is owned at every level); an `env:` key whose variable is in `UnsetAuthVars()` or `launch_clear` (`env:ANTHROPIC_AUTH_TOKEN`, Codex `env:OPENAI_API_KEY`) is wrapper-only with reason `cleared-source`; exec-hook `env` variables are wrapper-only with reason `hook-env`; an absent `config.toml` gives `no-provider`.
- [X] T012 Implement `internal/launch/doc.go` and `internal/launch/carrier.go` per the plan's Interfaces block: `Doc`, `FileDoc`, `ReadDoc(def, configDir)` (reads each `folder_launch` file once, records `Exists`, `Linked`, parse `Err`), and `Decide(p *profile.Profile, def *harness.Definition, doc Doc) []Setting` with `Setting{Kind, Variable, Value string; Carrier; Reason; EnvDependent bool; Ref *secret.Reference}`; `Decide` is pure.
- [X] T013 [P] Write failing tests in `internal/launch/keycmd_test.go`: the `shell` command run with `/bin/sh -c` prints exactly what the wrapper's `IFS= read -r` resolution yields for key files containing backslashes, leading and trailing spaces, a CRLF line ending, no final newline, and a path containing a single quote and spaces; the `env:` form prints the variable; no profile value can inject shell code (path with `$(…)`, backticks, `;`); the `provider-auth` table is `command = "sh"`, `args` per `contracts/harness-definition.md`, with the path as a separate argument; invalid variable names are rejected.
- [X] T014 Implement `internal/launch/keycmd.go` (`ShellCommand(ref) (string, error)`, `ProviderAuth(ref) (map[string]any, error)`) using `secret.ShellQuote` (moved in T008) and `secret.ValidEnvVarName`.
- [X] T015 Write failing tests in `internal/launch/plan_test.go` for `Plan(...)` over JSON and TOML fixtures: desired entries (folder-carried variables, `APROFILE_SET_VARS` in fixed order: model, endpoint, extra variables in profile order, exec-hook `env` variables, only when a variable is folder-carried; an absent, unlinked `settings.json` is created holding only placed entries; an `env` key set to `null` is treated as an empty object; a provider block holding `experimental_bearer_token` keeps it and never records or reports its value; a parse error in a file when every setting is wrapper-only yields a note, not an error; empty neutralizing entries for every `UnsetAuthVars()` name when the key is folder-carried and `folder_launch.env` is set, except names the profile supplies as extra variables, so a literal `CLAUDE_CODE_USE_VERTEX=1` keeps `"1"` and a wrapper-only `CLOUD_ML_REGION=env:CLOUD_ML_REGION` gets no blank entry; key command); first placement over a user entry → `replace-user` step naming the entry; user-edited placed entry → restore; entry no longer desired → remove; `auth` removal restores recorded `env_key`; selected-provider switch moves `auth` and restores the old block; Claude at share `all` (settings file is a link) → no steps and record dropped, while Codex at `all` still places its `auth` block; unplaced detection (launch_clear names, auth-clearing names, profile extra names, `apiKeyHelper` not in record); parse error, `env` not an object, selected provider not a table → error naming file and key; plan for an up-to-date file is empty (stale = non-empty plan).
- [X] T016 Implement `internal/launch/plan.go`: `Plan(p *profile.Profile, def *harness.Definition, doc Doc, rec Record) (Result, error)` (pure, takes the `Doc` from `ReadDoc`) with `Result{Settings []Setting; Steps []Step; Unplaced []Entry; NewRecord Record}`; `Step{Kind ("set"|"remove"|"replace-user"|"restore"), File, Location, Note string}` mapped to sync actions per the plan's Interfaces block; reuse `layer.DecodeJSONObject`, `layer.EncodeJSON` and go-toml v2; never write into a linked file.
- [X] T017 Write failing tests in `internal/launch/apply_test.go`: `Apply` re-reads the file, re-plans against the fresh content and writes atomically keeping the file mode; JSON untouched values stay byte for byte (key order and indentation normalized); TOML re-encoded; a file the harness changed between plan and apply (simulated) keeps the harness's change; never writes through a link or into the default setup; a second `Apply` is a no-op (idempotent); dry run writes nothing and reports `toml-reencode` when a TOML file would change.
- [X] T018 Implement `internal/launch/apply.go`: `Apply(p *profile.Profile, def *harness.Definition, configDir, recPath string, dryRun bool) (Result, error)` writing the files and the record (re-read with `ReadDoc` and re-plan immediately before each write); and `Status(p, def, configDir, recPath) (StatusReport, error)` with `StatusReport{Settings, Sync ("synced"|"stale"|"unknown"), Unplaced, Err}` for `list`, `show` and `doctor`.

### Carrier-aware wrapper

- [X] T019 Write failing tests in `internal/wrapper/render_test.go` and `internal/wrapper/environ_test.go` (rewrite the now-wrong expectations in `render_test.go` L104-152 and L199-206, `internal/cli/login_test.go` `TestLoginEnvCallerOverridesInSameProfile` L127-153 and `internal/cli/login_hook_env_test.go`): a self-referencing pass-through `--env CLOUD_ML_REGION=env:CLOUD_ML_REGION` still reaches the harness because `env:` sources are captured before the cleanup block; an `env:ANTHROPIC_AUTH_TOKEN` key stays wrapper-carried and exported from the captured source; exec-hook `env` variables are set unconditionally and listed in `APROFILE_SET_VARS` (rewrite `internal/cli/login_hook_env_test.go` accordingly); replace `TestRenderModelEndpointOverridable` with tests that wrapper-carried variables are set unconditionally (no `${VAR:-}`), folder-carried variables are only unset, the cleanup block runs without comparing profile names and clears `launch_clear`, inherited `APROFILE_SET_VARS` names (skipping invalid names, `PATH`, `HOME`, the config-folder variable and `APROFILE_*`) and the profile's own launch variables; a folder-carried key is resolved into a private shell variable, fails closed with the existing message, is unset and never exported, and the profile's own auth variable is cleared; `APROFILE_SET_VARS` is exported only when wrapper-carried; Codex wrapper ends with `exec 'codex' '-c' 'model="…"' "$@"` with TOML and shell escaping (model `1.5`, model with `"` and `\`); `TestEnvironMatchesRenderedWrapper` covers all of these.
- [X] T020 Rewrite the wrapper blocks in `internal/wrapper/wrapper.go` (a new first block capturing every `env:` reference source, `renderNestedCleanup` → unconditional clear, `renderAuth` with check-without-export, `renderOverridable` → unconditional `renderLaunch` driven by `launch.Decide`, `renderGuardAndExec` with `def.ExpandModelArg`) and mirror them in `internal/wrapper/env.go` (`Environ`, `overridableVars` → carrier-aware set), keeping `Expected()` the single source for sync and stale checks. Update the package doc and comments describing caller overrides (`wrapper.go` L1-6, `env.go` L14-18 and L41, `internal/cli/login.go` L121-124).

**Checkpoint**: definitions, planner, writer and wrapper ready; stories can proceed.

---

## Phase 3: User Story 1 - A Claude API-key profile folder launches on its own (Priority: P1) MVP

**Goal**: `sync` makes every eligible Claude profile folder carry endpoint, model, aliases, extra variables and key command; user content preserved; idempotent.

**Independent Test**: integration test syncs a Claude API-key profile and inspects `settings.json` and the key command's output.

- [X] T021 [US1] Write failing integration tests in `tests/integration/launch_claude_test.go`: after `sync`, `settings.json` `env` holds the profile's model, endpoint, literal extra variables, `APROFILE_SET_VARS` and empty neutralizing entries; `apiKeyHelper` output (run with `/bin/sh -c`) equals the key; the user's own `env` entries (for example `MLFLOW_TRACKING_URI`) and other keys are unchanged; the `model` key Claude Code writes is untouched; a pre-existing user `ANTHROPIC_BASE_URL` is replaced and reported by name; a second `sync` reports zero changes and writes no files (`snapshotMtimes`); editing a placed entry is restored by the next `sync`; removing the model from the profile removes `env.ANTHROPIC_MODEL` only; an `oauth-token` profile keeps its key wrapper-only; a profile at `all` gets no entries; a broken `settings.json` leaves file, wrapper and registry entry unchanged and `sync` exits non-zero.
- [X] T021a [US1] Add a table-driven test `TestSC001DailyProfileFixtures` in `tests/integration/launch_daily_test.go` over nine fixtures shaped like the API-key daily profiles (`claude/deepseek`, `claude/glm`, `claude/work` (endpoint, no model), `claude/work-deepseek`, `claude/work-glm` with the alias extra variables; `codex/deepseek`, `codex/glm`, `codex/work-deepseek`, `codex/work-glm` with a custom `enmaas`-style provider and `env_key`): after `sync`, each folder carries endpoint, model (Claude), aliases and a key command whose `/bin/sh` output equals the fixture key; Codex wrappers carry `-c model="…"` when a model is set; 9 of 9 pass. Real-harness launches against a stub endpoint are manual (walkthrough).
- [X] T022 [US1] Add a `syncLaunch` step in `internal/sync/sync.go` after `syncWriteConfig` (L353) and before `wrapper.Expected` (L361): call `launch.Apply`; on error record a problem and `continue` so `preserve()` keeps the wrapper and registry entry (FR-009a); add `LaunchChanges` to `Result` and `Result.Changed()`; add dry-run `StepPlan`s with actions `launch-set`, `launch-remove`, `launch-replace-user` and the `toml-reencode` note.
- [X] T023 [US1] Print launch steps in `internal/cli/sync.go` text output per `contracts/cli.md` (`<profile>: <file>: set env NAME`, `replaced user entry env NAME`, never values) and include them in `--json` (`syncJSON`).
- [X] T024 [P] [US1] Make `layer.differsFromDefault` in `internal/layer/treatment.go` (called at L241) ignore the profile's placed entries: add a `PlacedKeys map[string][]string` (file → `env.NAME`/`apiKeyHelper` locations) field to the treatment/status options, filled from `launch.Load` by the callers in `internal/sync/sync.go`, `internal/cli/edit.go` and `internal/cli/list_show.go` (`layer` must not import `launch`), with a test in `internal/layer/treatment_test.go` that a `content` profile with only placed entries does not read "differs from the default setup".
- [X] T025 [US1] In `internal/cli/settings.go` `runSettingsPull`, re-place launch entries via `launch.Apply` after applying the pull and list them with the pulled keys; extend `tests/integration/settings_pull_test.go` so launch entries are present after `settings pull` and `settings pull --replace`.
- [X] T026 [US1] In `internal/cli/edit.go`: `rename` moves the launch record and `remove --purge` deletes it (next to the hooks record handling, L603-607, L709, L793); `changeShare` to `all` drops the record and prints `launch settings become wrapper-only (settings.json is linked)`, from `all` prints `launch settings will be placed into settings.json by the next sync`, in both `--dry-run` and apply; extend `tests/integration/level_change_test.go` (`TestSC006DryRunMatchesApply` stays green).

**Checkpoint**: Claude folders are self-contained (MVP).

---

## Phase 4: User Story 2 - A Codex API-key profile folder launches on its own (Priority: P2)

**Goal**: Codex provider `auth` block by command, `env_key` replaced and restorable, model by launch argument, `--model` accepted for Codex.

**Independent Test**: integration test syncs a Codex API-key profile with a custom provider and inspects `config.toml`, the record and the wrapper's arguments through the stub.

- [X] T027 [US2] Install the stub harness also as `codex` in `tests/integration/harness_test.go` (`BuildBinary(..., "codex")`).
- [X] T028 [US2] Write failing integration tests in `tests/integration/launch_codex_test.go`: `sync` writes `[model_providers.<selected>.auth]` with `command = "sh"` and the reference path as an argument, removes `env_key` and records it; `sync --dry-run` lists the placement, the replacement and the comment-loss note; switching the profile to `chatgpt-login` removes `auth` and restores `env_key`; changing `model_provider` moves the block; a config with no custom provider leaves the key wrapper-only; running `codex-<name>` through the stub shows args `-c model="1.5"` before the caller's args and a caller `-m x` after them; no `OPENAI_API_KEY` exported when the folder carries the key.
- [X] T029 [US2] Replace every `def.ModelVar != ""` check with `def.CarriesModel()` in `internal/cli/add.go` (L357-359, flag help L47 per `contracts/cli.md`), `internal/cli/edit.go` (L224-231), `internal/importer/importer.go` (L503, L532) and the TUI `SupportsModel` (`internal/cli/add.go` L250, `internal/tui/forms.go`); in `internal/wrapper/fit.go` `IgnoredSettingIssues` (L68 passes `def.ModelVar` as the variable name) report a model as ignored only when `!CarriesModel()` and name the launch argument otherwise; `internal/cli/copy.go` `applyCrossHarnessSettings`/`droppedCrossSettings` drop the model unconditionally on cross-harness copies and stay unchanged (verify, no edit); reject control characters in a model for a `model_arg` harness in `internal/profile/profile.go` `Validate`; tests in the existing add/edit/copy tests for Codex `--model`.

**Checkpoint**: Codex folders carry the key; Codex wrappers start on the profile's model.

---

## Phase 5: User Story 3 - The registry tells tools each profile's model (Priority: P2)

**Goal**: `registry.json` carries `model`.

**Independent Test**: set a model, `sync`, read `registry.json`.

- [X] T030 [P] [US3] Write failing tests in `internal/registry/registry_test.go` (field order `harness, name, label, color, icon, auth_kind, model, primary, …`; `model` present as `""` when unset; `format_version` 1) and extend `tests/integration/registry_test.go` (field list, a `/model`-style edit of `settings.json` does not change the registry model).
- [X] T031 [US3] Add `Model string \`json:"model"\`` to `registry.Entry` in `internal/registry/registry.go` after `AuthKind`, and set `Model: p.Model` when building entries in `internal/sync/sync.go` (L388-401).

---

## Phase 6: User Story 4 - One source per setting, no surprises from the shell (Priority: P2)

**Goal**: caller exports never change a profile's settings; no leaks between sessions.

**Independent Test**: run wrappers through the stub with exported variables and inherited `APROFILE_SET_VARS`.

- [X] T032 [US4] Rewrite `TestScenario2CallerOverrideAndIdempotentSync` in `tests/integration/sync_idempotent_test.go` to assert the caller's `ANTHROPIC_MODEL`/`ANTHROPIC_BASE_URL` do not reach the stub (folder-carried: unset; wrapper-only harness such as OpenCode: profile value) while `--model` passes through as an argument; update `tests/integration/login_test.go` (L42), `tests/integration/registry_test.go` (L70) and `tests/integration/launch_test.go` `TestScenario1Launch` (L41-42 expects `ANTHROPIC_MODEL=X` in the stub env; a folder-carried model is no longer in the wrapper env) for the new behavior.
- [X] T033 [US4] Add `tests/integration/launch_leak_test.go`: for every ordered pair of fixture profiles (Claude API-key with aliases, Claude login profile, Codex API-key, OpenCode), start the second's wrapper from an environment containing the first's launch variables and `APROFILE_SET_VARS` (as the first's wrapper or folder session would leave them); assert none of the first's launch variables reach the stub (SC-006), and that `PATH`/`HOME` survive a hostile inherited `APROFILE_SET_VARS`.

---

## Phase 7: User Story 5 - Know whether a folder is self-contained, and register it with Corgi (Priority: P3)

**Goal**: `show`, `doctor`, `list` report self-containment, stale folders and unplaced entries; docs explain Corgi registration.

**Independent Test**: `show --json` and `doctor --json` on fixture profiles of each kind.

- [X] T034 [US5] Write failing integration tests in `tests/integration/launch_report_test.go`: `show --json` `folder` object per `contracts/cli.md` for a self-contained Claude profile, an `env:` key profile (`self-contained-env`, variable named), an `oauth-token` profile, a profile at `all`, a Codex profile with a model (`harness-rewrites`), a reference-valued extra variable, an `env:ANTHROPIC_AUTH_TOKEN` key (`cleared-source`); `list` shows `folder stale` after a placed entry is edited; `doctor --json` has `info` findings for wrapper-only settings, a `problem` for a stale folder with fix `run: aprofile sync`, and a `problem` for an unplaced `env.ANTHROPIC_BASE_URL` with fix `remove the entry or set it on the profile`; at share level `all`, an `env.ANTHROPIC_BASE_URL` in the linked default `settings.json` yields the `overrides the value the wrapper sets` problem from `contracts/cli.md`; no key value appears in any output.
- [X] T035 [US5] Implement the `folder` block in `internal/cli/list_show.go` (`showJSON`, text lines, a `folderStatus` helper beside `wrapperStatusErr` using `launch.Status`) and the `folder stale` bracket and `folder_sync` field in `list`.
- [X] T036 [US5] Add `launchFindings` to `internal/doctor/doctor.go` `Run` (info for self-containment and wrapper-only settings, problem for stale folder, problem for unplaced entries) and make sure `internal/cli/doctor.go` prints them.
- [X] T037 [P] [US5] Write `docs/guide/docs/user/corgi.md` (register each profile with `corgi account add <agent> <name> --dir <config_dir>` using `aprofile show` or `registry.json`; one-time cleanup: `corgi account remove` for old cc-deck folders and duplicates, delete saved custom commands, hide default rows that duplicate a named profile; what stays wrapper-only and why; Claude display may show a stale `/model` pick; Codex folder launches follow the last saved `/model`; aprofile never writes into Corgi) and link it in `docs/guide/docs/SUMMARY.md`.
- [X] T038 [P] [US5] Update `docs/README.md` (built-in harness table and Codex model paragraph L17-24, wrapper section L71-85 replacing the `${VAR:-default}` rule with the single-source rule and `--model`, registry section with `model`, doctor section), `README.md` (L30 example, "How they fit"), and `docs/guide/docs/reference/commands.md`, `user/manage-profiles.md`, `user/troubleshooting.md`, `user/share-levels.md` (launch settings at each level), `overview/profile-basics.md`.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T039 Extend `tests/integration/canary_test.go` (`walkAndAssertNoCanary`) to cover `settings.json`, `config.toml`, `managed-launch/` records, `sync --json`, `show --json` and `doctor --json` for profiles with `file:` and `env:` key references (SC-003).
- [X] T040 Run `gofmt -l .`, `go vet ./...` and `go test ./...`; fix any failures.
- [X] T041 Walk through `specs/004-corgi-integration/quickstart.md` sections 1 to 5 in a throwaway home and fix any divergence between the quickstart and actual output. Section 6 (Corgi registration, SC-007) and the real-harness stub-endpoint checks (SC-001 behavior part) are verified in the walkthrough stage (`/speckit-spex-walkthrough`), not here.

---

## Dependencies & Execution Order

- Phase 1 (T001, T002) has no code dependencies; do it first so reviews read the amended rules.
- Phase 2 blocks all stories: T003 → T004 → T005 → T006 → T007; T008, T009 → T010; T011 → T012; T013 → T014; T015 → T016 (needs T010, T012, T014); T017 → T018 (needs T016); T019 → T020 (needs T012, T006).
- US1 (T021 to T026) needs Phase 2. US2 (T027 to T029) needs Phase 2; T028 needs T027. US3 (T030, T031) needs only T004 and can start after Phase 2. US4 (T032, T033) needs T020 and T022. US5 (T034 to T038) needs T018, T022; docs T037 and T038 can be written any time after Phase 2.
- Polish runs last.

### Parallel opportunities

- Phase 2: T007, T008, T009, T011, T013 in parallel once T004 to T006 are in.
- After Phase 2: US1, US2, US3 can proceed in parallel (different files, except `internal/sync/sync.go`, touched by T022 and T031: do T022 first).
- US5 docs (T037, T038) in parallel with US5 code (T034 to T036).

## Implementation Strategy

1. Phase 1 and Phase 2, then US1: Claude folders self-contained (MVP; covers six of eleven daily profiles).
2. US2 (Codex), US3 (registry model), US4 (leak and override tests) in that order.
3. US5 reporting and docs, then Polish.
4. Commit after each task or logical group; `go test ./...` after each task (inter-task checkpoints).
