# Feature Specification: Self-Contained Profile Folders

**Feature Branch**: `004-corgi-integration`

**Created**: 2026-11-09

**Status**: Draft

**Input**: Brainstorm `brainstorm/003-corgi-integration.md` (aprofile side of the Corgi integration):
every profile folder carries its own launch settings so a tool that only sets the harness's
config-folder variable launches it correctly; the registry gains the profile's model; wrappers stop
offering environment overrides.

## Context

Corgi (a terminal agent manager) and similar tools start an agent "account" by setting only the
harness's config-folder variable (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`) to a folder. aprofile keeps an
API-key profile's key reference, endpoint, model and model aliases only in the generated wrapper,
so such a folder starts without a key, against the wrong endpoint, on the wrong model. Profiles with
an interactive login have no gap: the login lives in the folder.

This feature makes every profile folder self-contained wherever the harness can read the setting
from its own files, keeps the wrapper as the way to start a profile from a shell, publishes the
profile's model in the registry, and replaces the wrapper's caller-override rule with a single
source per setting. A Corgi patch that reads the registry, launches through the wrapper and shows
model, label and color is a separate effort in another repository and out of scope here.

Harness facts this spec relies on (verified 2026-11-09, recorded in the brainstorm):

- Claude Code 2.1.319 applies the `env` block of the folder's `settings.json` before gateway model
  discovery; model aliases from the block apply; the block beats the launching environment; the
  block's `ANTHROPIC_MODEL` beats the `model` key that `/model` writes. The `apiKeyHelper` command's
  output is sent as both API key and bearer token. An exported `ANTHROPIC_API_KEY` or
  `ANTHROPIC_AUTH_TOKEN` in the launching environment beats the helper in its header; an empty
  entry for that variable in the `env` block neutralizes it, and the helper's key is sent in both
  headers.
- Codex 0.167.0 reads a provider's bearer token from a command
  (`[model_providers.<provider>.auth]`); `/model` saves the pick into `config.toml` by default;
  `-c model=<model>` beats `config.toml` and gives way to a caller's `-m`.

## Clarifications

### Session 2026-11-09

- Q: Where does aprofile record which settings-file entries it placed? → A: In aprofile's own data
  directory, per profile; never inside the harness's files.
- Q: Which wrapper variables are not launch settings and always stay in the wrapper? → A: The
  config-folder variable, the `APROFILE_*` contract variables, `HERDR_AGENT` and the auth-clearing
  lists. (Revised after review: `APROFILE_SET_VARS` may be carried by the folder, FR-014, and the
  folder also neutralizes the auth-clearing list when it carries the key, FR-006a.)
- Q: Must the key command be injection-safe and match the wrapper's reference semantics? → A: Yes:
  the reference is quoted so no value can inject shell code, and only the first line is output.
- Q: Should `list`, `show` and `doctor` report a folder whose placed entries differ from the
  profile? → A: Yes, as stale with the fix `aprofile sync`, like stale wrappers.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A Claude API-key profile folder launches on its own (Priority: P1)

The user registers `claude/work-deepseek`'s config folder in Corgi (or any tool that only sets
`CLAUDE_CONFIG_DIR`) and starts an agent from it. The agent talks to the profile's endpoint with the
profile's key and starts on the profile's model, exactly as the `claude-work-deepseek` wrapper does.

**Why this priority**: Claude is the main harness and six of the eleven daily profiles are Claude
profiles; without this, Corgi cannot launch any API-key profile.

**Independent Test**: Run `aprofile sync`, then start the harness with only the config-folder
variable set (empty environment otherwise) against a local stub endpoint, and observe endpoint,
key and model of the requests.

**Acceptance Scenarios**:

1. **Given** a Claude profile with auth kind `api-key`, a `file:` key reference, a model, an
   endpoint and extra variables, **When** the user runs `aprofile sync` and starts Claude Code with
   only `CLAUDE_CONFIG_DIR` set to the profile folder, **Then** model discovery and requests go to
   the profile's endpoint with the key from the referenced file, and the first request uses the
   profile's model.
2. **Given** the same profile, **When** the user inspects the folder's settings file, **Then** it
   holds the endpoint, model, aliases and extra variables plus a command that reads the key file,
   and no key value.
3. **Given** the user's own entries in the settings file's `env` block (for example tracing
   variables) and other settings keys, **When** `sync` runs, **Then** those entries and keys are
   unchanged.
4. **Given** the user picked a different model with `/model` in the last session, **When** a new
   session starts from the folder alone, **Then** it starts on the profile's model.
5. **Given** `sync` has run, **When** `sync` runs again with no profile change, **Then** it reports
   zero changes and writes no files.

---

### User Story 2 - A Codex API-key profile folder launches on its own (Priority: P2)

The user registers `codex/work-glm`'s folder in Corgi and starts an agent from it. Codex
authenticates against the profile's provider with the key from the referenced file, without any
key in the launching environment.

**Why this priority**: Codex is used occasionally; four daily profiles are Codex API-key profiles.

**Independent Test**: Run `sync`, start Codex with only `CODEX_HOME` set and no key variable
against a local stub endpoint, and observe the bearer token.

**Acceptance Scenarios**:

1. **Given** a Codex profile with auth kind `api-key`, a `file:` key reference and a config file
   that selects a custom provider, **When** `sync` runs and Codex starts with only `CODEX_HOME`
   set, **Then** requests carry the referenced file's key as bearer token.
2. **Given** that profile, **When** the user inspects `config.toml`, **Then** the selected
   provider's block holds a command that reads the key file and no key value; every other value
   in the file is kept (comments and key order may change, FR-004).
3. **Given** a Codex profile with a configured model, **When** it is started through its wrapper
   after a `/model` pick was saved as default, **Then** the session starts on the profile's model;
   a caller's own `-m` still wins for that run.

---

### User Story 3 - The registry tells tools each profile's model (Priority: P2)

A tool reading `registry.json` (the planned Corgi patch, a status line, a script) shows each
profile's configured model next to its label and color.

**Why this priority**: The Corgi patch and any picker need the model without parsing harness files.

**Independent Test**: Set a model on a profile, run `sync`, read `registry.json`.

**Acceptance Scenarios**:

1. **Given** a profile with a configured model, **When** `sync` writes the registry, **Then** the
   profile's entry carries that model.
2. **Given** a profile without a configured model, **When** `sync` writes the registry, **Then** the
   entry's `model` is an empty string and the format version is unchanged.
3. **Given** the user changed the model inside a session with `/model`, **When** `sync` runs,
   **Then** the registry still shows the profile's configured model.

---

### User Story 4 - One source per setting, no surprises from the shell (Priority: P2)

A user with a stale `export ANTHROPIC_MODEL=…` in their shell, or a user starting one profile's
wrapper from inside another profile's session, always gets the started profile's settings. One-run
changes go through the harness's own flags.

**Why this priority**: The settings file beats the launching environment for Claude, so the old
caller-override rule cannot hold; the replacement must be predictable and leak-free.

**Independent Test**: Export model and endpoint variables, or start a wrapper from a shell that has
another profile's variables, and observe what the harness receives.

**Acceptance Scenarios**:

1. **Given** the caller exports `ANTHROPIC_MODEL` and `ANTHROPIC_BASE_URL`, **When** the user runs
   `claude-work-deepseek`, **Then** the session uses the profile's model and endpoint.
2. **Given** a shell started inside a `claude-work-deepseek` session (so it carries that profile's
   endpoint, model and alias variables), **When** the user runs `claude-private`, **Then** none of
   those variables reach the harness.
3. **Given** the user wants another model for one run, **When** they run
   `claude-work-deepseek --model <x>` or `codex-work-glm -m <x>`, **Then** that run uses `<x>`.

---

### User Story 5 - Know whether a folder is self-contained, and register it with Corgi (Priority: P3)

The user checks which profiles a config-folder-only tool can launch correctly, then registers them
in Corgi by following the documentation.

**Why this priority**: Registration is manual by design; the user needs to know what will work and
how to set it up, including the one-time cleanup of old Corgi rows.

**Independent Test**: `aprofile show` and `doctor` on profiles of each kind; follow the documented
registration steps.

**Acceptance Scenarios**:

1. **Given** a profile whose every launch setting lives in its folder, **When** the user runs
   `aprofile show`, **Then** it says the folder is self-contained.
2. **Given** a profile with a setting the folder cannot carry (for example an extra variable given
   as a reference, an `oauth-token` auth kind, share level `all` with launch settings, a Codex
   provider the folder cannot authenticate, or a Codex model), **When** the user runs `show` or
   `doctor`, **Then** each setting that only the wrapper provides is named with the reason.
3. **Given** a profile whose key the folder carries through an `env:` reference, **When** the user
   runs `show` or `doctor`, **Then** the folder is reported as self-contained but dependent on the
   launching environment, naming the variable.
4. **Given** the documentation, **When** the user follows it, **Then** they can register each daily
   profile with Corgi's own account command and remove old rows (former cc-deck folders, duplicate
   accounts, saved custom commands) without aprofile touching Corgi.

### Edge Cases

- **Key referenced through a variable that aprofile clears** (for example
  `env:ANTHROPIC_AUTH_TOKEN`, or `env:OPENAI_API_KEY` as `import` produces for Codex): the folder
  would read an emptied variable, so the key stays wrapper-only (reason: the referenced variable
  is cleared), and `show` and `doctor` say so.
- **Key referenced through an environment variable (`env:NAME`)**: the folder carries the key
  command, which reads that variable; a tool launching from the folder works only if its own
  environment has it. The folder counts as self-contained but environment-dependent, and `show`
  and `doctor` report this as a note naming the variable.
- **Key reference file missing or empty**: launching through the wrapper fails before the harness
  starts with the existing message naming the profile and reference; launching from the folder
  alone fails in the harness with its own authentication error.
- **Auth kind `oauth-token` (Claude)**: the token cannot be supplied through the key command; it
  stays wrapper-only and is reported.
- **Auth kinds with an interactive login** (`oauth-login`, `chatgpt-login`): nothing to carry; the
  folder is self-contained for auth.
- **Extra variable given as a reference** (`env:`/`file:` value): never written into the folder (it
  would become a literal secret); it stays wrapper-only and is reported.
- **Share level `all`**: the settings file is linked to the default setup. aprofile never writes
  launch settings into the default setup's file; a profile at `all` with launch settings keeps
  them wrapper-only and is reported.
- **User entry with the same name as a launch variable** already in the `env` block, or a user's
  own key command: the profile's value replaces it; `sync` names the entry (never its value). The
  replaced user value is not restored if the launch setting is later removed.
- **Share level change**: moving a profile to `all` (its settings file becomes a link) drops its
  record of placed entries and makes those settings wrapper-only; moving from `all` to `content`
  or `none` gives the profile its own settings file, into which the next `sync` places the launch
  entries. The level change's dry run lists these effects.
- **Unreadable or unexpected harness file**: a settings file or `config.toml` that does not parse,
  or whose `env` block or provider block has an unexpected shape, is left unchanged; `sync`
  reports it naming the profile, file and key, keeps that profile's other outputs, and exits
  non-zero.
- **User edits or deletes an entry aprofile placed**: the next `sync` restores it.
- **Launch setting removed from the profile** (model unset, extra variable deleted, auth kind
  changed to a login kind): the next `sync` removes exactly the entries and key command aprofile
  placed and nothing else.
- **`settings pull` on a `content` profile**: the profile's own launch entries are present again
  after the operation completes.
- **Codex config without a custom provider** (default provider) and auth kind `api-key`: the
  folder cannot carry the key; wrapper-only, reported.
- **Codex provider block with an environment-variable key setting**: aprofile replaces that setting
  with the key command, so the provider never depends on a variable the wrapper no longer exports.
  aprofile records the replaced setting and restores it when it removes the `auth` block.
- **Selected Codex provider changes** (the user edits `model_provider`): the next `sync` removes the
  `auth` block from the old provider's block, restores that block's replaced key setting, and
  places the `auth` block in the newly selected provider's block.
- **A launch variable or key command in the settings file that aprofile did not place** (brought in
  by the one-time settings copy, `settings pull`, a hand edit, or the linked default file at
  share level `all`): `show` and `doctor` report that it overrides the profile's own value, naming
  the entry but never its value.
- **Session started from the folder alone** (for example by Corgi, without the wrapper): the
  folder also carries `APROFILE_SET_VARS`, so a wrapper started from a shell inside that session
  clears the session's launch variables.
- **Profile without a configured model**: no model entry is written, the registry's `model` is
  empty, and
  the harness keeps choosing (for Claude, a `/model` pick persists as before).
- **Harness without self-contained support** (OpenCode, pi): the wrapper keeps setting every
  variable; behavior is unchanged except the removal of caller overrides.

## Requirements *(mandatory)*

### Functional Requirements

**Where launch settings live**

- **FR-001**: A profile's launch settings are its model, endpoint, extra variables and auth key.
  For each launch setting, exactly one carrier MUST supply it at launch: the profile folder when
  the harness definition declares where the folder holds that kind of setting and the value can
  be held there without a literal secret, otherwise the wrapper. The config-folder variable, the
  `APROFILE_*` contract variables, `HERDR_AGENT` (spec 001 FR-029) and auth clearing are not
  launch settings: the wrapper always sets or clears them, because process-based detection reads
  the launch environment. `APROFILE_SET_VARS` is the one exception; FR-014 decides its carrier.
- **FR-002**: The places where a harness reads launch settings from its folder MUST be declared in
  the harness definition (Constitution Principle I): for Claude Code, entries of the `env` block of
  `settings.json` for model, endpoint and extra variables and the `apiKeyHelper` key for the auth
  key; for Codex, the selected provider's `auth` command block in `config.toml` for the auth key.
  The definition MUST also declare, per auth kind, whether the folder can carry that kind's key
  (Claude: `api-key` and `auth-token` yes, `oauth-token` no), and the launch variable names every
  wrapper of that harness clears before launching (Claude: the model, endpoint, model alias and
  subagent model variables). Definitions that declare none keep every setting in the wrapper. A
  user definition in `harnesses.d` MAY add or override these declarations.
- **FR-002a**: How a wrapper passes a setting other than through a variable MUST also be definition
  data: for Codex, the model is passed as a launch argument (FR-016). A setting passed this way is
  wrapper-only; the Codex model is wrapper-only because Codex rewrites the model in its own file.
- **FR-003**: `aprofile sync` MUST write each folder-carried launch setting into the profile folder
  and keep it there on every run. Profile creation and edits leave the folder stale until the next
  `sync`, like wrappers today.
- **FR-004**: aprofile MUST own only the entries it places: the launch variables it writes into
  the `env` block (including `APROFILE_SET_VARS`, FR-014), the key command, and the selected
  provider's `auth` block together with the key setting it replaced there (FR-009). Every other
  value of these files MUST be kept, including the user's own `env` entries and the `model` key
  the harness writes. Rewrites follow the existing config-patch behavior: in JSON files untouched
  values are kept byte for byte while key order and indentation are normalized; TOML files are
  re-encoded, so comments in `config.toml` are lost on the first write that changes the file, and
  `sync --dry-run` says so; key order in `config.toml` may change as well. aprofile MUST record which entries it placed, and any key setting it
  replaced, in its own data directory, per profile, and MUST NOT add marker keys or comments to
  the harness's files.
- **FR-005**: When an entry aprofile placed is no longer a launch setting of the profile, the next
  `sync` MUST remove exactly that entry; removing a Codex `auth` block restores the key setting it
  replaced. When the user changed or deleted an entry aprofile placed,
  the next `sync` MUST restore the profile's value. When a user's own entry exists before aprofile
  first places its counterpart (an `env` entry with the same name as a launch variable, a key
  command, or an `auth` block in the selected Codex provider), the profile's value MUST replace it
  and `sync` MUST name the entry (never its value); the replaced user value is not restored later.
- **FR-006**: The auth key MUST be carried as a command that outputs the first line of the
  referenced file (`file:` reference) or the value of the referenced variable (`env:` reference),
  producing exactly the value the wrapper resolves for the same reference (the wrapper reads the
  first line with the shell's `read` builtin without field splitting, spec 001 FR-028a; the
  command MUST reproduce that result for backslashes, trailing whitespace and a carriage return). The reference MUST be quoted in the command so no
  profile or definition value can inject shell code (as spec 001 FR-028b requires for wrappers).
  No key value MUST ever be written into a profile file (Constitution Principle IV).
- **FR-006a**: When the folder carries the auth key, it MUST also carry empty `env` entries for the
  harness's auth-clearing list (the auth variables of every auth kind and the definition's
  provider switches), as placed entries, so a key or provider switch exported in a launching
  tool's environment cannot override the folder's key. This is the folder's counterpart of the
  wrapper's auth clearing.
- **FR-007**: Extra variables whose value is a reference MUST NOT be written into the folder; they
  stay wrapper-only.
- **FR-008**: For a profile at share level `all` whose settings file is linked to the default setup,
  aprofile MUST NOT write launch settings into that file; those settings stay wrapper-only, and the
  file holds no aprofile-owned entries, so it stays linkable (Constitution Principle III as amended
  by FR-024).
- **FR-008a**: When a profile's share level changes to `all`, aprofile MUST drop the profile's
  record of placed entries (the settings file becomes a link, which aprofile never writes), and its
  launch settings become wrapper-only. When it changes from `all` to `content` or `none`, the next
  `sync` places the launch entries into the profile's new own settings file. `edit --share
  --dry-run` MUST list these effects (amends spec 002 FR-017).
- **FR-009**: For Codex, the selected provider is the one the top-level `model_provider` key of the
  profile's `config.toml` names. When that provider is defined in the file, aprofile MUST write its
  `auth` command block and replace an environment-variable key setting in the same block,
  recording the replaced setting (FR-004). When the file selects no such provider, the auth key
  stays wrapper-only. When the selected provider changes, the next `sync` moves the `auth` block to
  the new provider and restores the old provider's replaced setting.
- **FR-009a**: When a settings file or `config.toml` does not parse, or its `env` block or selected
  provider block has an unexpected shape, `sync` MUST leave the file unchanged, leave that
  profile's wrapper and registry entry unchanged, report it naming the profile, file and key, and
  exit non-zero (as spec 001 FR-022). Writes MUST re-read the file immediately before writing and replace it atomically,
  because the harness writes the same file at runtime.
- **FR-010**: `settings pull` (spec 002) MUST leave the profile's folder-carried launch entries in
  place once it completes.
- **FR-011**: `sync` MUST stay idempotent: an unchanged profile yields no writes (Constitution
  Principle II).

**Wrappers**

- **FR-012**: Wrappers MUST NOT export a launch setting that the profile folder carries. For such
  a variable the wrapper only clears it (clear-only), so the folder is the single source. When the
  folder carries the auth key, the wrapper clears the profile's own auth variable as well.
- **FR-013**: Wrappers MUST set every wrapper-only launch setting to the profile's value
  unconditionally; a caller's environment no longer overrides model, endpoint or extra variables
  (replaces spec 001 FR-027's `${VAR:-default}` rule). Auth handling (setting the profile's own auth
  variables when wrapper-only, clearing every other auth kind's variables and the definition's
  provider switches) stays as in spec 001 FR-027.
- **FR-014**: Before launching, every wrapper MUST clear the launch variables a profile session may
  have left in the environment, without comparing profile names: the launch variable names its
  harness definition declares (FR-002) and every variable named in an inherited
  `APROFILE_SET_VARS`. `APROFILE_SET_VARS` names every launch variable the profile supplies,
  whether the folder or the wrapper carries it. When the folder carries any launch variable, the
  folder also carries `APROFILE_SET_VARS` as a placed `env` entry, so sessions started from the
  folder alone (without the wrapper) pass it on to the shells they start; otherwise the wrapper
  exports it.
- **FR-015**: When the folder carries the auth key, the wrapper MUST still check that the
  reference resolves before launching and fail with the existing message naming the profile and
  reference (spec 001 FR-028a), but MUST NOT export the key.
- **FR-016**: For Codex, a wrapper of a profile with a configured model MUST pass the model as a
  configuration override placed before the caller's arguments, so a caller's own model flag wins
  and the wrapper never duplicates the caller's flag. The model MUST be passed as a quoted string
  value, so a model name such as `1.5` is never read as a number.
- **FR-017**: One-run changes MUST go through the harness's own flags; documentation and `--help`
  text that describe environment overrides MUST be updated accordingly.

**Registry**

- **FR-018**: Each registry entry MUST carry a `model` field: the profile's configured model, or an
  empty string when the profile has none (like the existing optional fields `icon` and
  `history_group`). The change is additive; the format version stays 1 and the
  registry documentation notes the new field (Constitution Principle V, public interface).
- **FR-019**: The registry model MUST come from the profile store only, never from a harness file.

**Reporting and documentation**

- **FR-020**: `aprofile show` (text and `--json`) MUST state whether the profile folder is
  self-contained, self-contained but environment-dependent (an `env:` key reference, naming the
  variable), or not self-contained; when it is not, it MUST name each wrapper-only launch setting
  with its reason (reference value, auth kind, share level `all`, provider not defined in the
  folder, passed by the wrapper because the harness rewrites it, key referenced through a variable
  aprofile clears, harness without support).
  `doctor` MUST report the same as information, not as a problem.
- **FR-020a**: When a profile's placed entries differ from what `sync` would write (the profile
  changed, or the user edited or removed a placed entry), `list`, `show` and `doctor` MUST report
  the folder as stale with the fix `aprofile sync`, as they do for stale wrappers (spec 001
  FR-006b).
- **FR-020b**: When a profile's settings file holds a launch variable or key command that aprofile
  did not place (brought in by the settings copy, `settings pull`, a hand edit, or the linked
  default file at share level `all`), `show` and `doctor` MUST report that it sets a launch
  variable the profile does not set or, at share level `all`, overrides the value the wrapper sets,
  naming the entry but never its value. `doctor` reports it as a problem with the fix "remove the
  entry or set it on the profile".
- **FR-021**: `sync --dry-run` MUST list the folder entries it would add, change or remove, naming
  entries but never key values.
- **FR-022**: The documentation MUST describe registering profiles with Corgi using Corgi's own
  account command and the folder path from `aprofile show` or the registry, the one-time cleanup
  of old Corgi rows, and what stays wrapper-only. aprofile MUST NOT run, configure or write into
  Corgi (spec 001 FR-032).
- **FR-023**: Codex profiles MUST accept a model through the existing `--model` option, now that
  the Codex definition declares how the model is passed (spec 001 FR-006 no longer reports a Codex
  model as dropped).

**Constitution**

- **FR-024**: The constitution MUST be amended to version 2.0.0:
  Principle II allows aprofile-owned launch entries inside a harness's settings files (entries it
  placed, preserved rest); Principle I lists as definition data the folder locations of launch
  settings, how the wrapper passes a setting as a launch argument, the per-auth-kind flag whether
  the folder can carry the key, and the launch variable names wrappers clear; Principle III states that launch entries are owned only where the folder carries them, and
  a settings file linked at `all` carries none, so it stays linkable; Principle V replaces the
  caller-override rule with: "A profile's settings are fixed for every launch: the folder (or,
  where the folder cannot hold a setting or the harness rewrites it, the wrapper) is the single
  source. One-run changes go through the harness's own flags. Wrappers clear the launch variables
  a profile session may have set, so inherited values never leak into a launch." Exec-style
  wrappers, pull-only integration and the registry as a public interface stay. Spec 001 (FR-006,
  FR-026, FR-027, FR-030) and spec 002 (FR-009a, FR-010, FR-017, FR-018) MUST carry notes pointing to the
  superseding requirements, and the Claude Code definition's classification comment for
  `settings.json` is updated to match.

### Key Entities

- **Launch setting**: one of model, endpoint, extra variable or auth key of a profile, with its
  carrier (folder or wrapper) and, when wrapper-only, the reason.
- **Folder launch locations** (harness definition data): where a harness reads each kind of launch
  setting from its folder, if at all.
- **Placed entry**: an entry aprofile wrote into a harness settings file; aprofile updates and
  removes only these. The record of placed entries lives in aprofile's data directory, per
  profile.
- **Registry entry**: existing fields plus `model` (empty string when unset).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Test fixtures modeled on the nine API-key daily profiles (`claude/deepseek`,
  `claude/glm`, `claude/work`, `claude/work-deepseek`, `claude/work-glm`, `codex/deepseek`,
  `codex/glm`, `codex/work-deepseek`, `codex/work-glm`) start from their folder alone with the
  correct endpoint and key, verified against a stub endpoint: 9 of 9 (tests never touch the real
  home, Constitution Principle VII).
- **SC-002**: Every Claude profile with a configured model starts on that model from its folder
  alone even after a different `/model` pick, and every Codex profile with a configured model does
  so through its wrapper: 100% in tests.
- **SC-003**: Zero key values appear in any file aprofile writes, checked by a test that scans every
  written file for the test key.
- **SC-004**: User-owned entries and keys in the touched settings files are preserved in 100% of
  sync, edit, removal and `settings pull` scenarios covered by tests.
- **SC-005**: A second `sync` without changes writes zero files.
- **SC-006**: No launch variable of one profile reaches a harness started through another profile's
  wrapper from inside the first profile's session, in 100% of tested pairs.
- **SC-007**: Following the documentation, the user registers all eleven daily profiles in Corgi
  and removes the old rows in under 10 minutes (verified manually in the walkthrough).

## Assumptions

- Harness versions are at least Claude Code 2.1.319 and Codex 0.167.0; the verified behaviors
  above hold for them. Older versions are not supported for folder-carried settings.
- A Claude key command's output is accepted for both the `api-key` and the `auth-token` auth kinds
  (verified: it is sent as both API key and bearer token).
- The key command may use standard system utilities available on Linux and macOS; it runs when the
  harness starts, not when aprofile runs.
- Codex model support is added only as a launch argument; aprofile does not write `model` into
  `config.toml`, because Codex rewrites it on `/model` (never fight the harness). A Codex folder
  started without the wrapper therefore follows the last saved `/model` pick (accepted drift).
- Corgi's displayed model for a Claude folder reads the harness-written `model` key and may show a
  stale `/model` pick; accepted, and fixed by the separate Corgi patch.
- OpenCode and pi gain no folder-carried settings in this feature.
- The Corgi-side patch, the color Trick and any Corgi changes are out of scope.
