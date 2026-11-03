# Quickstart & validation guide: aprofile slice one

**Feature**: 001-aprofile-standalone-profile-cli. These scenarios prove the feature end to end and
map to the Success Criteria (SC-xxx) and each user story's Independent Test in [spec.md](./spec.md).
All scenarios run against a temporary `HOME`/XDG root and a no-op stub harness binary where noted,
never the developer's real home (Constitution VII, SC-009).

## Prerequisites

- Go toolchain (version fixed in [plan.md](./plan.md)); `go build ./cmd/aprofile`.
- A scratch root: `export HOME=$(mktemp -d)` plus `XDG_CONFIG_HOME`, `XDG_DATA_HOME`,
  `XDG_STATE_HOME` under it. Tests set these automatically.
- For wrapper scenarios: a POSIX shell (always present) and either a real harness or the test stub
  at `tests/stub/harness`.

## Scenario 1 - Create and launch (SC-001, US1)

```sh
aprofile add claude work --model X --endpoint Y --color blue --icon W --label "Work" \
  --auth-key env:WORK_TOKEN --auth-kind oauth-token
aprofile sync
# open a new shell so the PATH block applies, then:
claude-work --version
aprofile list --json
```

Expected: `claude-work` exists in the bin dir and is on PATH; the profile config dir exists;
`claude-work` execs the harness with `CLAUDE_CONFIG_DIR` set to the profile dir and `APROFILE_*`
set; `list --json` shows the profile. Pass bar: at most three commands before the harness login,
under 2 minutes (SC-001).

## Scenario 2 - Caller override and idempotent sync (US1 AS3, SC-003)

```sh
ANTHROPIC_MODEL=other claude-work --version   # harness sees "other" this run only
aprofile sync                                  # second run
```

Expected: the one-run override reaches the harness; the second `sync` reports 0 changes and writes
0 files (SC-003).

## Scenario 3 - Isolation (SC-002, US2)

```sh
aprofile add claude private --auth-key env:PRIV_TOKEN --auth-kind oauth-token
aprofile sync
# run a short session in each wrapper, then inspect both config dirs
```

Expected: each profile's state entries (sessions, transcripts, history, logs, auto-memory, caches)
are real files inside that profile's config dir, not links; credentials differ; a shared entry
edited in the default setup is visible in both without another sync. Pass bar: 0 entries of one
profile reachable via the other or the default setup (SC-002).

## Scenario 4 - Registry and env contract (SC-007, US3)

```sh
cat "$(aprofile registry path)"       # parse as JSON
# start a wrapper and read the harness process environment
aprofile registry export --format omarchy
```

Expected: one registry entry per profile with all fields + `format_version`, no secrets; the harness
process has the `APROFILE_*` variables; a process detector sees the real harness as foreground;
Omarchy files are written per provider and non-aprofile entries are preserved (SC-007).

## Scenario 5 - Copy and MCP conversion (SC-005, US4)

```sh
# source profile with 3 MCP servers: stdio w/ env refs, remote w/ header from env, one disabled
aprofile copy claude/work claude/client-x          # same-harness
aprofile copy claude/work codex/work               # cross-harness, converts MCP
aprofile mcp copy claude/work codex/work serverA   # selective
```

Expected: same-harness target has the owned layer verbatim + empty credentials/state, and the
report warns about any owned value that looks like a secret; cross-harness target has each server in
the target's native MCP format with references in the target syntax and no secret value in any file
aprofile writes; dropped fields and literal secrets are named in the report; plugins are not carried
(SC-005, SC-006).

## Scenario 6 - Secret handling (SC-006)

```sh
aprofile add claude bad --auth-key sk-REALSECRET    # rejected
aprofile add claude ok  --auth-key env:TOKEN        # accepted
aprofile sync
# with planted canary secrets in the environment/refs, grep every generated artifact
```

Expected: literal secret rejected (FR-028); references accepted; a missing reference at launch stops
the wrapper naming the profile/reference (FR-028a). Pass bar: 0 secret values in any wrapper,
registry, store, definition or `--json` output, verified with planted canaries (SC-006).

## Scenario 7 - New harness without a release (SC-004, US5)

```sh
# drop tests/fixtures/harnesses.d/fake.yaml (binary is a test script)
aprofile add fake demo && aprofile sync && fake-demo
aprofile harness show claude --json    # with one overridden field present in harnesses.d
```

Expected: a working wrapper for `fake` with 0 changes to the released tool (SC-004); an exec hook's
`env` output is included; `harness show` reflects the override and marks built-in vs overridden.

## Scenario 8 - Import cc-deck (SC-010, US6)

```sh
aprofile import cc-deck --dry-run    # against a fixture cc-deck config + credential dir
aprofile import cc-deck
```

Expected: dry-run changes nothing; real import creates each profile with settings + credentials and
a fresh state layer; no cc-deck file is modified; an existing `(harness,name)` is skipped and the
command exits non-zero; wrapper-name collisions on PATH are reported (SC-010).

## Scenario 9 - UI parity and wrapper overhead (SC-008, SC-011)

- `aprofile ui`: add a profile through the form, then confirm `aprofile list --json` shows it with
  the entered values; confirm every UI action has a command equivalent (SC-008).
- Overhead: median of 20 `claude-work` runs against a no-op stub vs 20 direct stub runs; the
  wrapper adds at most 50 ms with no exec hook (SC-011).

## Platforms (SC-009)

The full suite runs on Linux and macOS and never touches the real home directory. macOS-specific
credential behavior (Keychain) is covered per [research.md](./research.md).
