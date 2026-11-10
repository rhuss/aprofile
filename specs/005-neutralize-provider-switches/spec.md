# Feature Specification: Neutralize Provider Switches for Every Profile Folder

**Feature Branch**: `005-neutralize-provider-switches`

**Created**: 2026-11-10

**Status**: Draft

**Input**: Brainstorm `brainstorm/006-neutralize-provider-switches.md`: the profile folder blanks
inherited auth variables and provider switches only when it carries the key, so login profiles
launched from the folder alone inherit whatever the launcher's environment exports.

## Context

Spec 004 made profile folders self-contained so tools that only set the harness's config-folder
variable (Corgi, for example) launch a profile correctly. Its FR-006a gives the folder a
counterpart of the wrapper's auth clearing: empty `env` entries for the harness's auth-clearing
list (the auth variables of every auth kind plus the definition's provider switches, such as
`CLAUDE_CODE_USE_VERTEX`). FR-006a applies only when the folder carries the auth key. A profile
with an interactive login, a profile whose token is wrapper-only (`oauth-token`) and an API-key
profile whose key stays in the wrapper get no protection: launched from the folder alone, they run
with whatever provider switches and keys the launching tool exports.

Observed on 2026-11-10: Corgi's daemon still carried `CLAUDE_CODE_USE_VERTEX=1` from an old shell,
and the `claude/private` profile (Claude Max login) started on Google Vertex AI instead of the
login's subscription. That profile is at share level `all`, where the settings file is the linked
default setup and aprofile never writes into it (spec 004 FR-008). The folder cannot protect such
a profile; it can only say so.

Harness facts this spec relies on (spec 004 research R1 and R2, Claude Code 2.1.319): the `env`
block of the folder's `settings.json` beats the launching environment, and an empty entry for a
variable neutralizes an exported value (an empty `CLAUDE_CODE_USE_VERTEX` entry sends a launch
back to the default provider). Codex has no `env` block in `config.toml`.

## Clarifications

### Session 2026-11-10

- Q: Does a login profile at `content` or `none` whose folder holds only neutralizing entries count
  as self-contained? → A: Yes; neutralizing entries do not change the status rules of spec 004
  FR-020.
- Q: Do `list`, `show` and `doctor` all reflect the FR-005 status for a login profile at `all`?
  → A: Yes; all three use the same folder status, so `list` shows "not self-contained" too.
- Q: How does the FR-005 wrapper-only item appear in `show --json`? → A: As a wrapper-only setting
  named `auth-clearing` with no variable and the existing reason `share-all`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A login profile launched from its folder ignores exported provider switches (Priority: P1)

The user has `claude/work-login`, an interactive-login profile at share level `content`,
registered in Corgi by its folder path. Corgi's daemon environment exports
`CLAUDE_CODE_USE_VERTEX=1` and `ANTHROPIC_API_KEY` from an old shell. After `aprofile sync`, a
session Corgi starts from the folder uses the profile's login against the default provider,
exactly like a session started through the wrapper.

**Why this priority**: This is the failure that motivated the feature: a subscription login
silently routed to a cloud provider and billed there.

**Independent Test**: Create an interactive-login profile at `content`, run `aprofile sync`,
then start the harness with only the config-folder variable set and the provider switch exported.
The folder's settings file holds empty entries for the auth-clearing list, and the session does
not use the exported switch or key.

**Acceptance Scenarios**:

1. **Given** an interactive-login Claude profile at share level `content` or `none`, **When**
   `aprofile sync` runs, **Then** its settings file holds an empty `env` entry for every name in
   the harness's auth-clearing list, recorded as entries aprofile placed.
2. **Given** that profile, **When** it is launched from the folder alone with
   `CLAUDE_CODE_USE_VERTEX=1` exported, **Then** the session does not use Vertex AI.
3. **Given** that profile after `sync`, **When** `sync` runs again, **Then** nothing changes.
4. **Given** that profile, **When** the user deletes one of the empty entries by hand, **Then**
   `list`, `show` and `doctor` report the folder as stale and the next `sync` restores the entry.

---

### User Story 2 - The wrapper keeps working for profiles whose values come from the wrapper (Priority: P1)

The user has `claude/work-token`, an `oauth-token` profile whose token reference the wrapper
resolves into `CLAUDE_CODE_OAUTH_TOKEN`, and `claude/vertex-login`, a login profile that sets
`CLAUDE_CODE_USE_VERTEX=1` as an extra variable on purpose. After `sync`, both still start
correctly through their wrappers and from their folders' perspective nothing they supply is
blanked.

**Why this priority**: An empty folder entry beats the value the wrapper sets. Blanking a name
the profile itself supplies would break ordinary wrapper launches, which is worse than the gap
being closed.

**Independent Test**: Create an `oauth-token` profile and a login profile with an extra provider
switch, run `sync`, and check that the folder holds no empty entry for `CLAUDE_CODE_OAUTH_TOKEN`
or for the extra variable, and that a wrapper launch sees the token and the switch.

**Acceptance Scenarios**:

1. **Given** an `oauth-token` profile, **When** `sync` runs, **Then** the folder carries empty
   entries for the auth-clearing list except `CLAUDE_CODE_OAUTH_TOKEN`.
2. **Given** an API-key profile whose key stays wrapper-only (for example a key referenced through
   a variable aprofile clears), **When** `sync` runs, **Then** the folder carries no empty entry
   for that profile's own auth variable.
3. **Given** a profile that sets a name from the auth-clearing list as an extra or exec-hook
   variable, **When** `sync` runs, **Then** that name is not blanked (unchanged from spec 004).
4. **Given** an API-key profile whose folder carries the key, **When** `sync` runs, **Then** the
   folder's entries are exactly what spec 004 FR-006a places today.

---

### User Story 3 - A login profile at share level `all` says it is unprotected (Priority: P2)

The user's primary profile `claude/private` is an interactive-login profile at share level `all`.
`aprofile show claude/private` and `aprofile doctor` tell the user that, launched from the folder
alone, the profile inherits provider switches and keys the launching tool exports, and point to
the wrapper or a share level change as the fix. Profiles at `all` with other auth kinds show no
new message.

**Why this priority**: The incident profile cannot be protected by the folder without writing into
the user's default setup, which aprofile never does; naming the gap is the most aprofile can do.

**Independent Test**: Create an interactive-login profile at `all` and an API-key profile at
`all`; `show` and `doctor` report the gap for the first only.

**Acceptance Scenarios**:

1. **Given** an interactive-login profile at share level `all`, **When** the user runs `show`
   (text and `--json`), **Then** the folder status is "not self-contained" and auth clearing is
   listed as wrapper-only with the reason share level `all`.
2. **Given** the same profile, **When** the user runs `doctor`, **Then** it reports the gap as
   information (not a problem, so the exit status is unaffected) with a fix naming the wrapper and
   `aprofile edit --share content`.
3. **Given** an API-key or `oauth-token` profile at share level `all`, **When** the user runs
   `show` or `doctor`, **Then** no auth-clearing message appears (their wrapper-only key is
   reported as today).
4. **Given** any profile at share level `all`, **When** `sync` runs, **Then** nothing is written
   into its linked settings file.

---

### User Story 4 - Codex login profiles are checked and any gap is named (Priority: P3)

Codex profiles have no `env` block, so the folder cannot neutralize anything. The team verifies
whether a ChatGPT-login Codex session honors an exported `OPENAI_API_KEY` or `OPENAI_BASE_URL`.
If it does, `show` and `doctor` report the gap for ChatGPT-login Codex profiles the same way as
for Claude login profiles at `all`; if it does not, nothing changes for Codex.

**Why this priority**: No incident is known for Codex; the change is conditional on a finding.

**Independent Test**: Launch a ChatGPT-login Codex profile from its folder with both variables
exported against a logging stub, and record which endpoint and credential it uses.

**Acceptance Scenarios**:

1. **Given** the verification shows Codex honors an exported variable for a ChatGPT login,
   **When** the user runs `show` or `doctor` on a ChatGPT-login Codex profile, **Then** the gap is
   reported as information naming the honored variables.
2. **Given** the verification shows Codex ignores them, **When** the user runs `show` or `doctor`,
   **Then** Codex output is unchanged.

---

### Edge Cases

- A profile's own settings file already holds a non-empty entry for a name on the auth-clearing
  list that aprofile did not place (copied once from the default setup, pulled with `settings
  pull`, or added by hand). The profile's empty entry replaces it, `sync` names the entry (never
  its value), and the replaced value is not restored later (spec 004 FR-004). A profile that wants
  a provider switch sets it with `--env`, which exempts the name.
- A profile changes from share level `all` to `content` or `none`: the next `sync` places the
  neutralizing entries into the new own settings file. Changing to `all` drops the record and the
  entries go with the replaced file (spec 004 FR-008a).
- A profile's auth kind changes (for example from `api-key` to `oauth-token`): the next `sync`
  removes the empty entry for the new kind's own variable and places the one for the old kind's.
- A profile's extra variables change so a name on the list is newly supplied or no longer
  supplied: the next `sync` removes or places the empty entry.
- A profile's own settings file does not parse: `sync` fails for that file as spec 004 FR-009a
  describes; nothing is written.
- Existing profiles: right after upgrading, every `content` and `none` profile without a folder
  key is reported stale until the next `sync` places the new entries.
- A user's harness definition override changes the auth-clearing list: the next `sync` follows the
  new list.

## Requirements *(mandatory)*

### Functional Requirements

**Neutralizing entries (amends spec 004 FR-006a)**

- **FR-001**: For every profile whose harness declares a folder `env` location and whose settings
  file is the profile's own (share level `content` or `none`), the folder MUST carry an empty
  `env` entry for every name on the harness's auth-clearing list (the auth variables of every auth
  kind and the definition's provider switches), independent of the profile's auth kind and of
  whether the folder carries the key.
- **FR-002**: The folder MUST NOT carry an empty entry for a name the profile supplies through the
  wrapper: the profile's own auth variables when its key or token is wrapper-carried, and the
  profile's extra and exec-hook variables. When the folder carries the key, the existing spec 004
  FR-006a behavior stays unchanged.
- **FR-003**: The neutralizing entries MUST be placed entries in the sense of spec 004: recorded in
  aprofile's data directory, kept and restored by `sync`, removed when the profile no longer wants
  them, reported as stale when they differ, and listed by `sync --dry-run`. Repeated `sync` runs
  MUST yield the same files (Constitution Principle II).
- **FR-004**: For a profile at share level `all`, aprofile MUST NOT write neutralizing entries
  (spec 004 FR-008 unchanged).

**Reporting**

- **FR-005**: For a profile at share level `all` with an interactive login kind (an auth kind that
  sets no variables, such as `oauth-login` or `chatgpt-login`) whose harness clears auth variables,
  `list`, `show` (text and `--json`) and `doctor` MUST report the folder as not self-contained;
  `show` MUST list auth clearing as wrapper-only (in `--json`: setting `auth-clearing`, no variable,
  reason `share-all`) with the reason share level `all` (the settings file is linked to the default
  setup), and `doctor` MUST report the same as information with a fix naming the profile's wrapper
  and `aprofile edit --share content`.
- **FR-005a**: A profile at `content` or `none` whose folder holds only neutralizing entries (no
  other launch settings) MUST be reported as self-contained; neutralizing entries do not change
  the status rules of spec 004 FR-020.
- **FR-006**: Profiles at share level `all` with any other auth kind MUST NOT get the FR-005
  message.
- **FR-007**: Messages and `--json` output MUST name variables and entries, never values
  (Constitution Principle IV).

**Codex**

- **FR-008**: The team MUST verify, on a current Codex release, whether a ChatGPT-login Codex
  session launched from its folder alone honors an exported `OPENAI_API_KEY` or
  `OPENAI_BASE_URL`, and record the result in the feature's research notes and in the Codex
  harness definition's comments.
- **FR-009**: If the verification shows an exported variable is honored, `show` and `doctor` MUST
  report the gap for ChatGPT-login Codex profiles at every share level, as FR-005 does, naming the
  honored variables. If not, Codex behavior MUST stay unchanged.

**Documentation**

- **FR-010**: Spec 004 FR-006a and the user documentation (README and the Corgi guide) MUST
  describe the widened neutralization and the gap at share level `all` for login profiles.

### Key Entities

- **Auth-clearing list**: the harness definition's names that never leak from a launcher's
  environment into a profile: the auth variables of every auth kind and the provider switches.
- **Neutralizing entry**: an empty `env` entry in the profile's own settings file for one name on
  the auth-clearing list, placed and recorded by aprofile.
- **Supplied name**: a variable the profile sets itself through the wrapper (its wrapper-carried
  auth variable, extra variables, exec-hook variables); never neutralized.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For every Claude profile at share level `content` or `none`, whatever its auth kind,
  a launch from the folder alone with any name from the auth-clearing list exported uses the same
  provider as a launch through the wrapper.
- **SC-002**: For every profile, a launch through the wrapper behaves exactly as before the change
  (no supplied value is lost).
- **SC-003**: `sync` run twice in a row changes nothing on the second run, for every profile.
- **SC-004**: Every interactive-login profile at share level `all` is reported as not
  self-contained by `show` and `list`, and `doctor` names the gap without changing its exit status.
- **SC-005**: No file aprofile writes or prints contains a value from the launching environment or
  a profile secret.

## Assumptions

- "Interactive login kind" means an auth kind whose definition sets no variables (`oauth-login`,
  `chatgpt-login`); `oauth-token` is excluded from FR-005 because its token is already reported as
  wrapper-only. (Brainstorm decision; the exclusion is the brainstorm author's call.)
- The `doctor` report for FR-005 is information, not a problem, matching how spec 004 FR-020
  reports wrapper-only settings, so a primary profile at `all` does not make `doctor` fail
  permanently.
- Replacing a user's own entry for a name on the auth-clearing list follows spec 004 FR-004; such
  entries are already reported as problems today (spec 004 FR-020b), with the fix to set them on
  the profile.
- Fixing Corgi to launch through the wrapper is out of scope (Constitution Principle V: aprofile
  never requires changes in Corgi).
