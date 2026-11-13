# Feature Specification: Profile Features (Curated Harness Capability Toggles)

**Feature Branch**: `006-profile-features`

**Created**: 2026-11-13

**Status**: Draft

**Input**: Brainstorm `brainstorm/007-profile-features.md`: a curated catalog of high-level
features (OTel telemetry, the Auto-mode decision log) that aprofile exposes through a dedicated
`features` command and maps to each harness's own mechanism, with state in the profile record and
placement through `sync`.

## Context

aprofile owns each profile's settings file and the launch wrapper, but has no way to say "this
profile emits OTel traces" or "this profile writes the Auto-mode decision log". The only entries
aprofile writes into a harness's settings today are the auth-neutralizing env entries of spec 005.
A user who wants such a capability must know the harness-specific mechanism (an undocumented env
var, a `config.toml` section), set it through `--env-literal`, and remember to remove it again.
Nothing in `show` reflects it, and nothing detects conflicts.

Two concrete first needs drive the v1 catalog:

1. OTel telemetry: enable tracing and/or OTel log emission to an endpoint. Claude Code does this
   through env vars in the profile's `settings.json`; Codex through an `[otel]` section in
   `config.toml`.
2. Extra logging: `AUTOMODE_DECISION_LOG=1` makes Claude Code write `.automode_decisions.jsonl`
   in the session's working directory, which AgentTacho ingests for token accounting. The
   variable is undocumented and version-dependent, and has no settings key; it goes in the `env`
   block of `settings.json`.

Harness facts this spec relies on (verified 2026-11-13, recorded in the brainstorm):

- Claude Code enables OTel through `CLAUDE_CODE_ENABLE_TELEMETRY=1`, per-signal
  `OTEL_TRACES_EXPORTER` / `OTEL_LOGS_EXPORTER` (`otlp`, `console`, `none`),
  `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL` (`grpc`, `http/json`,
  `http/protobuf`). Traces additionally need `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` while they
  are beta. Claude Code ignores `OTEL_*` variables in repo-level `.claude/settings.json`; the
  profile-level `settings.json` (where aprofile places entries) is honored.
- Codex configures OTel through the `[otel]` section of `config.toml`: `exporter` (logs),
  `trace_exporter` (traces), each an enum of `none`, `statsig`, `otlp-http` or `otlp-grpc` with
  `endpoint`, `headers`, `protocol` and `tls` fields, plus boolean log gates
  (`log_user_prompt`, `log_agent_responses`, `log_guardian_assessments`) and a
  `headers_helper` command for exporter auth. The exact TOML encoding of the exporter enum
  (inline table) still needs verification against a current Codex release (open question in the
  brainstorm).

This feature adds a curated catalog with one command over it. The raw `--env` / `--env-literal`
flags remain the escape hatch for anything outside the catalog. Feature flags do not appear on
`add` or `edit`: those commands manage a profile's identity and isolation, `features` manages
the capability layer (user decision in the brainstorm).

## Clarifications

### Session 2026-11-13

- Q: How do mutation flags combine (does `--enable` pair with parameter flags)? → A:
  `--enable <feature>` and `--disable <feature>` select the feature; per-parameter flags
  (`--traces`, `--logs`, `--endpoint`, `--protocol`) set values and imply enabling the feature.
  Enabling telemetry without an endpoint is an error asking for `--endpoint` (see Edge Cases).
- Q: Is the protocol parameter a shared enum or a free string? → A: An enum whose values are
  declared per harness in the capability mapping (Claude Code: `grpc`, `http/json`,
  `http/protobuf`; Codex: `otlp-http` json/binary, `otlp-grpc`); values are validated at
  mutation time and free strings outside the harness's list are rejected.
- Q: Does bare status output group features? → A: Yes; the status output renders features in
  groups (telemetry, logging), one line per feature with its parameter values, matching the
  brainstorm's grouping.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Turn telemetry on for one profile (Priority: P1)

Rohul runs a local OTLP collector and wants his `claude/work` profile to send traces and logs
there, without hand-editing `settings.json` or remembering Claude Code's variable names. He runs
`aprofile features claude/work --enable telemetry --traces on --logs on --endpoint
https://otlp.example.com`, then `aprofile sync`. The profile's `settings.json` now carries the
env entries that make Claude Code export both signals to that endpoint. `aprofile features
claude/work` shows telemetry on with its parameter values, and `aprofile show claude/work`
lists it among the profile's launch settings.

**Why this priority**: the catalog exists to make the common capability (telemetry) a one-command
affair; this is the story that proves the whole mechanism (record, sync placement, status,
drift reporting) end to end.

**Independent Test**: enable telemetry on a `content`-level Claude profile, run `sync`, and
inspect the settings file for the master switch, exporter selection, endpoint and protocol
entries, each recorded as a placed entry. Disabling and re-syncing removes exactly those
entries.

**Acceptance Scenarios**:

1. **Given** a `claude` profile at share level `content` or `none`, **When** the user runs
   `aprofile features <ref> --enable telemetry --traces on --logs on --endpoint <url>`, **Then**
   the command stores the feature state in the profile record and reminds the user to run
   `sync`.
2. **Given** a profile with telemetry enabled and synced, **When** the user runs
   `aprofile sync`, **Then** the profile's own settings file carries the env entries for the
   master switch, both signal exporters, the endpoint and the protocol, and each entry is
   recorded in aprofile's data directory like other launch entries.
3. **Given** a profile with telemetry enabled, **When** the user runs `aprofile features <ref>`
   (bare), **Then** the output lists telemetry as on, with the values of every parameter
   (traces, logs, endpoint, protocol).
4. **Given** a profile with telemetry enabled and synced, **When** the user disables it
   (`--disable telemetry`) and runs `sync`, **Then** the
   env entries aprofile placed for telemetry are removed and nothing else in the file changes.

---

### User Story 2 - Enable the Auto-mode decision log (Priority: P1)

Rohul wants AgentTacho to account for Auto-mode classifier tokens on his `claude/private`
profile. He runs `aprofile features claude/private --enable automode-log`, then `sync`. The
profile's `settings.json` carries `AUTOMODE_DECISION_LOG=1` as a placed env entry; Claude Code
writes `.automode_decisions.jsonl` in each session's working directory, which AgentTacho
ingests.

**Why this priority**: this is the second real consumer of the mechanism and the only way to
reach it today is undocumented knowledge; it proves the catalog can absorb a version-dependent,
undocumented mechanism behind a stable name.

**Independent Test**: enable automode-log on a Claude profile, run `sync`, inspect the env block
for the placed entry; disable it and verify removal on the next sync.

**Acceptance Scenarios**:

1. **Given** a `claude` profile at share level `content` or `none`, **When** the user runs
   `aprofile features <ref> --enable automode-log`, **Then** the state is stored and `sync`
   places `AUTOMODE_DECISION_LOG=1` in the profile's `settings.json` env block.
2. **Given** an enabled automode-log, **When** the user disables it and runs `sync`, **Then**
   the placed entry is removed.

---

### User Story 3 - Discover what a harness supports (Priority: P2)

Rohul forgets whether Codex supports the telemetry feature and which parameters it takes. He
runs `aprofile features codex/work` (bare) and sees every feature the harness supports, on or
off, with parameter values and the effective defaults. Asking for a flag the harness does not
support fails with an error naming the harness.

**Why this priority**: the command's bare form doubles as the catalog's discovery surface; a
curated catalog nobody can list is undocumented knowledge again.

**Independent Test**: run `features` bare on profiles of two harnesses and compare the listed
feature sets; attempt an unsupported flag and check the error.

**Acceptance Scenarios**:

1. **Given** any profile, **When** the user runs `aprofile features <ref>` with no mutation
   flags, **Then** the output lists every feature the profile's harness supports, its state,
   and its parameter values, including defaults that apply.
2. **Given** a profile whose harness does not support a flag, **When** the user passes that
   flag, **Then** the command fails with an error naming the harness and the unsupported flag,
   and changes nothing.
3. **Given** a harness whose telemetry support differs (for example a protocol default of
   `otlp-http` json on Codex versus `http/protobuf` on Claude Code), **When** the user enables
   telemetry without naming a protocol, **Then** the harness's default applies and the status
   output shows it.

---

### User Story 4 - Codex telemetry through the same command (Priority: P2)

Rohul enables telemetry on a `codex` profile with the same command he used for Claude Code.
`sync` writes the `[otel]` section's exporter and trace exporter into the profile's
`config.toml` as placed entries, preserving any hand-written `[otel]` keys. Disabling removes
only what aprofile placed.

**Why this priority**: the catalog's promise is one command across harnesses; Codex is the
second harness and exercises a different mechanism (TOML, not env).

**Independent Test**: enable telemetry on a `codex` profile, run `sync`, inspect `config.toml`;
hand-write an extra `[otel]` key, disable telemetry, re-sync, and verify only aprofile-placed
keys were removed.

**Acceptance Scenarios**:

1. **Given** a `codex` profile at share level `content` or `none`, **When** the user enables
   telemetry with an endpoint and runs `sync`, **Then** `config.toml` gains the `[otel]` keys
   aprofile places for the log and trace exporters, recorded as placed entries with nested-TOML
   locations.
2. **Given** a `config.toml` with hand-written `[otel]` keys, **When** aprofile places or
   removes telemetry entries, **Then** the hand-written keys are preserved.
3. **Given** a disabled Codex telemetry feature, **When** the user runs `sync`, **Then** only
   the entries aprofile placed for the feature are removed.

---

### User Story 5 - Features respect isolation and copy (Priority: P2)

Rohul copies a traced profile to a new name and the copy is traced too. He tries to enable a
feature on a profile at share level `all` and is refused with a hint. He moves a profile that
has features to share level `all` with `edit --share all` and is warned that its features were
disabled.

**Why this priority**: features must fit the isolation model (Principle III) and the existing
commands' contracts rather than poking holes in them.

**Independent Test**: copy a profile with an enabled feature and inspect the copy's feature
state; attempt `features` at `all`; run `edit --share all` on a profile with features and
inspect the output and record.

**Acceptance Scenarios**:

1. **Given** a profile with an enabled feature, **When** the user copies it (same or cross
   harness), **Then** the copy carries the feature state where the target harness supports the
   feature, and the copy report names any feature not carried.
2. **Given** a profile at share level `all`, **When** the user runs any `features` mutation,
   **Then** the command refuses with a message pointing at `aprofile edit <ref> --share
   content`, and the bare status form still works.
3. **Given** a profile with enabled features, **When** the user runs `aprofile edit <ref>
   --share all`, **Then** the edit warns that the features were disabled, clears the feature
   state from the record, and reports it in the edit output.

---

### User Story 6 - Conflicts with hand-set variables are caught (Priority: P3)

Rohul previously set `AUTOMODE_DECISION_LOG=1` through `--env-literal` on a profile. Enabling
the automode-log feature on that profile fails with an error naming the colliding variable.
The same check runs the other way: `edit --env-literal` on a profile with the feature enabled
fails. Collisions that appear anyway (for example through direct file edits) surface as `sync`
problems and `show` warnings.

**Why this priority**: silent overwrite of a user's own entry would break the trust the
placed-entry mechanism is built on; the check is small but must be specified.

**Independent Test**: create both collision directions through the mutating commands and check
for hard errors; hand-edit the settings file to create a collision and check `sync` and `show`.

**Acceptance Scenarios**:

1. **Given** a profile whose record carries an extra env var named like a feature's env entry,
   **When** the user enables that feature, **Then** the command fails naming the variable and
   the stored state is unchanged.
2. **Given** a profile with an enabled feature, **When** the user runs `edit --env` or
   `--env-literal` with a name the feature places, **Then** the edit fails naming the variable.
3. **Given** a settings file whose feature-placed entry was changed outside aprofile, **When**
   the user runs `sync` or `show`, **Then** the drift is reported as a sync problem and a
   `show` warning.

---

### Edge Cases

- What happens when a feature is enabled with neither `--traces` nor `--logs` on? The telemetry
  feature requires at least one signal on at enable time (an error naming both parameters).
- What happens when a user enables telemetry with `--traces on` but no endpoint? The endpoint
  is required for telemetry to be useful; enabling without one is an error asking for
  `--endpoint`.
- What happens when the profile's share level changes to `all` between `features` and `sync`?
  The feature state was already cleared by the `edit --share all` warning (User Story 5); a
  level change made by other means (for example a future command) leaves the entries unplaceable
  and `sync` reports a problem rather than writing into the linked default setup.
- What happens when the same profile is synced twice? Placement is idempotent (Principle II).
- What happens when a harness upgrade renames or drops an undocumented variable
  (`AUTOMODE_DECISION_LOG`)? aprofile places what the catalog says; whether the harness honors
  it is outside aprofile's control. `doctor` reporting for version-dependent mechanisms is an
  open question (see Assumptions).
- What happens to feature state when a profile is removed? It is part of the profile record and
  is deleted with it; placed entries live in the profile's own files, which the removal
  discards.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The CLI MUST provide a `features <harness>/<name>` command whose bare form lists,
  for the profile's harness, every feature in the catalog the harness supports, grouped
  (telemetry, logging), with its state and parameter values (including effective defaults), and
  exits without modifying anything.
- **FR-002**: The same command with mutation flags MUST update the stored feature state of the
  profile and remind the user to run `sync`, following the output contract of `edit`. Mutation
  flags are `--enable <feature>` / `--disable <feature>` plus per-parameter flags; any
  per-parameter flag implies enabling the feature.
- **FR-003**: The v1 catalog MUST contain exactly two features: telemetry (parameters: traces
  on/off, logs on/off, shared endpoint, protocol) and automode-log (no parameters).
- **FR-004**: The telemetry protocol parameter MUST default per harness (`http/protobuf` for
  Claude Code, `otlp-http` json for Codex) and MUST be overridable at enable time. Its values
  are an enum declared per harness in the capability mapping and validated at mutation time;
  values outside the harness's list MUST be rejected. On Claude Code the accepted values are
  `grpc`, `http/json` and `http/protobuf`; on Codex the value carries transport and encoding
  together (`otlp-http json`, `otlp-http binary`, `otlp-grpc`), with the exact set confirmed by
  the encoding verification in Assumptions.
- **FR-005**: Feature ids, their parameters and their types MUST live in a cross-harness catalog
  in Go code; the per-harness statement of which features and parameters a harness supports,
  and the mapping to the harness mechanism, MUST live in the harness definition records and be
  overridable through `harnesses.d` (Principle I).
- **FR-006**: Adding a feature or a parameter to the catalog MUST NOT require changes outside
  the catalog, the definitions and the placement logic (no per-feature command code).
- **FR-007**: A mutation flag unsupported by the profile's harness MUST fail with an error
  naming the harness and the flag; it MUST NOT be accepted as a no-op or warning.
- **FR-008**: `add` and `edit` MUST NOT gain feature flags; the `features` command is the only
  mutation path for feature state.
- **FR-009**: Feature state MUST be stored in the profile record alongside extra env vars, and
  `copy` MUST carry it, reporting any feature the target harness does not support instead of
  failing.
- **FR-010**: `sync` MUST materialize each enabled feature into the profile's own settings
  files as placed entries, recorded and maintained like other launch entries (spec 005's
  mechanism), and MUST remove exactly those entries when a feature is disabled. Placement MUST
  be idempotent.
- **FR-011**: For Claude Code, telemetry MUST map to env entries in the profile's
  `settings.json`: the master switch, the per-signal exporter selection, the endpoint and the
  protocol; with traces on, the traces beta flag MUST also be placed. A signal that is off
  places its exporter entry as `none`, so an inherited `OTEL_*` value cannot turn it back on
  (wrappers clear launch variables only); signal flags omitted at enable time default to off.
  automode-log MUST map to `AUTOMODE_DECISION_LOG=1` in the same env block.
- **FR-012**: For Codex, telemetry MUST map to placed entries in the `[otel]` section of the
  profile's `config.toml` (log exporter, trace exporter, endpoint, protocol). aprofile MUST
  remove only the keys it placed; hand-written `[otel]` keys MUST be preserved. A hand-written
  non-empty value at a key path a feature places MUST be handled like spec 005's replaced user
  entries: placement replaces it, `sync` names the key path (never its value) in a note, and
  the replacement is never silent. The launch
  record MUST support placed-entry locations for nested TOML subtrees.
- **FR-013**: A mutation on a profile at share level `all` MUST be refused with an error
  pointing at `aprofile edit <harness>/<name> --share content`. Bare status output MUST still
  work at `all`.
- **FR-014**: `edit --share all` on a profile with enabled features MUST warn, clear the
  feature state from the record, and report the cleared features in the edit output, so no
  feature state is left stranded as unrepairable drift.
- **FR-015**: A mutating command (`features`, `edit --env`, `edit --env-literal`) MUST fail
  with an error naming the variable when its change would create a collision between a
  feature's entries and a hand-set extra env var, in both directions. Collisions discovered at
  `sync` or `show` time MUST be reported as sync problems and `show` warnings, never silently
  resolved.
- **FR-016**: `show` MUST list each enabled feature with its parameter values in a features
  section, and MUST report drift between the recorded feature state and the placed entries.
- **FR-017**: `features` status and mutation output MUST be available as stable `--json`
  (Principle VI).
- **FR-018**: Feature-placed entries are placed entries in the sense of spec 004 (as spec 005
  FR-003 restates: recorded in aprofile's data directory, kept and restored by `sync`, removed
  on disable, reported as stale when they differ, listed by `sync --dry-run`, idempotent).
  Because they extend Principle II's launch-entry writes beyond launch variables, key command
  and auth blocks, the plan's Constitution Check MUST record this extension explicitly, either
  as a justified exception or through a PATCH amendment naming feature-placed entries
  (spec 005's precedent).

### Key Entities

- **Feature catalog**: the cross-harness list of feature ids with named, typed parameters
  (boolean, string, enum), each feature assigned to a display group (telemetry, logging). Fixed
  in Go code; extended by data, not code branching.
- **Capability mapping**: per harness, which catalog features exist, which parameters they take
  (including per-harness defaults), and the mechanism entries each enabled feature places
  (env names for `settings.json`, TOML key paths for `config.toml`). Lives in the harness
  definition records.
- **Feature state**: per profile, the enabled features and their parameter values. Part of the
  profile record; carried by `copy`; cleared by `edit --share all`.
- **Placed feature entries**: the file locations aprofile writes for enabled features,
  recorded in aprofile's data directory like other launch entries, including nested TOML
  subtree locations for `config.toml`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Enabling telemetry or automode-log on a profile and running `sync` places every
  mapped entry in one step, with no hand-editing of harness files; the same operation works for
  Claude Code and Codex profiles through identical commands.
- **SC-002**: Disabling a feature and re-running `sync` leaves the settings file byte-identical
  to its pre-enable state, apart from entries the harness or user changed in between.
- **SC-003**: `aprofile features <ref>` on any profile lists the harness's supported features
  and parameter values in under a second, without touching the filesystem beyond the profile
  record.
- **SC-004**: Every collision case in User Story 6 produces a hard error at mutation time or a
  reported problem at sync/show time; none is silently resolved.
- **SC-005**: Adding a hypothetical third feature to the catalog touches only the catalog, a
  harness definition and tests; a reviewer finds no new command code (FR-006).

## Assumptions

- This feature builds on spec 005's placed-entry mechanism (the auth-neutralizing entries and
  the record that keeps them), which lives on branch `005-neutralize-provider-switches` and is
  not yet merged into `main`; planning and implementation MUST start from a tree that includes
  spec 005.
- The Codex `[otel]` exporter enum's exact TOML encoding (inline table) is verified against a
  current Codex release during planning; the brainstorm's open question stands. If the encoding
  differs, only the capability mapping changes, not the model.
- `features` reminds the user to run `sync` but does not run it, matching `edit`'s contract.
- Whether `doctor` should warn about features depending on undocumented, version-dependent
  mechanisms (the automode log) is an open question from the brainstorm; this spec does not add
  such a warning.
- How `settings pull` treats feature-placed entries (overwrite and report as drift, or skip) is
  an open question from the brainstorm; until decided, `settings pull` follows its existing
  rules for placed entries.
- Secret-bearing parameters (OTLP headers, auth) do not exist in v1; the ref/literal question
  is deferred until one lands (brainstorm open question).
- Privacy/quiet mode, debug logging and proxy features are v2 candidates and out of scope here.
