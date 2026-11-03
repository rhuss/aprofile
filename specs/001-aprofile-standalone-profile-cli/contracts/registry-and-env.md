# Contract: registry format and environment contract (public interfaces)

These are public interfaces (Constitution V, FR-030, FR-032). Breaking changes require a bumped
`format_version` and a migration note.

## Registry file

- Location: `<aprofile data dir>/registry.json` (fixed, independent of the bin directory). Printed
  by `aprofile registry path`.
- Written atomically by `sync`. Contains no timestamps or volatile fields: an unchanged `sync`
  rewrites nothing (FR-030). Contains no secret values (FR-014, FR-028).

```jsonc
{
  "format_version": 1,
  "profiles": [
    {
      "harness": "claude",
      "name": "work",
      "label": "Work",
      "color": "blue",
      "icon": "W",
      "auth_kind": "oauth-token",
      "primary": true,
      "config_dir": "/abs/path/to/profile/config/dir",
      "wrapper_path": "/abs/path/to/bin/claude-work"
    }
  ]
}
```

Field notes: `color`/`icon` are the display values from the profile. `config_dir` is the directory
the wrapper points the harness at. `wrapper_path` is the generated launcher. No credentials, no
references.

## Omarchy export (opt-in, FR-031)

Written only on `aprofile registry export --format omarchy`, one file per provider at
`$XDG_STATE_HOME/omarchy/agents/accounts/<provider>.json` (fallback `~/.local/state`, override
`--output-dir`):

```jsonc
{ "accounts": [ { "id": "aprofile:claude/work", "label": "Work", "home": "/abs/config/dir", "primary": true } ] }
```

Entries whose `id` lacks the `aprofile:` prefix are preserved unchanged; `aprofile:` entries for
profiles that no longer exist are removed. `provider` comes from the harness definition.

## Environment contract (APROFILE_*)

Every wrapper sets these, never caller-overridable (FR-026):

| Variable | Value |
|----------|-------|
| `APROFILE_HARNESS` | harness id |
| `APROFILE_NAME` | profile name |
| `APROFILE_LABEL` | profile label |
| `APROFILE_COLOR` | profile color |
| `APROFILE_ICON` | profile icon |
| `APROFILE_CONFIG_DIR` | the profile config dir |
| `APROFILE_SET_VARS` | space-separated names of the model/endpoint/extra variables the wrapper set |

The wrapper also sets the harness's own config-dir variable (e.g. `CLAUDE_CONFIG_DIR`) and the
profile's auth variables, and unsets other auth kinds' variables. Consumers (corgi, herdr, cc-deck)
read the registry file or these variables; aprofile requires no change in them (FR-032).

## Exec hook protocol (FR-011)

Executable `aprofile-harness-<name>` on PATH. Invoked with the operation name as the sole argument;
`key=value` lines on stdin; `key=value` lines expected on stdout. Every call receives `harness`,
`profile`, `config_dir`.

| Operation | Called by | Extra input keys | Output keys |
|-----------|-----------|------------------|-------------|
| `env` | `sync` (wrapper render) | `auth_kind`, `model`, `endpoint` | any `VAR=value`; secret values MUST use `env:`/`file:` syntax; output is subject to FR-028's prefix check only |
| `pre-launch` | the wrapper, before exec | none | none (exit status only) |
| `write-config` | `sync`, after preparing config dir | `auth_kind`, `model`, `endpoint` | `changed=true\|false` |
| `auth-status` | `show`, `doctor` | `auth_kind` | `status=ok\|missing\|unknown`, optional `detail` |
| `login` | `login` | `auth_kind` | none (interactive) |

Exit 64 = operation unsupported (no-op). Any other non-zero, a timeout (10 s, except `login` and
`pre-launch`), or non-`key=value` output aborts the command naming the hook and operation.
