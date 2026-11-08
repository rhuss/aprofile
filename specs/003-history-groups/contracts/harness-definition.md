# Contract: Harness definition changes for history groups

**Feature**: 003-history-groups. Extends
`specs/002-profile-isolation-levels/contracts/harness-definition.md`. Same YAML format for
built-ins and `harnesses.d` files.

## History fields on classification rules

```yaml
classification:
  - { entry: "projects/",               layer: state, history: true, merge: per-file }
  - { entry: "history.jsonl",           layer: state, history: true }          # merge: none
  - { entry: "session_index.jsonl",     layer: state, history: true }
  - { entry: "session_index.jsonl.lock", layer: state, history: true }
  - { entry: "sessions/",               layer: state }                          # private
```

- `history: true` marks a state entry as history: a member of a history group links it to the
  group store; an isolated profile keeps it private as before.
- `merge: per-file | none` (default `none`) decides what a join does with a profile's existing
  entry: `per-file` moves each file whose relative path does not exist in the store, recursively,
  and sets aside the rest; `none` sets aside the whole entry.
- Validation (exit at load with field and message, as other definition errors):
  - `history` only on `layer: state`; never on a glob rule, a mixed entry, or an entry whose path
    is not safe (`entryPathSafe`).
  - `merge` only together with `history: true`; `per-file` only on directory entries.
- Database companions inherit `history` and `merge` from their rule.
- `harnesses.d` overrides still replace a rule by `entry`; an override of a history entry MUST
  restate `history` (and `merge`) to keep it. `harness classify --as state` keeps the existing
  history fields; any other category drops them and prints a note.
- A definition supports history groups when at least one rule has `history: true`. `harness show`
  lists the history entries with their merge mode, or "history groups not supported".

## History note

```yaml
history_note: "pi migrates session format v1/v2 to v3 on load; older pi versions may not read newer sessions."
```

Optional, free text; `harness show` and `help sharing` print it. Added to `allFieldNames`.

## Built-in values

See data-model.md, "Built-in values", and research R1 for the evidence. OpenCode declares no
history entries because its session database also holds credentials (FR-003).
