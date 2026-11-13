# Research: Profile Features (Curated Harness Capability Toggles)

**Date**: 2026-11-13 | **Spec**: [spec.md](spec.md)

Phase 0 research. Unknowns from the spec and brainstorm, each with the decision, rationale and
alternatives. Sources: the aprofile tree on this branch (post-merge of 005), the Claude Code
monitoring docs, and the openai/codex sources on `main`.

## R1: Codex `[otel]` TOML encoding (spec assumption, brainstorm open question)

**Decision**: externally-tagged inline tables; `exporter = { otlp-http = { endpoint = "...",
headers = {}, protocol = "json" } }`, `trace_exporter` the same shape, `metrics_exporter =
"none"` as a bare string for variant-only values. Codex protocol values in the capability
mapping: `otlp-http json`, `otlp-http binary`, `otlp-grpc`.

**Live confirmation (T022, 2026-11-13, codex-cli 0.167.0)**: a profile `config.toml` with
`exporter = { otlp-http = { endpoint = "https://otlp.example.com", protocol = "json" } }`,
`trace_exporter = { otlp-grpc = { endpoint = "https://otlp.example.com" } }` and
`metrics_exporter = "none"` loads and runs (`codex exec` parses the config without error),
while `exporter = "otlp-http"` fails with
`Error loading config.toml: invalid type: unit variant, expected struct variant in
'otel.exporter'`, and `exporter = "none"` (unit variant as a bare string) loads. The
externally-tagged shape is confirmed; since TOML inline tables and `[section]` tables decode
identically, go-toml's table-syntax rendering of the same structure is equivalent.

**Rationale**: `OtelExporterKind` in `codex-rs/config/src/types.rs` derives
`Serialize`/`Deserialize` with `rename_all = "kebab-case"` and no `untagged` or `tag`
attribute, so serde's default external tagging applies: a table whose single key is the
variant name (`otlp-http`) mapping to the variant's fields (`endpoint`, `headers`,
`protocol`, `tls`). `OtelHttpProtocol` has variants `Binary`/`Json`. Unit variants (`none`,
`statsig`) encode as bare strings. Verified against the source on 2026-11-13; the tasks
include one live-release confirmation (a real `codex` reading a profile `config.toml` written
this way, or a source-level round-trip test with the upstream crate) before the codex
capability mapping is considered final.

**Alternatives considered**: writing `exporter = "otlp-http"` with sibling keys (rejected:
serde external tagging would not decode it); deferring codex support to v2 (rejected: User
Story 4 is the cross-mechanism proof).

## R2: Where the catalog lives (FR-005, brainstorm "my call")

**Decision**: `internal/feature` in Go: feature ids, parameter names, types, display groups.
Per-harness support, defaults, enums and placement rules in the harness definition YAML under
a new `features:` key, overridable via `harnesses.d` like every other definition section.

**Rationale**: the catalog is cross-harness vocabulary (like share levels), while everything
that varies per harness (which env var, which TOML path, which protocol values) is exactly
the knowledge Principle I pushes into definitions. Overrides must work without a release, so
the capability mapping cannot be Go code.

**Alternatives considered**: whole catalog in YAML per harness (rejected: duplicates shared
feature metadata four times, drifts); whole mapping in Go (rejected: violates Principle I,
needs a release to add a harness's telemetry).

## R3: Capability mapping schema shape (FR-005, FR-006)

**Decision**: per feature, the definition declares `params` (each with optional `default` and
`values` for enums) and `place`, a list of placement rules. A rule is `file` (a definition
classification path, e.g. `settings.json`), `location` (the launch-record location:
`env.<NAME>` for JSON env entries, a dotted TOML path like `otel.exporter` for TOML), and
`value`: a literal or a `{param: <name>}` reference resolved against the feature state. The
telemetry rules are conditional on the traces/logs params (a signal that is off places its
exporter entry as `none`, FR-011), expressed as `when: <param>` on the rule; the rule's value
template can reference the param's on/off state.

**Rationale**: two mechanisms (JSON env, TOML keys) and two features must be expressible
without per-feature Go code (FR-006). Literals cover fixed entries (`AUTOMODE_DECISION_LOG=1`,
the master switch, the beta flag); param references cover endpoint and protocol. The
launch-record `Entry` (file + dotted location + `Replaced` map) already represents both
mechanisms, so placement needs no new record fields.

**Alternatives considered**: Go functions per feature (rejected: FR-006); a full expression
language in YAML (rejected: `when: <param>` plus literal/param-reference values covers v1;
extend when a third feature needs more).

## R4: Feature state in the profile record (FR-009)

**Decision**: `Features []FeatureState` with `FeatureState{Feature string, Params
map[string]string}`, YAML key `features:`, stored sorted by feature id. Params hold strings;
the catalog validates types and enums at mutation time, not at load time (invalid stored
values load with a warning, like other `LoadWarnings`, and place nothing).

**Rationale**: sits beside `ExtraEnv` in the record; `copy` and rename carry it with the rest
of the struct. String params keep YAML human-editable and match `--json` output.

**Alternatives considered**: `map[string]map[string]string` (rejected: no place for future
per-feature fields, unsorted iteration); typed params per feature in the record (rejected:
couples the record schema to the catalog).

## R5: Signal-off semantics (review finding 5, FR-011)

**Decision**: a telemetry signal that is off places its exporter entry as `none` rather than
omitting it. Omitted signal flags at enable time default to off. On Codex the analogous rule
places the exporter key as `none`.

**Rationale**: the profile folder is the single source (constitution Principle V, spec 004):
an inherited `OTEL_LOGS_EXPORTER=otlp` from the shell or the wrapper's launch environment must
not re-enable a signal the user turned off. Wrappers clear only launch variables, so the
settings file must carry the `none` explicitly.

**Alternatives considered**: omitting the entry for off signals (rejected: an inherited value
would silently win, exactly the class of leak spec 005 neutralizes).

## R6: Collision detection surface (FR-015)

**Decision**: one shared helper answers "which env names would feature X place on harness H"
(record-independent, from the capability mapping). `features` mutation and `edit
--env`/`--env-literal` call it against the other side's stored names and hard-error naming
the variable. `sync` and `show` compare placed entries against extra env names and report
problems/warnings using the existing sync-problem and show-warning channels.

**Rationale**: the check must run in both directions at mutation time (spec User Story 6);
a single helper keeps `features`, `edit` and `sync` from drifting (the codebase's SC-006
lesson: one decision table, one implementation).

**Alternatives considered**: checking only at sync time (rejected: the spec demands hard
errors at mutation time); per-command inline checks (rejected: three copies drift).

## R7: `edit --share all` feature clearing (FR-014)

**Decision**: the existing `launchLevelEffects` path in `internal/cli/edit.go`, which already
drops launch-record entries when a file becomes a link, gains: clear `Features` from the
record, print a warning line naming each cleared feature. `sync` then removes the now-orphaned
placed entries through the normal record maintenance.

**Rationale**: reuses the established level-change flow; the warning matches how edit reports
other consequences of a level change.

**Alternatives considered**: refusing the edit (rejected: blocks a legitimate operation);
keeping inert state (rejected: unrepairable drift, the review's finding 1).

## R8: Branch lineage (spec assumption)

**Decision**: `006-profile-features` merges `005-neutralize-provider-switches` (done, commit
1e2cf35) because the spec builds on spec 005's placed-entry widening. The completion prompt
must surface this: merging 006 to `main` before 005 lands there would carry 005's changes in
the 006 squash/PR. Recommended order: land 005 first (`/speckit-spex-finish` on its branch),
then rebase or re-merge for 006.

**Rationale**: spec assumption is explicit ("planning and implementation MUST start from a
tree that includes spec 005").

**Alternatives considered**: rebasing 006 onto 005 (equivalent; merge chosen because the
branch had a commit already and merge preserves both histories); reimplementing placement
without 005 (rejected: duplicates the mechanism).
