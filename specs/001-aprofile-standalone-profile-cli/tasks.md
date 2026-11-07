---
description: "Task list for aprofile slice one implementation"
---

# Tasks: aprofile slice one (standalone profile CLI)

**Input**: Design documents from `specs/001-aprofile-standalone-profile-cli/`
**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: INCLUDED. Constitution Principle VII mandates test-first (a failing test precedes
implementation). Within each story, write the test tasks first and confirm they fail.

**Organization**: grouped by the seven user stories (US1..US7) so each is independently testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1..US7 for story-phase tasks; Setup/Foundational/Polish carry no story label
- File paths are relative to the repo root; module is `github.com/cc-deck/aprofile`.

---

## Phase 1: Setup (Shared Infrastructure)

- [X] T001 Create the Go module layout per plan.md: `cmd/aprofile/`, `internal/{config,secret,harness,profile,layer,mcp,wrapper,registry,sync,importer,doctor,cli,tui,testutil}/`, `tests/{integration,fixtures}/`.
- [X] T002 Initialize `go.mod` (module `github.com/cc-deck/aprofile`, Go 1.23) and pin dependencies: `spf13/cobra`, `charmbracelet/lipgloss`, `charmbracelet/bubbletea`, `charmbracelet/huh`, `gopkg.in/yaml.v3`, `github.com/pelletier/go-toml/v2`.
- [X] T003 [P] Add CI (`.github/workflows/ci.yml`) running `gofmt -l`, `go vet ./...`, `go test ./...` on Linux and macOS; add `Makefile` targets; add `.gitignore` entries ensuring `.mcp.json` and any credential files are never committed.
- [X] T004 [P] Implement `internal/testutil`: hermetic `HOME`/XDG helpers over `t.TempDir()`+`t.Setenv()` (Principle VII) and a no-op stub harness binary built under `tests/fixtures/stub/` for wrapper and overhead tests.

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: no user-story work begins until this phase is complete.

- [X] T005 Implement `internal/config`: resolve `XDG_CONFIG_HOME`/`XDG_DATA_HOME`/`XDG_STATE_HOME` with standard fallbacks on Linux and macOS; derive the aprofile config dir (`$XDG_CONFIG_HOME/aprofile`), data dir (`$XDG_DATA_HOME/aprofile`), configurable bin dir (default `<data dir>/bin`), backups dir; load/save the global settings YAML (bin-dir location, disable-shell-edit flag) (FR-005, FR-046).
- [X] T006 [P] Tests for `internal/config`: XDG fallbacks on both platforms, bin-dir override, global settings round-trip.
- [X] T007 [P] Implement `internal/secret`: `Reference` parsing for `env:NAME` (`NAME` matches `[A-Za-z_][A-Za-z0-9_]*`) and `file:/absolute/path` (absolute, no newlines); the **prefix check** (reject known secret prefixes `sk-`, `ghp_`, `xox`, ...); the **length check** (reject a value of 32+ characters without spaces); a `literal` flag that skips the length check but not the prefix check; and a launch-time resolution renderer for wrappers (FR-028, FR-028a, FR-028b).
- [X] T008 [P] Table tests for `internal/secret`: refs accepted; literal secret rejected by prefix and by length; `--env-literal` value skips length but is rejected on prefix; hook-`env` path applies prefix check only; fail-closed resolution snippet.
- [X] T009 Implement `internal/harness` definition model: `name`, `binary`, `wrapper_prefix`, `config_env` (ordered map var→path relative to the profile root; `.`=root), `default_config_location`, `model_var`, `endpoint_var`, `auth_kinds`, `classification` (per-entry layer incl. `mixed` with per-key layers), `config_patches`, `mcp`, `skills_location` (path + `per-profile`|`shared`), `plugin_mechanism`, `omarchy_provider`, `capabilities`, `exec_hook`; load built-ins via `go:embed`; validation reports file+field and isolates a bad definition so others keep working (FR-007, FR-008, FR-012).
- [X] T010 [P] Tests for `internal/harness` model: valid load, mixed-file key parsing, invalid definition reported with file+field without blocking others.
- [X] T011 Implement `internal/profile`: profile model (all Key-Entities fields incl. `primary`, `auth_kind`, `auth_refs`, `extra_env` with per-var `literal` flag, `herdr_agent`); YAML store, one file per profile at `$XDG_CONFIG_HOME/aprofile/profiles/<harness>/<name>`; atomic write (temp+rename); name validation regex `^[a-z][a-z0-9-]{0,62}[a-z0-9]$`; auth refs accept only `env:`/`file:`; FR-028 checks applied on load (FR-006, FR-002, FR-028).
- [X] T012 [P] Tests for `internal/profile`: store round-trip, name-regex accept/reject, literal marking persisted, hand-edited literal secret rejected on load.
- [X] T013 Implement `internal/layer` classification engine: map each config-dir entry (exact or glob) to its layer; resolve `mixed` files to per-key layers; flag entries the definition does not classify as unclassified (FR-013, FR-018). Include table tests.
- [X] T014 Implement `internal/registry`: `registry.json` model (`format_version` + per-profile `harness,name,label,color,icon,auth_kind,primary,config_dir,wrapper_path`), atomic write, no timestamps/volatile fields so an unchanged write is byte-identical, no secret values (FR-030, FR-014). Include tests.

**Checkpoint**: core data structures ready; user stories can begin.

---

## Phase 3: User Story 1 - Create a profile and launch it from any shell (Priority: P1) 🎯 MVP

**Goal**: add a profile, `sync`, and launch the harness through a PATH wrapper with the profile's
config dir, model/endpoint and `APROFILE_*` set.

**Independent Test**: on a clean hermetic root with the stub harness, `add` one profile, `sync`,
start the wrapper; the stub starts with the config-dir variable and `APROFILE_*` set and `list` shows
it (quickstart Scenario 1).

### Tests for User Story 1 ⚠️ write first, confirm they FAIL

- [X] T015 [P] [US1] Integration test `tests/integration/launch_test.go`: Scenario 1 (add→sync→wrapper execs stub with `CLAUDE_CONFIG_DIR` + `APROFILE_*`; `list --json` shows the profile; config dir exists).
- [X] T016 [P] [US1] Integration test `tests/integration/sync_idempotent_test.go`: Scenario 2 (`ANTHROPIC_MODEL=other` override honored for one run; a second `sync` reports 0 changes and writes 0 files — SC-003).
- [X] T017 [P] [US1] Unit test `internal/wrapper/render_test.go`: `APROFILE_*` and `config_env` vars always set; auth vars set and competing auth kinds unset; model/endpoint rendered `${VAR:-default}`; literals shell-quoted; references resolved at launch; config-dir-exists guard; aprofile marker present (FR-026, FR-027, FR-028a, FR-028b).

### Implementation for User Story 1

- [X] T018 [US1] Add `internal/harness/builtin/claude.yaml`: `config_env {CLAUDE_CONFIG_DIR: "."}`; classification (`.claude.json` mixed: account=credentials, `mcpServers`=owned, `projects`/`numStartups`=state; `.credentials.json`=credentials; `projects/`,`statsig/`,logs=state; `skills/`=owned; `*`=shared); auth kinds `api-key`,`auth-token`,`oauth-login`,`oauth-token`; `mcp` mcpServers JSON; `omarchy_provider: anthropic`; capabilities (per research.md item 1).
- [X] T019 [US1] Implement `internal/wrapper`: POSIX-sh renderer that sets `config_env` vars and `APROFILE_*` (never overridable), sets the profile's auth vars and unsets other auth kinds', renders model/endpoint/extra as `${VAR:-default}` and exports `APROFILE_SET_VARS`, sets `HERDR_AGENT` only when the profile declares it (FR-029), resolves references at launch (fail-closed, FR-028a), shell-quotes all other values (FR-028b), checks the config dir exists then `exec`s the binary (FR-026, FR-027).
- [X] T020 [US1] Implement `internal/sync` core: prepare each profile config dir by layer (owned/state created empty-real, shared symlinks created lazily when the default setup exists, mixed files seeded once), render+write one wrapper per profile, remove only aprofile-marked stale wrappers, call `internal/registry` write; `--dry-run` lists changes; idempotent (FR-022, FR-023, FR-025).
- [X] T021 [US1] Implement `internal/sync` PATH management: idempotent marked block in the `$SHELL` startup file (`${ZDOTDIR:-$HOME}/.zshrc`; bash `~/.bashrc` Linux / `~/.bash_profile` macOS; fish `conf.d/aprofile.fish`); print manual instructions when it cannot; exit non-zero on PATH failure only with `--require-path`; `--remove-path` removes the block; never replace a symlinked/read-only rc file (FR-024, edge cases).
- [X] T022 [US1] Implement `internal/cli` root + `add <harness> <name>` with flags `--model/--endpoint/--auth-kind/--auth-key/--env/--env-literal/--color/--icon/--label/--primary/--herdr-agent/--adopt`; warn when the harness binary is missing (FR-001, FR-003, edge case).
- [X] T023 [US1] Implement `internal/cli` `list` and `show` (`--json`; `show` reports whether credentials exist without revealing them) (FR-001, FR-039, FR-041).
- [X] T024 [US1] Implement `internal/cli` `edit`, `rename <new>` (`--force`; refused while the harness runs; moves file+config dir together), `remove` (`--purge` confirmed); none of these write or remove wrappers (FR-001, FR-006a, FR-006b, FR-004).
- [X] T025 [US1] Wire `sync` (and `--dry-run/--require-path/--remove-path`) into `internal/cli`; implement `aprofile login <h>/<n>` running the harness's native login flow with the profile's environment (using the exec-hook `login` operation where a definition implements it) (FR-039); ensure `list`/`show` emit stable `--json`.

**Checkpoint**: US1 fully functional and independently testable (MVP).

---

## Phase 4: User Story 2 - Keep history, memory, credentials and cost apart per profile (Priority: P1)

**Goal**: credentials/state are real and isolated per profile; shared entries are links; mixed files
keep harness-owned keys intact.

**Independent Test**: two profiles of one harness; each profile's state entries are real (not links),
credentials differ, and a shared entry edited in the default setup is visible in both (Scenario 3).

### Tests for User Story 2 ⚠️ write first, confirm they FAIL

- [X] T026 [P] [US2] Integration test `tests/integration/isolation_test.go`: Scenario 3 (state entries real not links; credentials differ; shared entry visible in both; SC-002 = 0 cross-profile reachability).
- [X] T027 [P] [US2] Unit tests `internal/layer/mixed_test.go`: owned keys merged, non-owned keys preserved byte-for-byte; shared keys seeded once then never rewritten; drift detectable.

### Implementation for User Story 2

- [X] T028 [US2] Extend `internal/layer` + `internal/sync`: enforce layer rules during config-dir preparation — credentials/state real, empty, never linked/copied; owned real; shared symlinked to the default setup; unclassified left in place and reported; keep a real file if a shared link target name already exists (FR-013..FR-018, edge cases).
- [X] T029 [US2] Implement mixed-file merge in `internal/layer`: decode to `map[string]json.RawMessage` (JSON) / `map[string]any` (TOML), modify only owned keys, re-encode preserving unknown keys; seed shared keys once from the default setup; merge-only config patches (FR-013, FR-019).
- [X] T030 [US2] Implement `internal/doctor` checks for isolation: unclassified entries, state entries that are links, credentials that are links, and leaking-link detection; add `sync --fix-links` to replace a leaking link with an empty real entry after confirmation, never touching the link target (FR-018, edge cases).
- [X] T031 [US2] Wire `doctor` (`--json`) into `internal/cli` for the above checks.

**Checkpoint**: US1 + US2 both work independently.

---

## Phase 5: User Story 3 - Other tools can tell which profile a pane runs (Priority: P2)

**Goal**: a machine-readable registry and an `APROFILE_*` env contract, plus opt-in Omarchy export.

**Independent Test**: read the registry file (all fields + version, no secrets); inspect a running
wrapper's process env for `APROFILE_*`; run the Omarchy export and confirm non-aprofile entries are
preserved (Scenario 4).

### Tests for User Story 3 ⚠️ write first, confirm they FAIL

- [X] T032 [P] [US3] Integration test `tests/integration/registry_test.go`: Scenario 4 (registry fields+`format_version`, no secrets; wrapper process env carries `APROFILE_*`; Omarchy export writes per-provider files with `id aprofile:<h>/<n>` and preserves non-aprofile entries; SC-007).

### Implementation for User Story 3

- [X] T033 [US3] Implement Omarchy export in `internal/registry`: one accounts file per provider at `$XDG_STATE_HOME/omarchy/agents/accounts/<provider>.json` (fallback `~/.local/state`, `--output-dir` override), `{accounts:[{id,label,home,primary}]}`, `id = aprofile:<harness>/<name>`, preserve non-`aprofile:` entries, remove stale `aprofile:` entries (FR-031).
- [X] T034 [US3] Implement `internal/cli` `registry path` and `registry export --format omarchy` (FR-030, FR-031).
- [X] T035 [US3] Add a registry `--json` schema document test and extend the wrapper test to assert the full `APROFILE_*` contract set (FR-030, FR-026).

**Checkpoint**: registry and env contract verified; no changes required in consumer tools (FR-032).

---

## Phase 6: User Story 4 - Copy a profile with its MCP servers, skills and plugins (Priority: P2)

**Goal**: `copy` within and across harnesses, converting MCP through a neutral record and reporting
what could not be carried.

**Independent Test**: copy a profile with three MCP servers (stdio+env refs, remote+header-from-env,
disabled) to the same harness and to each other harness; compare the target's native MCP config with
the expected projection (Scenario 5), with a secret-canary sweep (Scenario 6).

### Tests for User Story 4 ⚠️ write first, confirm they FAIL

- [X] T036 [P] [US4] Round-trip conversion tests `internal/mcp/convert_test.go` for every built-in harness pair with the `mcp` capability: preserve 100% of fields both sides represent, name every dropped field (SC-005).
- [X] T037 [P] [US4] Integration test `tests/integration/copy_test.go`: Scenario 5 (same-harness owned carried verbatim + warn on secret-looking values; cross-harness converts, drops literal secrets, plugins not carried) and Scenario 6 (literal secret rejected, canary sweep finds 0 secrets — SC-006).

### Implementation for User Story 4

- [X] T038 [US4] Implement `internal/mcp` neutral `MCPServerRecord` (name, transport, command, args, env refs, working_dir, url, headers, auth ref, enabled, timeout, tool_allow/deny, description, extra) with per-harness parse/emit: Claude JSON `mcpServers`, Codex TOML `[mcp_servers.*]`, OpenCode JSON `mcp`, pi JSON `mcpServers` (FR-034; schemas per research.md).
- [X] T039 [US4] Implement `internal/mcp` conversion: references→target syntax; a literal value matching a secret pattern is dropped and reported (never written to target); unrepresentable fields reported; when the target lacks the `mcp` capability every server is reported skipped (FR-035, Constitution IV).
- [X] T040 [US4] Implement `internal/cli` `copy <h>/<n> <h>/<n>`: same-harness carries all settings + owned layer verbatim and warns on secret-looking owned values (FR-033a); cross-harness carries label/color/icon + converted MCP, drops auth/model/endpoint, applies target default auth kind unless `--auth`, carries skills only to a per-profile location else reports (FR-036), never carries plugins; target gets empty credentials+state; `--overwrite` replaces only what the copy writes (FR-033, FR-033a, FR-033b).
- [X] T041 [US4] Implement `internal/cli` `mcp list <h>/<n>` and `mcp copy <from> <to> [server...]` (`--overwrite` leaves existing target servers alone otherwise), both with a `--json` ConversionReport (FR-037, FR-041).

**Checkpoint**: copy and MCP conversion verified across all pairs.

---

## Phase 7: User Story 5 - Support a new or changed harness without a release (Priority: P2)

**Goal**: `harnesses.d` overrides/additions and an exec hook, with all four built-in definitions.

**Independent Test**: drop a definition for a fake harness (binary = test script), add a profile,
sync, run the wrapper; override one field of a built-in and confirm `harness show` reflects it
(Scenario 7).

### Tests for User Story 5 ⚠️ write first, confirm they FAIL

- [X] T042 [P] [US5] Integration test `tests/integration/harness_def_test.go`: Scenario 7 (fake `harnesses.d` harness → working wrapper, 0 core change — SC-004; field override shown as merged; invalid file names file+field while others keep working; exec-hook `env` output included; exit 64 treated as no-op).
- [X] T043 [P] [US5] Unit tests `internal/harness`: override-merge marks built-in vs file source; exec-hook client handles protocol, exit 64, the 10 s timeout (except `login`/`pre-launch`), and aborts on non-`key=value` output.

### Implementation for User Story 5

- [X] T044 [US5] Implement `harnesses.d` loader + per-field override-merge over the built-in, marking each effective field's source (FR-009).
- [X] T045 [US5] Implement `internal/harness` exec-hook client: call `aprofile-harness-<name>` with the operation as the sole arg, `key=value` on stdin/stdout, every call receiving `harness/profile/config_dir`; operations `env`,`pre-launch`,`write-config`,`auth-status`,`login`; exit 64 = unsupported no-op; 10 s timeout (none for `login`/`pre-launch`); wire `env` output into wrapper rendering (prefix check only) and `pre-launch` into the wrapper (FR-011).
- [X] T046 [US5] Add `internal/harness/builtin/{codex.yaml,opencode.yaml,pi.yaml}` with the `config_env`, classification, `mcp`, `skills_location` and auth values fixed in research.md (Codex `CODEX_HOME:.`, TOML `[mcp_servers.*]`, `$CODEX_HOME/skills` owned + `~/.agents/skills` shared; OpenCode XDG set, JSON `mcp`, `~/.config/opencode/skills` owned + `~/.claude`/`~/.agents` shared; pi `PI_CODING_AGENT_DIR:agent` + `PI_CODING_AGENT_SESSION_DIR:agent/sessions`, JSON `mcpServers`, `agent/skills` owned).
- [X] T047 [US5] Implement `internal/cli` `harness list` and `harness show <name>` (`--json`; show marks overridden vs built-in fields) (FR-010).

**Checkpoint**: all four harnesses supported data-only; new harnesses need no release.

---

## Phase 8: User Story 6 - Move existing cc-deck profiles over once (Priority: P3)

**Goal**: one-time `import cc-deck` that never modifies cc-deck files.

**Independent Test**: point the import at a cc-deck fixture; dry-run changes nothing; real import
creates profiles + credentials + fresh state and touches no cc-deck file (Scenario 8).

### Tests for User Story 6 ⚠️ write first, confirm they FAIL

- [X] T048 [P] [US6] Integration test `tests/integration/import_test.go`: Scenario 8 (`--dry-run` changes nothing; import creates profiles+creds+fresh state, 0 cc-deck files modified; existing `(harness,name)` skipped + exit non-zero; `--overwrite` keeps creds; `--overwrite-credentials` backs up to `<data dir>/backups/<harness>/<name>/<timestamp>/`; mixed file backed up with only credential keys replaced; wrapper-name collisions reported — SC-010).

### Implementation for User Story 6

- [X] T049 [US6] Implement `internal/importer`: read cc-deck profile config + credential files, create aprofile profiles with settings + credentials + fresh state, never modify/delete a cc-deck file; backups under `<data dir>/backups/<harness>/<name>/<timestamp>/`; for a mixed file back up the whole file and replace only its credentials-layer keys (FR-045).
- [X] T050 [US6] Implement `internal/cli` `import cc-deck` (`--dry-run/--overwrite/--overwrite-credentials/--json`); report which wrapper wins on the current `PATH` and how to remove old cc-deck wrappers (FR-045, edge case).

**Checkpoint**: existing cc-deck users migrate with 0 manual edits.

---

## Phase 9: User Story 7 - Browse and edit profiles interactively (Priority: P3)

**Goal**: a TUI browse view and forms over the same operations.

**Independent Test**: open the browse view, add a profile through the form, confirm `list --json`
shows it; a non-TTY invocation fails naming the missing flags instead of opening a form (Scenario 9).

### Tests for User Story 7 ⚠️ write first, confirm they FAIL

- [X] T051 [P] [US7] Behavior test `tests/integration/ui_parity_test.go`: form-add equals the equivalent command with flags; when stdin or stdout is not a TTY, `add`/`edit` without required values fail naming the missing flags and never open a form (FR-043).

### Implementation for User Story 7

- [X] T052 [US7] Implement `internal/tui` browse view: profiles grouped by harness with label, color, icon and wrapper sync status (FR-043, FR-042).
- [X] T053 [US7] Implement `internal/tui` huh forms for add/edit/copy/remove, invoked by `aprofile ui` and by `add`/`edit` only when stdin and stdout are both TTYs; all actions route through the same CLI operations (FR-043, Constitution VI).

**Checkpoint**: every UI action has a command equivalent (SC-008).

---

## Phase 10: Polish & Cross-Cutting Concerns

- [X] T054 [P] Complete `internal/doctor` to the full FR-044 set: bin dir on PATH, wrapper shadowing, stale/missing wrappers, missing harness binaries, unclassified entries, state/credentials that are links, invalid definitions, unresolved references (FR-028a), hook `auth-status`, and seeded mixed-file keys that differ from the default setup; each finding names the profile and a fix.
- [X] T055 [P] Add the SC-011 wrapper-overhead benchmark: median of 20 wrapper runs vs 20 direct stub runs is ≤ 50 ms with no exec hook.
- [X] T056 [P] Add the SC-006 canary sweep test: plant canary secrets and assert 0 appear in any wrapper, registry, profile store, definition or `--json` output.
- [X] T057 [P] Add output-degradation tests: color/icons on a TTY, plain text when not a TTY or `NO_COLOR` is set (FR-042).
- [X] T058 Run every quickstart.md scenario end-to-end on Linux and macOS in CI, hermetic (SC-009).
- [X] T059 [P] Write README/usage docs (no secrets, no committed `.mcp.json`); document the four built-in harnesses and the exec-hook protocol.
- [X] T060 Ensure `gofmt`, `go vet` and the full `go test ./...` pass on Linux and macOS.

---

## Dependencies & Execution Order

- **Setup (Phase 1)** → **Foundational (Phase 2)** blocks everything.
- **US1 (P1)** is the MVP; after it, **US2 (P1)** hardens isolation. US1 and US2 together are the
  recommended first release.
- **US3, US4, US5 (P2)** each depend only on Foundational (+ US1 for a synced profile to inspect);
  they can proceed in parallel once US1 exists.
- **US6, US7 (P3)** depend on Foundational + the CLI operations from US1/US4.
- **Polish (Phase 10)** depends on the stories it touches.

### Within each user story

- Write the test tasks first and confirm they fail (Principle VII), then implement.
- Models/data before services; services before CLI wiring; core before integration.

### Parallel opportunities

- Setup: T003, T004 in parallel.
- Foundational: T006, T007/T008, T010, T012 in parallel (different packages); T005, T009, T011, T013,
  T014 are the serial backbone.
- Within a story, all `[P]` test tasks run together; built-in definition files (T046) are independent
  of each other.
- Across stories: once Foundational + US1 land, US3/US4/US5 can be built concurrently.

## Parallel Example: User Story 1 tests

```text
Task: T015 Integration test tests/integration/launch_test.go
Task: T016 Integration test tests/integration/sync_idempotent_test.go
Task: T017 Unit test internal/wrapper/render_test.go
```

## Implementation Strategy

- **MVP**: Phases 1–2 then US1 (T015–T025). Stop and validate Scenario 1 + 2.
- **First release**: add US2 (isolation). This satisfies the two P1 stories and SC-001/002/003.
- **Incremental**: US3 → US4 → US5 (P2), then US6 → US7 (P3), each independently testable.
- Commit after each task or logical group; never commit `.mcp.json` or credential files.

## Notes

- `[P]` = different files, no incomplete-task dependency.
- Verify each test fails before implementing it.
- Keep core packages free of `if harness == "..."` branching (Principle I); per-harness behavior
  lives only in definitions and the exec hook.
- Live-verification items V1–V4 in research.md are confirmed against a real install when finalizing
  the corresponding built-in definition (T018, T046); each already has a safe default.

## Phase 11: Convergence

- [X] T061 CRITICAL: Apply the FR-028 prefix and length checks to every literal in MCP `args` (per element) and `url` (whole value, query and userinfo) in all encoders (`internal/mcp/{claude,codex,opencode,pi}.go`, `refs.go`); drop and report each hit as `literal-secret` naming server and field; make every decoder collect native fields it does not model (e.g. Claude `cwd`, `timeout`) and report them as unrepresentable drops instead of discarding them per FR-035, FR-034, SC-005 (partial)
- [X] T062 Make `aprofile login` run the harness login flow: add a definition field for the login command and args (claude `auth login`, codex `login`, opencode `auth login`), call the hook `login` operation when declared, otherwise exec the declared command with the profile env, and fail clearly when neither exists (`internal/cli/login.go`, `internal/harness/definition.go`, built-in YAML) per FR-039, FR-011 (contradicts)
- [X] T063 Add a `clears` list to the auth schema and declare the Claude provider switches and their settings (`CLAUDE_CODE_USE_VERTEX`, `CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_FOUNDRY`, `ANTHROPIC_VERTEX_PROJECT_ID`, `CLOUD_ML_REGION`, `ANTHROPIC_BEDROCK_BASE_URL`, `AWS_BEARER_TOKEN_BEDROCK` and similar; verify the current set) in `claude.yaml`; unset them in the wrapper and `loginEnv` and treat them as reserved in hook env, so a shell export cannot override a profile's auth kind per FR-007, FR-027, FR-038 (missing)
- [X] T064 In `convertMCP` (`internal/cli/copy.go`) and `runMCPCopy` (`internal/cli/mcp.go`), keep an existing target file's mode and create new MCP files as 0600, so a 0600 `.claude.json` stays 0600; add a test per FR-014 (partial)
- [X] T065 Rewrite `readCCProfiles`/`buildProfile` in `internal/importer/importer.go` for cc-deck's real `config.yaml` (`profiles.<harness>.<name>{auth{login|api_key|credentials}, env, color, backend, project, region}`, `default_profile` as primary) with credential files from `profiles/<harness>/<name>/`; add fixture tests built from that schema per FR-045, SC-010 (contradicts)
- [X] T066 Call the hook `write-config` operation in `sync` after preparing the config dir (honoring `changed=`) and `auth-status` from `show`; make `show` report credentials as present for env/file reference profiles (whether each reference resolves, never the value) and for Keychain-backed logins via the hook per FR-011, FR-039 (missing)
- [X] T067 In `copy`, treat only `ErrNotFound` from loading the target as absent and fail on any other load error; fail naming the dir when the target config dir exists without a profile unless `--adopt`; validate `--auth` before preparing the target dir per FR-004, FR-033 (missing)
- [X] T068 When a harness override in `harnesses.d` fails to load, mark that harness failed in `sync`: keep its profiles' wrappers and registry entries unchanged, report the error and exit non-zero instead of rendering with the built-in per FR-022 (contradicts)
- [X] T069 Reject a wrapper name another profile already has in `add`, `rename`, `copy` and `import`, naming that profile; report duplicate `wrapper_prefix` values across effective definitions as a load error per FR-002 (missing)
- [X] T070 Validate color (ANSI name or `#rrggbb`) and icon (single grapheme) in `profile.Validate` so load, store and import all check them; make `import` move the primary flag from the harness's previous primary and report it per FR-001 (missing)
- [X] T071 Apply each definition's `config_patches` during `sync` as a merge-only write that keeps unknown keys (or reject the field at load until supported), and make doctor's seeded-key drift check compare every seeded key (not owned, credentials or state) with the default setup per FR-019, FR-007, FR-044 (partial)
- [X] T072 Complete copy reports: scan every owned-layer value (all MCP fields, all owned-file keys) for secret patterns in same-harness copies; report each owned entry or key a cross-harness copy does not convert (e.g. Codex `config.toml` model/providers); list skipped plugins by name with the skills and MCP servers they contain; label same-harness carried servers as carried, not Converted per FR-033a, FR-033b, FR-036 (partial)
- [X] T073 Make `sync --require-path` exit non-zero for every manual-instructions outcome (unsupported shell, symlinked rc), and refuse to replace a read-only rc file (check the mode before the atomic write) by falling back to manual instructions (`internal/sync/path.go`, `internal/cli/sync.go`) per FR-024 (partial)
- [X] T074 Omarchy export: rewrite every existing `<provider>.json` in the output dir to drop `aprofile:` entries without a matching profile, and keep foreign entries and unknown top-level keys as raw JSON (`internal/registry/omarchy.go`) per FR-031 (partial)
- [X] T075 Fix profile-setting consistency: give `loginEnv` the wrapper's nested-session cleanup and `APROFILE_SET_VARS` semantics; reject a `copy` target whose auth kind needs a reference that was not supplied (add `--auth-key` to `copy`); clear refs that do not belong to the new kind on `edit --auth-kind`; dedupe repeated `--env NAME` in `add` (last wins) and repeated server names in `mcp copy` per FR-027, FR-033b, FR-001, FR-037 (partial)
- [X] T076 Resolve aprofile's own dirs from the pre-redirect XDG values when run inside an OpenCode profile session; treat a declared but missing hook executable as an error for `env` and `pre-launch`; make the importer copy symlinked directories and write credentials atomically at 0600 per FR-020, FR-011, FR-045 (partial)
- [X] T077 Seed Claude's `.claude.json` from `~/.claude.json` (where Claude Code keeps it without `CLAUDE_CONFIG_DIR`) via `default_locations` in `claude.yaml`, and fix the fixture in `internal/testutil/defaults.go`; before that, declare credential-like top-level keys (e.g. `primaryApiKey`, `oauthAccount`) as credentials so they are never seeded per FR-013 (contradicts)
- [X] T078 In cross-harness `copy`, when the target's skills are linked or escape to `$HOME`, report which source skills exist in the target's effective skills locations and which are missing, and name the escapes per FR-021, FR-036 (partial)
- [X] T079 Add `--json` to `sync --dry-run` with a versioned schema, and add `format_version` to `show --json` per FR-041 (missing)
- [X] T080 Map ANSI color names (including bright variants and gray/grey) to ANSI codes before lipgloss in `internal/cli/output.go` and `internal/tui/view.go`; add tests per FR-042 (partial)
- [X] T081 Report undeclared top-level keys of mixed files in `sync` and `doctor` as unclassified (use or remove `layer.ClassifyKey`), or amend FR-013 to say undeclared keys are seeded per FR-013 (contradicts)
- [X] T082 Make `rename` ask (or require `--force`) when the running check cannot tell, as `edit --share` does per FR-006a (partial)
- [X] T083 Remove hygiene debt: dead `if` and `trimStart` alias (`sync/path.go`), double settings read (`config.go`/`cli/root.go`), unused hook fields, exports without non-test callers (`HookResult.Map`, `LoadFromBytes`, `SaveSettings`, `Paths.SettingsPath`, `tui.RenderBrowse`, `wrapper.Render`, `LoadBuiltins` outside testutil), unused `mcp.Server.Extra`, duplicated helpers across `cli/copy.go`, `importer` and `layer`, case-sensitive `.json` check (`layer/creds.go`), nondeterministic auth-ref pick (`cli/edit.go`), per-render re-sort (`tui/view.go`, `tui/browse.go`) per Constitution code quality (unrequested)
