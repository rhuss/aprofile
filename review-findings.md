# Review Findings

**Date:** 2026-11-13
**PR:** https://github.com/rhuss/aprofile/pull/1
**Branch:** 006-profile-features
**Reviewed head:** bc66112236e013716540ea9f0cf765df17e1330a
**Fix state:** working tree after `bc66112` (fixes not yet pushed)
**Base:** docs/profile-guide (ec7c9996c467d605a0dfb6dd7ab0eb7e35d5be15)
**Scope:** Full PR; no trustworthy completed personal baseline
**Rounds:** 1 review round; 1 fix round
**Gate Outcome:** PASS (after fix round 1)
**Invocation:** standalone cc-review:review

## Summary

The first review found three Important issues, five Minor findings, and one Notable requirements observation. Fix round 1 addressed all nine findings. The follow-up gate passed, and the repository's full `make check` gate passed after the changes.

| Severity | Found | Fixed | Remaining |
|---|---:|---:|---:|
| Critical | 0 | 0 | 0 |
| Important | 3 | 3 | 0 |
| Minor | 5 | 5 | 0 |
| Notable | 1 | 1 | 0 |
| **Total** | **9** | **9** | **0** |

**Reviewers completed:** 8/9 selected (8/11 discovered). CodeRabbit failed authentication; Copilot and Greptile were disabled.

## Important findings

### Copied features stay active after disable

**FINDING-2 · Important · confidence 100% · `internal/cli/copy.go:339`
**Source:** correctness (also reported by gator, codex)
**Verdict:** confirmed · **Resolution:** fixed (round 1)

Same-harness copy copies the source settings before copying its feature state, but it does not transfer or remove the source's managed-entry ownership information. On the target's first sync, the copied feature value looks like a preexisting user setting. The planner stores that value as the original to restore later.

The isolated CLI reproduction enabled automode-log on a Claude content profile, synced it, copied it, synced the target, disabled automode-log on the target, and synced again. Every command succeeded. The final settings still contained `AUTOMODE_DECISION_LOG="1"`, while the target no longer recorded ownership of that entry. Sync reported that the entry was removed because it had restored the copied enabled value. This means the feature command reports off while the harness keeps logging. Telemetry uses the same replacement mechanism and can remain active too.

Preserve provenance and original replacement values for copied managed entries, or copy the source's settings after reconstructing their pre-placement state. The latter lets target sync place its own entries cleanly. The change should cover overwritten targets as well as new copies and verify the full copy → sync → disable → sync lifecycle. This is a bounded copy/placement integration change; copying feature state alone does not fix it.

**How it was resolved:** Copy now reconstructs the source settings from its launch record before copying them. The target receives the pre-feature values and records those as its own recovery originals. Overwritten targets also drop recovery entries for the files and mixed-file keys replaced by the copy. Regression coverage exercises Claude and Codex, absent and replaced user values, new and overwritten targets, and checks that source settings stay unchanged.

### Overlapping syncs lose restoration data

**FINDING-5 · Important · confidence 100% · `internal/launch/apply.go:105`
**Source:** production
**Verdict:** confirmed · **Resolution:** fixed (round 1)

Apply loads the managed-launch record once. Before writing it re-reads the settings, but it plans again using that original record. Atomic file replacement protects each individual file; it does not make the settings and recovery record one transaction.

Two initial syncs can both load an empty record and plan against the user's original setting. The first sync saves the original value and writes the feature's enabled value. The second then reads the updated settings with its stale empty record, mistakes the first sync's managed value for a user setting, and overwrites the saved original with it. Disabling the feature later restores the enabled value instead of the user's original. A deterministic Go overlay probe interleaved these operations through the existing pre-write hook and failed with `got "1", want original`.

Serialize the per-profile record/settings transaction across processes, from record loading through the final save. Refreshing the document or using atomic renames alone leaves the race. The locking should include callers that update the same pair; this is a moderate change because lock lifetime and error cleanup need care. The reproduction models a plausible overlap, not evidence that ordinary single-process sync corrupts settings.

**How it was resolved:** `launch.Apply` now acquires a per-profile cross-process file lock before loading either the settings or the launch record and holds it through the final save. A process-level regression test reproduces the overlapping sync interleaving and verifies that the original value is restored. Record-only edits use the same lock through `launch.UpdateRecord`.

### Recovery records weaken credential protection

**FINDING-9 · Important · confidence 99% · `internal/launch/record.go:111`
**Source:** security
**Verdict:** confirmed · **Resolution:** fixed (round 1)

Codex telemetry placement preserves the original exporter subtree so it can restore user settings when telemetry is disabled. Supported exporter configuration can include literal Authorization headers. The entire replaced subtree is serialized into the managed-launch record.

A hermetic Go overlay probe started with a mode-0600 config.toml containing a fake bearer token in an exporter Authorization header. After enabling telemetry, the token was also present in the recovery JSON with mode 0644, under aprofile directories created with mode 0755. The original private file therefore gains a less protected copy. Another local user can read it when the configured home/XDG ancestors are traversable; this is conditional on those ancestors and is not a claim that every user's home is public. A private home does not repair the broader artifact permissions themselves.

Write recovery records with 0600 permissions and keep their directory tree private (0700), or put sensitive snapshots in a private backup store and retain only references in public metadata. Also repair existing permissions when the serialized bytes have not changed; the current equality fast path otherwise leaves older public files untouched. This is a small permissions change plus a migration/repair check. The fake-token probe confirms the confidentiality regression without using real credentials.

**How it was resolved:** Recovery records are written with mode `0600`, and aprofile's owned `managed-launch` directories are repaired to `0700`. An unchanged record now gets a permission-only repair, so existing public records are fixed without a content rewrite. The regression test verifies the original Authorization header is restored and that dry runs do not change permissions.

## Smaller findings

### Declare the repository-wide secret-scanning policy change

**FINDING-1 · Minor · confidence 91% · `.gitleaks.toml:6` · source: goal-alignment · fixed (round 1)

**What is wrong and why it matters:** Full review at bc66112236e013716540ea9f0cf765df17e1330a. The new Gitleaks configuration excludes every _test.go file and the entire tests tree, while .betterleaksignore adds scanner-specific fixture exceptions. The PR summary and the inspected profile/corgi/neutralization specifications describe capability toggles and launch placement, not changing the repository scanning policy. Anyone running Gitleaks with the repository configuration now skips those sources rather than inspecting them, a substantive configuration change beyond adding the new feature fixtures. This finding is about undeclared scope, not a security-vulnerability assessment. No prior review thread exists.

**Suggested fix:** Explicitly declare and justify the new scanner configurations and their repository-wide exclusions in the PR scope, or move them into a separately described change.

**Calibration:** The new .gitleaks.toml:6-11 does exclude all _test.go files and tests/ paths, so the policy change is substantive and should be disclosed. However, this finding explicitly concerns undeclared PR scope and requests a scope explanation or separate change; it establishes neither a leaked credential nor a failing scanner invocation or current application defect. Important overstates the demonstrated impact of the disclosure gap. Minor is proportionate; any separate scanner-security defect would need its own evidence.
**Skeptic assessment:** The broad exclusion is real (.gitleaks.toml:6-11), and the PR body does not mention it. However, .gitleaks.toml:3-5 explicitly documents why test sources are excluded, and .betterleaksignore:1-9 explicitly identifies the exceptions as deliberate scanner fixtures and explains their maintenance. Thus the change is justified and visible in the configuration itself; the additional claim that omission from the feature PR summary is a material goal-alignment problem is a scope judgment rather than a demonstrated functional defect.

**How it was resolved:** The PR description now has a secret-scanner configuration section that names both files, explains why the fixtures need exceptions, and states that Gitleaks does not cover accidental real secrets in `_test.go` files or the `tests/` tree.

### Derive parameter handling from the catalog to deliver FR-006

**FINDING-4 · Minor · confidence 98% · `internal/cli/features.go:191` · source: goal-alignment · fixed (round 1)

**What is wrong and why it matters:** Full review at bc66112236e013716540ea9f0cf765df17e1330a. Spec 006 FR-006 requires adding a feature or parameter without changing command code, and SC-005 explicitly makes this a reviewable outcome. The command instead registers only traces/logs/endpoint/protocol (lines 54-57), detects only those flags (lines 89-90), repeats them in these fixed maps, and fixes their feature owner to telemetry (line 184). Adding a parameter to the catalog and harness mapping therefore leaves it impossible to supply through features; a parameterized third feature cannot use the parameter path either. This is a goal-delivery gap, rather than a claim about correctness of the existing two toggles. The PR does not acknowledge this limitation. No prior review thread exists. The current two built-in features work through these fixed flags; this limitation concerns the explicit extension requirement and does not establish a present launch failure.

**Suggested fix:** Build parameter flag registration, mutation detection, ownership and value collection from catalog metadata, so adding a catalog parameter or a parameterized feature does not require modifying internal/cli/features.go.

**How it was resolved:** Parameter flags are now registered from `feature.All()`, values are inferred from flags changed on the command, and mutations are routed to the parameter's catalog owner. Ambiguous parameter names and mixed-owner mutations fail with the feature names. Tests cover a new parameter and a parameterized feature without command changes.

### Keep PATH available before starting the harness

**FINDING-6 · Minor · confidence 75% · `internal/launch/carrier.go:100` · source: codex · fixed (round 1)

**What is wrong and why it matters:** For a Claude content/none profile using `--env-literal PATH=...`, this classifies PATH as folder-carried. `newModel` consequently clears PATH before the wrapper's executable lookup without restoring it in the shell. Because settings.json cannot supply PATH until Claude starts, the wrapper exits 127 when Claude resides in a custom directory, even when both the incoming and configured PATH contain that directory. Bootstrap variables such as PATH need wrapper-side handling.

**Suggested fix:** Bootstrap variables such as PATH need wrapper-side handling.

**How it was resolved:** `PATH` is now classified as a bootstrap variable carried by the wrapper, including when the harness settings file can carry literal environment variables. The reason is rendered as "required before the harness starts." Tests run the generated wrapper with a custom harness directory and verify login receives the same `PATH`.

### Surface harness-specific feature validation failures

**FINDING-7 · Minor · confidence 75% · `internal/launch/features.go:53` · source: codex · fixed (round 1)

**What is wrong and why it matters:** When a stored parameter is catalog-valid but unsupported by its harness, such as `protocol: http/protobuf` on Codex, profile loading emits no warning and this branch silently skips the feature. Sync then removes any recorded exporters and reports success, while `features` and `show` still report telemetry enabled without a warning or drift. This can also occur after narrowing a harness override's accepted values. Report the resolution failure rather than silently presenting an unapplied feature as enabled.

**Suggested fix:** Report the resolution failure rather than silently presenting an unapplied feature as enabled.

**How it was resolved:** `launch.ValidateFeatures` now rejects enabled states that the harness cannot resolve, and the planner calls it before planning removals. The `features` command reports the error directly, `show` reports it as a warning, and `sync` fails without changing the previously placed exporters. CLI and planner regression tests cover an invalid Codex protocol.

### Reject unsafe recorded paths before planning writes

**FINDING-8 · Minor · confidence 75% · `internal/launch/plan.go:132` · source: codex · fixed (round 1)

**What is wrong and why it matters:** If a managed-launch record contains an unsafe file path such as `../../escape.json` with a `Replaced` value, `ReadDocFor` skips reading it, but this loop reintroduces it into the write plan. The planner treats the missing document as empty and generates a restoration write; `Apply` then joins that path to configDir and overwrites a file outside the profile, losing its existing contents. Validate recorded paths before planning or applying changes, not only before reading them.

**Suggested fix:** Validate recorded paths before planning or applying changes, not only before reading them.

**How it was resolved:** Record loading, planning, and apply now reject paths with absolute roots, empty segments, or `.` and `..` segments. Tests cover malformed in-memory records and a real apply attempt that confirms a file outside the profile is not overwritten.

## Notable observation

**FINDING-3 · `internal/cli/features.go:124` · Reconcile off-feature discovery between the spec and CLI contract · fixed (round 1)

Full review at bc66112236e013716540ea9f0cf765df17e1330a. Spec 006 FR-001 and User Story 3 require the bare discovery form to list supported features on or off with parameter values and effective defaults. A fresh codex/work profile instead gets telemetry off with params {} in JSON and no parameter lines in text: defaults are resolved only inside this enabled-state branch, and text rendering repeats the enabled guard at line 150. A user cannot discover the supported telemetry parameters or the Codex-specific protocol default before enabling, which is the concrete task in User Story 3. contracts/features-cli.md and the test narrow defaults to enabled features, but the mandatory spec and scenario were not amended and the PR does not disclose partial delivery. No prior review thread exists. Because the implementation matches the narrower CLI contract, this is a documented-requirements inconsistency, not an unambiguous current correctness defect.

**Follow-up:** Confirm the intended discovery contract and align spec 006 FR-001/User Story 3 with contracts/features-cli.md; if defaults must be discoverable before enable, resolve and render them for disabled features too.

**How it was resolved:** Feature defaults are now resolved for disabled features as well as enabled ones, and both text and JSON output include the supported parameters. The CLI contract now documents that behavior. Tests cover Claude and Codex discovery in both output formats.

## What went well

- Harness capability mappings keep JSON/TOML mechanisms and defaults in definition data (`internal/harness/features.go` and built-in YAML).
- The launch planner supplies the same named steps to writes, dry runs and status reports (`internal/launch/plan.go`, `internal/launch/apply.go`).
- Wrapper rendering and login use one model for carrier decisions and environment cleanup (`internal/wrapper/env.go`).
- Existing feature lifecycle, placement, replacement, isolation and shell tests have substantive assertions; the test-quality reviewer found no additional reportable test defect.

## Review coverage

| Reviewer | Findings before merge | Status | Actual scope |
|---|---:|---|---|
| Architecture & Idioms | 0 | completed | full |
| Correctness | 1 | completed | full |
| Goal Alignment | 3 | completed | full |
| Production Readiness | 1 | completed | full |
| Security | 1 | completed | full |
| Test Quality | 0 | completed | full |
| Gator (Principal Engineer) | 1 | completed | full |
| Codex | 4 | completed | full |
| Copilot | - | disabled: disabled | — |
| CodeRabbit | - | failed: session or Git provider authentication requires renewal (coderabbit auth login) | — |
| Greptile | - | disabled: disabled | — |

Scope covered 151 changed paths, with 105 source/documentation paths dispatched after excluding specs/brainstorm from the source list. Relevant specs were used as requirements evidence. Deleted files were reviewed through the diff. Three independently reported copy findings were merged into one.

## Follow-up progress and limitations

This is an initial personal review. History collection was complete and contained no earlier personal review, inline thread, material concern, or pending question to carry forward. No thread resolution or continuation is proposed.

The review covers the pinned full PR; the remote head was checked and still matched. CodeRabbit required renewed authentication (`coderabbit auth login`), so the selected-reviewer coverage is incomplete and this run cannot establish a completed personal baseline. That limitation does not erase the concrete findings from the eight completed reviewers. External Codex findings retain the configured P2 → Minor mapping and the critic pass-through policy.

## Test and reproduction evidence

- `make check`: passed (exit 0), including gofmt, go vet, package tests and the integration suite. This is the repository suite after fix round 1.
- `go test ./internal/launch ./internal/cli ./internal/wrapper`: passed after the focused fixes and again after the final lock and reporting adjustments.
- `go test -race ./internal/launch ./internal/cli ./internal/wrapper`: passed.
- `go test -count=1 ./tests/integration`: passed uncached after the final code changes.
- Copy/disable CLI reproduction: confirmed the feature value remains after disable and sync.
- Concurrent-sync overlay probe: intentionally failed the original-value assertion at the reviewed head, demonstrating the race. The added process-level regression test now passes.
- Security overlay probe: confirmed the fake token appeared in a 0644 recovery record at the reviewed head. The added privacy and permission-repair regression test now passes.
- Overlays and isolated HOME directories left repository code unchanged.

## Meta-review

Skeptic: eight confirmed, one weak (scanner-scope disclosure). No false positives removed. Calibrator: scanner-scope disclosure downgraded Important → Minor; no contradictions.

| Agent | Findings assessed | Precision | Signal/Noise |
|---|---:|---:|---:|
| goal-alignment | 3 | 0.67 | 0.85 |
| correctness | 1 | 1.0 | 1.0 |
| production | 1 | 1.0 | 1.0 |
| codex | 3 | 0.95 | 0.95 |
| security | 1 | 1.0 | 1.0 |

## Need, fit and goal delivery

Need and Fit Summary:

| Judgment | Verdict | Evidence |
|----------|---------|----------|
| Need | ESTABLISHED | Referenced spec 006 US1: Rohul wants collector telemetry without hand-editing harness settings; US2 names AgentTacho accounting. Specs 004/005 establish the stacked folder-launch and provider-leak use cases. |
| Fit | FITS | README profile isolation/configuration purpose; constitution I harness knowledge as data, II recorded/idempotent writes, III private credentials and shared-default boundaries. |
| Implementation value | JUSTIFIED | One stable command serves two concrete capabilities across two different mechanisms and reuses the stack's placed-entry lifecycle. |

Goal Delivery Summary:

| Goal | Status | Evidence |
|------|--------|----------|
| FR-001: bare form lists supported features, state, parameters and effective defaults | DELIVERED | `featuresStatus` resolves and renders parameters for enabled and disabled features in text and JSON; the CLI contract documents the behavior. |
| FR-002: mutations store state and remind to sync; parameter flags imply enable | DELIVERED | `runFeatures`, `featuresMutate`, `featuresMutationJSON`. |
| FR-003: exactly telemetry and automode-log in v1 | DELIVERED | `feature/catalog.go` catalog. |
| FR-004: per-harness protocol defaults and enum validation | DELIVERED | Built-in Claude/Codex YAML; `harness.ResolveFeatureParams`. |
| FR-005: catalog in Go, capability mappings in overridable definitions | DELIVERED | `feature/catalog.go`, `harness/features.go`, loader feature-node merge. |
| FR-006: adding a feature or parameter requires no command changes | DELIVERED | `registerFeatureParams` derives flags and owners from `feature.All()`; `featureParamMutation` collects changed values without feature-specific command branches. |
| FR-007: reject unsupported mutations with harness/flag identity | DELIVERED | `featuresMutate` support checks and `ResolveFeatureParams`. |
| FR-008: dedicated mutation command, no feature flags on add/edit | DELIVERED | Root registers `newFeaturesCmd`; add/edit flag surface retains identity/isolation settings. |
| FR-009: store/copy features and report unsupported target state | DELIVERED | `profile.FeatureState`; `copy.go:carryFeatures`, `carryFeaturesForLevel`, report fields. |
| FR-010: sync materializes/removes recorded entries idempotently | DELIVERED | `launch.FeatureDesired`, `PlanWith`, JSON/TOML planners, locked `Apply`, `syncLaunch`; copy and overlap regressions pass. |
| FR-011: Claude telemetry switches/exporters/endpoint/protocol/beta flag and automode env mapping | DELIVERED | `builtin/claude.yaml:features`; signal-off rules explicitly place `none`. |
| FR-012: Codex nested TOML placement, preserve other keys, name replacements | DELIVERED | `builtin/codex.yaml:features`; `planTOML` captures replaced values, sets nested paths and reports key names. |
| FR-013: refuse all-level mutations, retain status | DELIVERED | `runFeatures` checks share level after read-only dispatch. |
| FR-014: edit to all warns and clears features | DELIVERED | `applyEditFields`, `launchLevelEffects` show and clear feature state. |
| FR-015: reject extra-env collisions in both mutation directions; report sync/show conflicts | DELIVERED | `featuresMutate`, `checkFeatureEnvCollision`, `PlanWith`, `featureDriftWarnings`. |
| FR-016: show enabled features/parameters and drift | DELIVERED | `list_show.go` feature section/JSON integration; `featureDriftWarnings`. |
| FR-017: stable status/mutation JSON | DELIVERED | `featureJSON`, grouped status schema, `featuresMutationJSON`. |
| FR-018: recorded placed-entry lifecycle and explicit constitutional extension | DELIVERED | Launch record/plan/apply/dry-run paths; constitution Principle II and PATCH 2.0.1 explicitly name feature entries. |
| Stacked 004: self-contained folder carriers, fixed wrapper settings, registry model and reporting | DELIVERED | `launch/carrier.go`, `keycmd.go`, `apply.go`; `wrapper` model/render/env; registry `Model`; `printFolder`, doctor `launchFindings`. |
| Stacked 005: neutralize unsupplied auth/provider names and describe all-level login gap | DELIVERED | `desiredState` neutralization and wrapper-supplied-name exclusions; `authClearingGap`; `Folder`, doctor, Corgi guide. |
| Spec 001 FR-031 withdrawal: remove Omarchy export | DELIVERED | `registry.go` CLI no longer registers export; exporter and provider fields removed. Explicit breaking change in the project spec. |

Scope Changes Summary:

| Change | Tier | Impact | Files |
|--------|------|--------|-------|
| New secret-scanner exclusions and fixture exception policy | Substantive | Repository Gitleaks now skips all Go test files and the tests tree. The PR description now discloses the policy and its coverage limit. | `.gitleaks.toml`, `.betterleaksignore`, associated canary fixture substitutions |


## Reproduction artifacts

The original reproduction artifacts are retained at `/tmp/cc-review-pr1.hZfDv4`; the regression tests now live with the affected packages.

- `final-findings.json`: normalized findings and critic assessments
- `correctness-copy-repro.py` / `correctness-copy-repro-output.txt`
- `production-overlay.json` / `production-apply-overlay_test.go` / `production-probe-output.txt`
- `security-overlay.json` / `security-overlay-apply_test.go` / `security-probe-output.txt`
- `check.out` / `check.rc`

<!-- cc-review:run-id:714c8d6c-8572-4bc0-89c2-1b31e3592c8b -->
<!-- cc-review-incomplete:v1 -->
