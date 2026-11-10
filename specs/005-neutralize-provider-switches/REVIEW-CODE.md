# Code Review: Neutralize Provider Switches for Every Profile Folder

**Spec:** specs/005-neutralize-provider-switches/spec.md
**Date:** 2026-11-10
**Reviewer:** Claude (speckit.spex-gates.review-code, ship pipeline)

## Compliance Summary

**Overall Score: 100%**

- Functional Requirements: 11/11 (FR-001 to FR-010 incl. FR-004a, FR-005a)
- Acceptance scenarios: 14/14 (US1 to US4)
- Edge cases: 8/8 (the "Changing to `all` drops the record" case for login profiles without launch settings was fixed during review, see Deep Review FINDING-1)
- Success criteria: 5/5 (SC-001's real-login check is a manual RUNBOOK step, as the spec states)

## Functional Requirements

| Req | Implementation | Status |
|-----|----------------|--------|
| FR-001 neutralize list for every own settings file | internal/launch/plan.go `desiredState` (gated on `canCarryEnv`), carrier.go `canCarryEnv`/`envCarrier`/`linked` | Compliant |
| FR-002 never blank supplied names | plan.go `desiredState` supplied set: every launch variable plus wrapper-carried key variable; folder-carried key stays blanked | Compliant |
| FR-003 placed, recorded, restored, stale, dry-run, idempotent | existing planner/record path; tests `TestPlanLoginNeutralizesAuthClearing`, `TestPlanLoginRestoresDeletedEntry`, `TestLaunchClaudeLoginNeutralizes` | Compliant |
| FR-004 nothing written at `all` | `linked()` covers linked and absent-at-all files; `TestPlanLoginShareAllPlacesNothing`, `TestLaunchReportAuthClearingAtAll` | Compliant |
| FR-004a verification recorded | research.md R1 (Claude Code 2.1.319) | Compliant |
| FR-005 share-all login gap in show/doctor | apply.go `authClearingGap`, report.go `Folder()`, doctor.go special fix | Compliant |
| FR-005a neutralizing-only folder is self-contained | `TestStatusLoginNeutralizingOnlyIsSelfContained` | Compliant |
| FR-006 no message for other kinds/harnesses; list unchanged | `authClearingGap` kind and env-location checks; `TestStatusAuthClearingItem` | Compliant |
| FR-007 names only, never values | steps/records/JSON carry names; canary tests | Compliant |
| FR-008 Codex verification recorded | research.md R2, codex.yaml comment | Compliant |
| FR-009 Codex unchanged (negative finding) | no Codex code change | Compliant |
| FR-010 docs | README.md, docs/guide/docs/user/corgi.md, spec 004 amendment notes | Compliant |

## Extra Features (not in spec)

None.

## Known Open Item: `settings pull --replace` lists "removed: env"

Judgement: real, Minor, a direct widening caused by FR-001 (spec 004 had it for key-carrying folders only). No FR is broken: the end state is correct because `launch.Apply` places the entries again, and a test now asserts that. The costs are a misleading listing, a prompt and backup on every run, and a short window without the entries. Recommended follow-up: have `planSettingsPull` ignore locations recorded in the launch record. Until then, record it as a known limitation in spec 005. Classified ambiguous (scope/deferral decision), not auto-fixed. Details: review-findings.md FINDING-3.

## Deep Review Report

**Gate: PASS** (after fix round 1). No unambiguous Critical or Important finding remains. One Important finding (FINDING-2, upgrade replaces a copied provider switch with little visibility) is left open as **ambiguous**: the spec edge case explicitly accepts the replacement, so how loud to make it is the author's decision.

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 0 | 0 | 0 |
| Important | 2 | 1 | 1 (ambiguous) |
| Minor | 12 | 6 | 6 |
| Notable | 6 | - | 6 |

| Agent | Found | Fixed | Remaining | Status |
|---|---|---|---|---|
| Correctness | 3 | 1 | 2 | completed |
| Architecture & Idioms | 5 | 3 | 2 | completed |
| Security | 2 | 0 | 2 | completed |
| Production Readiness | 6 | 0 | 6 | completed |
| Test Quality | 5 | 2 | 3 | completed |
| Goal Alignment | 0 | 0 | 0 | skipped (no PR found) |
| CodeRabbit (external) | 0 | 0 | 0 | failed (auth; `--files` unsupported) |
| Copilot (external) | 0 | 0 | 0 | skipped (disabled in config) |
| Codex (external) | 0 | 0 | 0 | completed |
| Test Suite (regression) | 0 | 0 | 0 | passed |

(Agent counts are before dedup. The settings-pull item was reported by four agents and merged into FINDING-3.)

MVP: Production Readiness (6 findings)

Key fixes applied:
1. `edit --share` now drops the launch record and prints the level-change lines for login profiles that have only neutralizing entries, with a new regression test (correctness)
2. Corrected stale `canCarryEnv`, `WrapperOnly` and `SettingName` docs; removed parameter shadowing (architecture)
3. Doctor's `--share content` fix is now limited to reason `share-all` (architecture)
4. Tightened `TestHandEditedLevel` and added a re-placement assertion after `settings pull --replace` (test quality)

Remaining (ambiguous, needs author decision):
- FINDING-2 (Important): upgrade silently moves a profile whose copied `settings.json` set a provider switch; the spec accepts it, so visibility is a design choice
- FINDING-3 (Minor): `settings pull --replace` "removed: env" noise; follow-up or known-limitation note

Notable: `ANTHROPIC_BASE_URL` is not neutralized for folder launches (outside FR-001 scope, possible follow-up spec).

Fix commits: 60dd564, 649d8d3.

Details: [review-findings.md](review-findings.md).

Final checks: `GOFLAGS=-count=1 make test` passed, `go vet ./...` clean, `gofmt -l` empty.

## Resolution of Ambiguous Findings (2026-11-10)

- **Upgrade replaces a profile's own provider switch (Important):** decided 1b. `sync` now prints a
  separate note whenever an empty neutralizing entry overwrites a non-empty value in the profile's
  own settings file (a replaced user entry or a hand edit of a placed entry), naming the variable,
  never its value, with the fix `aprofile edit <id> --env NAME=<value>`. Covered by
  `TestPlanLoginReplacesUserSwitch` and `TestPlanLoginRestoresDeletedEntry`; verified end to end in
  a throwaway `HOME` (note shown, fix hint restores the value, second `sync` makes 0 changes).
- **`settings pull --replace` lists "removed: env" (Minor):** decided follow-up; tracked in
  `brainstorm/idea-inbox.md` as `settings-pull-ignore-placed-entries`.
