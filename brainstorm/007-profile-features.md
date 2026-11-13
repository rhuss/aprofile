# Brainstorm: Profile Features (Curated Harness Capability Toggles)

**Date:** 2026-11-13
**Status:** active

Seeded from a conversation on OTel tracing and Auto-mode classifier accounting. Motivated by the AgentTacho brainstorm on classifier usage (agentmeter repo, `45-claude-automode-classifier-usage.md`). Related: [006 Neutralize provider switches](006-neutralize-provider-switches.md) (placed env entries, share level `all`), [003 Corgi integration](003-corgi-integration.md) (launch settings).

## Problem Framing

aprofile owns each profile's settings file and the launch wrapper, but has no way to say "this profile emits OTel traces" or "this profile writes the Auto-mode decision log". The only env entries aprofile writes today are the auth-neutralizing ones from 006. A user who wants such a feature must know the harness-specific mechanism (an undocumented env var, a `config.toml` section), set it through `--env-literal`, and remember to remove it again. Nothing in `show` reflects it, and nothing detects conflicts.

Two concrete first needs:

1. OTel telemetry: enable tracing and/or OTel log emission to an endpoint. Claude Code does this through env vars (`CLAUDE_CODE_ENABLE_TELEMETRY`, `OTEL_*_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT`); Codex through an `[otel]` section in `config.toml`.
2. Extra logging: `AUTOMODE_DECISION_LOG=1` makes Claude Code write `.automode_decisions.jsonl` in the session's working directory, which AgentTacho wants to ingest for token accounting (agentmeter brainstorm 45). The variable is undocumented and version-dependent, and has no settings key; it goes in the `env` block of `settings.json`.

**Success:** enabling a high-level feature on a profile and running `sync` makes the harness actually do the thing, through one command that is the same across harnesses; `show` reports it; disabling it removes the underlying entries on the next sync.

**Anti-goals:**
- Becoming a generic env var manager. The raw `--env` / `--env-literal` flags stay the escape hatch for anything outside the catalog.
- Feature flags on `add` or `edit`. Those commands manage a profile's identity and isolation; features are a separate concern (user decision).
- Writing anything into the linked default setup at share level `all` (FR-008 unchanged).
- Covering every harness knob. Timeouts, output limits and similar tuning values are not features.
- Auto-bumping a profile's share level to make features fit.

## Approaches Considered

### A: Dedicated `features` command over a curated catalog (chosen)
`aprofile features <harness>/<name>` shows status (every feature the harness supports, its state and parameter values) and, with flags, mutates it. A per-harness capability table maps feature ids plus parameters to the harness-specific mechanism: env entries in `settings.json` for Claude Code, an `[otel]` section in `config.toml` for Codex.
- Pros: one concern per command; bare invocation doubles as discovery ("what can I enable here?"); the catalog and capability tables are data, so new features and parameters need no CLI surgery; harness gating falls out of the table.
- Cons: new command and capability-table machinery; creating a profile with features on is two steps (`add`, then `features`).

### B: Feature flags on `add` and `edit`
- Pros: one-shot profile creation.
- Cons: mixes the identity/isolation layer with the capability layer; `edit`'s flag list grows without bound; no natural status surface. (User decision: only the dedicated command, to keep the layers orthogonal. This supersedes the initial inclination to also allow setting features at `add` time.)

### C: Raw env passthrough only (status quo)
- Pros: no new work.
- Cons: harness-specific knowledge stays in the user's head; no status, no removal, no conflict detection.

### Share level `all` handling (within A)
Refuse (chosen) versus store-the-state-but-warn versus auto-bump. Refusing keeps `features` status and reality from drifting, and matches how 006 treats the same wall. History sharing is unaffected: history groups are orthogonal to share levels, so a profile that wants shared history plus features goes `content` plus `--group`.

## Decision

Approach A. `features` is its own command, refusing at share level `all` with a hint at `aprofile edit --share content`.

## Key Requirements

- Command surface: `aprofile features <harness>/<name>` bare shows status; with mutation flags it updates the profile record and reminds the user to run `sync`, the same contract `edit` has today.
- v1 catalog: the telemetry feature (traces on/off, logs on/off, shared endpoint, protocol) and the automode-decision-log feature. *(User decision: start with these capabilities; privacy/quiet mode, debug logging and proxy are v2 candidates. The protocol parameter was added after review: both harnesses require choosing a transport, and a wrong fixed default means telemetry silently goes nowhere.)*
- The `protocol` parameter defaults per harness (`http/protobuf` for Claude Code, `otlp-http` with json for Codex) and is overridable. *(User decision.)*
- Feature model: a feature is an id plus named, typed parameters. The capability table declares per harness which features and parameters exist. Adding a feature or parameter is a data change, not a redesign. *(User decision: plan for telemetry extension, per-signal endpoints and auth later.)*
- The shared endpoint parameter is named `--endpoint` within telemetry, so per-signal flags (`--traces-endpoint`, `--logs-endpoint`, auth) extend it without a rename. *(My call.)*
- Harness gating: a flag the profile's harness does not support is a hard error naming the harness, not a warning. *(My call.)*
- No feature flags on `add` or `edit`. *(User decision.)*
- `copy` carries feature state along with the rest of the profile. *(My call.)*
- Storage and sync: feature state lives in the profile record next to extra env. `sync` materializes each enabled feature as placed entries, recorded and maintained like other launch entries (006's mechanism). Disabling a feature removes its entries on the next sync. A collision between a feature's entries and a hand-set extra env var is flagged, not silently resolved. *(User decision.)*
- Claude Code mapping: `CLAUDE_CODE_ENABLE_TELEMETRY=1` as master switch, per-signal `OTEL_*_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL`; traces additionally need `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` while traces are beta. Profile-level `settings.json` is a valid place for these (Claude Code ignores `OTEL_*` in repo-level settings, not profile level).
- Codex mapping: the `[otel]` section of `config.toml`; logs via `exporter`, traces via `trace_exporter`, each `otlp-http`/`otlp-grpc` with endpoint, headers and protocol; `headers_helper` exists as the auth extension point. Writing placed entries into `config.toml` is a new surface for aprofile.
- automode-decision-log mapping: `AUTOMODE_DECISION_LOG=1` in the env block. Undocumented, version-dependent, writes `.automode_decisions.jsonl` into the session's working directory. The consumer is AgentTacho (brainstorm 45 there); aprofile only enables it.
- Share level `all`: `features` refuses with a message pointing at `aprofile edit --share content`. *(User decision, option 1 of the three above.)*
- The reverse direction: `edit --share all` on a profile with enabled features warns and disables them, clearing the feature state from the record and reporting it in the edit output. This avoids both blocking a legitimate edit and stranding feature state as drift that `sync` can never repair. *(User decision, added after review.)*
- Codex `[otel]` ownership: aprofile removes only the keys it placed; hand-written `[otel]` keys are never touched. Placed-entry locations for nested TOML subtrees are an explicit v1 requirement of the record schema, not an implementation detail. *(My call, added after review.)*
- Collision semantics, concretely: mutating commands (`features`, `edit --env` / `--env-literal`) hard-error when they would create a name collision, checked in both directions; collisions that appear anyway surface as `sync` problems and `show` warnings. *(My call, added after review.)*
- Data model: the cross-harness catalog (feature ids, typed parameters) lives in Go code; the per-harness capability mapping lives in the harness definition YAML, overridable via `harnesses.d` like every other harness property. *(My call, added after review.)*
- Cross-harness `copy`: feature state is carried where the target harness supports the feature, otherwise reported in the copy report, following the hook-set precedent. No hard error. *(My call, added after review.)*
- `show` gains a features section, one line per enabled feature, so drift between recorded state and the settings file is visible. *(My call.)*

## Open Questions

- Exact TOML encoding for Codex exporters (inline-table enum shape such as `exporter = { otlp-http = { endpoint = "...", protocol = "json" } }`) needs verification against a current Codex release.
- Does `features` remind to run `sync` only, or offer to run it? `edit` reminds; matching it is the default.
- Should parameter values that carry secrets (OTLP headers, auth) use the ref/literal distinction from `--env`? No auth parameter exists in v1; decide when one lands.
- Should `doctor` warn when an enabled feature depends on an undocumented, version-dependent mechanism (the automode log) that the installed harness may not honor?
- Final feature id and flag naming (`tracing` versus `--trace-mode`, and so on) in status output and flags.
- How `features` interacts with `settings pull`: does pulling a profile's settings overwrite feature-placed entries, and if so, is that reported as drift?
