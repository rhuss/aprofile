---
runbook_version: 1
feature: 004-corgi-integration
status: passed
mode: light
steps_total: 15
step_results: passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed
drafted_at: ddeb5cd
drafted_fingerprint: 9cfbbfe5b94a7c732848b39fa8acf38e09f3def37fc48c0a570f1c0d88960e8a
tested_at: 88b6d1c
tested_fingerprint: 35605f379c35494df8144ae8dd7f210735a449c13d9597bcedc499682bcf20d0
updated: 2026-11-10
---

# Try it: Self-Contained Profile Folders

<!-- runbook-status -->
> **Status:** passed · 15 of 15 steps (15 passed) · Mode: light · Tested at 88b6d1c · 2026-11-10
<!-- /runbook-status -->
















## What you're testing

aprofile now writes each profile's launch settings into the harness's own files, so a tool that
only sets `CLAUDE_CONFIG_DIR` or `CODEX_HOME` (Corgi) launches the profile against the right
endpoint, with the right key, on the profile's model. Wrappers no longer take environment
overrides, and the registry publishes each profile's model.

The journey runs entirely in a throwaway home inside this spec's scratch folder:

1. Build aprofile and set up the throwaway home (steps 1 to 2).
2. A Claude API-key profile: `sync` fills its `settings.json`; check the key command, idempotency
   and a restored edit (steps 3 to 7).
3. The Claude wrapper ignores exported overrides and clears leaked variables (step 8).
4. The real Claude Code starts from the folder alone against a local stub endpoint (steps 9 to 10).
5. A Codex API-key profile: provider `auth` block, `env_key` replaced, model passed by the wrapper
   (steps 11 to 13).
6. Registry `model`, `show` and `doctor` reporting (steps 14 to 15).

Every step starts by loading `env.sh` from the scratch folder, so each command can be pasted into
a fresh shell.

## Test environment

One quick fixture: a local HTTP stub that logs request paths, auth headers and models
(`stub-endpoint`, a Python process on port 18801, started in step 9 by the agent with
`spex-runbook-fixture.sh start … --kind process`; teardown: `spex-runbook-fixture.sh stop
specs/004-corgi-integration/.walkthrough --quick`, stopped at wrap-up). Fake `claude` and `codex`
programs in the scratch `fakebin` print their environment and arguments for the wrapper checks.

## Steps

### Step 1 of 15: Build aprofile and create the throwaway home (US1)

**Why this step:** everything below must run against the branch's binary and never touch your
real home, harness configs or shell files.

**Needs:** nothing.

**Run:**

```sh
W="$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough" && rm -rf "$W/home" "$W/fakebin" && mkdir -p "$W/home" "$W/fakebin" && go build -o "$W/aprofile" ./cmd/aprofile && printf '%s\n' "export W='$W'" "export HOME='$W/home'" 'unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_STATE_HOME ZDOTDIR APROFILE_SET_VARS APROFILE_NAME APROFILE_HARNESS ANTHROPIC_MODEL ANTHROPIC_BASE_URL ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN OPENAI_API_KEY CLAUDE_CONFIG_DIR CODEX_HOME' 'export SHELL=/bin/sh' 'export PATH="$W/fakebin:$HOME/.local/share/aprofile/bin:$PATH"' 'ap() { "$W/aprofile" "$@"; }' > "$W/env.sh" && . "$W/env.sh" && printf 'sk-walkthrough-canary\n' > "$HOME/work.key" && echo "home: $HOME" && ap registry path
```

**What you should see:** `home:` followed by the scratch path ending in `.walkthrough/home`, then the
registry path inside that home (`…/.walkthrough/home/.local/share/aprofile/registry.json`). No
errors from `go build`.

**If you see something else:** a `go build` error means the branch does not compile; `ap: command
not found` means `env.sh` was not sourced.

**Result:**

Passed (agent). Runbook corrected: aprofile has no `--version`, so the step now ends with `ap registry path`. Output: `home: …/.walkthrough/home` and `…/.walkthrough/home/.local/share/aprofile/registry.json`.

### Step 2 of 15: Seed a default Claude setup and fake harness programs (US1, US4)

**Why this step:** a `content` profile copies the default setup's settings once, so the default
needs a user `env` entry to prove aprofile keeps it. The fake programs let us see exactly what a
wrapper passes to the harness.

**Needs:** step 1.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && mkdir -p "$HOME/.claude" && printf '%s\n' '{"env":{"MLFLOW_TRACKING_URI":"http://mlflow.local:5000"},"theme":"dark"}' > "$HOME/.claude/settings.json" && for b in claude codex; do printf '%s\n' '#!/bin/sh' 'echo "ARGS: $*"' 'env | sort | grep -E "^(ANTHROPIC|OPENAI|CLAUDE_CONFIG_DIR|CODEX_HOME|APROFILE_SET_VARS|LEAKED)" | sed "s/^/ENV: /"' > "$W/fakebin/$b"; chmod +x "$W/fakebin/$b"; done && ls "$W/fakebin" && cat "$HOME/.claude/settings.json"
```

**What you should see:** `claude` and `codex` listed, then the default settings JSON with
`MLFLOW_TRACKING_URI`.

**If you see something else:** permission errors mean the scratch folder is not writable.

**Result:**

Passed (agent). `claude` and `codex` listed in fakebin; default settings show `MLFLOW_TRACKING_URI` and `theme`.

### Step 3 of 15: Add a Claude API-key profile and sync (US1)

**Why this step:** this is the core of the feature: `sync` places the launch settings into the
profile's `settings.json`.

**Needs:** step 2.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && ap add claude demo --share content --auth-kind api-key --auth-key "file:$HOME/work.key" --model gw-model --endpoint http://127.0.0.1:18801 --env-literal ANTHROPIC_DEFAULT_HAIKU_MODEL=gw-haiku && ap sync
```

**What you should see:** `sync` lists launch steps for `claude/demo` such as `settings.json: set env
ANTHROPIC_MODEL`, `set env ANTHROPIC_BASE_URL`, `set env ANTHROPIC_DEFAULT_HAIKU_MODEL`, `set env
APROFILE_SET_VARS`, `set apiKeyHelper`, and one line per blanked auth variable. No key value
(`sk-walkthrough-canary`) anywhere in the output. A PATH note may say the shell file could not be
edited (expected with `SHELL=/bin/sh`).

**If you see something else:** an error naming `settings.json` means the planner could not parse
the copied settings; no launch lines means the carrier decision put everything in the wrapper.

**Result:**

Passed (agent). Launch lines `set env ANTHROPIC_MODEL`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `APROFILE_SET_VARS`, 25 neutralizing `set env` lines and `set apiKeyHelper`; test key appears 0 times in the output; PATH note for `sh` as expected. Log: `.walkthrough/step-3.log`.

### Step 4 of 15: Inspect the folder's settings and run the key command (US1)

**Why this step:** proves the folder holds endpoint, model, aliases and a key command, but no key,
and that the command produces exactly the key the wrapper would.

**Needs:** step 3.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && S="$HOME/.local/share/aprofile/profiles/claude/demo/settings.json" && jq '{theme, env, apiKeyHelper}' "$S" && printf 'helper output: ' && sh -c "$(jq -r .apiKeyHelper "$S")" && echo && (grep -c sk-walkthrough-canary "$S" || true)
```

**What you should see:** `theme: "dark"`; `env` holds `MLFLOW_TRACKING_URI` (kept),
`ANTHROPIC_MODEL: "gw-model"`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL: "gw-haiku"`,
`APROFILE_SET_VARS` naming those three variables, and empty strings for `ANTHROPIC_API_KEY`,
`ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_USE_VERTEX` and the other auth-clearing names;
`apiKeyHelper` is a `read` command naming `work.key`. `helper output: sk-walkthrough-canary`. The
final count is `0`.

**If you see something else:** a missing `MLFLOW_TRACKING_URI` means user entries were dropped
(FR-004); a count above 0 means a key leaked into the file (Principle IV).

**Result:**

Passed (agent). `theme` and `MLFLOW_TRACKING_URI` kept; `ANTHROPIC_MODEL: gw-model`, endpoint, `gw-haiku`; `APROFILE_SET_VARS: ANTHROPIC_MODEL ANTHROPIC_BASE_URL ANTHROPIC_DEFAULT_HAIKU_MODEL`; all auth-clearing names empty; `apiKeyHelper` is `k=; IFS= read -r k < '…/work.key' …`; helper printed the test key [redacted]; count in file `0`.

### Step 5 of 15: Sync again changes nothing (US1)

**Why this step:** `sync` must be idempotent (SC-005).

**Needs:** step 3.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && ap sync
```

**What you should see:** no launch lines and a summary of zero changes.

**If you see something else:** repeated `set env` lines mean the planner and writer disagree
(normalization or ordering problem).

**Result:**

Passed (agent). `already up to date (0 changes)`.

### Step 6 of 15: Edit a placed entry, see it reported stale, sync restores it (US1, US5)

**Why this step:** the profile is the single source; a hand edit or a stray tool write is
reported and undone (FR-005, FR-020a).

**Needs:** step 3.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && S="$HOME/.local/share/aprofile/profiles/claude/demo/settings.json" && jq '.env.ANTHROPIC_MODEL="edited"' "$S" > "$S.tmp" && mv "$S.tmp" "$S" && ap show claude/demo | grep -E '^ *folder( sync)?:' && ap sync && jq -r .env.ANTHROPIC_MODEL "$S"
```

**What you should see:** `folder:` says `self-contained`; `folder sync:` says `stale` with the hint
to run `aprofile sync`; `sync` prints a `set env ANTHROPIC_MODEL` line; the last line is
`gw-model`.

**If you see something else:** `folder sync: synced` after the edit means stale detection misses
edits.

**Result:**

Passed (agent). Runbook corrected: `show` indents fields, so the grep is now `^ *folder( sync)?:`. Output: `folder: self-contained`, `folder sync: stale (run: aprofile sync)`; sync `set env ANTHROPIC_MODEL`; value back to `gw-model`.

### Step 7 of 15: A `/model` pick in the harness's key is left alone (US1)

**Why this step:** Claude Code writes the `model` key on `/model`; aprofile must not own or rewrite
it, and the profile's `env.ANTHROPIC_MODEL` still wins at the next start.

**Needs:** step 3.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && S="$HOME/.local/share/aprofile/profiles/claude/demo/settings.json" && jq '.model="picked-in-session"' "$S" > "$S.tmp" && mv "$S.tmp" "$S" && ap sync && jq -r '.model, .env.ANTHROPIC_MODEL' "$S"
```

**What you should see:** `sync` reports no launch changes; output lines `picked-in-session` then
`gw-model`.

**If you see something else:** a changed `model` key means aprofile fights the harness
(Principle II).

**Result:**

Passed (agent). `already up to date (0 changes)`; `picked-in-session` then `gw-model`.

### Step 8 of 15: The wrapper ignores exported overrides and clears leaked variables (US4)

**Why this step:** environment overrides are gone; folder-carried variables are only cleared; an
inherited `APROFILE_SET_VARS` from another session cannot leak its variables (FR-012 to FR-014).

**Needs:** step 3.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && ANTHROPIC_MODEL=from-shell ANTHROPIC_API_KEY=decoy APROFILE_SET_VARS=LEAKED_ALIAS LEAKED_ALIAS=old claude-demo --model one-run
```

**What you should see:** `ARGS: --model one-run`; `ENV:` lines show `CLAUDE_CONFIG_DIR` pointing at
the profile folder and no `ANTHROPIC_MODEL`, no `ANTHROPIC_API_KEY`, no `LEAKED_ALIAS`, and no
`APROFILE_SET_VARS` (the folder carries it).

**If you see something else:** `ANTHROPIC_MODEL=from-shell` means caller overrides still work;
`LEAKED_ALIAS=old` means unconditional clearing is missing; `ANTHROPIC_API_KEY` with the key means
the wrapper exports a folder-carried key.

**Result:**

Passed (agent). `ARGS: --model one-run`; only `ENV: CLAUDE_CONFIG_DIR=…/profiles/claude/demo`; no ANTHROPIC_MODEL, ANTHROPIC_API_KEY, LEAKED_ALIAS or APROFILE_SET_VARS.

### Step 9 of 15: Start the stub endpoint (US1)

**Why this step:** the real Claude Code needs somewhere harmless to send requests, so we can see
endpoint, key and model without any real service or key.

**Needs:** step 1.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && printf '%s\n' 'import http.server, sys, json' 'class H(http.server.BaseHTTPRequestHandler):' '    def _log(self, body=b""):' '        try: m = json.loads(body or b"{}").get("model")' '        except Exception: m = None' '        open(sys.argv[2], "a").write(json.dumps({"m": self.command, "p": self.path.split("?")[0], "xkey": self.headers.get("x-api-key"), "auth": self.headers.get("Authorization"), "model": m}) + "\n")' '    def do_GET(self):' '        self._log(); o = json.dumps({"data": [], "has_more": False}).encode(); self.send_response(200); self.send_header("content-type", "application/json"); self.send_header("content-length", str(len(o))); self.end_headers(); self.wfile.write(o)' '    def do_POST(self):' '        b = self.rfile.read(int(self.headers.get("content-length") or 0)); self._log(b); o = b"{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"stub\"}}"; self.send_response(400); self.send_header("content-type", "application/json"); self.send_header("content-length", str(len(o))); self.end_headers(); self.wfile.write(o)' '    def log_message(self, *a): pass' 'http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()' > "$W/stub.py" && : > "$W/stub.log" && .specify/extensions/spex/scripts/spex-runbook-fixture.sh start specs/004-corgi-integration/.walkthrough --name stub-endpoint --kind process -- python3 -I "$W/stub.py" 18801 "$W/stub.log"
```

**What you should see:** the fixture script reports `stub-endpoint` started with a PID.

**If you see something else:** "address already in use" means port 18801 is taken; stop the other
process or change the port in steps 3, 9 and 10.

**Result:**

Passed (agent). `FIXTURE_STARTED name=stub-endpoint kind=process`; stub answered HTTP 200.

### Step 10 of 15: Real Claude Code starts from the folder alone (US1, SC-001)

**Why this step:** this is what Corgi does: only `CLAUDE_CONFIG_DIR`, nothing else, and a hostile
environment with an exported key and a Vertex switch.

**Needs:** steps 3 and 9, and Claude Code 2.1.319 or later on your normal PATH.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && C="$(PATH="${PATH#*:}" command -v claude)" && : > "$W/stub.log" && env -i HOME="$HOME" PATH=/usr/bin:/bin TERM=xterm CLAUDE_CONFIG_DIR="$HOME/.local/share/aprofile/profiles/claude/demo" ANTHROPIC_API_KEY=decoy CLAUDE_CODE_USE_VERTEX=1 "$C" -p hi < /dev/null > /dev/null 2>&1; jq -c '{m, p, xkey, auth, model}' "$W/stub.log" | sort -u
```

**What you should see:** `POST /v1/messages` lines (plus a `GET /v1/models` line if gateway model
discovery is on), all with `xkey` and
`auth` carrying `sk-walkthrough-canary` (Bearer for `auth`) and the POST `model` `gw-model`. No
`decoy` key, and the requests reached the stub (not Vertex).

**If you see something else:** no lines means Claude Code went elsewhere (Vertex) or failed before
discovery; `decoy` means the neutralizing entries are missing (FR-006a). Note: this step runs
the real Claude Code binary against the local stub only, inside the throwaway home.

**Result:**

Passed (agent). Claude Code 2.1.319, folder only, with exported `ANTHROPIC_API_KEY=decoy` and `CLAUDE_CODE_USE_VERTEX=1`: `{"m":"POST","p":"/v1/messages","xkey":"[redacted]","auth":"Bearer [redacted]","model":"gw-model"}` (the folder's test key in both headers, no decoy, stub reached instead of Vertex). No GET /v1/models: gateway discovery is not enabled on this profile.

### Step 11 of 15: A Codex API-key profile with a custom provider (US2)

**Why this step:** Codex reads its key from a provider `auth` command; Codex refuses `auth` next to
`env_key`, so aprofile must replace and record it.

**Needs:** step 1.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && D="$HOME/.local/share/aprofile/profiles/codex/demo" && mkdir -p "$D" && printf '%s\n' '# my provider' 'model_provider = "gw"' '' '[model_providers.gw]' 'name = "gw"' 'base_url = "http://127.0.0.1:18801/v1"' 'wire_api = "responses"' 'env_key = "OPENAI_API_KEY"' 'requires_openai_auth = false' > "$D/config.toml" && ap add codex demo --adopt --share content --auth-kind api-key --auth-key "file:$HOME/work.key" --model 1.5 && ap sync --dry-run
```

**What you should see:** the dry run lists `codex/demo: config.toml: set model_providers.gw.auth
(replaces env_key)` and a note that `config.toml` is re-encoded so comments are dropped. Nothing is
written yet.

**If you see something else:** `wrapper-only` or no Codex line means the provider was not
recognized as selected.

**Result:**

Passed (agent). `codex/demo: config.toml: set model_providers.gw.auth (replaces env_key)` and `note: codex/demo: config.toml is re-encoded: comments are dropped`; nothing written (no `codex-demo` wrapper, comment still in the file). Note: the dry run labels the wrapper line `written`, an existing wording of `sync --dry-run`.

### Step 12 of 15: Apply and inspect the Codex folder and wrapper (US2)

**Why this step:** proves the `auth` block holds only a command, `env_key` is gone, and the wrapper
passes the model as a quoted TOML string before the caller's arguments.

**Needs:** step 11.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && ap sync > /dev/null && D="$HOME/.local/share/aprofile/profiles/codex/demo" && cat "$D/config.toml" && echo --- && codex-demo -m caller-model exec hi
```

**What you should see:** `config.toml` has `[model_providers.gw.auth]` with `command = "sh"` and
`args` ending in the path to `work.key`, no `env_key`, no key value, and no `# my provider`
comment. After `---`: `ARGS: -c model="1.5" -m caller-model exec hi`, and no `OPENAI_API_KEY` in
the `ENV:` lines.

**If you see something else:** `model=1.5` without quotes would make Codex fail ("floating point");
`-m` before `-c` would mean the wrapper's model wins over the caller's.

**Result:**

Passed (agent). `[model_providers.gw.auth]` with `command = 'sh'`, `args` ending in the `work.key` path; no `env_key`, no key value, comment dropped. `ARGS: -c model="1.5" -m caller-model exec hi`; no OPENAI_API_KEY in ENV.

### Step 13 of 15: Switching the Codex profile to a login restores `env_key` (US2)

**Why this step:** removal must give the user's provider block back as it was (FR-005, FR-009).

**Needs:** step 12.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && ap edit codex/demo --auth-kind chatgpt-login && ap sync && grep -nE 'env_key|auth' "$HOME/.local/share/aprofile/profiles/codex/demo/config.toml"
```

**What you should see:** `sync` prints a `removed model_providers.gw.auth` line noting the restored
`env_key`; `grep` shows `env_key = "OPENAI_API_KEY"` and no `.auth` table.

**If you see something else:** no `env_key` means the record lost the replaced value.

**Result:**

Passed (agent). `codex/demo: config.toml: removed model_providers.gw.auth (restored env_key)`; grep: `env_key = 'OPENAI_API_KEY'`, no `.auth` table (the other match is `requires_openai_auth`).

### Step 14 of 15: The registry publishes each profile's model (US3)

**Why this step:** tools reading `registry.json` (the planned Corgi patch) need the configured
model, never the last `/model` pick.

**Needs:** steps 7 and 13.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && jq -c '.format_version, (.profiles[] | {harness, name, model})' "$(ap registry path)"
```

**What you should see:** `1`, then `claude/demo` with `model: "gw-model"` (not
`picked-in-session`) and `codex/demo` with `model: "1.5"`.

**If you see something else:** `picked-in-session` means the model is read from a harness file
(FR-019).

**Result:**

Passed (agent). `1`, `{claude, demo, gw-model}`, `{codex, demo, 1.5}`.

### Step 15 of 15: Reporting: self-contained, wrapper-only, unplaced (US5)

**Why this step:** you need to know which folders a config-folder-only tool can launch, and what
overrides a profile.

**Needs:** step 3.

**Run:**

```sh
. "$(git rev-parse --show-toplevel)/specs/004-corgi-integration/.walkthrough/env.sh" && ap add claude tok --share content --auth-kind auth-token --auth-key env:ANTHROPIC_AUTH_TOKEN > /dev/null && ap sync > /dev/null && S="$HOME/.local/share/aprofile/profiles/claude/demo/settings.json" && jq '.env.OTEL_EXPORTER="x" | .env.CLAUDE_CODE_SUBAGENT_MODEL="stray"' "$S" > "$S.tmp" && mv "$S.tmp" "$S" && ap show claude/tok | grep -A2 '^ *folder:' ; ap doctor 2>&1 | grep -E 'claude/(demo|tok)' | grep -viE 'wrapper .* (synced|ok)'
```

**What you should see:** `claude/tok`: `folder: not self-contained` with `wrapper-only: key
(…referenced variable is cleared…)`. `doctor`: an info line for `claude/tok`'s wrapper-only key,
and a problem for `claude/demo` saying `settings.json` sets `env CLAUDE_CODE_SUBAGENT_MODEL`, which
this profile does not set, with the fix to remove the entry or set it on the profile. No problem
for `OTEL_EXPORTER` (not a launch variable). No key values anywhere.

**If you see something else:** no unplaced problem means FR-020b is not wired into doctor.

**Result:**

Passed (agent). `claude/tok`: `folder: not self-contained`, `wrapper-only: key (key referenced through a variable aprofile clears)`. doctor: info for tok's wrapper-only key; problem `[claude/demo] settings.json sets env CLAUDE_CODE_SUBAGENT_MODEL, which this profile does not set` with fix `remove the entry or set it on the profile`; nothing for OTEL_EXPORTER; doctor --json contains the key 0 times. Also reported (expected): tok's `env:ANTHROPIC_AUTH_TOKEN` is unset in this shell, and demo's settings differ from the default.

## Not covered here

- **Registering the real daily profiles in Corgi (SC-007)**: needs the merged build and your real
  profiles; follow `docs/guide/docs/user/corgi.md` after merging.
- **Real Codex against the stub**: the Codex folder-only launch was verified on 0.167.0 during
  research (`research.md` R3); step 12 checks the written block and arguments without running the
  real Codex.
- **Share level changes and `settings pull`**: covered by integration tests; they need a richer
  default setup than this throwaway home.

## Coverage

- US1 (Claude folder): steps 3 to 7 and 10.
- US2 (Codex folder): steps 11 to 13.
- US3 (registry model): step 14.
- US4 (no overrides, no leaks): step 8 (and step 10's hostile environment).
- US5 (reporting): steps 6 and 15; Corgi registration itself is under "Not covered here".

## Results

15 steps: 15 passed, 0 failed, 0 skipped (2026-11-10, agent-run, Claude Code 2.1.319).

Two steps needed runbook corrections, not code fixes: step 1 (no `--version` command) and steps 6 and 15 (`show` indents its fields). The journey shows that `sync` makes a Claude API-key folder self-contained (endpoint, model, aliases, key command, neutralized auth variables) while keeping user entries and the harness-written `model` key; that the real Claude Code launched from the folder alone reaches the right endpoint with the right key and model even with a hostile environment; that wrappers ignore exported overrides and leaked variables; that a Codex folder gets a provider `auth` command with `env_key` replaced and restored; that the registry carries the configured model; and that `show` and `doctor` report self-containment, wrapper-only keys and unplaced entries. Not tried here: Corgi registration with real profiles, real Codex against the stub, share level changes and `settings pull`.
