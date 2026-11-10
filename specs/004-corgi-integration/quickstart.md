# Quickstart: validating self-contained profile folders

Run everything in a throwaway home so the real setup is untouched:

```sh
export HOME=$(mktemp -d) XDG_CONFIG_HOME= XDG_DATA_HOME= XDG_STATE_HOME=
go build -o /tmp/aprofile ./cmd/aprofile
printf 'sk-test-canary\n' > "$HOME/key"
```

## 1. Claude folder carries its launch settings (US1, FR-003 to FR-006a)

```sh
/tmp/aprofile add claude demo --auth-kind api-key --auth-key "file:$HOME/key" \
  --model gw-model --endpoint http://127.0.0.1:18801 --share content \
  --env ANTHROPIC_DEFAULT_HAIKU_MODEL=gw-haiku
/tmp/aprofile sync
jq '{env, apiKeyHelper}' "$(/tmp/aprofile show claude/demo --json | jq -r .config_dir)/settings.json"
```

Expected: `env` holds `ANTHROPIC_MODEL`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`,
`APROFILE_SET_VARS` and empty entries for the auth-clearing list; `apiKeyHelper` reads
`$HOME/key`; `sk-test-canary` appears nowhere (`grep -r sk-test-canary "$HOME/.local" "$HOME/.config"`
finds only `$HOME/key`). Running the helper with `sh -c "$(jq -r .apiKeyHelper …)"` prints the key.

Manual (real Claude Code, stub server from research.md): `env -i HOME=$HOME PATH=$PATH
CLAUDE_CONFIG_DIR=<dir> claude -p hi` sends discovery and messages to the endpoint with the key.

## 2. Idempotency and stale folder (SC-005, FR-020a)

```sh
/tmp/aprofile sync          # 0 changes
jq '.env.ANTHROPIC_MODEL="x"' settings.json > t && mv t settings.json
/tmp/aprofile show claude/demo   # folder sync: stale
/tmp/aprofile sync               # restores gw-model
```

## 3. Codex provider auth and model argument (US2, FR-009, FR-016)

Seed `config.toml` with `model_provider = "gw"` and `[model_providers.gw]` holding `base_url` and
`env_key = "OPENAI_API_KEY"`, then:

```sh
/tmp/aprofile add codex demo --auth-kind api-key --auth-key "file:$HOME/key" --model 1.5 --adopt
/tmp/aprofile sync --dry-run    # lists auth placement, env_key replacement, comment note
/tmp/aprofile sync
grep -A3 'model_providers.gw.auth' <dir>/config.toml
tail -1 "$(/tmp/aprofile registry path | xargs dirname)/bin/codex-demo"   # exec 'codex' '-c' 'model="1.5"' "$@"
```

Switching the profile to `chatgpt-login` and syncing removes the `auth` block and restores
`env_key`.

## 4. No caller overrides, no leaks (US4, FR-013, FR-014)

With the stub harness on `PATH` as `claude`:

```sh
ANTHROPIC_MODEL=other claude-demo          # stub env shows no ANTHROPIC_MODEL (folder carries it)
APROFILE_SET_VARS="ANTHROPIC_DEFAULT_HAIKU_MODEL" ANTHROPIC_DEFAULT_HAIKU_MODEL=leak claude-private
                                           # stub env shows no ANTHROPIC_DEFAULT_HAIKU_MODEL
```

## 5. Reporting and registry (US3, US5)

```sh
/tmp/aprofile show claude/demo           # folder: self-contained
/tmp/aprofile doctor                     # info lines; problem for an unplaced env entry
jq '.profiles[] | {name, model}' "$(/tmp/aprofile registry path)"
```

## 6. Corgi registration (FR-022, manual, SC-007)

Follow `docs/guide/docs/user/corgi.md`: `corgi account add claude work-deepseek --dir <config_dir>`
for each daily profile, then the cleanup steps; all rows launch and show the expected model.
