# Brainstorm: History Groups

**Date:** 2026-11-07
**Status:** active

Builds on the research brief `docs/superpowers/specs/2026-11-07-session-history-brainstorming.md`.

## Problem Framing

Every aprofile share level (`all`, `content`, `none`) keeps runtime state private, including conversation history, sessions and project memory (spec 002). A conversation started in one profile cannot be resumed from another, even when both profiles belong to the same person and differ only in account, endpoint or model.

Switching profiles is not only about accounts. Authentication and endpoint configuration decide which models are reachable (for example an internal model-serving endpoint), so changing the model can require a different profile.

The author wants:

- Some profiles of a harness (personal aliases, for example) to share one history, so a conversation can continue under whichever profile fits.
- Other profiles to share only among themselves (all work profiles, for example).
- Some profiles (a client or an isolated work profile) to keep fully isolated history and memory, as today.
- The plain default setup (`~/.claude`, `~/.codex`, ...) to take part in sharing.

Success: picking up a conversation from another profile in the same group works through the harness's native resume, with that profile's credentials, endpoint and model, and with no duplicated copies of the conversation.

Anti-goals:

- Copying or syncing history between profiles. Two copies that both keep growing produce a split brain.
- Implicit group creation. A typo in a group name must not silently start a new, empty history.
- Weakening isolation for anyone who does not opt in.

If supporting isolated and shared history together turns out to be too complex, the author prefers shared memory over isolated memory.

## Approaches Considered

### A: Groups anchored on the default setup (chosen)

Every harness has a built-in `home` group whose store is the default setup's own history entries. Profiles in `home` link to those entries, the same way `all` links content today. Other groups keep their store in aprofile's data directory.

- Pros: the default setup is never modified, so constitution Principle II holds. Tools that read only `~/.claude` (cc-session, Codex `/import`, the Codex plugin's transfer) see the whole `home` group's history.
- Cons: a store lives in one of two places depending on the group. The default setup is always in `home` and cannot join another group.

### B: Every group store in aprofile's data directory

The default setup joins a group by having its history entries replaced with links, after confirmation and a backup.

- Pros: one uniform store location.
- Cons: rewrites the default setup, which needs a constitution amendment. The default's history would depend on aprofile's directory, even after uninstalling aprofile.

### C: Copy or sync history between profiles (dropped)

Copy session files on demand or mirror them periodically.

- Dropped: two writers on two copies diverge (split brain), and SQLite-backed stores cannot be merged safely.

### Implicit group creation (dropped)

Create a group the first time a profile refers to it.

- Dropped: typos create new empty histories without warning.

## Decision

Approach A with explicit group management. Each harness has a built-in, undeletable `home` group backed by the default setup. Further groups are created and deleted explicitly. A profile belongs to at most one group or stays isolated. History groups are the first spec; cross-harness transfer and a profile-aware session browser follow separately.

## Key Requirements

- Every harness has a built-in `home` group whose store is the harness's default location. It cannot be created, renamed or deleted, and it always exists.
- Group names are scoped per harness (`claude/work`), like profile names. A group holds profiles of one harness only, because session formats differ between harnesses.
- Groups are managed explicitly: create, list, delete. Referring to a group that does not exist is an error. Deleting a group that still has members is refused.
- A profile belongs to at most one group. A profile in no group keeps private history, exactly as today.
- New profiles are in no group unless the user names one. (Author's choice: avoids naming the built-in group `default`, which would suggest new profiles land there.)
- Group membership is independent of the share level: `content` plus a group, or `all` without one, are both valid.
- Members share conversations, the session index, prompt history, file checkpoints and project memory (for Claude Code, memory lives in the same `projects/` tree as conversations).
- Credentials, account data, locks, caches, logs, telemetry and daemon state stay private in every profile, grouped or not.
- The default setup is never modified. Members of `home` link to its entries; members of other groups link to the group store in aprofile's data directory.
- The default setup is always in `home` and cannot join another group.
- `show` and `doctor` report each profile's group and the linked history entries.
- Spec 002's "runtime state is always private" requirement and the user guide gain an explicit, opt-in exception for history groups.
- Proposal (Claude's call, reason: sessions are separate files with unique IDs, so moving them is safe): joining a group with existing history moves that history into the group store for Claude Code and Pi.
- Proposal (Claude's call, reason: Codex and OpenCode keep their index in SQLite, and merging two databases is risky): for Codex and OpenCode, joining requires empty history or sets the old history aside.
- Proposal (Claude's call, reason: never delete data on a membership change): leaving a group gives the profile fresh private history and leaves the group store untouched.
- Proposal (Claude's call, reason: two terminals in one profile can already do this): running the same conversation from two profiles at once is warned about, not blocked.

## Follow-up Capabilities (out of scope for the first spec)

- **Cross-harness transfer.** Explicit conversion of one conversation to another harness. Best candidate from research: [resume-from](https://github.com/alexei-led/resume-from) (TypeScript, MIT), which covers Claude, Codex and Pi in all directions and reads `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and `PI_CODING_AGENT_DIR`. OpenCode profiles can copy sessions with `opencode session export/import`. All converters are lossy (reasoning and model state dropped, tool calls at best kept as text).
- **Profile-aware session browser.** Keep cc-session as a separate tool. It currently reads only `CLAUDE_HOME` or `~/.claude` and resumes with `claude -r`. aprofile would publish each profile's store path, group and wrapper (for example via a JSON listing); cc-session would label sessions by profile or group and resume through the wrapper. [agent-sessions](https://github.com/jazzyalex/agent-sessions) already browses all four harness stores on macOS.
- **Upstream report.** Codex's built-in `/import` reads Claude sessions only from `$HOME/.claude` and ignores `CLAUDE_CONFIG_DIR`.

## Open Questions

- Exact history entry list per harness: which of Claude's `sessions/`, `session-env/`, `todos/`, `plans/`, `tasks/` and `transcripts/` are history (shared in a group) and which are per-process runtime state (private)? Same question for Codex, OpenCode and Pi.
- Can OpenCode's history be grouped at all when sessions, auth and other state share one SQLite database? If not, OpenCode is excluded from groups at first.
- Does Codex tolerate its `state_5.sqlite` and session index being linked and used by several profiles running different Codex versions?
- Where do non-`home` group stores live in aprofile's data directory, and how are they backed up?
- Command and flag syntax for group management and membership (`aprofile group ...`, `--group` on `add`, `copy` and `set`).
- What happens to a group's store when its last member leaves: kept until the group is deleted, or offered for deletion?
- How does `copy` treat group membership: carry it to the destination, or leave the destination isolated?
- How does a profile leave `home` when the user wants to stop sharing with the default setup?
