# Data Model: Profile Features (Curated Harness Capability Toggles)

**Date**: 2026-11-13 | **Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

## Entities

### Feature catalog entry (`internal/feature`)

| Field | Type | Notes |
|-------|------|-------|
| `ID` | string | `telemetry`, `automode-log`; stable, used in CLI flags and records |
| `Group` | string | display group: `telemetry` or `logging` |
| `Params` | []Param | ordered; empty for automode-log |

**Param**:

| Field | Type | Notes |
|-------|------|-------|
| `Name` | string | `traces`, `logs`, `endpoint`, `protocol` |
| `Type` | enum | `bool`, `string`, `enum` |
| `Values` | []string | for `enum` params; cross-harness union, validated per harness against the capability mapping |

Built-in v1 catalog: `telemetry` (group telemetry; params traces:bool, logs:bool,
endpoint:string, protocol:enum), `automode-log` (group logging; no params).

### Capability mapping (harness definition YAML, `features:` key)

Per feature id, in each `internal/harness/builtin/*.yaml` and overridable in `harnesses.d`:

| Field | Type | Notes |
|-------|------|-------|
| `params` | map name → param support | `default` (value), `values` (enum list, per harness) |
| `place` | []placement rule | ordered; the entries an enabled feature places |

**Placement rule**:

| Field | Type | Notes |
|-------|------|-------|
| `file` | string | definition-classified settings path, e.g. `settings.json`, `config.toml` |
| `location` | string | launch-record location: `env.<NAME>` (JSON env) or dotted TOML path (`otel.exporter`) |
| `value` | literal or `{param: <name>}` | resolved against feature state; literals for fixed values |
| `when` | param name (optional) | rule applies only when the bool param is on (telemetry signals) |

Claude Code `features.telemetry` places (in order): `env.CLAUDE_CODE_ENABLE_TELEMETRY=1`,
`env.OTEL_TRACES_EXPORTER=otlp` (when traces), `env.OTEL_LOGS_EXPORTER=otlp` (when logs),
`env.OTEL_EXPORTER_OTLP_ENDPOINT={param:endpoint}`, `env.OTEL_EXPORTER_OTLP_PROTOCOL={param:protocol}`,
`env.CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` (when traces). A signal that is off places its
exporter entry as `none` (FR-011), so the rule set covers both branches of each signal.
`features.automode-log` places `env.AUTOMODE_DECISION_LOG=1`.

Codex `features.telemetry` places: `otel.exporter` and `otel.trace_exporter` (inline-table
values per research R1, `none` for off signals), `otel` endpoint/protocol fields as required by
the encoding. Codex declares no `automode-log`.

Validation rules (loader): feature ids must exist in the catalog; param names must match the
catalog; enum `values`/`default` must be consistent (default ∈ values); placement `file` must
be a classified settings path the profile owns at `content`/`none`.

### Feature state (profile record, `internal/profile`)

```yaml
features:
  - feature: telemetry
    params:
      traces: "on"
      logs: "off"
      endpoint: https://otlp.example.com
      protocol: http/protobuf
```

| Field | Type | Notes |
|-------|------|-------|
| `Feature` | string | catalog id |
| `Params` | map[string]string | string values; validated at mutation time; invalid stored values load with a `LoadWarning` and place nothing |

Lifecycle: created/updated by `features` mutations; carried by `copy` (dropped with a report
line when the target harness lacks the feature); cleared by `edit --share all` (with a
warning); deleted with the profile.

### Launch record entries (unchanged schema, new producers)

Feature placement reuses `launch.Entry{File, Location, Replaced}`:

- JSON env entries: `File: settings.json`, `Location: env.OTEL_TRACES_EXPORTER`,
  `Replaced` holds a hand-written value it replaced (key `env.OTEL_TRACES_EXPORTER`).
- Nested TOML entries: `File: config.toml`, `Location: otel.exporter` (dotted path; the
  existing dotted-path convention covers nested subtrees, no schema change), `Replaced` holds
  replaced keys of the subtree.

State transitions (maintained by `sync`, idempotent): absent → placed (feature enabled),
placed → removed with restore of `Replaced` (feature disabled), placed → reported stale
(value differs from the derived want), orphaned → removed (record entry without desired
state, e.g. after `edit --share all`).

## Relationships

- Profile 1..* FeatureState (ordered by feature id in the record).
- FeatureState *..1 catalog Feature (id reference; unknown ids at load warn and place
  nothing).
- Capability mapping: harness *..* catalog feature (a harness supports a subset).
- FeatureState + capability mapping → desired launch entries → launch Record entries.

## Invariants

- The record never stores values aprofile derived, only locations and replaced user values
  (Principle IV).
- A profile at share level `all` has no feature placements; mutations are refused and stored
  state is cleared on the way in via `edit --share all` (FR-013/FR-014).
- A feature's env names never collide with the profile's extra env names (checked at mutation
  time, both directions; drift reported at sync/show time).
- Placement is idempotent: two syncs yield one state (Principle II).
