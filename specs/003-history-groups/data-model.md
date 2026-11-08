# Data Model: History Groups

## Harness definition: history fields on classification rules

`harness.EntryRule` gains two optional fields (contracts/harness-definition.md):

| Field | Type | Rule |
|-------|------|------|
| `history` | bool | Only valid on `layer: state`; not valid on globs or mixed entries. |
| `merge` | `per-file` \| `none` | Only valid with `history: true`; default `none`. |

Database companions (`database: true`) inherit both fields. A definition supports groups when at
least one rule has `history: true` (`Definition.HistoryRules()`).

Built-in values (research R1):

| Harness | History entries (merge) |
|---------|-------------------------|
| claude | `projects/` (per-file), `history.jsonl` (none), `paste-cache/` (per-file), `file-history/` (per-file), `todos/` (per-file), `plans/` (per-file), `tasks/` (per-file) |
| codex | `sessions/` (per-file), `archived_sessions/` (per-file), `session_index.jsonl` (none, with its `.lock`), `history.jsonl` (none) |
| pi | `agent/sessions/` (per-file) |
| opencode | none (groups not supported) |

## Profile

`profile.Profile` gains:

| Field | YAML | Rule |
|-------|------|------|
| `HistoryGroup string` | `history_group,omitempty` | Empty means isolated. Must name `home` or an existing group of the profile's harness when set through a command; a stored name that does not exist is reported by `doctor` and left alone by `sync`. Name rules as profile names. |

## History group

Record file `<config>/aprofile/groups/<harness>/<group>.yaml` (none for `home`):

```yaml
harness: claude
name: work
created: 2026-11-07T15:04:05Z
```

In memory (`internal/history.Group`):

| Field | Meaning |
|-------|---------|
| `Harness`, `Name` | identity `<harness>/<group>`; `home` is reserved |
| `Builtin bool` | true for `home` |
| `Store string` | `home`: the harness's default location root; others: `<data>/aprofile/groups/<harness>/<group>` |
| `Members []string` | profile names whose `history_group` equals `Name`; `home` also lists the default setup |

Store path of one entry: `home` uses `def.DefaultPathUnder(home, rel)` (honors
`default_locations`); other groups use `filepath.Join(Store, rel)`.

## Set-aside history

Directory `<data>/aprofile/set-aside/<harness>/<name>/<timestamp>/` holding moved items at their
relative paths, plus `manifest.json`:

```json
{
  "profile": "claude/personal",
  "group": "home",
  "created": "2026-11-07T15:04:05Z",
  "items": [
    {"path": "projects/-Users-me-src/memory/MEMORY.md", "reason": "exists-in-store"},
    {"path": "history.jsonl", "reason": "not-mergeable"}
  ]
}
```

Moves with the profile on `rename`; deleted only by `remove --purge`.

## Membership state transitions

```text
isolated --edit --group G--> member(G)      join: plan, confirm if anything moves or is set aside
member(G) --edit --group H--> member(H)     relink only; no data moves; G's store unchanged
member(G) --edit --no-group--> isolated     unlink; create empty private entries; store unchanged
member(G) --edit --group G--> member(G)     complete missing entries (FR-022b) or "nothing to do"
```

Every transition is refused while the profile runs (exit 4) and supports `--dry-run`.

## Change plan steps (internal/layer)

New step kinds, used by join, leave and sync:

| Kind | Meaning | Confirm |
|------|---------|---------|
| `move-to-store` | move one file from the profile entry into the store (`os.Link` + remove; on `EXDEV`, copy to an `O_EXCL` target; never overwrites) | yes (part of the join confirmation) |
| `set-aside` | move one file or entry into the set-aside directory | yes |
| `link-history` | link a history entry to its store path (a missing file target is linked anyway) | no |
| `relink-history` | replace a history link pointing elsewhere with the correct one | no |

Existing kinds reused: `unlink`, `create` (empty private entry), `report`, and `mkdir` for store
directories (non-`home` stores) or, only with `ExplicitJoin`, a missing history directory in the
default location (note `default setup`).

A definition may carry a top-level `history_note` (free text about version sensitivity), shown by
`harness show` and `help sharing`.

## Registry entry and wrapper contract

- `registry.Entry` gains `HistoryGroup string` (`json:"history_group"`), additive at
  `format_version` 1, empty for isolated profiles.
- The wrapper always exports `APROFILE_HISTORY_GROUP` (empty for isolated profiles) and
  `wrapper.Environ` mirrors it.

## JSON output

- `list --json` and `show --json` profile objects gain `history_group` (string, empty when
  isolated); `show --json` gains `set_aside` (list of `{dir, items}`) and per-entry
  `treatment: "linked"` with `reason` naming the group for history links.
- `group list --json`: `[{harness, name, builtin, store, members: [..], default_setup_member}]`.
- `group show --json`: the same object plus `entries: [{entry, merge, size_bytes, exists}]`.
