# CLI Contract: `aprofile features`

**Date**: 2026-11-13 | **Spec**: [spec.md](spec.md) FR-001..FR-004, FR-007, FR-013, FR-017

## Command

```
aprofile features <harness>/<name> [flags]
```

One positional argument, the profile ref (same syntax as `edit`/`show`). Bare (no mutation
flags): read-only status. With mutation flags: updates the stored feature state and reminds
the user to run `sync` (the `edit` output contract; does not run it).

## Flags

| Flag | Type | Meaning |
|------|------|---------|
| `--enable <feature>` | string | enable a catalog feature |
| `--disable <feature>` | string | disable a feature, removing its entries on the next sync |
| `--traces <on\|off>` | enum | telemetry param; implies enabling telemetry |
| `--logs <on\|off>` | enum | telemetry param; implies enabling telemetry |
| `--endpoint <url>` | string | telemetry param; implies enabling telemetry |
| `--protocol <value>` | enum | telemetry param; implies enabling telemetry; values per harness |
| `--json` | bool | stable JSON output for status and mutation |
| `--dry-run` | bool | report the would-be changes without storing (mutation only) |

Validation at mutation time (hard errors, exit non-zero):

- Unsupported feature or flag for the profile's harness: error naming the harness and the
  flag (FR-007).
- Unknown feature id or param value outside the harness's enum: error naming it.
- Enabling telemetry with neither signal on: error naming `--traces` and `--logs`.
- Enabling telemetry without `--endpoint`: error asking for `--endpoint` (a stored endpoint
  from a previous enable satisfies it).
- Env-name collision with the profile's extra env vars: error naming the variable (FR-015).
- Any mutation on a profile at share level `all`: error pointing at
  `aprofile edit <harness>/<name> --share content` (FR-013). Bare status still works.

## Status output (human)

```
claude/work
  telemetry:
    telemetry:    on
      traces:     on
      logs:       off
      endpoint:   https://otlp.example.com
      protocol:   http/protobuf
  logging:
    automode-log: off
```

Features the harness does not support are not listed. Effective defaults are shown for
both enabled and disabled features with unset params. Only parameters supported by the
profile's harness are listed; required enable-time values (such as an unset endpoint)
are shown empty while disabled. Off a TTY or with `NO_COLOR`, plain text (Principle VI).

## Status output (`--json`)

```json
{
  "profile": "claude/work",
  "harness": "claude",
  "groups": [
    {
      "name": "telemetry",
      "features": [
        {
          "id": "telemetry",
          "enabled": true,
          "params": {
            "traces": "on",
            "logs": "off",
            "endpoint": "https://otlp.example.com",
            "protocol": "http/protobuf"
          }
        }
      ]
    },
    {
      "name": "logging",
      "features": [
        { "id": "automode-log", "enabled": false, "params": {} }
      ]
    }
  ]
}
```

Disabled features also include their supported parameters and effective defaults.
Invalid stored parameters for the harness produce an actionable error in `features`
and a warning in `show`; `sync` reports the problem without removing placed entries.

Field order and names are stable; `params` is `{}` for parameterless features. This schema is
a public interface (constitution Principle V registry note applies to breaking changes).

## Mutation output

Human: one line per change ("telemetry: enabled (traces on, logs off, endpoint …)"), then the
sync reminder ("run aprofile sync to apply"). `--json`: an object with `changed` (same feature
objects as status, post-change) and `reminder: "sync"`. Errors go to stderr with exit 1.

## `show` integration (FR-016)

`aprofile show <ref>` gains a features section after the launch settings, one line per enabled
feature with its params, plus a warning line per drift between recorded state and placed
entries. `--json` output of `show` gains a `features` array with the same feature objects.
