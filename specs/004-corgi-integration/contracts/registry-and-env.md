# Contract: registry, environment contract and wrapper behavior (feature 004)

Extends `specs/001-aprofile-standalone-profile-cli/contracts/registry-and-env.md`.

## Registry: `model` (additive, `format_version` stays 1)

Every entry gains `model`, a string: the profile's configured model from the profile store, or
`""` when the profile has none. It is never read from a harness file, so a `/model` pick never
shows up here.

```jsonc
{
  "harness": "claude", "name": "work-deepseek", "label": "Claude DeepSeek (work)",
  "color": "#E27C5A", "icon": "🐋", "auth_kind": "api-key",
  "model": "rits/deepseek-ai/DeepSeek-V4.1-Flash-1017[1m]",
  "primary": false, "config_dir": "/…/profiles/claude/work-deepseek",
  "wrapper_path": "/…/bin/claude-work-deepseek", "share": "content", "history_group": "work"
}
```

## Environment contract changes

| Variable | Change |
|---|---|
| `APROFILE_SET_VARS` | Names every launch variable the profile supplies, whether the folder or the wrapper carries it. Carried by the folder (`env` block) when the folder carries any launch variable, otherwise exported by the wrapper. Every wrapper clears the names it inherits, unconditionally. |
| model, endpoint, extra variables | No longer caller-overridable. Folder-carried ones are cleared by the wrapper and supplied by the harness from its folder; wrapper-carried ones are set unconditionally. |
| auth variables | Unchanged rules, plus: when the folder carries the key, the wrapper checks the reference resolves (same failure message) but exports no key; the profile's own auth variable is cleared. |

## Wrapper order (shell and `wrapper.Environ`)

0. Capture the sources of every `env:` reference (auth and extra variables) into private shell
   variables, so a self-referencing pass-through (`CLOUD_ML_REGION=env:CLOUD_ML_REGION`) survives
   the clearing below.
1. Clear: definition `launch_clear`, names in inherited `APROFILE_SET_VARS` (skipping invalid names,
   `PATH`, `HOME`, the config-folder variable and `APROFILE_*`), the profile's own launch
   variables, `APROFILE_SET_VARS`.
2. Config-folder variable.
3. `APROFILE_*` contract variables.
4. Auth: capture `env:` sources, clear every auth kind's variables and `auth_clears`; set the key
   when wrapper-carried; when folder-carried, resolve into a private shell variable, fail closed,
   then unset it.
5. `HERDR_AGENT` when declared.
6. Wrapper-carried launch variables, set unconditionally; `APROFILE_SET_VARS` when wrapper-carried.
7. Config-dir guard, binary guard.
8. `exec <binary> <model_arg…> "$@"`.

One-run changes use the harness's flags: `claude-<name> --model <m>`, `codex-<name> -m <m>` (the
wrapper's `-c model="…"` gives way to the caller's `-m`).
