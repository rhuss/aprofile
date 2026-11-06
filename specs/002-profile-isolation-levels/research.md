# Research: Profile Share Levels and Hook Sets

**Feature**: `002-profile-isolation-levels` | **Date**: 2026-11-04

Evidence was gathered read-only on the maintainer's machine (macOS) on 2026-11-04. Harness
versions: Claude Code 2.1.318, Codex CLI 0.166.0, OpenCode 2.1.4, pi 1.3.0. Tests never read
these directories (Constitution VII); the entry lists below become fixtures under a temporary home.

## R1. Classification per harness (FR-002, FR-003, FR-005)

**Decision**: replace the catch-all with explicit rules. Categories: `credentials`, `state`,
`owned`, `mixed`, and the four borrowable categories `content`, `settings`, `plugin-store`,
`hook-location`. Entries written by other tools (corgi, herdr, macmd, cost trackers) stay
unclassified in the built-ins and are reported; users classify them (R7).

### Claude Code (`~/.claude`, lever `CLAUDE_CONFIG_DIR`)

| Category | Entries |
|----------|---------|
| credentials | `.credentials.json`, `.credentials.lock` |
| mixed | `.claude.json` (unchanged from slice one: `oauthAccount`, `account` credentials; `mcpServers` owned; `projects`, `numStartups` state) |
| state | `projects/`, `history.jsonl`, `sessions/`, `session-env/`, `file-history/`, `todos/`, `shell-snapshots/`, `tasks/`, `teams/`, `plans/`, `paste-cache/`, `debug/`, `logs/`, `telemetry/`, `statsig/`, `stats-cache.json`, `jobs/`, `daemon/`, `daemon.log`, `daemon.lock`, `daemon.status.json`, `daemon-auth-status.json`, `daemon-auth-cooldown`, `ide/` (see R8), `backups/`, `cache/`, `state/`, `transcripts/`, `mcp-needs-auth-cache.json`, `.last-cleanup`, `.last-update-result.json` |
| content | `CLAUDE.md`, `skills/`, `agents/`, `commands/`, `output-styles/`, `themes/`, `keybindings.json`, `hooks/` (hook scripts; added after the walkthrough) |
| settings | `settings.json` (hook location is its `hooks` key), `settings.local.json` |
| plugin-store | `plugins/` |
| hook-location | `settings.json#hooks` (form `event-groups`) |

`settings.json` holds no owned or credentials keys today (`model` is carried by
`ANTHROPIC_MODEL`), so it is linkable at `all`. Its `env` key may hold literal values; see R6.

### Codex (`~/.codex`, lever `CODEX_HOME`)

| Category | Entries |
|----------|---------|
| credentials | `auth.json`, `auth.lock` |
| owned | `config.toml` (holds `mcp_servers`; FR-009a keeps it private at every level, `all` included) |
| state | `sessions/`, `history.jsonl`, `session_index.jsonl`, `session_index.jsonl.lock`, `log/`, every `*.sqlite` with `-wal`, `-shm` (R2): `goals_1`, `logs_2`, `memories_1`, `queue_1`, `state_5`, `thread_history_1`; `.sqlite-maintenance.lock`, `app-server-control/`, `app-server-daemon/`, `thread-writer-locks/`, `shell_snapshots/`, `tui-thread-reference-capabilities/`, `cache/`, `tmp/`, `.tmp/`, `models_cache.json`, `version.json`, `installation_id`, `.personality_migration`, `.sandbox_migration` |
| content | `AGENTS.md`, `prompts/`, `skills/` |
| settings | `rules/` (Codex writes "always allow" decisions into `rules/default.rules` at runtime) |
| plugin-store | `plugins/`, `packages/` |
| hook-location | `hooks.json#hooks` (form `event-groups`, same shape as Claude's `hooks` key) |

Slice one listed `logs/`; Codex writes `log/`. The definition fixes the name.

### OpenCode (XDG set: `config/`, `data/`, `state/`, `cache/` under the profile root)

| Category | Entries |
|----------|---------|
| credentials | `data/opencode/auth.json`, `data/opencode/mcp-auth.json`, `data/opencode/account.json` |
| mixed | `config/opencode/opencode.json` (`mcp` owned; `plugin` settings; other keys seeded once, slice one) |
| state | `data/opencode/` (sessions database with `-wal`/`-shm`, `storage/`, `snapshot/`, `log/`, `tool-output/`, `repos/`, `bin/`), `state/opencode/` (`prompt-history.jsonl`, `model.json`, `locks/`), `cache/opencode/` |
| content | `config/opencode/skills/`, `config/opencode/agent/`, `config/opencode/command/`, `config/opencode/themes/` |
| settings | `config/opencode/tui.jsonc`, `config/opencode/cli.json` |
| plugin-store | `config/opencode/node_modules/`, `config/opencode/package.json`, `config/opencode/bun.lock`, `config/opencode/package-lock.json` |
| hook-location | `config/opencode/plugins/` (form `files`) |

`opencode.json` is mixed, so it is never linked; at `all` its non-owned keys are seeded once, as
in slice one. `harness show opencode` states this difference (FR-022).

### pi (`~/.pi`, lever `PI_CODING_AGENT_DIR` = `agent/`)

| Category | Entries |
|----------|---------|
| credentials | `agent/auth.json`, `agent/mcp-auth.json` |
| owned | `agent/mcp.json` |
| state | `agent/sessions/`, `agent/logs/`, `agent/trust.json`, `agent/models-store.json` |
| content | `agent/skills/`, `agent/prompts/`, `agent/themes/` |
| settings | `agent/settings.json` (holds only `theme` and `lastChangelogVersion`; slice one called it owned only because it was the plugin mechanism; no owned keys, so it becomes settings) |
| hook-location | `agent/extensions/` (form `files`) |

pi has no separate plugin store; extensions are single files.

**Rationale**: every entry seen on disk is either classified with a reason or knowingly left
unclassified (other tools' files). The two misnamed entries (`history/`, `logs/`) are fixed.

**Alternatives considered**: keeping the catch-all and adding state rules only. Rejected: the next
harness release would leak again, and Constitution 1.1.0 forbids catch-alls in built-ins.

## R2. Databases and companions (FR-004)

**Decision**: a rule's `entry` may be a glob (`*.sqlite`); a database rule implicitly covers
`<name>-wal`, `<name>-shm`, `<name>-journal` and `<name>.lock`. The loader expands companions so
`show` lists them with the database's reason.

**Rationale**: SQLite opens the companions by name next to the database; a split layer would
corrupt or leak. **Alternatives**: listing every companion by hand (error-prone, already missed).

## R3. Hook location forms (FR-024, FR-028)

**Decision**: two forms, declared in the definition:

- `event-groups`: a JSON object `{<Event>: [ {matcher, hooks: [...]}, ... ]}` at a key path
  (Claude `settings.json#hooks`, Codex `hooks.json#hooks`). A hook-set body for this form is the
  same object shape. Managed entries are whole matcher groups.
- `files`: a directory of files (OpenCode `config/opencode/plugins/`, pi `agent/extensions/`). A
  hook-set body lists files in the library; each becomes a link in the profile's directory.

**Rationale**: both shapes exist today; the Codex and Claude shapes are identical, so one flavor
serves both, matching how MCP flavors work. **Alternatives**: launch-time injection via
`claude --settings` (Claude only, not data-driven across harnesses; rejected for this slice).

## R4. Identifying managed hook entries (FR-027, FR-028, FR-020)

**Decision**: aprofile records, per profile, the canonical JSON fingerprint (SHA-256 of the
key-sorted group) of every matcher group or file link it placed, with the set name, in
`<aprofile data dir>/managed-hooks/<harness>/<name>.json`. On `sync`:

- a recorded group found unchanged is managed: updated when its set changes, removed when the set
  is detached or stops applying;
- a recorded group not found unchanged is diverged: left alone, reported by `sync` and `doctor`,
  not re-added; `aprofile hook-sets reapply <profile>` re-places the set's current entries after a
  dry run;
- every other group is the user's or another tool's and is never touched.

For `files`, a managed entry is a link whose target is inside the library; a real file or a link
elsewhere is never touched.

**Rationale**: the hook formats have no id field; fingerprints identify exactly what aprofile wrote
without adding markers the harness might reject. The record lives in aprofile's own data dir so the
harness config dir gains no unknown entry. **Alternatives**: a marker key inside each group
(harnesses may validate strictly); matching by command string (breaks when two sets share a
script).

## R5. Hook set library format (FR-023)

**Decision**: one YAML file per set at `<aprofile config dir>/hooks.d/<name>.yaml`, the same
format family as `harnesses.d`:

```yaml
name: jira
description: Link Jira keys in tool output
every_profile: false
bodies:
  claude:            # form event-groups
    PostToolUse:
      - matcher: Bash
        hooks: [{ type: command, command: ~/.local/share/hooks/jira-link.sh }]
  opencode:          # form files
    files: [jira-link.ts]   # relative to hooks.d/files/jira/
```

Bodies are validated against the harness's declared form; secret-looking literals are rejected
like profile values (Constitution IV). Several sets on one event are placed in set-name order
(alphabetical), after the user's own groups, so order is deterministic.

## R6. Settings copy and secrets (FR-010, FR-018, Principle IV)

**Decision**: the one-time copy and `settings pull` carry values verbatim (the user wrote them into
their own default setup) and warn once per value matching a secret pattern, naming the key, never
the value. This mirrors the Principle IV exception for owned-layer files a harness maintains;
Constitution 1.1.0 is patched in this plan to name settings copies in that exception.

**Alternatives**: refusing to copy files with secret-looking values (would block common `env`
usage); stripping them (silently changes behavior).

## R7. Entries only the user knows (FR-001, FR-015)

**Finding**: the maintainer's `~/.claude` holds about twenty user or tool files that no definition
can know (`RTK.md` and `jira.local.md`, imported from `CLAUDE.md`; `cpr.sh`; `handoffs/`; `rosa/`;
`copyedit/`). With the catch-all gone they are no longer linked into profiles, which can break
`@` imports in a linked `CLAUDE.md` if Claude resolves them next to the link.

**Decision**: classification overrides in `harnesses.d` merge by `entry` (a keyed list), so a user
adds one rule without copying the built-in list. A convenience command writes that override:
`aprofile harness classify claude RTK.md --as content`. `show` lists default-setup entries that
are not borrowed because they are unclassified, with this command as the fix; `doctor` reports them
once per harness as information. This needs a small spec addition (FR-001a), made with this plan.

**Rationale**: keeps Principle III (unknown is never shared silently) while making the fix one
command. **Alternatives**: auto-detecting `@` imports (fragile, Claude-specific); a global
"also share" list outside definitions (splits harness knowledge, Principle I).

**Verification (T058, 2026-11-06)**: Claude Code resolves `@` imports against the link target. A
`content` profile whose `CLAUDE.md` links to `~/.claude/CLAUDE.md` loaded `@RTK.md` although
`RTK.md` was not in the profile (RUNBOOK Step 24). Linked `CLAUDE.md` imports keep working without a
rule; the R7 override remains for entries a harness reads directly from its config dir.

## R8. Entries other tools write (FR-005, Edge Cases)

**Finding**: `ide/` holds lock files that IDE extensions write; Claude Code's daemon writes
`daemon.lock`, `daemon.status.json` and `daemon-auth-*` into the profile dir itself (seen in an
existing cc-deck profile), so daemon state is already per config dir.

**Decision**: classify `ide/` as state. IDE extensions started from a shell that runs a profile
wrapper inherit `CLAUDE_CONFIG_DIR` and write into the profile; an IDE started outside a wrapper
writes into the default setup and its sessions connect only to the default setup, which is the
correct isolation. The README notes this; a task verifies `/ide` with a profile.

**Verification (T058, 2026-11-06)**: Cursor started with the profile's `CLAUDE_CONFIG_DIR` and a
current Claude Code extension (2.1.318) wrote its lock file into the profile's `ide/`
(`ide/31378.lock`, owned by that Cursor process); `~/.claude/ide` stayed empty (RUNBOOK Step 25).
The extension 1.0.35 wrote no lock file at all, so `/ide` needs a current extension.

## R9. When a share level change takes effect (FR-016, FR-017, FR-017a)

**Decision**: `edit --share` computes a change plan with the same planner `sync` uses, prints it,
asks once when a step replaces profile data (backup first), and applies it. `sync` runs the planner
in "safe" mode: steps that add links, create entries or remove links (never their targets) run; steps that replace data are
reported with the completing command. Running-profile detection reuses `rename`'s check.

## R10. Upgrade of slice one profiles (FR-033, FR-034)

**Decision**: a missing `share` field loads as `all` without rewriting the file. Slice one
definitions linked state entries through the catch-all; after the upgrade those links are reported
as leaking and repaired only by `sync --fix-links` (existing mechanism). Entries that were linked by
the catch-all and are now unclassified (R7) are left as links and reported, never removed, so an
upgraded profile keeps working until the user decides. Real entries that slice one gave every
profile where `all` now links one (the skills directory, which was owned; pi's
`agent/settings.json`) are kept and reported as `kept-from-upgrade` information, never replaced or
flagged as problems; `sync --relink` shares them after a backup. Only `--share` stores a level, so
a later `edit --label` or `rename` does not end this protection (walkthrough audit, 2026-11-06).

## Verification results (T058, T059; 2026-11-04, macOS, Darwin 25.6)

| Item | Result | How |
|------|--------|-----|
| R11: `edit --share` refuses while the profile runs (macOS) | Verified | `TestLevelChangeRefusedWhileRunning` (exit 4) and `TestRunningDetectsChildProcess`. Finding: macOS hides the environment of Apple platform binaries (for example `/bin/sleep`) from `kern.procargs2`; harness binaries are not platform binaries and are detected. |
| R11: same on Linux | Verified (2026-11-06) | `TestRunningDetectsChildProcess` and `TestLevelChangeRefusedWhileRunning` pass in a `golang:1.24` container (podman, Linux 7.0.11 aarch64), exercising the `/proc` path. |
| R7: `@` imports in a linked `CLAUDE.md` | Verified (2026-11-06) | RUNBOOK Step 24: a `content` profile loaded `@RTK.md` through its linked `CLAUDE.md` although `RTK.md` is not in the profile; imports resolve against the link target. |
| R8: `/ide` from a profile | Verified (2026-11-06) | RUNBOOK Step 25: Cursor started with the profile's `CLAUDE_CONFIG_DIR` wrote its lock file into the profile's `ide/`, none into `~/.claude/ide` (needs a current extension). |
| Quickstart sections 1 to 6 (T059) | Passed after one fix | One deviation found and fixed: after `all -> content -> all`, `doctor` reported `settings.local.json` as "stopped sharing" although the default setup has no such file. Such an entry is now the profile's own (`TestTreatmentRealEntryWithoutDefault`). The quickstart prerequisites now seed a small fake default setup inline; the full R1 lists are the fixtures in `internal/testutil/defaults.go`. |
| Quickstart section 7 (upgrade with the slice one binary built from `9d6faf8`) | Passed | Links for `settings.json`, `CLAUDE.md`, `RTK.md` unchanged; `history.jsonl` and `sessions` reported as leaking; `RTK.md` reported as an unclassified link; profile file unchanged; `show` reports `all` with the `--share content` hint. |
