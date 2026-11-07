# Contract: CLI command surface

**Feature**: 001-aprofile-standalone-profile-cli. Derived from spec FRs. Every operation is
available non-interactively (FR-040); the TUI is a convenience layer over the same operations
(Constitution VI).

## Conventions

- Profile reference syntax: `<harness>/<name>` where a command takes a single profile, or
  `<harness> <name>` as two positional args for `add`/`remove` (see each command).
- `--json` is accepted by read commands and reports; output follows a documented, versioned schema
  (FR-041). All `--json` output omits secret values (FR-028).
- Exit codes: `0` success; non-zero on any error. Specific non-zero cases called out below
  (partial import, path setup failure with `--require-path`, unresolved references at launch).
- Color/icons on a TTY; plain text when not a TTY or `NO_COLOR` is set (FR-042).
- Secret inputs accept only `env:`/`file:` references; literals are rejected (FR-028), except
  `--env-literal` for non-secret extra variables.

## Commands

### Profile management (FR-001, FR-002, FR-003, FR-004, FR-006a)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile add <harness> <name>` | `--model`, `--endpoint`, `--auth-kind`, `--auth-key env:… \| file:…`, `--env NAME=ref`, `--env-literal NAME=VALUE`, `--color`, `--icon`, `--label`, `--primary`, `--herdr-agent`, `--adopt` | Create a profile file. Refused when its wrapper name is another profile's, naming that profile (FR-002). A repeated `--env`/`--env-literal NAME` is stored once (last wins). Warns if the harness binary is missing. `--adopt` attaches a leftover config dir. Opens a form if required values are missing and stdin+stdout are TTYs (FR-043). |
| `aprofile list` | `--json` | List profiles grouped by harness. Prints one line about aprofile-marked wrappers in the bin dir that belong to no profile (removed by the next `sync`, FR-006b). |
| `aprofile show <harness>/<name>` | `--json` | Show one profile; reports whether credentials exist without revealing them (FR-039): credential files in the config dir, whether each auth reference resolves now, and the exec hook's `auth-status` (Keychain logins). |
| `aprofile edit <harness>/<name>` | same value flags as `add` | Update fields; form when no flags and TTY. `--auth-kind` without `--auth-key` keeps only the references of variables the new kind sets, and is refused when the new kind needs a key it does not have. Does not write wrappers (FR-006b). |
| `aprofile rename <harness>/<name> <new-name>` | `--force` | Move profile file + config dir together (FR-006a), plus the managed-hooks record and the imported credentials dir (`<data dir>/credentials/<harness>/<name>/`); `file:` references and path values into that dir are rewritten to the new location. Refused with exit 4 while the harness runs unless `--force`. Where the running check cannot tell (an unsupported platform), it asks first, or needs `--force` off a terminal (exit 3 without it). Refused when the new wrapper name is another profile's (FR-002). Says that the old wrapper is removed by the next `sync` (FR-006b). |
| `aprofile remove <harness> <name>` | `--purge`, `--yes` | Remove the profile file. Config dir retained unless `--purge` (confirmed, or `--yes`), which also deletes the imported credentials dir. Says that the wrapper is removed by the next `sync` (FR-006b). |

### Sync and launch (FR-022..FR-027)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile sync` | `--dry-run`, `--json`, `--require-path`, `--remove-path`, `--fix-links` | Write one wrapper per profile, prepare config dirs by layer, remove stale wrappers, write registry, ensure bin dir on PATH. Idempotent (FR-025). `--dry-run` lists changes only. `--json` prints the plan (with `--dry-run`) or what the sync did, schema below. Exits non-zero on load failures (a harness whose `harnesses.d` override fails to load is failed: its wrappers and registry entries stay unchanged, FR-022), and, only with `--require-path`, on every outcome that leaves the PATH setup to the user: unsupported shell, symlinked or read-only rc file (never replaced), a failed edit, or shell edits disabled in the settings (FR-024). |
| `aprofile login <harness>/<name>` | (none) | Run the harness login flow with the profile's environment (FR-039). |
| `<wrapper-prefix>-<name> [args...]` | (harness args) | Generated wrapper; execs the harness with the profile environment (FR-026). |

### Harness definitions (FR-007..FR-012)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile harness list` | `--json` | List harnesses with source (built-in / file path). |
| `aprofile harness show <name>` | `--json` | Effective definition; marks overridden vs built-in fields. |

### Registry and integration (FR-030..FR-032)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile registry path` | (none) | Print the registry file location. |
| `aprofile registry export --format omarchy` | `--output-dir` | Write Omarchy accounts files per provider, opt-in (FR-031). A profile whose harness declares no `omarchy_provider` is skipped with a note on stderr; this is not an error. |

### Copy and MCP (FR-033..FR-037)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile copy <h>/<n> <h>/<n>` | `--overwrite`, `--yes`, `--adopt`, `--auth`, `--auth-key`, `--label`, `--color`, `--icon` | Copy a profile. Same-harness carries all settings + owned layer verbatim (warns on secret-looking owned values); cross-harness converts MCP, drops auth/model/endpoint. Target gets empty credentials + state. `--overwrite` replaces only what the copy writes, backs up what it replaces under `<data dir>/backups/` and asks first; off a terminal it needs `--yes` (exit 3 without it); a target that fails to load is never overwritten. `--adopt` reuses a leftover config dir of a new target (FR-004). `--auth-key env:NAME\|file:/abs/path` supplies the reference a cross-harness target's auth kind needs (required when the kind sets a variable). A wrapper name another profile has is rejected. `--json` report. |
| `aprofile mcp list <h>/<n>` | `--json` | List a profile's MCP servers. |
| `aprofile mcp copy <from> <to> [server...]` | `--overwrite`, `--json` | Convert named servers (or all; a name given twice counts once); leaves existing target servers unless `--overwrite`. |

### Migration, UI, diagnostics (FR-043..FR-045)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile import cc-deck` | `--dry-run`, `--overwrite`, `--overwrite-credentials`, `--share all\|content\|none`, `--source <dir>`, `--source-data <dir>`, `--json` | Create profiles from cc-deck config + credentials; never modifies cc-deck files. `--source` is the cc-deck config dir holding `config.yaml` and `--source-data` the cc-deck data dir holding the live harness config dirs (defaults: the `cc-deck` dir beside the aprofile config dir and data dir). `--share` sets the imported profiles' level (default: `default_share` in the settings, else `content`). `--overwrite-credentials` implies `--overwrite`. Skips existing `(harness,name)` and exits non-zero; skips a profile whose wrapper name another profile has, naming it, and exits non-zero (FR-002); reports wrapper-name collisions on PATH. |
| `aprofile ui` | (none) | Browse view with add/edit/copy/remove via forms (FR-043). |
| `aprofile doctor` | `--json` | Run all checks in FR-044; each finding names the profile (if any) and a fix. |

## JSON schemas (FR-041)

Every `--json` document starts with `"format_version": 1`. Fields are only ever added at the
same version; removing or changing the meaning of a field bumps `format_version`.

### `aprofile show <h>/<n> --json`

```jsonc
{
  "format_version": 1,
  // profile fields, the same as one element of `list --json` "profiles":
  "harness": "claude", "name": "work", "label": "Work", "color": "blue", "icon": "W",
  "primary": true, "auth_kind": "oauth-token", "auth_refs": { "CLAUDE_CODE_OAUTH_TOKEN": "env:WORK_TOKEN" },
  "model": "…", "endpoint": "…", "extra_env": [ { "name": "X", "kind": "reference|literal", "reference": "env:X" } ],
  "herdr_agent": "…", "has_credentials": false, "wrapper_status": "synced|stale|missing|unknown",
  "config_dir": "/abs", "wrapper_path": "/abs/bin/claude-work",
  // feature 002 (specs/002-profile-isolation-levels/data-model.md):
  "share": "content", "share_description": "…", "share_implicit": false,
  "entries": [ /* entry treatment */ ], "hook_sets": [ { "name": "…", "reason": "…" } ],
  "settings_differ": false, "home_escapes": [], "unborrowed_default_entries": [],
  // FR-006: where the profile does not fit its definition (an undeclared auth kind, a model
  // or endpoint the harness has no variable for); [] when it fits.
  "problems": [ "auth kind \"nope\" is not declared by harness claude (valid kinds: …); fix: aprofile edit claude/work --auth-kind <kind>" ],
  // T066 (FR-039, FR-011): where the credentials come from; never a value.
  "credentials": {
    "present": true,                      // same as has_credentials
    "sources": [ "files", "references", "auth-status" ],  // the ones that hold
    "references": [ { "variable": "CLAUDE_CODE_OAUTH_TOKEN", "reference": "env:WORK_TOKEN",
                      "resolves": false, "problem": "reference env:WORK_TOKEN is unset or empty" } ],
    "auth_status": { "status": "ok|missing|unknown|unsupported|not-installed|error",
                     "detail": "…", "error": "…" }   // only when the hook declares auth-status
  }
}
```

`has_credentials` is true when a credentials-layer file or key exists in the config dir, when every
variable of the profile's auth kind has a reference that resolves now, or (show only) when the
exec hook's `auth-status` answers `ok`. `list --json` does not call the hook, so for a Keychain
login it can report `false` where `show` reports `true`.

Literal `extra_env` values and secrets are never printed (FR-028).

### `aprofile sync --json` (with or without `--dry-run`)

```jsonc
{
  "format_version": 1,
  "dry_run": true,            // true: nothing was changed, actions are the plan
  "changed": true,            // anything (would) change
  "profiles": [               // one per profile file, sorted by id
    {
      "profile": "claude/work",
      "status": "ok",         // ok | failed (not processed: wrapper and registry entry left unchanged)
      "wrapper": { "name": "claude-work", "path": "/abs/bin/claude-work",
                   "action": "write" },   // write | unchanged | kept (failed profile)
      "config_dir": {
        "path": "/abs/profiles/claude/work",
        "steps": [ { "action": "mkdir", "path": "projects", "note": "state: private", "mutating": true } ]
      },
      "hooks": [ { "action": "place-hook", "path": "settings.json", "note": "…", "mutating": true } ],
      "registry": "add",      // add | update | unchanged | kept (failed, old entry carried over) | none
      "problems": [],         // this profile's problems (also in the top-level list)
      "notes": []
    }
  ],
  "wrappers_removed": [ "claude-old" ],   // stale aprofile-marked wrappers (would be) removed
  "registry": { "path": "/abs/registry.json", "changed": true, "removed": [ "claude/old" ] },
  "path": {
    "action": "added",        // added | removed | unchanged | manual | disabled
    "rc_file": "/home/u/.zshrc",
    "changed": true,
    "needs_manual": false,    // true: --require-path exits non-zero
    "reason": "…",            // why the rc file was not edited (manual, disabled)
    "manual": "…"             // the instructions to follow by hand
  },
  "problems": [ "…" ],        // every problem; any problem means a non-zero exit
  "notes": [ "…" ]
}
```

Step `action` values are the change-plan kinds of feature 002's data model (`link`, `unlink`,
`mkdir`, `copy-once`, `seed-mixed`, `replace-with-link`, `fix-link`, `place-hook`, `remove-hook`,
…), plus `report` for a reported condition, `needs-confirm` for a step `sync` skips because it
replaces profile data, `patch` for a config patch that set keys of a file (FR-019; the note names
the keys), and `write-config` for the exec hook's operation (mutating only when the hook answered
`changed=true`; non-mutating and not called under `--dry-run`). A step `path` is relative to the profile config dir, except the step that
creates the config dir itself (absolute). With `--json`, stdout carries only the JSON document;
problems are also printed to stderr, and `--relink` prompts go to stderr. The exit status is the
same as without `--json`.

### `aprofile list --json`

```jsonc
{
  "format_version": 1,
  "profiles": [ /* one profile object per profile, the fields of `show --json` up to
                   "share_implicit" (not the entries, hook sets or credentials detail) */ ],
  "leftover_wrappers": [ "claude-zap" ]  // marked wrappers in the bin dir that belong to no
                                         // profile (left by remove or rename); always present,
                                         // [] when none; the next sync removes them (FR-006b)
}
```

### `aprofile doctor --json`

```jsonc
{
  "format_version": 1,
  "findings": [
    { "profile": "claude/work",   // omitted for installation-wide findings
      "severity": "problem",      // problem | info; only problems make the exit status non-zero
      "message": "…",
      "fix": "run: aprofile sync" }  // omitted when there is no fix to name
  ]
}
```

### `aprofile harness list --json`

```jsonc
{
  "format_version": 1,
  "harnesses": [
    { "name": "claude", "binary": "claude", "wrapper_prefix": "claude",
      "source": "built-in",        // built-in | "built-in + overrides from <files>" | "<files>"
      "override_files": [ "/abs/harnesses.d/claude.yaml" ] }  // omitted when none
  ],
  "errors": [                      // omitted when every definition loaded
    { "file": "/abs/harnesses.d/x.yaml", "field": "wrapper_prefix", "message": "…" }  // field omitted when unknown
  ]
}
```

### `aprofile harness show <name> --json`

The effective definition with the same field names as the definition file
([harness-definition.md](./harness-definition.md)), plus the schema version, the origin of each
field and the treatment per share level:

```jsonc
{
  "format_version": 1,
  "name": "claude", "binary": "claude", "wrapper_prefix": "claude",
  "config_env": [ { "var": "CLAUDE_CONFIG_DIR", "path": "." } ],   // ordered list
  // Present when set: default_config_location, model_var, endpoint_var, auth_kinds
  // ([{name, vars}]), auth_clears, default_auth_kind, login_command ({args, note}),
  // classification ([{entry, layer, keys, database, companion_of, deprecated, origin}]),
  // config_patches ([{file, merge}]), mcp ({location, flavor}), skills_location
  // ({path, scope}), plugin_mechanism, omarchy_provider, capabilities, exec_hook
  // ({operations}), default_locations, share_policy, hooks ({location, form}),
  // home_escapes ([{path, what}]).
  "source": { "binary": "builtin", "wrapper_prefix": "/abs/harnesses.d/claude.yaml" },  // field -> "builtin" or the file that set it
  "levels": {                      // feature 002 (FR-022), per category: all / content / none
    "content":       { "all": "link", "content": "link",      "none": "own" },
    "settings":      { "all": "link", "content": "copy-once", "none": "minimal" },
    "plugin-store":  { "all": "link", "content": "link",      "none": "own" },
    "hook-location": { "all": "link", "content": "managed",   "none": "managed" }
  }
}
```

A treatment is `link`, `own`, `copy-once`, `minimal` or `managed`; a level the category does not
apply to is omitted.

### `aprofile mcp list <h>/<n> --json`

```jsonc
{
  "format_version": 1,
  "profile": "claude/work",
  "servers": [                     // sorted by name; [] when the profile has none
    { "name": "fs", "transport": "stdio", "command": "npx" },
    { "name": "web", "transport": "http", "url": "https://example.com/mcp",
      "enabled": false }           // command, url and enabled are omitted when unset
  ]
}
```

`command` and `url` are for display: references in them are shown as `${NAME}`, and every part of
`url` that a conversion would drop as a literal secret (FR-035) is replaced by `***`. Arguments,
environment and headers are never printed.

### `aprofile copy <h>/<n> <h>/<n> --json` and `aprofile mcp copy <from> <to> --json`

Both print the same report document; `mcp copy` fills only the MCP part.

```jsonc
{
  "format_version": 1,
  "source": "claude/work", "target": "codex/work",
  "mode": "cross-harness",         // same-harness | cross-harness | mcp-copy
  "mcp": {                         // MCP conversion (FR-035)
    "converted": [ "fs" ],         // servers newly written ([] for a same-harness copy, see mcp_carried; never null)
    "replaced": [ "web" ],         // existing target servers overwritten (--overwrite)
    "skipped": [ { "name": "x", "reason": "…" } ],
    "drops": [ { "server": "web", "field": "headers[X]", "reason": "literal-secret" } ]  // or "unrepresentable"
  },
  "skills_carried": false,
  "skills_report": [ "…" ],
  "dropped_settings": [ "auth" ],  // settings a cross-harness copy does not carry (FR-033b)
  "plugins_skipped": [ "…" ],
  "secret_warnings": [ { "server": "web", "field": "headers[X]" } ],  // same-harness, verbatim carry (FR-033a)
  "mcp_carried": [ "fs", "web" ],  // same-harness: servers carried verbatim
  "owned_secret_warnings": [ "<file>: <key path>" ],  // secret-looking owned values outside MCP servers
  "owned_not_converted": [ "<entry>", "<file>#<key>" ],  // cross-harness
  "skipped_plugins": [ { "name": "…", "skills": [ "…" ], "mcp_servers": [ "…" ] } ],
  "skills_in_target": [ "…" ], "skills_missing_in_target": [ "…" ], "skills_home_escapes": [ "…" ],
  // feature 002 (FR-007a):
  "share": "content",              // the target's level; "" for mcp copy
  "settings_carried": [ "…" ], "hook_sets_carried": [ "…" ], "hook_sets_left_out": [ "…" ],
  "settings_warnings": [ "…" ],
  "backup": "/abs/backups/codex/work/<timestamp>"  // copy --overwrite: where the replaced data was
                                                   // backed up; omitted when nothing was replaced
}
```

`format_version`, `source`, `target`, `mode`, `mcp.converted`, `skills_carried` and `share` are
always present; every other field is omitted when empty. Field names in `drops` and warnings name
the server and field, never the value.

### `aprofile import cc-deck --json` (with or without `--dry-run`)

```jsonc
{
  "format_version": 1,
  "source_dir": "/home/u/.config/cc-deck",
  "source_data_dir": "/home/u/.local/share/cc-deck",
  "dry_run": true,                 // true: nothing was changed, actions are the plan
  "warnings": [ "…" ],             // run-wide warnings; omitted when none
  "actions": [                     // one per cc-deck profile
    {
      "id": "claude/work",
      "status": "import",          // import | overwrite | skip | failed
      "reason": "…",               // why it was skipped or failed
      "credentials_replaced": true, // real run with --overwrite-credentials only
      "backup_dir": "/abs/backups/claude/work/<timestamp>",  // real run, when something was backed up
      "primary_moved_from": [ "claude/old" ],  // profiles whose primary flag (would) move to this one
      "warnings": [ "…" ]
    }
  ],
  "collisions": [ { "wrapper_name": "claude-work", "path": "/usr/local/bin/claude-work" } ]  // wrapper names already on PATH
}
```

Every action field after `status` is omitted when empty. A dry run reports the same statuses a
real run would produce, except `failed`, which only a real run can report. `collisions` lists only
profiles that are imported or overwritten. The exit status is non-zero when an action failed or
was skipped because the profile already exists or another profile has its wrapper name (FR-045,
FR-002); other skips (no harness definition, a cc-deck profile that cannot be read) are reported
with their reason and do not change the exit status.
