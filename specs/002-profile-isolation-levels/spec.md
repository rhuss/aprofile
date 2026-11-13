# Feature Specification: Profile Share Levels and Hook Sets

**Feature Branch**: `002-profile-isolation-levels`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "Profile isolation levels, hook sets, and corrected harness
classification for all four harnesses (Claude Code, Codex, OpenCode, pi). Understandability is a
first-class requirement: users must be able to pick the right isolation level for their use case
without reading internals." (Full description, including the verified list of leaking entries, is
recorded in the conversation that produced this spec and summarized under Background.)

## Terminology

Terms from slice one (`specs/001-aprofile-standalone-profile-cli/spec.md`) keep their meaning:
harness, profile, harness definition, wrapper, default setup, layers. New terms:

- **Share level**: a per-profile setting, chosen with `--share`, that answers "how much of my
  default setup does this profile use?". There are exactly three levels: `all`, `content` and
  `none`.
- **Private**: an entry that exists only inside the profile's config dir and is never linked to
  anything. Credentials and runtime state are private at every share level.
- **Content**: read-mostly material a user writes by hand and wants reused, such as global
  instructions (`CLAUDE.md`, `AGENTS.md`), skills, agents, commands, prompts, output styles, themes
  and keybindings.
- **Settings**: configuration the harness itself also writes at runtime, such as permissions and
  "always allow" decisions, the enabled plugin set and the status line.
- **Plugin store**: where a harness keeps installed plugin or extension code and its install
  records (for example Claude Code's `plugins/`). Which plugins are enabled is a setting; which
  ones are installed is the plugin store.
- **Hook location**: where a harness keeps its hook configuration (a key of a settings file, a
  hooks file, or a directory of plugin or extension files). It is managed separately from
  settings.
- **Hook set**: a named, reusable group of hooks kept in one central library, with one body per
  harness, that profiles opt into by name.
- **Managed hook entry**: a hook entry that aprofile placed into a profile because of a hook set.
  Every other hook entry in the profile belongs to the user or to another tool.
- **`$HOME` escape**: a location a harness reads relative to the home directory rather than its
  config dir, so no share level can separate it (for example `~/.agents/skills`).

## Clarifications

### Session 2026-11-04

- Q: What are the three levels called? → A: A `--share` flag with the values `all`, `content` and
  `none`, read as an answer to "what does this profile share with my default setup?". Because
  "all" could suggest that logins and history are shared, every description of `all` states that
  they stay private (FR-014).
- Q: How do the settings of a `content` profile relate to the default setup over time? → A: They
  are copied once when the profile is created and are the profile's own from then on.
  `aprofile settings pull` refreshes them on request; `show` and `doctor` say when they differ from
  the default setup. Hooks are not part of this copy; hooks reach profiles through hook sets.
- Q: Do hook sets marked "applies to every profile" also apply to `none` profiles? → A: No. A
  `none` profile gets only the hook sets it names.
- Q: How should a profile's skills directory behave at each share level? → A: Skills are content:
  linked at `all` and `content`, absent at `none`; per-profile skills exist only at `none`. As a
  consequence, a `none` profile borrows nothing from the default setup, not even a one-time copy
  of MCP servers (FR-011).
- Q: Which share level should profiles created by `aprofile import cc-deck` get? → A: The same as
  `add`: the global default, or `--share` when given (FR-007). Removing `import` altogether is
  tracked separately in `brainstorm/idea-inbox.md` (`remove-cc-deck-import`).
- Q: When `settings pull` refreshes a `content` profile, what happens to settings that exist only
  in the profile? → A: They are kept; pull merges by default. `--replace` makes the profile's
  settings an exact copy of the default setup's, hook location and owned keys excluded (FR-018).

### Spec review 2026-11-04

Fixes accepted by the user: a settings file that also holds owned keys stays private at every
level, and the per-harness meaning of `all` is shown (FR-009a, FR-022); the plugin store is its
own category, linked at `all` and `content` and private at `none`, and is the one writable entry
SC-002 allows (FR-009); `edit --share` applies the change itself, `sync` performs only the
non-destructive part (FR-017a); entries other tools write into the default location are verified
before they become private (FR-005, Edge Cases); `copy` across levels (FR-007a); file-form hooks in
`hook-sets import` (FR-031); excluding an every-profile set from a `content` profile (FR-026);
SC-004 is a manual walkthrough check.

## Background

Slice one gives every profile private credentials and private state, and links "everything else"
to the default setup. For Claude Code and Codex, "everything else" is a catch-all rule, so every
entry the definition forgot is linked. On a real machine (2026-11-03) this linked the following
runtime state into the default setup:

- Claude Code: `history.jsonl` (the definition lists a non-existent `history/`), `sessions/`,
  `session-env/`, `file-history/`, `todos/`, `shell-snapshots/`, `tasks/`, `plans/`,
  `paste-cache/`, `debug/`, `telemetry/`, `jobs/`, `daemon/`, `ide/`, `backups/`.
- Codex: `memories_1.sqlite`, `logs_2.sqlite`, `goals_1.sqlite`, `queue_1.sqlite` (each with its
  write-ahead and shared-memory companion files), `app-server-daemon/`, `thread-writer-locks/`,
  `session_index.jsonl`, `shell_snapshots/`, `hooks.json`; Codex writes `log/` while the
  definition lists `logs/`.

Two profiles running at the same time therefore mix prompt history and memory across accounts,
and share live databases and lock directories. The linked settings file also means a permission
granted or a plugin enabled in one profile applies to all of them, which contradicts the slice one
README (it calls the enabled plugin set per profile). Finally, a harness that saves a file by
writing a temporary file and renaming it over the original replaces the link with a real file;
the profile then silently stops sharing, and neither `sync` nor `doctor` says so.

OpenCode and pi have no catch-all rule; their unclassified entries are already reported instead of
linked. This feature brings all four harnesses to that behavior and adds a user-facing choice of
how much each profile shares.

## User Scenarios & Testing *(mandatory)*

### Use cases and the recommended share level

The share levels exist for these use cases. The README decision guide and the in-tool help MUST
present this mapping (FR-012). At every level, logins, history, sessions, memory, logs and caches
are private to the profile; the level only decides how much of the user's setup the profile uses.

| # | Use case | Recommended | Why |
|---|----------|-------------|-----|
| UC1 | Work and personal accounts of the same harness, used by one person, often running at the same time | `--share content` | Same instructions and agents everywhere, but a permission granted at work does not appear in the personal account, and no live settings file is written by two accounts at once |
| UC2 | A client or consultancy account that must never see personal memory, history, instructions or plugins (compliance) | `--share none` | Nothing is shared; `show` and `doctor` list the remaining `$HOME` escapes honestly |
| UC3 | A throwaway profile for trying a plugin, hook or new model endpoint | `--share content` | Plugin and settings experiments stay in the profile; `remove --purge` throws it away |
| UC4 | One person using Claude Code and Codex with a common set of hooks | `--share content` for each profile, plus the same hook sets | Hook sets carry one body per harness; content stays per harness (see Assumptions) |
| UC5 | A profile for a local or alternative endpoint (vLLM, a gateway) that should behave exactly like the default setup except for credentials and model | `--share all` | Settings, plugins and hooks stay identical to the default setup and change with it |
| UC6 | A demo or recording profile with nothing personal on screen | `--share none` | History is clean at every level; `none` also keeps personal instructions, permissions and plugins out of view |
| UC7 | A CI or automation profile with only explicitly chosen hooks | `--share none` with named hook sets | Predictable: no borrowed settings, and only the hook sets the profile names |

### What each share level does

| Category | `all` | `content` | `none` |
|----------|-------|-----------|--------|
| Credentials | private | private | private |
| Runtime state (history, sessions, memory, logs, caches, databases, locks) | private | private | private |
| Owned (MCP servers, model and endpoint) | private, travels with `copy`; MCP servers start as a copy of the default setup's | private, travels with `copy`; MCP servers start as a copy of the default setup's | private, travels with `copy`; starts empty |
| Content (instructions, skills, agents, commands, prompts, themes, keybindings) | linked to the default setup | linked to the default setup | not present; harness defaults apply; per-profile skills live here |
| Settings (permissions, enabled plugins, status line) | linked: one live file shared with the default setup, except a settings file that also holds owned keys (FR-009a) | the profile's own copy, taken from the default setup once at creation | the profile's own, starting minimal |
| Plugin store (installed plugin code) | linked | linked; which plugins are enabled stays per profile | private; plugins are installed per profile |
| Hooks | the default setup's hooks (a separate hook file is linked; hooks inside a linked settings file come with it) | hook sets: named ones plus those that apply to every profile, minus excluded ones | hook sets: named ones only |

Note (spec 003, history groups): the runtime state row has one opt-in exception. The entries a
harness definition declares as history are linked to the group store for a member of an
explicitly chosen history group, at every level; credentials and all other state stay private,
and a profile in no group keeps this table unchanged. See
`specs/003-history-groups/spec.md` (FR-025) and Constitution 1.2.0.

What `all` shares therefore differs per harness (for example, Codex keeps MCP servers inside its
settings file, so that file stays private). `aprofile harness show <name>`, `aprofile show` and
`aprofile help sharing` state per harness what each level shares (FR-022).

### User Story 1 - Profiles running side by side never share runtime state (Priority: P1)

A user runs `claude-work` and `claude-private` in two terminals, and `codex-client` in a third.
Prompt history, sessions, memory, logs, caches, databases and lock files of each profile stay
inside that profile, at every share level, including profiles created by slice one. When a
harness update adds an entry the definition does not know, the entry stays where the harness put
it and is reported; it is never linked.

**Why this priority**: separate history, memory and billing per account is why aprofile exists
(Constitution Principle III). Today this promise is broken for Claude Code and Codex.

**Independent Test**: on a temporary home seeded with the verified entries from Background, create
one profile per built-in harness at each share level, run `sync`, and confirm that no state or
credentials entry is a link and that every linked entry is classified as content, settings,
plugin store or hook location.

**Acceptance Scenarios**:

1. **Given** a Claude Code profile and a default setup containing `history.jsonl`, `sessions/` and
   `todos/`, **When** the user runs `sync`, **Then** these entries are private to the profile and
   are not links.
2. **Given** a Codex profile and a default setup with `memories_1.sqlite`,
   `memories_1.sqlite-wal` and `memories_1.sqlite-shm`, **When** the user runs `sync`, **Then** the
   database and its companion files are all private; none of them is linked.
3. **Given** a definition without a rule for a new entry `foo.json` the harness created, **When**
   the user runs `sync` or `doctor`, **Then** `foo.json` is left untouched, is not linked, and is
   reported with the profile, the entry and a suggested fix.
4. **Given** a slice one profile whose `history.jsonl` is a link into the default setup, **When**
   the user runs `sync`, **Then** the link is reported as leaking and `sync --fix-links` replaces
   the link (never its target) with an empty private entry.

---

### User Story 2 - Pick a share level for a new profile from a use case (Priority: P1)

A user creating a profile does not know what "layers" are. `aprofile add`, the TUI and
`aprofile help sharing` describe each level in one plain sentence and point to the use case
table. The user picks a level, or accepts the default, and gets a profile that behaves as the
description promised.

**Why this priority**: a correct mechanism the user cannot choose correctly still leaks; this is
the user's stated first-class requirement.

**Independent Test**: show the help topic and the `add` flag help to someone who has not read the
README, give them the seven use cases, and check that they choose the recommended level for each;
then create one profile per level and check that `show` matches the level table.

**Acceptance Scenarios**:

1. **Given** no `--share` flag, **When** the user runs `aprofile add claude work`, **Then** the
   profile gets the default level (`content`) and the output names the level and its one-line
   description.
2. **Given** the TUI's add form, **When** the user moves through the share choice, **Then** each
   level shows its one-line description and the use cases it fits.
3. **Given** the user runs `aprofile help sharing`, **Then** the terminal shows the level table,
   the use case table and the sentence "logins, history, sessions and memory are always private".
4. **Given** the user runs `aprofile add claude ci --share none`, **When** they run `sync`,
   **Then** the profile has no content links, its own minimal settings, and private state.
5. **Given** a default setup whose settings contain permissions and a status line, **When** the
   user creates a `--share content` profile and runs `sync`, **Then** the profile's settings start
   as a copy of those values, without the default setup's hooks.

---

### User Story 3 - See, preview and refresh what a profile shares (Priority: P2)

A user wants to know, for one profile, what is private, what is linked to the default setup and
what is the profile's own copy, and why. Before changing a profile's share level, or refreshing
its settings from the default setup, the user wants to see exactly what will change.

**Why this priority**: understandability does not end at creation; users must be able to verify
and change their choice safely.

**Independent Test**: for a profile at each level, `show` lists every entry with its treatment and
reason; `edit --share <level> --dry-run` and `settings pull --dry-run` list the changes; applying
them performs exactly those changes and nothing else.

**Acceptance Scenarios**:

1. **Given** a `content` Claude Code profile, **When** the user runs `aprofile show claude/work`,
   **Then** each entry shows one treatment (private, linked, own copy, managed by hook set,
   unclassified, `$HOME` escape) and a short reason such as "runtime state: always private".
2. **Given** an `all` profile, **When** the user runs
   `aprofile edit claude/work --share content --dry-run`, **Then** the output lists each entry
   that would change, how, and confirms that nothing is deleted.
3. **Given** a `content` profile with its own settings, **When** the user moves it to `all`,
   **Then** aprofile shows that the profile's own settings would be replaced by the shared file,
   backs the profile's file up, and asks for confirmation before replacing it.
4. **Given** a `content` profile whose settings differ from the default setup, **When** the user
   runs `aprofile settings pull claude/work --dry-run`, **Then** the output lists the keys that
   would change; without `--dry-run`, aprofile backs up the profile's file, asks for
   confirmation, and leaves the profile's hook configuration untouched.
6. **Given** a `content` profile with a permission granted only in that profile, **When** the user
   runs `settings pull`, **Then** the permission is still there afterwards; with `--replace`, the
   dry run lists it as removed.
5. **Given** `--json`, **When** the user runs `show`, **Then** the per-entry treatment and reason
   are part of the stable output schema.

---

### User Story 4 - Choose hooks per profile with hook sets (Priority: P2)

A user keeps hooks in one place but wants only some of them in some profiles: a Jira hook only in
the work profile, a token-saving hook in every profile, nothing extra in the CI profile. Hook
scripts stay in one location; only which sets a profile uses differs.

**Why this priority**: shared hooks are critical for the user, and per-profile hooks only become
possible once settings stop being a single shared file.

**Independent Test**: define two hook sets, attach one to profile A and none to profile B, run
`sync`, and verify A's hook configuration contains exactly the managed entries of that set plus
any pre-existing user entries, and B's contains none of them.

**Acceptance Scenarios**:

1. **Given** hook sets `jira` and `rtk` in the library, **When** the user runs
   `aprofile edit claude/work --hook-sets jira,rtk` and `sync`, **Then** the work profile's hook
   configuration contains the entries of both sets and still contains every hook entry the user
   added by hand.
2. **Given** `jira` is removed from the profile, **When** the user runs `sync`, **Then** only the
   managed entries that came from `jira` are removed.
3. **Given** a hook set without a body for Codex, **When** the user attaches it to a Codex profile,
   **Then** the command fails, naming the set and the harness, and changes nothing.
4. **Given** an `all` profile, **When** the user attaches a hook set, **Then** the command fails
   with a message that `all` profiles use the hooks of the default setup and suggests
   `--share content`.
5. **Given** a hook set marked as applying to every profile, **When** the user runs `show` on a
   `content` profile, **Then** the set is listed with "applies to every profile" as reason; on a
   `none` profile that does not name it, the set is not applied.
6. **Given** hooks in the default setup's settings, **When** the user runs
   `aprofile hook-sets import claude --name base`, **Then** a hook set `base` with a Claude Code
   body holding those hooks is created in the library, and the default setup is not modified.

---

### User Story 5 - Keep the setup honest over time (Priority: P3)

Weeks later, a harness update has replaced a linked file with a real one, a managed hook entry
was edited by hand, and the user wonders whether their client profile is really separate.
`doctor` tells them each of these, with a fix.

**Why this priority**: drift is silent today; it undermines the guarantees of the other stories.

**Independent Test**: create each drift condition on a temporary home and check that `doctor`
reports it with profile, entry and fix, and that `sync` does not "repair" any of them silently.

**Acceptance Scenarios**:

1. **Given** a linked entry that the harness replaced with a real file, **When** the user runs
   `doctor`, **Then** it reports that the profile stopped sharing that entry and offers
   `sync --relink`, which backs up the real file and asks before relinking.
2. **Given** a `none` OpenCode profile, **When** the user runs `doctor` or `show`, **Then** the
   `$HOME` escapes (`~/.agents/skills`, `~/.claude/skills`) are listed as "shared with every
   profile; no share level can separate this".
3. **Given** a managed hook entry the user edited in the profile, **When** the user runs `sync`,
   **Then** aprofile leaves the edited entry as is and reports it as diverged from its hook set.
4. **Given** a `content` profile whose settings differ from the default setup, **When** the user
   runs `doctor`, **Then** the difference is listed as information (not a problem) with
   `settings pull` as the way to refresh.

---

### User Story 6 - Existing profiles keep working after the upgrade (Priority: P3)

A user with slice one profiles (some imported from cc-deck) upgrades. Nothing changes until they
run `sync`; then leaking state links are reported, and the profiles keep their present behavior
for settings and content.

**Why this priority**: the upgrade must not surprise anyone, but it applies once per machine.

**Independent Test**: create slice one profiles on a temporary home, upgrade, run `sync`, and
compare: settings and content links are unchanged, leaking state links are reported, nothing is
deleted.

**Acceptance Scenarios**:

1. **Given** a slice one profile without a share level, **When** the new version loads it,
   **Then** it is treated as `all` and `show` says so, with a hint about `--share content`.
2. **Given** a leaking state link whose target holds data, **When** the user runs
   `sync --fix-links`, **Then** the link is replaced with an empty private entry, the target is
   not modified, and the output says that past entries remain in the default setup.

### Edge Cases

- A harness atomically rewrites a linked settings or content file, turning the link into a real
  file: `sync` keeps the real file and reports it; only `sync --relink` relinks, after a backup
  and a confirmation (FR-021).
- The default setup lacks a content entry: no link is created now; a later `sync` creates it once
  the entry appears (slice one behavior).
- The default setup has no settings when a `content` profile is created: the profile's settings
  start minimal, as for `none`, and `sync` reports that nothing was copied.
- A database and its write-ahead or shared-memory companions are always in the same layer; a rule
  for the database covers its companions (FR-004).
- A share level change is requested while the profile is running: it is refused with a message
  naming the running process, like `rename` in slice one.
- A share level change would replace profile-owned data (moving to `all`): backup and confirmation
  are required; the other directions only add or remove links and never delete profile data.
- Moving from `all` to `content` turns the settings link into the profile's own copy of the
  default setup's current settings. Hooks that came from the default setup are not copied; the
  dry run lists them and points to `hook-sets import`.
- Moving from `content` to `none` removes content links but keeps the profile's own settings,
  because they belong to the profile; hook sets that apply to every profile and are not named by
  the profile are detached.
- A hook set references a script that does not exist: `sync` still writes the managed entry,
  reports the missing script, and `doctor` keeps reporting it.
- A hook set referenced by a profile is deleted from the library: `sync` leaves the profile's
  managed entries unchanged, reports the missing set and exits non-zero.
- The user edits a managed hook entry by hand: aprofile no longer treats it as managed for
  updates, never overwrites it, and reports the divergence.
- Another tool (for example cc-deck) adds hooks to a profile: those entries are not managed and
  are never touched.
- A `none` profile starts and the harness shows its first-run onboarding: expected; the help text
  for `none` says so.
- Another tool writes into the default location rather than the profile (for example an IDE
  extension writing lock files into `~/.claude/ide/`): making that entry private could break the
  integration. FR-005 checks each such entry; an entry needed for an integration may be classified
  differently from other state (for example as linked), with the evidence recorded in the plan and
  the reason shown by `show`.
- Two profiles at `all` or `content` install or update plugins at the same moment: both write to
  the linked plugin store. This is the same behavior as two sessions of one account and is
  documented; `none` avoids it.
- A harness update adds a new entry: it is reported as unclassified at every level, never linked,
  until a definition (built-in or in `harnesses.d`) classifies it.
- A user definition in `harnesses.d` restores a catch-all rule that links every unknown entry:
  allowed for user definitions but reported by `doctor` as "every unknown entry is linked".

## Requirements *(mandatory)*

### Functional Requirements

**Classification fixes (all share levels)**

- **FR-001**: The built-in definitions for Claude Code and Codex MUST NOT contain a catch-all rule
  that links every unclassified entry. In all four built-in definitions, an unclassified entry
  MUST be left in place, neither linked nor copied, and reported by `sync`, `show` and `doctor`.
- **FR-001a**: Classification overrides in `harnesses.d` MUST merge with the built-in rules by
  entry, so one added rule does not require copying the built-in list.
  `aprofile harness classify <harness> <entry> --as <category>` MUST write such an override.
  `show` MUST list default-setup entries that are not borrowed because they are unclassified,
  with this command as the fix, and `doctor` MUST report them once per harness as information.
  (Added during planning, research R7: user files such as `RTK.md` imported from `CLAUDE.md`.)
- **FR-002**: The Claude Code definition MUST classify as state: `history.jsonl`, `sessions/`,
  `session-env/`, `file-history/`, `todos/`, `shell-snapshots/`, `tasks/`, `plans/`,
  `paste-cache/`, `debug/`, `telemetry/`, `jobs/`, `daemon/`, `ide/`, `backups/`, plus the
  entries slice one already lists.
- **FR-003**: The Codex definition MUST classify as state: `log/`, `sessions/`, `history.jsonl`,
  `session_index.jsonl`, every `*.sqlite` database, `app-server-daemon/`, `thread-writer-locks/`,
  `shell_snapshots/`, plus lock files and temporary directories it creates. Its `hooks.json` is
  its hook location (FR-024).
- **FR-004**: A rule that classifies a database MUST cover its write-ahead and shared-memory
  companion files and its lock files.
- **FR-005**: The classification lists of all four built-in definitions MUST be re-verified
  against current harness versions during planning; the plan records the evidence (harness
  version and observed entries) for every entry it classifies. The verification MUST include
  entries that other tools (IDE extensions, daemons, companion apps) write into the harness's
  default location, and whether the integration still works when the entry is private.

**Share levels**

- **FR-006**: Every profile MUST have exactly one share level: `all`, `content` or `none`.
- **FR-007**: The level MUST be settable with `--share <level>` on `add`, `edit`, `copy` and
  `import`, in the TUI, and through a global default setting. The built-in default for new
  profiles, including imported ones, is `content`.
- **FR-007a**: `copy` MUST give the target the level `--share` names, or the source's level when
  the flag is absent. When both source and target have their own settings, the target's settings
  start as a copy of the source's (hook location excluded); otherwise they are created as FR-010
  or FR-011 describe. The source's named and excluded hook sets travel with the copy if the
  target's harness has a body for them; sets without one are reported and left out. When the
  target already exists with a different level, `copy` MUST refuse with exit 2 and name the
  `edit --share` command that changes the target's level first (a level change is planned,
  confirmed and refused while running only by `edit --share`, FR-017).
- **FR-008**: At every level, credentials, state and owned entries MUST keep their slice one
  treatment (private; owned travels with `copy`). The skills directory moves from owned to
  content: it is linked at `all` and `content`, and is the profile's own (starting empty) at
  `none`, where it travels with `copy`. (Spec 003 adds an opt-in exception for state: the
  history entries of a member of a history group are linked to the group store; see
  `specs/003-history-groups/spec.md`, FR-014 to FR-016 and FR-025.)
- **FR-009**: Harness definitions MUST split the former shared layer into **content** entries,
  **settings** entries, the **plugin store** and the **hook location**. The treatment of each
  category at each level MUST come from the level table above and the definition data; no core
  logic may branch on a harness name (Constitution Principle I).
- **FR-009a**: A settings file that also holds owned or credentials keys (such as a file with MCP
  servers in it) MUST NOT be linked at any level; at `all` it is the profile's own and is created
  as at `content`. A definition MAY declare such a file mixed (per slice one FR-013) when its format
  supports per-key layers. Where the hook location is a separate entry, it MUST be linked at `all`.
  *Clarified by spec 004 (FR-008): launch entries are owned only where the folder carries them; a
  settings file linked at `all` carries none and stays linkable.*
- **FR-010**: At `content`, settings entries MUST be the profile's own. When aprofile creates them,
  it MUST copy them once from the default setup, leaving out the hook location; afterwards aprofile
  never changes them except through `settings pull`, a share level change, or the owned keys and
  managed hook entries it already maintains.
  *Extended by spec 004 (FR-003 to FR-005): `sync` also maintains the launch entries it placed in
  the profile's own settings.*
- **FR-011**: At `none`, the profile MUST have no content links and its own settings entries,
  created with only the keys aprofile owns; the harness's own defaults apply to everything else.
  At `none`, aprofile MUST NOT seed or copy any entry or key from the default setup, including the
  owned keys of mixed files (such as MCP servers) that slice one seeds.

**Understandability**

- **FR-012**: The README MUST contain a decision guide organized by use case that reproduces the
  use case table and the level table of this spec (condensed wording is allowed; every row and every
  level's treatment of each category must be present), and states up front that logins, history,
  sessions and memory are always private.
- **FR-013**: `aprofile help sharing` MUST print the same guide in the terminal, without needing a
  browser or the README.
- **FR-014**: Each level MUST have a one-line description of at most 80 characters, used
  identically in `add` and `edit` flag help, the TUI, `show` and the help topic. The description of
  `all` MUST say that logins and history stay private.
- **FR-015**: `aprofile show <profile>` MUST list every entry of the profile config dir with one
  treatment (private, linked, own copy, managed by hook set, unclassified, `$HOME` escape) and a
  reason in plain language, and MUST say whether the profile's own settings differ from the
  default setup; `--json` MUST include all of this in a stable schema.
- **FR-016**: `edit --share <level> --dry-run` MUST list every entry the change would add, remove,
  replace or back up, and MUST state whether any profile data would be replaced. Applying the
  change without `--dry-run` MUST perform exactly the listed changes.
- **FR-017**: A share level change MUST only add or remove links and create profile-owned entries,
  except a change that replaces profile-owned data with a link, which MUST back the data up and
  ask for confirmation (or require `--yes` when not on a terminal). A share level change MUST be
  refused while the profile is running.
  *Extended by spec 004 (FR-008a): a change to `all` drops the profile's record of placed launch
  entries and makes them wrapper-only; a change from `all` lets the next `sync` place them; the
  dry run lists both effects.*
- **FR-017a**: `edit --share` (and the TUI) MUST apply the config dir changes of a level change
  itself, as one operation, and then mark the wrapper and registry entry stale until the next
  `sync`, as slice one does for other edits. When the level in a profile file changes by other
  means (hand editing), `sync` MUST perform only the changes that add links, create entries, or
  remove links the new level no longer shares (removing a link never removes its target or any
  profile data), and MUST report any change that would replace profile data, naming the
  `edit --share` command that completes it.
- **FR-018**: `aprofile settings pull <profile> [key...]` MUST merge the current settings of the
  default setup (all keys, or the named top-level keys) into a `content` profile's own settings,
  never touching the hook location or the keys aprofile owns. Merge rules: objects merge by key,
  lists gain the default setup's entries that are missing (profile entries stay, order kept),
  scalars take the default setup's value, and keys or entries only the profile has are kept.
  `--replace` instead makes the pulled keys (or the whole file) an exact copy of the default
  setup's, still excluding the hook location and owned keys. It MUST support `--dry-run`, back up
  the profile's file before writing, and ask for confirmation (or require `--yes` when not on a
  terminal). It MUST refuse `all` and `none` profiles with a message explaining why.
  *Extended by spec 004 (FR-010): the profile's folder-carried launch entries are placed again
  once the pull completes.*
- **FR-019**: The registry entry of each profile and the wrapper contract MUST include the share
  level (an additive registry field and an `APROFILE_SHARE` variable), so hooks and other tools
  can read it.

**Drift and honesty**

- **FR-020**: `doctor` MUST report as problems: a linked content or settings entry that is no
  longer a link; managed hook entries that diverged from their hook set; hook sets that are
  referenced but missing; scripts that hook sets reference but that do not exist. It MUST report as
  information: `content` profiles whose settings differ from the default setup, and every `$HOME`
  escape that applies to a profile, stating that no share level separates it.
- **FR-021**: `sync` MUST NOT relink an entry that became a real file. `sync --relink` MUST back
  up the real file, show what will change and ask for confirmation before relinking.
- **FR-022**: `harness show <name>` MUST list, for that harness, what each level shares (content,
  settings, plugin store, hooks) and the `$HOME` escapes the definition declares; `show` MUST list
  the escapes for every profile they apply to, and `help sharing` MUST list each built-in
  harness's differences from the level table.

**Hook sets**

- **FR-023**: Users MUST be able to keep hook sets in a central library (one file per set, in the
  aprofile config dir). A set has a name, an optional description, a body per harness it
  supports, and a flag that makes it apply to every profile.
- **FR-024**: Each harness definition MUST declare its hook location and form (for example a key of
  a settings file, a hooks file, or a directory of plugin or extension files), analogous to its MCP
  location. A harness without such a declaration does not support hook sets, and attaching one
  MUST fail with a message saying so.
- **FR-025**: Users MUST be able to attach hook sets to a profile by name with `--hook-sets` on
  `add`, `edit` and `copy`, and in the TUI. Attaching a set without a body for the profile's
  harness MUST fail without changes.
- **FR-026**: Sets that apply to every profile MUST apply to every `content` profile of a harness
  they have a body for, unless the profile excludes them by name with `--exclude-hook-sets` (on
  `add`, `edit` and `copy`, and in the TUI). They MUST NOT apply to `none` profiles; a `none`
  profile gets only the sets it names.
- **FR-027**: Attaching a hook set to an `all` profile MUST fail with a message that `all`
  profiles use the default setup's hooks, suggesting `--share content`.
- **FR-028**: `sync` MUST place the managed hook entries of a profile's sets into the profile's
  hook location, update them when the set changes, and remove only those it placed when a set is
  detached or stops applying. It MUST preserve every hook entry it did not place, and MUST NOT
  update or re-add a managed entry the user changed or removed (FR-020 reports it).
  `aprofile hook-sets reapply <profile>` MUST re-place the current entries of the profile's sets
  after a dry run and a confirmation.
- **FR-029**: Hook scripts that sets reference MUST be referenced in place, never copied into
  profiles. Where the harness's hook form is a file (a plugin or extension), the profile entry MUST
  be a link to the file in the library.
- **FR-030**: aprofile MUST NOT translate a hook body from one harness to another.
- **FR-031**: `aprofile hook-sets import <harness> --name <set>` MUST create a hook set from the
  hooks in that harness's default setup, so users can move existing hooks into the library. Where
  the hook form is files (plugins or extensions), the files are copied into the library and the set
  refers to the copies. It MUST NOT modify the default setup and MUST fail without changes if the
  set exists.
- **FR-032**: The README MUST document, next to hook sets, two alternatives: a guard on
  `APROFILE_NAME` or `APROFILE_SHARE` inside a hook script (no aprofile change needed), and
  per-profile plugin enablement for larger hook bundles (available at `content` and `none`, where
  the enabled plugin set is per profile).

**Migration**

- **FR-033**: A profile created before this feature, without a share level, MUST be treated as
  `all`. Loading it MUST NOT rewrite its file, and edits that do not set a level (`edit` without
  `--share`, `rename`, moving the primary flag) MUST NOT write one; the level is written only when
  the user sets it with `--share`. A real entry such a profile already has where `all` would link
  one (slice one gave profiles their own skills directory, and pi its own `agent/settings.json`)
  MUST be kept and reported as information, not replaced and not reported as a problem;
  `sync --relink` replaces it with the shared entry after a backup and a confirmation.
- **FR-034**: After the upgrade, `sync` MUST report every state or credentials entry that is a
  link into the default setup; `sync --fix-links` replaces the link with an empty private entry,
  never touches the link target, and tells the user that earlier data stays in the default setup.
- **FR-035**: The constitution MUST be amended before implementation (Principle III: share levels,
  content (including skills, which leave the owned layer), settings, plugin store and hook
  location, the enabled plugin set being per profile except at `all`;
  Principle II: the new writes, namely the one-time settings copy and `settings pull`, managed hook
  entries, links to hook files, and link removal on a share level change).

### Key Entities

- **Share level**: one of `all`, `content`, `none` per profile; has a one-line description and
  the use cases it fits; stored in the profile and published in the registry.
- **Entry treatment**: the outcome for one entry of one profile (private, linked, own copy,
  managed by hook set, unclassified, `$HOME` escape) plus the reason; derived from the definition,
  the share level and the hook sets; shown by `show`.
- **Content entry / settings entry / plugin store / hook location**: the four kinds of formerly
  shared entries, declared per harness in the definition.
- **Hook set**: name, description, per-harness bodies, applies-to-every-profile flag; lives in the
  central library.
- **Managed hook entry record**: which entries aprofile placed into which profile for which set,
  so it can update or remove exactly those.
- **Change plan**: the list of entry changes a share level change or `settings pull` performs;
  shown by `--dry-run` and executed as listed.
- **`$HOME` escape**: per harness definition, a location outside the config dir that the harness
  reads and no share level can separate.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On a temporary home seeded with every entry listed in Background, after `sync`, zero
  state or credentials entries of any built-in harness are links, at all three share levels.
- **SC-002**: For two `none` profiles of the same harness, the set of entries they share with
  each other or with the default setup is empty. For two `content` profiles, the only linked
  entries are content entries and the plugin store, and the plugin store is the only one of them
  the harness writes at runtime.
- **SC-003**: Each of the seven use cases maps to exactly one recommended share level in the README
  guide and in `aprofile help sharing`, and the two texts agree.
- **SC-004**: In a walkthrough with at least three people who have not read the README, each picks
  the recommended share level for at least six of the seven use cases using only the in-tool help.
  This is checked by hand, not by the automated test suite. **Deferred (2026-11-06)**: the study runs
  after the feature lands, with the script in `walkthrough.md`; its results are recorded there and a
  failed result is handled as a follow-up change to the help texts. It does not block merging.
- **SC-005**: For any profile, one command answers "is this entry private, linked or the profile's
  own, and why" for 100% of the entries in its config dir.
- **SC-006**: A share level change or `settings pull` applied after a dry run performs exactly the
  changes the dry run listed, verified for every pair of levels and every built-in harness.
- **SC-007**: No `sync`, share level change, `settings pull` or hook set change deletes or
  replaces profile data without a backup and a confirmation, verified for every operation in the
  test suite. Placing, updating and removing the managed hook entries aprofile itself placed
  (FR-028) is not replacing profile data: `sync` does it without asking and never touches any
  other entry; `hook-sets reapply` asks and backs the file up first.
- **SC-008**: Attaching or detaching a hook set changes the hook configuration of that profile
  only, after one `sync`, and preserves 100% of the hook entries aprofile did not place.
- **SC-009**: Upgrading a machine with slice one profiles and running `sync` leaves every settings
  and content link unchanged and deletes nothing.

## Assumptions

- The verified entry lists in Background come from one machine on 2026-11-03; FR-005 re-verifies
  them per harness version during planning.
- Content is read-mostly. A harness may write to content (for example appending to `CLAUDE.md`);
  such a write is intended to reach every profile that links it, which is why content stays linked
  at `all` and `content`.
- At `all`, the settings file is one live file shared with the default setup. Two profiles that
  change settings at the same moment can overwrite each other's change; this is the same behavior
  as two sessions of one account and is documented, not prevented.
- Where a harness keeps hooks inside its settings file (Claude Code), the hook location is a key of
  that file; at `content` and `none` the file is the profile's own, so managed hook entries and the
  user's own hook entries live side by side in it.
- Instructions shared across harnesses (one file used as both `CLAUDE.md` and `AGENTS.md`) are out
  of scope; each harness's content links to that harness's own default setup. Hook sets are the
  cross-harness sharing mechanism of this feature.
- Repointing `$HOME`, automatic translation of hooks between harnesses, and release packaging are
  out of scope.
- This feature is part of slice two, which the constitution reserves for plugin toggling and
  related work; per-project MCP activation and permission presets remain separate specs.
- Pull-only integration stays: cc-deck or other tools that install hooks into profiles keep doing
  so, their entries are never touched (FR-028), and hook sets give them an alternative (open
  question in `brainstorm/001-aprofile-standalone-profile-cli.md`).
