# Contract: CLI changes for history groups

**Feature**: 003-history-groups. Extends `specs/001-aprofile-standalone-profile-cli/contracts/cli.md`
and `specs/002-profile-isolation-levels/contracts/cli.md`; their conventions (profile references,
`--json`, TTY handling, backups, `--dry-run`, exit codes 2, 3, 4) apply unchanged.

## Conventions

- A group reference is `<harness>/<group>`; `--group <group>` on profile commands names a group of
  the profile's harness (of the target's harness for `copy`). `home` is built in for every harness
  whose definition declares history entries.
- Exit 2 widens to "operation not valid": unknown group, group of another harness, deleting a group
  with members or a `home` group, creating `home` or an existing group, a group for a harness
  without history entries, a `home` join whose default location is missing or unsuitable, and
  `group create` over a kept store. Nothing is created or changed on exit 2.
- Exit 3 and 4 as in spec 002 (confirmation needed off a terminal; profile running).

## New commands

| Command | Flags | Behavior |
|---------|-------|----------|
| `aprofile group list` | `--json`, `--harness <h>` | One row per group, `home` first per harness: harness, name, store path, member count, `built in` marker. The default setup is counted as the implicit member of `home`. |
| `aprofile group show <h>/<g>` | `--json` | Store path, members (default setup marked), history entries with merge mode, presence and size. |
| `aprofile group create <h>/<g>` | `--json` | Creates the record and the empty store (directories for directory entries). Exit 2 for `home`, an existing group, a harness without history entries, an invalid name, or an existing kept store at the path (names the path). |
| `aprofile group delete <h>/<g>` | `--purge`, `--yes`, `--json` | Exit 2 while members exist (lists them) or for `home`. Removes the record; keeps the store and prints its path. `--purge` also deletes the store (only under the data dir) after confirmation, `--yes` off a terminal (exit 3 without). |

## Changed commands

| Command | New flags | Behavior change |
|---------|-----------|-----------------|
| `aprofile add <h> <n>` | `--group <g>` | Validates the group before creating anything (exit 2, lists the harness's groups). The new profile's history entries are linked to the store; on a `home` join, missing history directories are created empty in the default location. Prints the concurrent-use notice once (FR-023). |
| `aprofile edit <h>/<n>` | `--group <g>`, `--no-group` (with `--dry-run`, `--yes`) | Membership change (data-model.md, transitions): prints the plan; `--dry-run` stops there; refused while running (exit 4); asks once when anything moves or is set aside (exit 3 off a terminal without `--yes`); applies exactly the plan. `--group` with the current group completes missing entries. `--group` and `--no-group` are mutually exclusive. Independent of `--share`; both may be given, the share change runs first. |
| `aprofile copy <src> <dst>` | `--group <g>` | Target isolated unless `--group`; when the source is a member and a group of that name exists in the target's harness, the report names it and the flag (FR-018). Never transfers history. |
| `aprofile rename <h>/<n> <new>` | (none) | Keeps membership; moves set-aside history with the profile. |
| `aprofile remove <h> <n> --purge` | (none) | Also deletes the profile's set-aside history; never follows history links (FR-019). |
| `aprofile show <h>/<n>` | (none) | Group line; history entries shown as `linked` with reason `history group <g>`; set-aside history with paths; for a `none` profile in `home`, a line that it shares history with the default setup. |
| `aprofile list` | (none) | Group column (`-` when isolated); `--json` adds `history_group`. |
| `aprofile sync` | (none) | Members: links missing history entries where nothing is in the way (creating missing directories only in non-`home` stores), relinks history links pointing elsewhere, reports real entries and names `edit --group <g>`; never moves data or writes into the default setup. Replaces a link for an entry no longer declared as history with an empty private entry. Leaves profiles of unknown groups unchanged and reports them. |
| `aprofile doctor` | (none) | Problems: member history entry not a link, link to the wrong target, missing non-`home` store, unknown group. Info: kept stores of deleted groups, set-aside history. A member's history link is no longer reported as a leak. |
| `aprofile harness show <name>` | (none) | Lists history entries with merge mode and the definition's `history_note`, or "history groups not supported". |
| `aprofile harness classify ...` | (none) | Keeps `history`/`merge` of an existing rule when the new category is `state`; drops them with a note otherwise. |
| `aprofile help sharing` | (none) | Adds a "History groups" section: `home`, own groups, isolated profiles, use cases, the concurrent-use warning, that shared conversations show which account wrote them, and each harness's `history_note`. |

## Output samples

```text
$ aprofile group list
HARNESS  GROUP  MEMBERS            STORE
claude   home   default setup, 2   ~/.claude
claude   work   2                  ~/.local/share/aprofile/groups/claude/work
codex    home   default setup      ~/.codex
pi       home   default setup      ~/.pi

$ aprofile edit claude/personal --group home --dry-run
Join claude/home (store ~/.claude):
  move-to-store  projects/-Users-me-src/3f2a....jsonl
  set-aside      projects/-Users-me-src/memory/MEMORY.md (exists in store)
  set-aside      history.jsonl (not mergeable)
  link-history   projects/ -> ~/.claude/projects
  ...
Set-aside history goes to ~/.local/share/aprofile/set-aside/claude/personal/20261107T150405Z/
```

## Wrapper and registry

- Wrapper exports `APROFILE_HISTORY_GROUP=<group>` (empty when isolated); always set, overriding
  inherited values.
- Registry entry adds `"history_group": "<group>"` (empty when isolated); additive at
  `format_version` 1.
