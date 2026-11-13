# Quickstart: Profile Features

**Date**: 2026-11-13 | **Spec**: [spec.md](spec.md)

Runnable validation scenarios for the finished feature. All use a throwaway `HOME`
(Principle VII): the test suite covers the same paths automatically; this guide proves it by
hand.

## Prerequisites

- Built binary: `go build -o /tmp/aprofile .`
- A scratch home with a default setup and at least one profile:

```sh
export HOME=/tmp/aprofile-qs && rm -rf $HOME && mkdir -p $HOME
/tmp/aprofile add claude work --share content --auth-kind oauth-login
/tmp/aprofile add codex work --share content --auth-kind api-key --auth-key file:/keys/x
```

## Scenario 1: telemetry on Claude Code (User Story 1)

```sh
/tmp/aprofile features claude/work --enable telemetry --traces on --logs on \
  --endpoint https://otlp.example.com
/tmp/aprofile sync
cat "$HOME/.claude-work/settings.json"   # or the profile's settings path from `show`
/tmp/aprofile features claude/work
/tmp/aprofile show claude/work
```

Expected: the settings file's `env` block carries `CLAUDE_CODE_ENABLE_TELEMETRY=1`,
`OTEL_TRACES_EXPORTER=otlp`, `OTEL_LOGS_EXPORTER=otlp`,
`OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.example.com`,
`OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` and
`CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`; `features` status shows telemetry on with all
params; `show` lists the feature and reports no drift.

Turn it off again and confirm removal:

```sh
/tmp/aprofile features claude/work --disable telemetry && /tmp/aprofile sync
```

Expected: exactly those entries are gone; the rest of the file is unchanged.

## Scenario 2: automode-log (User Story 2)

```sh
/tmp/aprofile features claude/work --enable automode-log && /tmp/aprofile sync
rg AUTOMODE_DECISION_LOG "$HOME/.claude-work/settings.json"
```

Expected: `AUTOMODE_DECISION_LOG: "1"` present as a placed env entry; disabling removes it.

## Scenario 3: discovery and gating (User Story 3)

```sh
/tmp/aprofile features codex/work        # telemetry only, no automode-log
/tmp/aprofile features codex/work --enable automode-log
```

Expected: the codex status lists telemetry (protocol default `otlp-http json`) and not
automode-log; the second command fails naming codex and the flag, changing nothing.

## Scenario 4: Codex telemetry in config.toml (User Story 4)

```sh
/tmp/aprofile features codex/work --enable telemetry --traces on --logs on \
  --endpoint https://otlp.example.com && /tmp/aprofile sync
rg -n "otel" "$HOME/.codex-work/config.toml"
```

Expected: `[otel]` keys for the log and trace exporters in the inline-table encoding
(research R1). Hand-write `environment = "prod"` under `[otel]`, disable telemetry, sync: the
hand-written key survives, aprofile's keys are gone.

## Scenario 5: isolation and copy (User Story 5)

```sh
/tmp/aprofile add claude tmp --share all --auth-kind oauth-login
/tmp/aprofile features claude/tmp --enable automode-log     # refused, hint at --share content
/tmp/aprofile features claude/tmp                          # bare status still works
/tmp/aprofile copy claude/work claude/work2
/tmp/aprofile features claude/work2                        # copy carries feature state
/tmp/aprofile edit claude/work2 --share all --yes          # warns, clears features
```

## Scenario 6: collisions (User Story 6)

```sh
/tmp/aprofile add claude old --share content --auth-kind oauth-login
/tmp/aprofile edit claude/old --env-literal AUTOMODE_DECISION_LOG=1
/tmp/aprofile features claude/old --enable automode-log    # fails naming the variable
/tmp/aprofile add claude new --share content --auth-kind oauth-login
/tmp/aprofile features claude/new --enable automode-log
/tmp/aprofile edit claude/new --env-literal AUTOMODE_DECISION_LOG=1   # fails the other way
```

## Automated coverage

`tests/integration/features_test.go` encodes every scenario above against temp homes; the
quickstart is the manual pass over the same ground.

## Pass record

- **2026-11-13**: scenarios 1-6 run by hand against a scratch `HOME`
  (`/tmp/aprofile-qs`) with the built binary, after the automated suite. All
  six pass: the six mapped Claude telemetry entries place and remove exactly
  (second sync a no-op); `AUTOMODE_DECISION_LOG` places and removes; codex
  status lists telemetry only and `--enable automode-log` fails naming codex;
  the `[otel]` exporters place in the verified encoding and the hand-written
  `environment` key survives disable; the share-level-all refusal, copy carry
  and `edit --share all` clearing behave as specified; both collision
  directions fail naming `AUTOMODE_DECISION_LOG`. Bare `features` status:
  0.022s (SC-003: under a second).
