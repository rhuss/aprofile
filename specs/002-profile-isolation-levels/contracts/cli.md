# Contract: CLI changes for share levels and hook sets

**Feature**: 002-profile-isolation-levels. Extends
`specs/001-aprofile-standalone-profile-cli/contracts/cli.md`; conventions there (profile
references, `--json`, exit codes, TTY handling, secret references) apply unchanged.

## Shared conventions added by this feature

- `--share all|content|none`. One-line descriptions (FR-014, at most 80 characters, used
  verbatim in flag help, TUI, `show` and `help sharing`):
  - `all`: `Uses your whole setup live; logins, history and memory stay private.`
  - `content`: `Uses your instructions and skills; keeps its own settings and hooks.`
  - `none`: `Shares nothing with your setup; starts clean, with only named hook sets.`
- `--hook-sets a,b` and `--exclude-hook-sets c` take hook-set names from the library.
- Commands that replace profile data back up first (`<data dir>/backups/<harness>/<name>/<UTC
  timestamp>/`), then ask once on a TTY; without a TTY they require `--yes` and otherwise exit 3
  without changes.
- `--dry-run` prints the change plan (data-model.md, Change plan) and exits 0 without writing.

## Changed commands

| Command | New flags | Behavior change |
|---------|-----------|-----------------|
| `aprofile add <harness> <name>` | `--share`, `--hook-sets`, `--exclude-hook-sets` | Default share level from the global setting, built-in `content` (FR-007). Output names the level and its description. Hook-set errors (unknown set, no body for harness, `all` level) fail without creating the profile (FR-025, FR-027). |
| `aprofile edit <h>/<n>` | `--share`, `--hook-sets`, `--exclude-hook-sets`, `--dry-run`, `--yes` | A share change applies its config dir plan immediately (FR-017a); refused with exit 4 while the profile runs (FR-017). `--share` with the current level re-applies that level's plan (the completing command `sync` names for a hand-edited level). Where the running check cannot tell (an unsupported platform), the change asks first, or needs `--yes` off a terminal (exit 3 without it). Only `--share` stores a level in a profile that has none (FR-033). Hook-set changes are recorded and placed by the next `sync`. |
| `aprofile copy <src> <dst>` | `--share`, `--hook-sets`, `--exclude-hook-sets` | Level defaults to the source's; settings and hook sets travel as FR-007a states. An existing target at another level is refused with exit 2, naming `edit --share`. |
| `aprofile import cc-deck` | `--share` | Imported profiles get `--share` or the global default (FR-007). |
| `aprofile show <h>/<n>` | (none) | Adds share level and description, per-entry treatment and reason, hook sets with reasons, settings-differ flag, `$HOME` escapes, unborrowed unclassified default entries (FR-015, FR-001a, FR-022). `--json` schema in data-model.md; the document carries `format_version` (001 contracts/cli.md, JSON schemas). |
| `aprofile list` | (none) | Adds a share column; `--json` adds `share`. |
| `aprofile sync` | `--relink`, `--yes` | Safe mode for hand-edited levels (FR-017a); reports replaced links; `--relink` backs up and relinks (FR-021); places managed hook entries (FR-028). |
| `aprofile doctor` | (none) | New checks (FR-020); each finding has `severity: problem\|info`. Exit status counts problems only. |
| `aprofile harness show <name>` | (none) | Adds per-level sharing summary, plugin store, hook location and form, `$HOME` escapes (FR-022). |

## New commands

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile help sharing` | (none) | Prints the level table, the use-case table, the always-private sentence and per-harness differences (FR-013, FR-022). |
| `aprofile settings pull <h>/<n> [key...]` | `--replace`, `--dry-run`, `--yes`, `--json` | Merge (default) or replace settings from the default setup into a `content` profile; never touches the hook location or owned keys; warns per secret-looking value (FR-018, research R6). Exit 2 for `all`/`none` profiles with an explanation. |
| `aprofile harness classify <harness> <entry> --as <category>` | `--dry-run` | Writes or updates `harnesses.d/<harness>.yaml` with one classification rule, merged by entry (FR-001a). Categories: `content`, `settings`, `plugin-store`, `state`, `credentials`, `owned`. |
| `aprofile hook-sets list` | `--json` | Sets in the library with harnesses covered, `every_profile`, and the profiles using each. |
| `aprofile hook-sets show <name>` | `--json` | One set's bodies and the profiles it applies to, with reasons. |
| `aprofile hook-sets import <harness> --name <set>` | `--description`, `--every-profile` | Create a set from the default setup's hooks; files for the `files` form are copied into the library; fails without changes if the set exists (FR-031). |
| `aprofile hook-sets reapply <h>/<n>` | `--dry-run`, `--yes` | Re-place the current entries of the profile's sets, diverged ones included, after a confirmation (or `--yes`; exit 3 without it off a terminal) and a backup of every file it changes (FR-028). |

## Exit codes added

| Code | Meaning |
|------|---------|
| 2 | Operation not valid for the profile's share level (`settings pull` on `all`/`none`, hook set on `all`, `copy` onto an existing target at another level). |
| 3 | Confirmation needed but not on a TTY and no `--yes`; nothing changed. |
| 4 | Refused because the profile is running. |

## JSON schemas (format_version 1)

`aprofile settings pull <h>/<n> --json` (with or without `--dry-run`):

```jsonc
{
  "format_version": 1,
  "profile": "claude/work",
  "mode": "merge",                 // merge | replace
  "dry_run": true,
  "changes": [                     // one per settings entry that changes or is skipped
    {
      "entry": "settings.json",
      "kind": "merge-settings",    // merge-settings | replace-settings | skip
      "changed": [ "permissions" ],// top-level keys changed ([] when none)
      "removed": [ ],              // keys removed (--replace only; [] when none)
      "secret_warnings": [ "…" ],  // secret-looking values pulled verbatim; omitted when none
      "note": "…",                 // reason for a skip; omitted when none
      "backup": "/abs/backups/…"   // real run, when the entry was backed up; omitted otherwise
    }
  ]
}
```

`aprofile hook-sets list --json`:

```jsonc
{
  "format_version": 1,
  "sets": [ /* one hook-set object per set, as below, without "bodies" */ ],
  "errors": [ "…" ]                // sets that failed to load; omitted when none
}
```

`aprofile hook-sets show <name> --json` prints one hook-set object with `format_version`:

```jsonc
{
  "format_version": 1,
  "name": "jira",
  "description": "…",              // omitted when empty
  "every_profile": false,
  "harnesses": [ "claude" ],       // harnesses the set has a body for
  "file": "/abs/hooks.d/jira.yaml",
  "profiles": [ { "profile": "claude/work", "reason": "named" } ],  // named | every-profile | excluded
  "bodies": { "claude": { /* the set's body for that harness */ } } // show only
}
```

## Wrapper and registry (extends `registry-and-env.md`)

- Wrappers export `APROFILE_SHARE=<level>` with the other `APROFILE_*` variables; it is not
  caller-overridable (Constitution V).
- Registry entries gain `"share": "all"|"content"|"none"`. The field is additive, so
  `format_version` stays 1 (slice one's contract requires a bump only for breaking changes); the
  slice one `registry-and-env.md` gets a note recording the addition.
- All errors other than the codes above keep exiting 1 (`cli.Execute`).
