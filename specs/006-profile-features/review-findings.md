# Deep Review Findings

**Date:** 2026-11-13
**Branch:** 006-profile-features
**Rounds:** 1
**Gate Outcome:** PASS
**Invocation:** quality-gate (ship pipeline, autonomous mode)

## Summary

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 0 | 0 | 0 |
| Important | 10 | 10 | 0 |
| Minor | 13 | 3 | 10 |
| Notable | 2 | - | 2 |
| **Total** | **25** | **13** | **12** |

**Agents completed:** 4/5 internal agents returned reports; the security agent
stalled and its leg was covered by an inline security pass in the reviewing
session (see Security below). External: CodeRabbit completed (7 findings), Codex
completed (2 findings), Copilot skipped (disabled in deep-review-config.yml).
Goal Alignment skipped (no PR found; the spec is the goal source and was
reviewed in Stage 1).

**Test suite:** `go build ./...`, `go vet ./...` and `go test ./...` green after
the fix round, including two new regression tests
(`TestFeaturesReplacedValueSurvivesResync`, `TestPlanFeatureLinkedFileIsAProblem`)
and a strengthened `TestFeaturesLoadWarnings` / `TestFeaturesCollisions`.

## Findings

Merged and deduplicated across sources. FINDING-1 merges Codex P1 with
CodeRabbit's identical finding.

### FINDING-1 (Important, fixed)
- **Severity:** Important
- **Confidence:** 95 (two independent sources)
- **File:** internal/launch/plan.go (env path, JSON generic path, TOML path)
- **Category:** correctness
- **Source:** codex (also reported by: coderabbit)
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** When a feature entry replaces a hand-written value, the
`Replaced` original was recorded only on the placement where `!recorded` held.
A subsequent change-less sync rebuilt `NewRecord` without carrying the saved
`Replaced` forward, so disabling after a re-sync deleted the user's value
instead of restoring it. A pre-existing value equal to the desired value was
never captured at all. All three placement paths (env block, generic JSON,
nested TOML) shared the defect.

**Why this matters:** SC-002 requires disabling a feature to leave the file in
its pre-enable state; the record is the only recovery data, and silently
dropping it turns a documented restore into data loss.

**How it was resolved:** Each placement path now carries the recorded `Replaced`
forward via `rec.Get` and captures a non-empty unrecorded value even when it
equals the desired value (restore then writes the same bytes). Regression test
`TestFeaturesReplacedValueSurvivesResync` runs enable, sync, re-sync, disable,
sync and asserts the hand-written `OTEL_LOGS_EXPORTER=console` is restored.

### FINDING-2 (Important, fixed)
- **Severity:** Important
- **Confidence:** 85
- **File:** internal/profile/profile.go (repairFeatures)
- **Category:** correctness
- **Source:** codex
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** A hand-edited profile with an invalid stored param value
(e.g. `protocol: carrier-pigeon`) had only that param dropped at load; parameter
resolution then treated it as missing and re-supplied the harness default, so a
malformed feature state still placed telemetry entries. This contradicted the
code's own contract ("an invalid stored value loads with a warning and places
nothing").

**Why this matters:** A malformed record silently activating telemetry is the
opposite of the documented repair semantics and can surprise a user who
hand-edited the file.

**How it was resolved:** `repairFeatures` now drops the whole feature state on
any invalid param (name, bool or enum), with a LoadWarning naming the feature
and the re-enable command. `TestFeaturesLoadWarnings` updated to assert the
feature is dropped.

### FINDING-3 (Important, fixed)
- **Severity:** Important
- **Confidence:** 90
- **File:** internal/cli/features.go (featureDriftWarnings), internal/cli/list_show.go
- **Category:** correctness
- **Source:** coderabbit
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** `runShow` can run with a nil harness definition (the
definition guard at the top of the JSON build), and the drift call sat outside
that guard: `len(def.Features)` panicked instead of showing the profile.

**How it was resolved:** `featureDriftWarnings` returns nil when `def == nil`.

### FINDING-4 (Important, fixed)
- **Severity:** Important
- **Confidence:** 95
- **File:** internal/cli/list_show.go (runShow)
- **Category:** correctness / production-readiness
- **Source:** coderabbit
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** The `feature_drift` assignment happened after the
`if asJSON { return writeJSON(...) }` early return, so the JSON form never
carried drift even when entries differed, were orphaned, or collided. Consumers
of the documented `--json` schema (FR-017) could not detect the conditions.

**How it was resolved:** Drift is computed before the JSON return and assigned
to `sj.FeatureDrift`; the text path renders it from the same field.
`TestFeaturesCollisions` now asserts `feature_drift` is present in
`show --json`.

### FINDING-5 (Important, fixed)
- **Severity:** Important
- **Confidence:** 80
- **File:** internal/harness/features.go (ResolveFeatureParams)
- **Category:** correctness
- **Source:** coderabbit
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** Resolution iterated every catalog param; a capability
mapping that declares only a subset (allowed by FR-005 and by
`validateFeatures`) failed with "has no value and no default" for the
undeclared params, making an accepted mapping unusable.

**How it was resolved:** `ResolveFeatureParams` resolves only the params the
mapping declares and rejects supplied params the mapping does not support.
`validateFeatures` now also requires gates and value references to name
mapping-declared params, so a partial mapping that references an undeclared
param is a load-time error instead of a silent place-nothing. `carryFeatures`
(copy) drops and names undeclared params for the target harness (correctness
agent's related Minor, folded here because the resolution change made it
reachable).

### FINDING-6 (Important, fixed)
- **Severity:** Important
- **Confidence:** 80
- **File:** internal/launch/plan.go (feature-entry removal paths)
- **Category:** correctness
- **Source:** coderabbit
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** The restore branches swallowed decode/encode/set errors
(`if err == nil { ... }`), then emitted `StepRemove` and dropped the record
entry anyway; a failed restore discarded the only recovery data (e.g. the user
turns `otel` into a scalar between enable and disable).

**How it was resolved:** Restore failures now propagate as plan errors before
any step is emitted or the entry dropped, on both the TOML and JSON removal
paths.

### FINDING-7 (Important, fixed)
- **Severity:** Important
- **Confidence:** 75
- **File:** internal/sync/sync.go (syncLaunch)
- **Category:** production-readiness
- **Source:** production agent
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** The "re-encoded: comments are dropped" note for rewritten
TOML files was emitted only under `--dry-run`; a real sync that placed feature
entries re-encoded the whole `config.toml` (comments and formatting dropped)
with no warning.

**How it was resolved:** The note loop moved outside the DryRun check and now
reports on both paths.

### FINDING-8 (Important, fixed)
- **Severity:** Important
- **Confidence:** 72
- **File:** internal/cli/features.go (featuresMutate), internal/feature/catalog.go
- **Category:** architecture
- **Source:** architecture agent
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** The two telemetry edge-case checks ("at least one signal
on", "endpoint required") were hardcoded in the CLI mutation path, beyond the
param-flag wiring the spec's Clarifications mandate, in tension with FR-006 /
SC-005 (adding a feature must not require command-code changes).

**How it was resolved:** The constraints moved into catalog data
(`RequireOneOf`, `Required` on `feature.Feature`, checked through
`CheckConstraints`), and the mutation path calls them generically. The
param-flag wiring stays in the CLI as the spec mandates, behind a named
`paramFlagFeature` constant.

### FINDING-9 (Important, fixed)
- **Severity:** Important
- **Confidence:** 85
- **File:** internal/launch/plan_test.go
- **Category:** test-quality
- **Source:** tests agent
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** The spec edge case "share level changed by other means"
(the dedicated linked-file error branch in plan.go) had zero coverage.

**How it was resolved:** `TestPlanFeatureLinkedFileIsAProblem` constructs a
feature-enabled profile with a linked settings file and asserts the error names
the file and the `--share content` fix, with no writes.

### FINDING-10 (Important, fixed)
- **Severity:** Important
- **Confidence:** 80
- **File:** tests/integration/features_test.go (TestFeaturesCollisions)
- **Category:** test-quality
- **Source:** tests agent
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** Both show assertions could pass for the wrong reason (a
bare variable name matches the placed-entry listing; bare "differ" matches the
always-printed settings-differ line), leaving US6 scenario 3's show-warning leg
effectively untested.

**How it was resolved:** The assertions anchor on the `features:       warning:`
block text.

## Notable Observations

Notable findings are informational; they do not count toward the gate.

### NOTABLE-1
- **File:** internal/launch/plan.go (Replaced subtree capture), internal/launch/record.go
- **Category:** security / architecture
- **Source:** coderabbit (confirmed by the inline security pass)

A hand-written Codex exporter subtree can contain authentication headers;
capturing it in `Replaced` stores those values in the launch record
(`~/.local/share/aprofile/managed-launch/...`, mode 0644). This is in tension
with the plan's no-secrets note, but spec 006's FR-012 mandates the
replace-and-restore behavior and its Assumptions explicitly defer secret-bearing
parameters to a later feature. Revisit when an OTLP-headers parameter lands:
either refuse to replace secret-bearing subtrees or store the recovery data
outside the record.

### NOTABLE-2
- **File:** internal/harness/builtin/claude.yaml (endpoint param)
- **Category:** security
- **Source:** inline security pass

The telemetry endpoint is a free string; a URL with embedded credentials
(`https://user:pass@host`) would land in the profile-private `settings.json`.
V1 has no secret-bearing params by spec Assumption; when one lands, the
ref/literal question the spec defers should also cover endpoint URLs.

## Security

The security review agent stalled without delivering a report; the leg was
covered by an inline pass over the feature's diff in the reviewing session:

- Values never leak into output: every replacement note names the variable or
  key path only (verified on all note-emitting paths in plan.go and report.go).
- Writes are confined to classified, profile-relative settings files; placement
  rules are validated at definition load (`entryPathSafe` + `classifiedFile`),
  including harnesses.d overrides, so no path traversal through a placement
  rule.
- All file content goes through `json.Marshal` / `toml.Marshal` (or the layer
  package's encoders); no string concatenation builds file content, so param
  values cannot inject structure.
- The FeatureValue YAML unmarshaling from user-controlled harnesses.d files
  decodes into typed structures with field errors and nil checks; no panic
  path found.
- No new auth surface: features only touch profile-owned settings.

Result: zero Critical/Important security findings; two Notables recorded above.

## Post-Fix Spec Coverage

The fix round changed behavior and tests; no requirement was dropped. All 18
functional requirements verified present after the fixes (see REVIEW-CODE.md
for the compliance matrix). The repairFeatures change strengthens the
"invalid stored state places nothing" data-model contract; the ResolveFeatureParams
change implements FR-005's per-harness parameter subsets more faithfully.

## Test Suite Results

| Round | Test Command | Exit Code | Failures | Status |
|-------|-------------|-----------|----------|--------|
| 1 | go test ./... | 0 | 0 | passed |

(One intermediate failure, `TestFeaturesLoadWarnings`, was the old assertion of
the pre-fix param-drop behavior that FINDING-2 deliberately changes; the test
was updated with the fix.)

## Remaining Findings (Minor)

Not auto-fixed per gate policy (fix loop addresses Critical/Important only).
All are small, localized, and none affects spec compliance:

1. `launch.FeatureEnvNames` in internal/launch/features.go is dead code with a
   misleading "one source" comment (architecture).
2. `featureDesiredFor` in internal/cli/features.go is a single-use delegation
   wrapper whose comment names a nonexistent function (architecture).
3. `envFeatureEntries` takes an unused `def` parameter (architecture).
4. The `{param:<name>}` reference regex and check code is duplicated between
   internal/harness/features.go and internal/launch/features.go (architecture).
5. The generic JSON feature path in plan.go duplicates the env path's
   Replaced-capture/note/removal bookkeeping (architecture).
6. The empty-key `Replaced[""]` sentinel convention is undocumented on the
   Entry type (architecture).
7. `show` plans the profile twice (folderStatus, then featureDriftWarnings)
   because the builtin definitions declare `features:`; reuse the first plan
   (production; SC-003 still met, this is work duplication, not a deadline).
8. Unanchored on/off substring assertions in the features status tests can
   pass for wrong reasons (tests).
9. No test asserts feature entries appear in `sync --dry-run` listing before
   the first feature sync (FR-018's dry-run leg) (tests).
10. `FeatureValue.MarshalYAML` has no direct coverage; the switch/struct forms
    are untested on the marshal side (tests).
