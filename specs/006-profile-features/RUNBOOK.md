---
runbook_version: 1
feature: 006-profile-features
status: drafted
mode: light
steps_total: 12
step_results: pending,pending,pending,pending,pending,pending,pending,pending,pending,pending,pending,pending
drafted_at: e6f8975
drafted_fingerprint: 02099fce96b6606a09a0cae1021ebed0e11b903bbd15a4cd6ce3802c919652f0
tested_at:
tested_fingerprint:
updated: 2026-11-13
---

# Try it: Profile Features (Curated Harness Capability Toggles)

<!-- runbook-status -->
> **Status:** drafted · 0 of 12 steps · Mode: light · Drafted at e6f8975 · 2026-11-13
<!-- /runbook-status -->

## What you're testing

The `aprofile features` command: a curated catalog of high-level features (OTel
telemetry, the Auto-mode decision log) that you enable per profile with one command,
`sync` materializes into each harness's own settings, and `show` reflects. The journey:
discover what a harness supports, turn telemetry and the decision log on for a Claude
profile, do the same for Codex through the identical command (a different mechanism
entirely), watch isolation and collisions behave, then turn everything off and confirm
the settings files return to their prior state.

## Test environment

No fixtures. The journey uses a scratch `HOME` and a built binary; everything else is
the aprofile CLI itself. Teardown is `rm -rf /tmp/aprofile-rb` (step 1 sets it up).

## Steps

### Step 1 of 12: Build the binary (US1)

**Why this step:** every later step runs the feature build, so it must exist first.

**Needs:** a Go toolchain.

**Run:**

```
go build -o /tmp/aprofile-rb/aprofile . && /tmp/aprofile-rb/aprofile --help | head -3
```

**What you should see:** the help header listing aprofile's commands (no error), and
among the commands `features` appears with the other subcommands.

**If you see something else:** a compile error means the working tree is not the
feature branch; `git branch --show-current` should print `006-profile-features`.

**Result:**

### Step 2 of 12: Create a scratch home and a profile (US1)

**Why this step:** features live on profiles; this gives you a `content`-level Claude
profile in a throwaway home so nothing real is touched.

**Needs:** step 1.

**Run:**

```
export HOME=/tmp/aprofile-rb && rm -rf "$HOME" && mkdir -p "$HOME" && /tmp/aprofile-rb/aprofile add claude work --share content --auth-kind oauth-login --yes
```

**What you should see:** a report that the profile `claude/work` was created at share
level `content`, with its own settings copied from the (empty) default setup.

**If you see something else:** a name validation error means the scratch home still
holds an older profile; the `rm -rf` in the command should prevent that.

**Result:**

### Step 3 of 12: Discover what the harness supports (US3)

**Why this step:** the bare form is the catalog's discovery surface; it must list
telemetry and automode-log for claude without changing anything.

**Needs:** step 2.

**Run:**

```
/tmp/aprofile-rb/aprofile features claude/work
```

**What you should see:** a `telemetry:` group with `telemetry: off` and a `logging:`
group with `automode-log: off`, with telemetry's parameters listed (traces, logs,
endpoint, protocol) and the effective protocol default `http/protobuf` shown.

**If you see something else:** an empty list means the harness definition's `features:`
mapping is missing from `internal/harness/builtin/claude.yaml`.

**Result:**

### Step 4 of 12: Enable telemetry (US1)

**Why this step:** the primary flow: one command stores the feature state and points
at sync.

**Needs:** step 3.

**Run:**

```
/tmp/aprofile-rb/aprofile features claude/work --enable telemetry --traces on --logs on --endpoint https://otlp.example.com
```

**What you should see:** a change line for telemetry (traces on, logs on, endpoint,
protocol `http/protobuf` applied as the default) and the reminder to run
`aprofile sync` to apply.

**If you see something else:** an error asking for `--endpoint` means the flag was
dropped; an error naming `--traces` and `--logs` means both signals were left off.

**Result:**

### Step 5 of 12: Sync and inspect the placed entries (US1)

**Why this step:** placement is the feature's promise: sync writes the six mapped OTel
env entries into the profile's own `settings.json` as recorded placed entries.

**Needs:** step 4.

**Run:**

```
/tmp/aprofile-rb/aprofile sync && cat "$HOME/.claude-work/settings.json"
```

**What you should see:** the sync report places entries in `claude/work`'s settings,
and the file's `env` block carries `CLAUDE_CODE_ENABLE_TELEMETRY=1`,
`OTEL_TRACES_EXPORTER=otlp`, `OTEL_LOGS_EXPORTER=otlp`,
`OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.example.com`,
`OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` and
`CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`.

**If you see something else:** missing entries with no error means the profile's
settings file is linked (share level `all`); `show` would name it.

**Result:**

### Step 6 of 12: Read status and show (US1, US3)

**Why this step:** the recorded state must be visible in both `features` status and
`show`, with no drift.

**Needs:** step 5.

**Run:**

```
/tmp/aprofile-rb/aprofile features claude/work && /tmp/aprofile-rb/aprofile show claude/work
```

**What you should see:** `features` lists telemetry on with all four parameter
values; `show` has a features section with one telemetry line and no drift warning.

**If you see something else:** a drift warning in `show` means the settings file was
changed after sync (or sync did not run); re-run `sync`.

**Result:**

### Step 7 of 12: Enable the Auto-mode decision log (US2)

**Why this step:** the second catalog feature: a single env entry, placed and removed
by the same machinery.

**Needs:** step 2.

**Run:**

```
/tmp/aprofile-rb/aprofile features claude/work --enable automode-log && /tmp/aprofile-rb/aprofile sync && rg AUTOMODE_DECISION_LOG "$HOME/.claude-work/settings.json"
```

**What you should see:** the enable stores automode-log, sync places one entry, and
the file shows `"AUTOMODE_DECISION_LOG": "1"` in the env block.

**If you see something else:** an error naming `AUTOMODE_DECISION_LOG` as a collision
means this profile already carried that variable through `--env-literal` (that is
step 11's behavior, working as specified).

**Result:**

### Step 8 of 12: Codex telemetry through the same command (US4)

**Why this step:** the catalog's cross-harness promise: the identical command maps to
a completely different mechanism (`[otel]` in `config.toml`).

**Needs:** step 2.

**Run:**

```
/tmp/aprofile-rb/aprofile add codex work --share content --auth-kind api-key --auth-key file:/tmp/aprofile-rb/key && /tmp/aprofile-rb/aprofile features codex/work --enable telemetry --traces on --logs on --endpoint https://otlp.example.com && /tmp/aprofile-rb/aprofile sync && rg -n "otel" "$HOME/.codex-work/config.toml"
```

**What you should see:** codex status enables telemetry with protocol default
`otlp-http json`, and `config.toml` gains `[otel]` keys for the log and trace
exporters in the inline-table encoding (`otlp-http = { endpoint = ... }` style).

**If you see something else:** a parse error from sync on `config.toml` means a
hand-edited file was malformed before the run; inspect it and re-run.

**Result:**

### Step 9 of 12: Hand-written keys survive disable (US4)

**Why this step:** aprofile owns only what it placed: disabling must remove its keys
and preserve yours.

**Needs:** step 8.

**Run:**

```
printf '\nenvironment = "prod"\n' >> "$HOME/.codex-work/config.toml" && /tmp/aprofile-rb/aprofile features codex/work --disable telemetry && /tmp/aprofile-rb/aprofile sync && rg -n "otel|environment" "$HOME/.codex-work/config.toml"
```

**What you should see:** after disable and sync, aprofile's `[otel]` exporter keys
are gone and the hand-written `environment = "prod"` line survives.

**If you see something else:** a missing `environment` line means the ownership rule
broke; that is a bug worth reporting.

**Result:**

### Step 10 of 12: Isolation, copy and share level all (US5)

**Why this step:** features must respect the isolation model: refused at `all`,
carried by copy, cleared with a warning on the way into `all`.

**Needs:** steps 4 and 2.

**Run:**

```
/tmp/aprofile-rb/aprofile add claude tmp --share all --auth-kind oauth-login --yes && /tmp/aprofile-rb/aprofile features claude/tmp --enable automode-log; /tmp/aprofile-rb/aprofile features claude/tmp && /tmp/aprofile-rb/aprofile copy claude/work claude/work2 --yes && /tmp/aprofile-rb/aprofile features claude/work2 && /tmp/aprofile-rb/aprofile edit claude/work2 --share all --yes
```

**What you should see:** the mutation on `claude/tmp` fails pointing at
`aprofile edit claude/tmp --share content` while its bare status still lists features;
the copy `claude/work2` carries telemetry and automode-log; `edit --share all` warns
that the features were disabled and clears them.

**If you see something else:** a successful mutation at `all` means the guard broke;
that writes into the linked default setup and is a Principle III violation.

**Result:**

### Step 11 of 12: Collisions are caught in both directions (US6)

**Why this step:** a feature must never silently take over a variable you set by
hand, and `edit` must not silently take over one a feature placed.

**Needs:** step 2.

**Run:**

```
/tmp/aprofile-rb/aprofile add claude old --share content --auth-kind oauth-login --yes && /tmp/aprofile-rb/aprofile edit claude/old --env-literal AUTOMODE_DECISION_LOG=1 && /tmp/aprofile-rb/aprofile features claude/old --enable automode-log; /tmp/aprofile-rb/aprofile add claude new --share content --auth-kind oauth-login --yes && /tmp/aprofile-rb/aprofile features claude/new --enable automode-log && /tmp/aprofile-rb/aprofile edit claude/new --env-literal AUTOMODE_DECISION_LOG=1
```

**What you should see:** the first `features --enable` fails naming
`AUTOMODE_DECISION_LOG` as already set on the profile; the later
`edit --env-literal` on `claude/new` fails the same way, naming the feature that
owns the variable.

**If you see something else:** a success on either side means the collision check
broke in that direction; note which direction.

**Result:**

### Step 12 of 12: Disable everything and verify exact removal (US1)

**Why this step:** the trust leg: disabling and re-syncing leaves the settings file
as it was before enabling, apart from anything changed in between.

**Needs:** steps 5 and 7.

**Run:**

```
cp "$HOME/.claude-work/settings.json" /tmp/aprofile-rb/before.json && /tmp/aprofile-rb/aprofile features claude/work --disable telemetry --disable automode-log && /tmp/aprofile-rb/aprofile sync && diff <(rg -v 'OTEL_|AUTOMODE_|CLAUDE_CODE_ENHANCED' /tmp/aprofile-rb/before.json) <(rg -v 'OTEL_|AUTOMODE_|CLAUDE_CODE_ENHANCED' "$HOME/.claude-work/settings.json") && echo REMOVED_EXACTLY
```

**What you should see:** sync removes the feature entries; the filtered diff of the
file before and after enable/disable shows no other change and prints
`REMOVED_EXACTLY`.

**If you see something else:** a diff means disable touched (or left) more than the
feature entries; a leftover `AUTOMODE_DECISION_LOG` line usually means the automode
disable was skipped.

**Result:**

## Not covered here

- Whether the harnesses actually honor the placed entries (Claude Code exporting
  spans to a collector, codex reading the `[otel]` exporters) needs real harness runs
  and a listening OTLP endpoint; aprofile only writes the settings. The codex
  encoding itself was verified live against codex-cli 0.167.0 (research R1).
- AgentTacho's ingestion of `.automode_decisions.jsonl` is the consumer's project
  (agentmeter brainstorm 45), not aprofile's.

## Coverage

- **US1** (telemetry on Claude Code): steps 3-6 and 12.
- **US2** (automode-log): step 7.
- **US3** (discovery, gating): steps 3, 6 and 8 (codex default protocol), plus the
  refusal shape in step 10.
- **US4** (Codex telemetry): steps 8-9.
- **US5** (isolation and copy): step 10.
- **US6** (collisions): step 11.
- All six user stories are covered; the only untried parts are the harness-side
  effects listed under Not covered here.

## Results
