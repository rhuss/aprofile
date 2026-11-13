# Contract: harness definition additions (feature 004)

Extends `specs/001-aprofile-standalone-profile-cli/contracts/harness-definition.md` and the
spec 002/003 additions. All fields are optional; a definition without them keeps every launch
setting in the wrapper. User files in `harnesses.d` override them with the existing merge rules
(mappings merge by key, lists replace, explicit `null` removes).

```yaml
# Claude Code (built-in)
folder_launch:
  env: "settings.json#env"
  key_command:
    location: "settings.json#apiKeyHelper"
    form: shell
launch_clear:
  - ANTHROPIC_MODEL
  - ANTHROPIC_BASE_URL
  - ANTHROPIC_DEFAULT_OPUS_MODEL
  - ANTHROPIC_DEFAULT_SONNET_MODEL
  - ANTHROPIC_DEFAULT_HAIKU_MODEL
  - ANTHROPIC_DEFAULT_FABLE_MODEL
  - CLAUDE_CODE_SUBAGENT_MODEL
auth_kinds:
  - { name: api-key,    vars: { ANTHROPIC_API_KEY: ref },       folder_key: true }
  - { name: auth-token, vars: { ANTHROPIC_AUTH_TOKEN: ref },     folder_key: true }
  - { name: oauth-token, vars: { CLAUDE_CODE_OAUTH_TOKEN: ref } }   # folder_key: false
  - { name: oauth-login, vars: {} }
```

```yaml
# Codex (built-in)
folder_launch:
  key_command:
    location: "config.toml#model_providers"
    form: provider-auth
    selector: model_provider
    replaces: [env_key]
model_arg: ["-c", "model={model:toml}"]
launch_clear: [OPENAI_BASE_URL]
auth_kinds:
  - { name: chatgpt-login, vars: {} }
  - { name: api-key, vars: { OPENAI_API_KEY: ref }, folder_key: true }
```

## Forms

| Form | Writes | Value |
|---|---|---|
| (env) | one entry per folder-carried variable in the object at `folder_launch.env` | the literal value; `""` for neutralizing entries |
| `shell` | a string at `key_command.location` | `k=; IFS= read -r k < '<path>' \|\| [ -n "$k" ]; printf '%s' "$k"` for `file:`; `printf '%s' "${NAME:-}"` for `env:` |
| `provider-auth` | table `auth` inside `<location>.<selected>` where `<selected>` is the value of top-level `<selector>` | `command = "sh"`, `args = ["-c", 'k=; IFS= read -r k < "$1" \|\| [ -n "$k" ]; printf "%s" "$k"', "aprofile-key", "<path>"]` for `file:`; `args = ["-c", 'printf "%s" "${NAME:-}"']` for `env:` |

`{model:toml}` in `model_arg` expands to the model as a TOML basic string (`"…"` with `\` and `"`
escaped); `{model}` expands to the raw model. The wrapper shell-quotes each argument.

## Validation errors (file and field named, FR-012 of spec 001)

- `folder_launch.env` / `key_command.location`: not `file#key`, or file not classified.
- `key_command.form`: unknown form; `provider-auth` without `selector`.
- `launch_clear`: invalid name, config-folder variable, or `APROFILE_*`.
- `model_arg`: placeholder missing or repeated.
- `auth_kinds[].folder_key: true` without `folder_launch.key_command`.

`harness show` prints each new field with its origin, like existing fields.
