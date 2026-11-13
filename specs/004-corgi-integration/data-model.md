# Data Model: Self-Contained Profile Folders

## Harness definition additions (data, Principle I)

| Field | Type | Meaning |
|---|---|---|
| `folder_launch.env` | `file#key` string | Location of an object of environment entries the harness applies at start (Claude: `settings.json#env`). Absent: no folder-carried variables. |
| `folder_launch.key_command.location` | `file#key` string | Where the key command goes (Claude: `settings.json#apiKeyHelper`; Codex: `config.toml#model_providers`). |
| `folder_launch.key_command.form` | enum `shell` \| `provider-auth` | `shell`: a shell command string at the location. `provider-auth`: an `auth` table (`command`, `args`) inside the provider the selector names. |
| `folder_launch.key_command.selector` | string | `provider-auth` only: top-level key naming the selected provider (Codex: `model_provider`). |
| `folder_launch.key_command.replaces` | list of strings | `provider-auth` only: keys in the provider table that conflict with `auth` and are replaced and recorded (Codex: `env_key`). |
| `launch_clear` | list of variable names | Launch variables every wrapper of this harness clears before launching (Claude: model, endpoint, `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU,FABLE}_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL`). |
| `model_arg` | list of strings | Arguments the wrapper passes before the caller's for the profile's model; `{model:toml}` is replaced by the model as a TOML basic string, `{model}` by the raw model (Codex: `["-c", "model={model:toml}"]`). |
| `auth_kinds[].folder_key` | bool (default false) | Whether the folder can carry this kind's key (Claude `api-key`, `auth-token`; Codex `api-key`). |

Derived: `CarriesModel()` = `model_var != "" || len(model_arg) > 0`.

Validation:
- Locations use the existing `file#key` syntax; the file must be classified (Claude `settings.json`
  as settings, Codex `config.toml` as owned).
- `launch_clear` names are valid variable names and may not name the config-folder variable or
  `APROFILE_*`.
- `model_arg` must contain `{model:toml}` or `{model}` exactly once.
- `folder_key: true` requires `folder_launch.key_command`.
- `auth_clears` no longer rejects the model and endpoint variables on the grounds that they are
  caller-overridable (that rule existed for `${VAR:-default}`); the other `auth_clears` rules stay.

## Launch setting (derived, not stored)

| Attribute | Values |
|---|---|
| kind | `model`, `endpoint`, `extra:<NAME>`, `key` |
| variable | the variable name (model/endpoint from the definition, extra from the profile, key from the auth kind) |
| carrier | `folder` \| `wrapper` |
| reason (wrapper only) | `no-support` (definition declares no location), `reference-value` (extra variable given as reference), `auth-kind` (`folder_key` false), `share-all` (settings file linked), `no-provider` (Codex selector names no provider defined in the file, or `config.toml` absent), `harness-rewrites` (passed by `model_arg`), `cleared-source` (`env:` key whose variable is in the auth-clearing list or `launch_clear`), `hook-env` (variable from the exec hook's `env` operation) |
| env_dependent | true for a folder-carried key from an `env:` reference |

Rules: the carrier decision is a pure function of profile, definition, share level and the current
file. Login auth kinds (`oauth-login`, `chatgpt-login`) have no key setting.

## Placed entry

| Attribute | Meaning |
|---|---|
| file | profile-relative file (`settings.json`, `config.toml`) |
| location | `env.<NAME>`, `apiKeyHelper`, `model_providers.<p>.auth` |
| replaced | for `provider-auth`: map of replaced keys to their original raw TOML value (`env_key = "OPENAI_API_KEY"`) |

Desired entries for a profile:
- `env.<VAR>` = value for each folder-carried variable (model, endpoint, literal extra variables).
- `env.APROFILE_SET_VARS` = launch variable names, when any variable is folder-carried.
- `env.<AUTHVAR>` = `""` for each name in the auth-clearing list, when the key is folder-carried and
  the definition has an `env` location, except names the profile supplies as extra variables
  (whichever carrier).
- `apiKeyHelper` = shell key command, or `model_providers.<p>.auth` = provider-auth table.

## Launch record (stored)

Path: `<XDG_DATA_HOME>/aprofile/managed-launch/<harness>/<name>.json`

```json
{
  "format_version": 1,
  "entries": [
    { "file": "settings.json", "location": "env.ANTHROPIC_MODEL" },
    { "file": "settings.json", "location": "apiKeyHelper" },
    { "file": "config.toml", "location": "model_providers.enmaas.auth",
      "replaced": { "env_key": "\"OPENAI_API_KEY\"" } }
  ]
}
```

Lifecycle:
- Created on the first `sync` that places an entry; rewritten only when its content changes;
  deleted when empty.
- Moved by `rename`, deleted by `remove --purge`, dropped by a share level change to `all`.
- Never holds a value of a placed `env` entry; only locations and replaced keys.

State transitions per entry:

```text
absent ──sync places──▶ placed ──profile no longer sets it──▶ removed (replaced keys restored)
   ▲                      │  ▲
   │       user edits/deletes│  │sync restores
   └──user entry replaced────┘  └─────────────
```

## Registry entry

Existing fields plus `model` (string; profile store's model, empty when unset). Field order:
`harness, name, label, color, icon, auth_kind, model, primary, config_dir, wrapper_path, share,
history_group`. `format_version` stays 1.

## Profile store

No new fields. `model` becomes valid for Codex (`CarriesModel()`); validation of a model value for
a `model_arg` harness additionally rejects control characters (they cannot be a TOML basic string).
