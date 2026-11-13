# Code Review: Profile Features (Curated Harness Capability Toggles)

**Spec:** specs/006-profile-features/spec.md
**Date:** 2026-11-13
**Reviewer:** Claude (speckit.spex-gates.review-code, ship pipeline)

## Compliance Summary

**Overall Score: 100%**

- Functional Requirements: 18/18 (FR-001 to FR-018)
- Acceptance scenarios: 18/18 (US1 to US6)
- Edge cases: 6/6
- Success criteria: 5/5
- Constitution Check: PATCH 2.0.1 recorded (FR-018)

## Functional Requirements

| Req | Implementation | Status |
|-----|----------------|--------|
| FR-001 bare status lists supported features grouped with state/params/defaults | internal/cli/features.go `featuresStatus` (groups from `feature.Groups()`); `TestFeaturesTelemetryClaudeEndToEnd`, `TestFeaturesDiscoveryAndGating` | Compliant |
| FR-002 mutation flags update state, remind to sync, edit's contract | `featuresMutate` + `runFeatures`; mutation output "run aprofile sync to apply"; `--dry-run` follows edit's pattern | Compliant |
| FR-003 exactly two catalog features | internal/feature/catalog.go (`telemetry`, `automode-log`) | Compliant |
| FR-004 protocol defaults per harness, enum validated at mutation | claude.yaml (`http/protobuf`), codex.yaml (`otlp-http json`); `ResolveFeatureParams` rejects out-of-list values; `TestFeaturesDiscoveryAndGating` | Compliant |
| FR-005 catalog in Go, mapping in definition records, harnesses.d-overridable | internal/feature (catalog), `Definition.Features` yaml `features:` in loader's field list and override merge; `validateFeatures` at load | Compliant |
| FR-006 adding a feature touches only catalog/definitions/placement | constraints are catalog data (`RequireOneOf`/`Required`/`CheckConstraints`, deep-review fix); no command code branches on feature ids beyond the spec-mandated param-flag wiring | Compliant |
| FR-007 unsupported flag is a hard error naming harness and flag | `featuresMutate` errors "harness %q does not support the %s feature"; `TestFeaturesDiscoveryAndGating` asserts nothing changed | Compliant |
| FR-008 add/edit gain no feature flags | no feature flags in add.go/edit.go; `features` is the only mutation path | Compliant |
| FR-009 state in record beside extra env; copy carries and reports | `Profile.Features` (profile.go), `carryFeatures`/`carryFeaturesForLevel` (copy.go) with carried/left-out reporting; `TestFeaturesIsolationAndCopy` | Compliant |
| FR-010 sync materializes/removes placed entries, idempotent | launch plan feature paths; `TestFeaturesTelemetryClaudeEndToEnd` (second sync no-op), `TestPlanFeatureIdempotent` | Compliant |
| FR-011 Claude env mapping incl. off-signal `none`, beta flag, automode-log | claude.yaml `features:` place rules with `when`/`when-not` gates; integration tests assert every entry incl. `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` | Compliant |
| FR-012 Codex `[otel]` placed entries, hand-written keys preserved, replacement never silent, nested TOML locations | codex.yaml switch/struct values; planTOML nested paths, `Replaced` capture with key-path-only notes; `TestFeaturesCodexTelemetryEndToEnd` | Compliant |
| FR-013 mutation refused at `all` with the edit hint; bare status works | `runFeatures` share-all refusal; `TestFeaturesIsolationAndCopy` | Compliant |
| FR-014 `edit --share all` warns, clears, reports | edit.go clearedFeatures block; integration assertion on "features disabled: telemetry" | Compliant |
| FR-015 collisions hard errors both directions; sync/show report | `featuresMutate` collision check, `checkFeatureEnvCollision` (edit), plan-time collision error, `featureDriftWarnings`; `TestFeaturesCollisions` | Compliant |
| FR-016 show lists features and drift | list_show.go features section + `FeatureDrift`; `feature_drift` now also in `--json` (deep-review fix) | Compliant |
| FR-017 stable `--json` for status and mutation | `featuresStatus`/`featuresMutationJSON`; contracts/features-cli.md | Compliant |
| FR-018 feature-placed entries are placed entries; Constitution Check | same record/restore/stale/dry-run path as launch entries; constitution PATCH 2.0.1 names feature-placed entries in Principle II | Compliant |

## Acceptance Scenarios and Edge Cases

All 18 scenarios across US1-US6 are covered by `tests/integration/features_test.go`
(scenarios 1-6 map to the six test functions) plus plan-level tests. All six
edge cases: signal-required and endpoint-required errors
(`TestFeaturesValidationErrors`), share-level change by other means
(`TestPlanFeatureLinkedFileIsAProblem`, added during deep review), double-sync
idempotency, undocumented-variable drift (spec's Assumptions keep `doctor`
silent, as stated), and feature state dying with the profile record (removal
discards both record and profile-owned files).

## Extra Features (not in spec)

None. `--dry-run` on `features` follows `edit`'s existing output contract
(FR-002 "following the output contract of `edit`") and is covered by the
features-cli contract document.

## Deep Review Report

**Gate: PASS** (after fix round 1). No Critical or Important finding remains.

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 0 | 0 | 0 |
| Important | 10 | 10 | 0 |
| Minor | 13 | 3 | 10 |
| Notable | 2 | - | 2 |

| Agent | Found | Fixed | Remaining | Status |
|---|---|---|---|---|
| Correctness | 1 | 1 | 0 | completed |
| Architecture & Idioms | 7 | 1 | 6 | completed |
| Security | 0 | 0 | 0 | stalled; covered inline (2 Notables recorded) |
| Production Readiness | 2 | 1 | 1 | completed |
| Test Quality | 7 | 4 | 3 | completed |
| Goal Alignment | 0 | 0 | 0 | skipped (no PR found) |
| CodeRabbit (external) | 7 | 5 | 2 | completed (1 out-of-scope skip, 1 Notable) |
| Codex (external) | 2 | 2 | 0 | completed |
| Copilot (external) | 0 | 0 | 0 | skipped (disabled in config) |
| Test Suite (regression) | 0 | 0 | 0 | passed |

MVP: Test Quality / CodeRabbit (7 findings each).

Key fixes applied (round 1):

1. `Replaced` originals survive intermediate syncs on all three placement
   paths, and equal pre-existing values are captured too, so a disable
   restores them (Codex + CodeRabbit; regression test added).
2. A malformed stored feature state now drops the whole feature instead of
   letting the harness default re-supply the bad param (Codex).
3. Nil harness definition no longer panics `show`'s feature-drift path
   (CodeRabbit).
4. `feature_drift` is emitted in `show --json` (CodeRabbit).
5. Partial capability mappings resolve their declared params only; supplied
   undeclared params are rejected; gates/references must name declared params;
   cross-harness copy drops and names undeclared params (CodeRabbit +
   correctness).
6. Restore failures propagate instead of discarding recovery data (CodeRabbit).
7. The TOML re-encode "comments are dropped" note reports on real syncs too
   (production agent).
8. Telemetry's enable-time constraints moved from CLI code into catalog data
   (`CheckConstraints`), resolving the FR-006 tension (architecture agent).
9. Coverage for the linked-file edge case and anchored show-warning
   assertions (tests agent).

Remaining findings: 10 Minor (dead code, small duplications, double planning in
show, three test-coverage gaps) and 2 Notable observations, all listed with
rationale in review-findings.md. None affects spec compliance.

Notable observations (for future brainstorming):

- Replaced-subtree capture can store exporter credentials (e.g. Codex OTLP
  headers) in the 0644 launch record; spec 006's Assumptions defer secret-bearing
  params and FR-012 mandates replace-and-restore, so this is a recorded tension,
  not a defect against the spec (CodeRabbit + inline security).
- The telemetry endpoint is a free string; an endpoint URL with embedded
  credentials would land in the profile-private settings.json (inline security;
  v1 defers per Assumptions).

CodeRabbit also flagged spec 005's research.md real-login verification
(out-of-scope for this feature; relayed to the author rather than fixed here).

Post-fix spec coverage: 18/18 requirements verified after the fix loop; no
requirement was dropped by any fix.

Test suite: `go build ./...`, `go vet ./...`, `go test ./...` all green after
the fixes.

Details: review-findings.md
