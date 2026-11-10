# Contract: CLI changes (feature 004)

No new commands or flags. Changed behavior and output only.

## `aprofile sync`

- Per profile, after hooks and before the wrapper: place launch entries (FR-003).
- Text output lines (names only, never values):
  - `claude/work-deepseek: settings.json: set env ANTHROPIC_MODEL`
  - `claude/work-deepseek: settings.json: set apiKeyHelper`
  - `claude/work-deepseek: settings.json: replaced user entry env ANTHROPIC_BASE_URL`
  - `codex/work-glm: config.toml: set model_providers.enmaas.auth (replaces env_key)`
  - `codex/work-glm: config.toml: removed model_providers.enmaas.auth (restored env_key)`
- `--dry-run` prints the same lines prefixed as today's dry-run steps, plus the note
  `config.toml is re-encoded: comments are dropped` when a TOML file would change.
- Error: `claude/x: settings.json: cannot place launch settings: <reason> (key env)`; the
  profile's wrapper and registry entry are left unchanged; exit non-zero.
- `--json`: launch steps appear in the profile's `config_dir.steps` with action `launch-set`,
  `launch-remove`, `launch-replace-user`, path `<file>`, note `<location>`; `changes` counts them.

## `aprofile show <harness>/<name>`

Text lines:

```text
folder:       self-contained            # or: self-contained (needs ANTHROPIC_KEY_VAR in the launching environment)
                                        # or: not self-contained
              wrapper-only: model (passed by the wrapper because the harness rewrites it)
              wrapper-only: OTEL_TOKEN (reference value)
folder sync:  synced                    # or: stale (run: aprofile sync)
unplaced:     env ANTHROPIC_BASE_URL (not set by this profile; overrides it)
```

`--json` adds:

```jsonc
"folder": {
  "status": "self-contained" | "self-contained-env" | "not-self-contained",
  "env_dependent": ["ANTHROPIC_KEY_VAR"],
  "wrapper_only": [ { "setting": "model", "variable": "", "reason": "harness-rewrites" } ],
  "sync": "synced" | "stale" | "unknown",
  "unplaced": [ { "file": "settings.json", "location": "env.ANTHROPIC_BASE_URL" } ]
}
```

## `aprofile list`

The status bracket includes `folder stale` when the folder needs `sync` (same rule as a stale
wrapper). `--json` adds `"folder_sync"`.

## `aprofile doctor`

- `info`: folder self-containment and each wrapper-only setting with reason (FR-020).
- `problem`: `profile folder is stale` → `run: aprofile sync` (FR-020a).
- `problem`: `settings.json sets env ANTHROPIC_BASE_URL, which this profile does not set` →
  `remove the entry or set it on the profile` (FR-020b).
- `problem` (share level `all`): `the linked settings.json sets env ANTHROPIC_BASE_URL, which
  overrides the value the wrapper sets` → `remove the entry from the default setup or use share
  level content` (FR-020b).

## `aprofile edit --share <level> [--dry-run]`

Adds lines to the plan: to `all`: `launch settings become wrapper-only (settings.json is linked)`;
from `all`: `launch settings will be placed into settings.json by the next sync`.

## `aprofile add|edit --model` for Codex

Accepted (FR-023). Help text: `model for the profile (set through the harness's model variable or
launch argument)`.

## `aprofile settings pull`

Unchanged interface; after applying, the profile's launch entries are placed again in the same
command and listed with the pulled keys.

## `aprofile harness show <name>`

Prints `folder_launch`, `launch_clear`, `model_arg` and `folder_key` per auth kind, with origins.
