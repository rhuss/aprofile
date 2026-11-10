# Register profiles with Corgi

<!-- Sources:
aprofile:specs/004-corgi-integration/spec.md
aprofile:specs/005-neutralize-provider-switches/spec.md
aprofile:internal/launch/carrier.go
aprofile:internal/launch/plan.go
aprofile:internal/harness/builtin/claude.yaml
aprofile:internal/harness/builtin/codex.yaml
-->

Corgi, and any tool that starts an agent by setting only the harness's config-folder variable (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`), can launch an aprofile profile directly from its folder. `aprofile sync` writes each profile's launch settings into the harness's own files wherever the harness can read them there, so the folder carries its endpoint, model, model aliases and a command that reads the key. aprofile never runs, configures or writes into Corgi; registration is a one-time manual step. The commands below assume you are in the aprofile checkout root and use `./bin/aprofile`.

## Check which profiles launch from their folder

1. Bring every folder up to date.

   ```sh
   ./bin/aprofile sync
   ```

   Check: sync lists the launch entries it places (`settings.json: set env ANTHROPIC_MODEL`, `config.toml: set model_providers.<p>.auth`) and never prints a value.
   <!-- expect: exit 0 -->

2. Look at each profile's folder status.

   ```sh
   ./bin/aprofile show claude/work
   ./bin/aprofile doctor
   ```

   Check: `folder:` says `self-contained`, `self-contained (needs NAME in the launching environment)`, or `not self-contained` with one `wrapper-only:` line per setting the folder cannot carry. `folder sync:` says `synced`.
   <!-- expect: exit 0 -->

## Register each profile

Use Corgi's own account command with the folder path from `aprofile show <harness>/<name>` (`config dir:`) or from the registry (`config_dir`):

```sh
corgi account add claude work-deepseek --dir "$(./bin/aprofile show claude/work-deepseek --json | jq -r .config_dir)"
corgi account add codex work-glm --dir "$(./bin/aprofile show codex/work-glm --json | jq -r .config_dir)"
```

The registry (`./bin/aprofile registry path`) lists every profile with its `config_dir`, `label`, `color`, `icon` and `model`, so a script can register all of them at once.

## Clean up old Corgi rows once

- Remove rows that point at old cc-deck folders and duplicate accounts with `corgi account remove`.
- Delete saved custom commands that started a wrapper or exported keys; the folder now carries those settings.
- Hide default rows that duplicate a named profile.

## What the folder blocks from the launching environment

A launching tool can carry stale exports, such as `CLAUDE_CODE_USE_VERTEX=1` or `ANTHROPIC_API_KEY` from an old shell. The wrapper clears these before it starts the harness. For a folder launch, `aprofile sync` writes an empty `env` entry into the profile's own `settings.json` for each provider switch and auth variable on Claude Code's clearing list. Claude Code treats an empty entry as unset, so the exported value has no effect. This applies to every Claude profile at share level `content` or `none`, whatever its auth kind, so a Claude subscription login stays on its login.

Names the profile sets itself are left alone: its extra variables (a provider switch set with `--env` keeps its value), exec-hook variables and the token or key the wrapper supplies (`CLAUDE_CODE_OAUTH_TOKEN` of an `oauth-token` profile).

A login profile at share level `all` cannot get these entries, because its `settings.json` is the linked default setup, which aprofile never writes. Started from the folder alone, it inherits whatever provider switches and keys the launching tool exports. `show` lists `wrapper-only: auth-clearing` for it and `doctor` reports the gap as information. Start such a profile through its wrapper, or give it its own settings with `./bin/aprofile edit claude/<name> --share content` and run `./bin/aprofile sync`.

Codex needs no such entries: a Codex login profile started from `CODEX_HOME` alone ignores exported `OPENAI_API_KEY` and `OPENAI_BASE_URL`.

## What stays with the wrapper

Some settings cannot live in the folder. They are named by `show` and `doctor` with the reason:

| Setting | Why it stays with the wrapper |
| --- | --- |
| An extra variable given as a reference (`env:`/`file:`) | Writing it into the folder would turn it into a literal secret |
| A Claude `oauth-token` key | The harness cannot read it through a key command |
| Any Claude Code setting of a profile at share level `all` | Its `settings.json` is linked to the default setup, which aprofile never writes (a Codex `config.toml` is never linked, so Codex keys stay in the folder) |
| Auth clearing of a Claude login profile at share level `all` (`auth-clearing`) | The linked `settings.json` cannot hold the empty entries that block exported provider switches and keys |
| A Codex key without a custom provider in `config.toml` | The folder has no provider block to carry it |
| A key read from a variable aprofile clears (`env:ANTHROPIC_AUTH_TOKEN`, `env:OPENAI_API_KEY`) | The folder would read an emptied variable |
| The Codex model | Codex saves `/model` picks into `config.toml`, so the wrapper passes `-c model="..."` instead |
| A variable from the exec hook's `env` operation | The hook runs at launch time, so only the wrapper has its output |
| Any setting of an OpenCode or pi profile | These harnesses read no launch settings from their folder |

A key referenced as `env:NAME` is carried by the folder, but the launching tool must have `NAME` in its environment.

## Known differences

- Corgi's model column for a Claude folder reads the `model` key Claude Code writes on `/model` and can show a stale pick; the session itself starts on the profile's model.
- A Codex folder started without the wrapper follows the last saved `/model` pick, because aprofile passes the Codex model only as a launch argument.
- One-run changes go through the harness's own flags: `claude-work --model <m>`, `codex-work-glm -m <m>`. Exported `ANTHROPIC_MODEL` or `ANTHROPIC_BASE_URL` no longer override a profile.
