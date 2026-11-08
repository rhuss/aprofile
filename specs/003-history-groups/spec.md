# Feature Specification: History Groups

**Feature Branch**: `003-history-groups`

**Created**: 2026-11-07

**Status**: Draft

**Input**: User description: "History groups for aprofile: some profiles of a harness share one
conversation history, others share only among themselves, and some stay fully isolated. The plain
default setup takes part through a built-in `home` group. Groups are managed explicitly."
(Full description and the approaches considered are in `brainstorm/002-history-groups.md` on the
`docs/profile-guide` branch; the decision is summarized under Background.)

## Terminology

Terms from slice one (`specs/001-aprofile-standalone-profile-cli/spec.md`) and from spec 002
(`specs/002-profile-isolation-levels/spec.md`) keep their meaning: harness, profile, harness
definition, wrapper, default setup, share level, private, content, settings. New terms:

- **History**: the part of a profile's runtime state that makes up its conversations: transcripts
  and sessions, the session index the harness uses to find and resume them, prompt input history,
  file checkpoints, per-session to-dos and plans, and project memory the harness keeps next to the
  transcripts. Logs, caches, telemetry, daemon state, locks and per-process files are runtime state
  but not history.
- **History entry**: an entry of a profile config dir that a harness definition declares as
  history. Every history entry is also state; all other state stays private as before.
- **History group** (or **group**): a named set of profiles of one harness that share one history.
  Identified as `<harness>/<group>`, like profiles.
- **Group store**: the place where a group's history entries live. Members link their history
  entries to it.
- **`home` group**: the built-in group of every harness whose store is the harness's default
  location (the default setup's own history entries).
- **Member**: a profile that belongs to a group. The default setup is always the implicit member
  of its harness's `home` group.
- **Isolated profile**: a profile in no group. Its history is private, exactly as spec 002
  defines.
- **Set-aside history**: history a profile had before it joined a group and that could not be
  moved into the group store. It is kept in aprofile's data directory under the profile's identity,
  never inside the profile config dir.

## Clarifications

### Brainstorm 2026-11-07

Decisions taken with the user before this spec:

- Some profiles share history, others share only among themselves, others stay isolated, so there
  can be several groups per harness and a profile may be in none.
- Copying or syncing history between profiles is rejected (two growing copies diverge).
- Groups are created explicitly; referring to an unknown group is an error, so a typo never starts
  a new empty history.
- Each harness has a built-in, undeletable `home` group backed by the default setup. The name is
  `home`, not `default`, so it does not suggest that new profiles land in it. New profiles are
  isolated unless a group is named.
- If supporting isolated and shared history together became too complex, shared memory would be
  preferred over isolated memory.

### Session 2026-11-07

Answered in ship pipeline mode with the recommended option (open to review):

- Q: Where does set-aside history live? → A: In aprofile's data directory, per profile, not inside
  the profile config dir. Inside the config dir it would be reported as an unclassified entry and
  could be scanned by the harness (FR-014, Key Entities).
- Q: May a profile at share level `none` join `home`? → A: Yes. Group membership is an explicit
  opt-in independent of the share level; `show` says that the profile shares history with the
  default setup even though it borrows nothing else (FR-012, FR-020).
- Q: Can a non-`home` group be renamed? → A: Not in this feature; a group is created and deleted
  only. Members can move to a new group with `edit --group` (FR-006).

### Verification 2026-11-08

Aligned with the implementation after verification, accepted by the user: `group list` text output
shows a member count, names are in `--json` and `group show` (FR-007); a reclassified entry's link
is removed and the entry gets its new category's treatment, and a `home` link of an entry
reclassified as credentials, owned or mixed is left to `sync --fix-links` (FR-022a). SC-006 (the
five-minute setup check by a person) is done after merging and is not a landing gate.

### Spec review 2026-11-07

Clear-cut fixes applied: Principle II joins the constitution amendment (FR-025); exit codes for
refusals (FR-013); exact conditions for refusing a `home` join (Edge Cases); the wrapper always
exports the group (FR-024); `copy --group` uses the target's harness (FR-018); `--purge` never
deletes a `home` store (FR-008). Design decisions accepted by the user:

- Q: What happens to members when a definition's history list changes? → A: A newly declared
  history entry is joined like any other (plan, confirmation, set-aside), but through
  `edit --group`, not `sync`; `sync` only adds links where nothing is in the way and reports the
  rest, so it never moves data or writes into the default setup. A link for an entry no longer
  declared as history is replaced with an empty private entry and reported (FR-022a, FR-022b;
  refined in the second review round).
- Q: How fine-grained is merging on join? → A: Recursive by path relative to the entry root;
  files of one session are not kept together as a unit; partial moves are allowed and reported
  (FR-014).
- Q: Must other members be stopped during a join? → A: No. A join only adds files that do not exist
  in the store, never overwrites, so only the joining profile must not be running (FR-013).

## Background

Spec 002 made runtime state private at every share level (FR-002, FR-003, FR-008), and the
constitution (Principle III) says state is "per profile, created empty, never symlinked, never
copied". A conversation started under one profile therefore cannot be resumed under another, even
when both profiles belong to the same person and differ only in account, endpoint or model.
Switching profiles is not only about accounts: authentication and endpoint configuration decide
which models are reachable, so changing the model can require changing the profile.

The user wants groups of profiles that share history, for example all personal aliases with the
default setup, and all work profiles among themselves, while a client profile keeps its own
history and memory. Continuing a conversation must use the harness's native resume, run with the
resuming profile's credentials, endpoint and model, and must not create a second copy of the
conversation.

Approach chosen (brainstorm approach A): members link their history entries to one group store.
For `home`, the store is the default setup itself, which is not rewritten. For other groups, the
store lives in aprofile's data directory. Rejected: rewriting the default setup's entries into
links to an aprofile-owned store (needs aprofile to modify the default setup); copying or mirroring
history (split brain; databases cannot be merged safely); creating groups implicitly (typos).

## User Scenarios & Testing *(mandatory)*

### Use cases

| # | Use case | Setup |
|---|----------|-------|
| UC1 | Personal aliases (different endpoints or accounts) continue each other's conversations and the conversations of the plain default setup | Each alias in `claude/home` |
| UC2 | Work profiles continue each other's conversations, never personal ones | `claude/work` created; each work profile in it |
| UC3 | A client profile that must never see other history or memory, and whose history no other profile sees | No group (isolated), as today |
| UC4 | A demo profile with a clean history | No group |

### User Story 1 - Continue a conversation under another profile of the same group (Priority: P1)

A user has `claude-personal` (subscription login) and `claude-lab` (an internal endpoint with other
models), both in `claude/home`. They start a conversation with plain `claude`, quit, and resume the
same conversation with `claude-lab --resume <id>`. The conversation continues with the lab
profile's credentials, endpoint and model, and later turns are visible when they resume again with
`claude-personal` or plain `claude`. No second copy of the conversation exists.

**Why this priority**: this is the reason for the feature; everything else supports it.

**Independent Test**: on a temporary home, create two profiles of one harness in `home`, write a
session through one profile's config dir, and confirm that the same session file is reachable
through the other profile's config dir and the default location, with only one copy on disk.

**Acceptance Scenarios**:

1. **Given** profiles `claude/personal` and `claude/lab` in `claude/home`, **When** a session file
   is written through either profile's config dir, **Then** it exists exactly once, in the default
   location, and is visible through both profiles and the default setup.
2. **Given** a member of `home`, **When** the user runs its wrapper, **Then** the wrapper sets the
   profile's own credentials, endpoint and model exactly as for an isolated profile.
3. **Given** a member of any group, **When** `aprofile show` runs, **Then** each history entry is
   listed as linked to the group store with the group's name, and credentials and the remaining
   state are listed as private.

---

### User Story 2 - Separate groups for separate worlds (Priority: P1)

A user creates `claude/work`, adds two work profiles to it and leaves a client profile isolated.
Work profiles continue each other's conversations; neither personal nor client conversations are
visible to them, and work conversations are visible nowhere else.

**Why this priority**: without separate groups, sharing would leak work history into personal
profiles; the isolated case protects compliance-bound profiles.

**Independent Test**: create `claude/work` with two members, one `home` member and one isolated
profile; write one session through each; confirm each session is visible exactly through the
members of its own group (or only its own profile when isolated).

**Acceptance Scenarios**:

1. **Given** `aprofile group create claude/work`, **When** the user adds `claude/acme` and
   `claude/corp` with `--group work`, **Then** both link their history entries to the `work` store
   and a session written by one is visible to the other.
2. **Given** members of `claude/work` and of `claude/home`, **When** a session is written in
   either group, **Then** it is not visible to members of the other group.
3. **Given** an isolated profile `claude/client`, **When** sessions exist in both groups, **Then**
   none of them is visible to `claude/client`, and its own sessions are visible to no other profile.
4. **Given** no group `claude/wrok` exists, **When** the user runs
   `aprofile add claude test --group wrok`, **Then** the command fails with exit 2, names the
   existing groups of the harness, and creates nothing (no profile, no store).

---

### User Story 3 - Join and leave a group without losing history (Priority: P2)

A user has an existing isolated profile with conversations and moves it into a group, later moves
it out again. No conversation is deleted by either step.

**Why this priority**: most users already have profiles with history; membership changes must be
safe before they are used on real data.

**Independent Test**: seed an isolated profile with sessions, join a group, verify every session
is either in the group store or in the profile's set-aside history; leave the group, verify the
group store is unchanged and the profile has fresh, empty, private history entries.

**Acceptance Scenarios**:

1. **Given** an isolated Claude Code profile with sessions, **When** the user runs
   `aprofile edit claude/personal --group home`, **Then** aprofile first shows the plan (which
   session files move into the store, which entries are set aside), asks for confirmation (or
   requires `--yes` when not on a terminal), moves session files that do not exist in the store,
   sets aside everything else, and links the history entries.
2. **Given** a history entry whose definition does not allow merging (for example Codex's
   `history.jsonl` and `session_index.jsonl`; Codex rollout files under `sessions/` merge per
   file, research R1), **When** a profile with existing history joins a group, **Then** that
   whole entry is set aside, none of it is merged, and the output names where the set-aside
   history is.
3. **Given** a member, **When** the user runs `aprofile edit <profile> --no-group`, **Then** the
   history links are removed, the profile gets fresh empty private history entries, and the group
   store is not changed.
4. **Given** a profile that is running, **When** the user changes its group, **Then** the change
   is refused with exit 4 and a message of the same form spec 002 uses for a share level change
   of a running profile (naming the process and saying to stop it first).
5. **Given** any membership change, **When** `--dry-run` is given, **Then** the listed changes are
   exactly the changes the command performs without `--dry-run`, and nothing is written.

---

### User Story 4 - Manage groups explicitly (Priority: P2)

The user lists, creates, inspects and deletes groups; the `home` group of each harness is always
there.

**Why this priority**: explicit management is what makes typos harmless and stores discoverable.

**Independent Test**: on a fresh temporary home, `group list` shows one `home` group per harness
that supports history groups; create, show and delete a group; confirm `home` cannot be deleted
or created.

**Acceptance Scenarios**:

1. **Given** a fresh setup, **When** the user runs `aprofile group list`, **Then** every harness
   that supports history groups shows its `home` group with the default location as its store and
   the default setup as its implicit member.
2. **Given** `claude/work` with members, **When** the user runs `aprofile group delete claude/work`,
   **Then** the command fails with exit 2 and lists the members.
3. **Given** `claude/work` without members, **When** the user deletes it, **Then** the group is
   removed and its store is kept and reported with its path; with `--purge`, the store is deleted
   after confirmation (or `--yes`).
4. **Given** any harness, **When** the user tries to create, rename or delete its `home` group,
   **Then** the command fails with exit 2 and explains that `home` is built in.
5. **Given** a harness whose definition declares no history entries, **When** the user creates a
   group for it, **Then** the command fails with exit 2 and says the harness does not support
   history groups.

---

### User Story 5 - See and keep group membership honest (Priority: P3)

`show`, `list`, `doctor`, `sync`, the registry and the wrapper contract tell the user and other
tools which group a profile is in, and drift is detected.

**Why this priority**: a harness may replace a link with a real file; without detection a member
silently starts its own history again.

**Independent Test**: replace one member's history link with a real directory, run `doctor` and
`sync`, confirm the drift is reported, `sync` does not overwrite the real entry, and
`sync --relink` repairs it after a backup and confirmation.

**Acceptance Scenarios**:

1. **Given** a member whose history entry became a real file or directory, **When** `doctor` runs,
   **Then** it reports a problem naming the profile, the entry and the `sync --relink` fix.
2. **Given** that drift, **When** `sync` runs, **Then** it does not replace the real entry;
   **When** `sync --relink` runs, **Then** it backs up the real entry, shows what changes, asks for
   confirmation, and restores the link.
3. **Given** a member whose history link is missing, **When** `sync` runs, **Then** the link is
   recreated (adding a link needs no confirmation).
4. **Given** any profile, **When** the registry is exported or the wrapper runs, **Then** the
   group name (empty for isolated profiles) is available to other tools.

### Edge Cases

- A `home` history entry does not exist yet in the default location (the harness was never run
  there): directory entries are created empty in the default location on an explicit join; file
  entries are linked anyway and come into existence when the harness first writes them (FR-016).
- The default location does not exist, is itself a symbolic link, lies inside aprofile's data
  directory, or is the config dir of a profile: joining `home` is refused with exit 2 and a message
  naming the location and the condition.
- A session file in a joining profile has the same name as one in the store: it is not moved; it
  stays in the set-aside history and is reported.
- Project memory files with the same name (for example a memory index file per project) collide
  the same way; they are set aside, never merged line by line.
- Two members resume the same conversation at the same time: not prevented; documented as a risk
  and mentioned when a profile joins a group (FR-023).
- Two members run different harness versions on a shared store: not prevented; the plan records
  which history entries are known to be version sensitive, and `harness show` and `help sharing`
  note it per harness.
- `remove --purge` of a member deletes the profile's links and its private entries, never the group
  store or anything a link points to.
- `rename` of a member keeps its membership. `edit --share` keeps the membership; group and share
  level are independent.
- `copy` to a new target: the target is isolated unless `--group` is given; the source's
  membership is reported as a hint. Copy never transfers history (spec 002 rule unchanged).
- The group store directory of a non-`home` group was deleted by hand: `doctor` reports it; `sync`
  recreates the store's entries empty and reports that history is missing.
- A deleted group's kept store: `doctor` lists it as information with its path until it is removed
  by hand (a deleted group no longer exists, so `group delete --purge` cannot reach it).
- Set-aside history is never read by the harness, never deleted by aprofile except by
  `remove --purge` of its profile, and is listed by `show`. `rename` moves it with the profile.
  There is no restore command; the user moves items back by hand.
- A `none` profile joins `home`: allowed; `show` states that it shares history with the default
  setup although it borrows nothing else.

## Requirements *(mandatory)*

### Functional Requirements

**History classification**

- **FR-001**: Harness definitions MUST be able to declare which state entries are history, and for
  each history entry whether it can be merged on join and how (per file, or not at all). No core
  logic may branch on a harness name (Constitution Principle I).
- **FR-002**: The history entry lists of the four built-in definitions MUST be verified during
  planning against current harness versions, with evidence (harness version, observed entries,
  whether the entry is keyed per session or per process) recorded in the plan. Initial candidates:
  Claude Code `projects/`, `history.jsonl`, `file-history/`, `todos/`, `plans/`, and possibly
  `sessions/`, `session-env/`, `tasks/`, `transcripts/`; Codex `sessions/`, `history.jsonl`,
  `session_index.jsonl` and its session state database with companions; pi's session directory;
  OpenCode's session database.
- **FR-003**: A harness MUST support history groups only if its definition declares at least one
  history entry. A definition MUST NOT declare as history an entry that also holds credentials or
  login data such as tokens, secrets or login records (for example a database that stores both
  sessions and logins). Account identifiers inside conversation records do not count as login
  data (research R2). If OpenCode's
  sessions cannot be separated from its other data, its definition declares no history entries and
  OpenCode does not support groups in this feature.
- **FR-004**: Entries that are state but not history MUST stay private for members exactly as for
  isolated profiles.

**Groups**

- **FR-005**: Every harness that supports history groups MUST have a built-in `home` group whose
  store is the harness's default location. It always exists and cannot be created, renamed or
  deleted.
- **FR-006**: `aprofile group create <harness>/<group>` MUST create a group and its empty store in
  aprofile's data directory. Group names follow the profile name rules; `home` is reserved.
  Creating an existing group fails with exit 2. Groups cannot be renamed in this feature. When a
  kept store of a deleted group with the same name exists, `group create` MUST refuse with exit 2,
  name the store path and say that it has to be moved or removed by hand first; aprofile never
  adopts a kept store implicitly.
- **FR-007**: `aprofile group list` and `aprofile group show <harness>/<group>` MUST show each
  group's harness, name, store path and members (the default setup listed as implicit member of
  `home`): `group list` shows the member count in its text output and the member names in
  `--json`; `group show` names the members and lists the history entries and their size. Both
  MUST support `--json`.
- **FR-008**: `aprofile group delete <harness>/<group>` MUST refuse with exit 2 while the group
  has members, listing them. Without `--purge` it removes the group and keeps the store, reporting
  its path. With `--purge` it also deletes the store after confirmation (or `--yes` when not on a
  terminal), and only when the store lies inside aprofile's data directory; a `home` store is never
  deleted.
- **FR-009**: Referring to a group that does not exist (in `add`, `edit`, `copy` or any group
  command) MUST fail with exit 2, create nothing, and list the existing groups of that harness. No
  command creates a group implicitly.
- **FR-010**: A group MUST contain profiles of its own harness only.

**Membership**

- **FR-011**: A profile MUST belong to at most one group. A new profile is isolated unless
  `--group <group>` is given on `add`.
- **FR-012**: `aprofile edit <profile> --group <group>` MUST move a profile into a group (from
  isolated or from another group); `aprofile edit <profile> --no-group` MUST make it isolated.
  Membership MUST be independent of the share level; every level, including `none`, may join any
  group of its harness.
- **FR-013**: A membership change MUST be refused with exit 4 while the profile is running, MUST
  exit 3 without changes when it needs confirmation, is not on a terminal and `--yes` is absent
  (the exit codes of spec 002), MUST support
  `--dry-run`, and MUST list before applying: links added and removed, files moved into the store,
  entries set aside, and entries created. Applying without `--dry-run` MUST perform exactly the
  listed changes.
- **FR-014**: Joining a group with existing history MUST move into the store only files of entries
  the definition marks mergeable and that do not exist in the store yet. Merging is recursive and
  keyed by the file's path relative to the entry root, creating directories as needed; files of
  one session are not treated as a unit, so a partial move is allowed and reported item by item.
  Other members of the group may be running during a join, because it never overwrites or changes
  an existing store file. The join MUST set aside everything
  else (including append-only files and databases) in the profile's set-aside history in
  aprofile's data directory. It MUST NOT modify or overwrite any existing file in the store. When anything
  would be moved or set aside, the change MUST ask for confirmation (or require `--yes` when not on
  a terminal).
- **FR-015**: Leaving a group (`--no-group`, or moving to another group) MUST remove the profile's
  history links, never their targets, and give the profile fresh empty private history entries (or,
  when moving, links to the new group's store). The old group store MUST NOT be changed.
- **FR-016**: For the `home` group, aprofile MUST NOT replace or rewrite any existing entry of the
  default setup. The only writes into the default location this feature allows are: creating a
  missing history directory empty on an explicit join, and moving mergeable session files in on an
  explicit, confirmed join (FR-014). Missing history files are linked and come into existence when
  the harness first writes them.
- **FR-017**: The default setup MUST always be the implicit member of its harness's `home` group
  and MUST NOT be offered or accepted as a member of any other group.
- **FR-018**: `copy` MUST leave the target isolated unless `--group` is given, and `--group` names a
  group of the target's harness (FR-010); when the source is a member and a group of that name
  exists in the target's harness, the output MUST name it and the flag that would add the target
  to it.
  `copy` MUST NOT transfer history. `rename` keeps membership; `edit --share` keeps membership.
- **FR-019**: `remove --purge` of a member MUST delete only the profile's own entries and links and
  its set-aside history; it MUST NOT follow a history link and MUST NOT delete anything in a group
  store. `rename` MUST carry the set-aside history to the new name.

**Visibility and drift**

- **FR-020**: `show` MUST list each history entry of a member as linked to the group store, naming
  the group; MUST list set-aside history with its path; MUST say, for a `none` profile in `home`,
  that it shares history with the default setup; and `list` MUST show each profile's group.
  Both MUST include this in `--json`.
- **FR-021**: `doctor` MUST report as problems: a member's history entry that is a real entry
  instead of a link, a link that points somewhere other than its group store, a missing store for a
  non-`home` group, and a profile assigned to a group that does not exist. It MUST report as
  information: kept stores of deleted groups, and set-aside history.
- **FR-022**: `sync` MUST recreate missing history links of members and missing empty entries of a
  non-`home` store, and MUST NOT replace a real entry; `sync --relink` MUST back it up, show the
  change and ask for confirmation, as spec 002 FR-021 does for borrowed entries.
- **FR-022a**: When a harness definition declares a new history entry, `sync` MUST NOT move or set
  aside anything and MUST NOT write into the default setup: it links the entry only where the
  member has no entry of that name yet (creating a missing store directory only for non-`home`
  stores), and otherwise reports the member and names `aprofile edit <profile> --group <group>`,
  which re-runs the join for the missing entries with the FR-013 plan and confirmation. When an
  entry is no longer declared as history, `sync`, leaving and moving MUST remove each member's
  link for it into the group store (never touching the store), after which the entry gets the
  normal treatment of its new category, and report it. Exception: a link into the default setup
  (`home`) for an entry reclassified as credentials, owned or mixed cannot be told apart from a
  link the user made by hand; it is reported as a leaking link and replaced only by
  `sync --fix-links`, as spec 002 does for every leaking link (`harness classify` names that
  command).
- **FR-022b**: `edit --group <group>` for a profile already in that group MUST complete its
  membership: join any declared history entry that is not yet linked (FR-014), and report
  "nothing to do" otherwise.
- **FR-022c**: `sync` MUST replace a member's history link that points somewhere other than its
  group store with the correct link (removing a link never touches its target), and MUST report
  without changing anything a profile assigned to a group that does not exist.
- **FR-023**: Joining a group MUST print a one-time notice that running the same conversation in
  two profiles at once is not prevented and can interleave its transcript; the README and
  `aprofile help sharing` MUST state the same.
- **FR-024**: The registry entry of each profile MUST include its group (additive field, empty for
  isolated profiles), and the wrapper MUST always export it as `APROFILE_HISTORY_GROUP` (empty for
  isolated profiles), always set by the wrapper and overriding any inherited value, as the other
  `APROFILE_*` variables are, so tools
  such as a session browser can find which profiles share a history.

**Rules and documentation**

- **FR-025**: The constitution MUST be amended, with a version bump:
  - Principle III: history entries of a member of an explicitly chosen history group are linked
    to the group store, while credentials and all other state stay private and isolated profiles
    keep the current rule.
  - Principle II: the allowed writes into a profile config dir gain adding, replacing and
    removing links for history entries of group members, creating fresh empty history entries when
    a profile leaves a group or an entry stops being history, and, on an explicit, confirmed join,
    moving mergeable history into the group store and moving the rest to set-aside history; the
    default-setup rule gains the writes FR-016 allows on an explicit join of `home` (never by
    `sync`).
  Spec 002's level table and FR-008 note this exception.
- **FR-026**: The README decision guide, `aprofile help sharing` and the use case table MUST
  explain groups, `home`, isolated profiles and the use cases above in plain language, and MUST
  keep stating that logins stay private at every level and in every group.
- **FR-027**: The TUI add and edit forms MUST offer the group choice with the same rules as the
  flags (Constitution Principle VI: every capability stays reachable from the CLI first).

### Key Entities

- **History group**: harness, name, store location (default location for `home`, aprofile data
  directory otherwise), built-in flag.
- **Membership**: the group name recorded in a profile's definition; empty for isolated profiles.
- **History entry declaration**: part of a harness definition; entry name, kind (file or
  directory), merge mode (per file or none).
- **Set-aside history**: per profile, a timestamped location in aprofile's data directory holding
  history that was not merged on join, with a record of where each item came from.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For every harness that supports groups, a session written through one member's
  config dir is reachable through every other member's config dir (and the default location for
  `home`) with exactly one copy on disk.
- **SC-002**: With no group configured, the set of linked entries of every profile is identical
  before and after upgrading to this feature (zero history links for isolated profiles).
- **SC-003**: No membership change, group deletion without `--purge`, `copy`, `rename` or
  `remove --purge` of a member reduces the number or the bytes of files in any group store or in
  the default setup; tests cover each operation.
- **SC-004**: A command naming an unknown group creates zero files and directories.
- **SC-005**: Every `--dry-run` of a membership change lists exactly the changes the real run
  performs (compared on a test home for join with merge, join with set-aside, move between groups
  and leave).
- **SC-006**: A user can set up UC1 and UC2 (create a group, add two profiles to it, add two
  profiles to `home`) in under five minutes with only `aprofile help sharing` and command help
  (manual walkthrough check, done after merging; it is not a gate for landing this feature).

## Assumptions

- Native resume of each harness finds a session by its identifier in the history entries of the
  running config dir; linking those entries is enough to make a conversation resumable from
  another member. Planning verifies this per harness (FR-002).
- Harnesses follow symbolic links for the history entries they read and write. Where a harness
  replaces a linked entry with a real one, drift detection (FR-021, FR-022) covers it.
- Running different versions of one harness against a shared store is the user's responsibility;
  aprofile documents version-sensitive entries but does not block.
- No automatic backup of group stores is part of this feature; backups before destructive steps
  follow the existing spec 002 rules.
- The user guide currently lives on the unmerged `docs/profile-guide` branch; guide pages are
  updated when that branch and this feature meet. This feature updates the README and in-tool help.
- Out of scope: converting or transferring conversations between harnesses, browsing history
  across profiles (a separate change in the cc-session tool, which can use FR-024), and reporting
  to Codex that its `/import` ignores `CLAUDE_CONFIG_DIR`.
- Claude's calls made in the brainstorm (merge on join for per-file stores, set aside for
  databases, leaving gives fresh private history, concurrent use warned not blocked) and in this
  spec (copy does not carry membership, deleting a group keeps its store unless `--purge`, the
  registry and wrapper expose the group) are proposals open to review.
