# Code Review: History Groups (spec 003)

**Spec:** specs/003-history-groups/spec.md
**Date:** 2026-11-07
**Reviewer:** Claude (speckit-spex-gates-review-code, ship pipeline)
**Diff:** `git diff main...HEAD` (branch 003-history-groups)

## Compliance Summary

**Overall Score: 97% (before the fix loop); 98% after it**

- Functional requirements: 30/30 implemented (FR-001 to FR-027 with FR-022a/b/c). One partial: FR-013, see the review guide below.
- Acceptance scenarios: 23/24. US3 AS2 names Codex as "does not allow merging", but codex.yaml merges `sessions/` per file (research-backed). This is a spec evolution candidate.
- Edge cases: 15/16. When a store was deleted by hand, sync lists the recreated directories but prints no explicit "history is missing" message. doctor does say it.
- Success criteria: SC-001 to SC-005 are covered by tests. SC-006 is a manual walkthrough.
- Spec-declared constants: none.

## Code Review Guide

Where to look and in what order:

1. `internal/history/join.go`: membership plans.
   - PlanJoin: merge, set-aside, link.
   - PlanDetach and PlanLeave.
   - The safety guards `checkNotSelf`, `sameFile` and `linkedComponent`. They compare file identity, never path strings.
2. `internal/layer/plan.go`: the shared planner for member history entries.
   - `planHistory`, `historyLink`, `storeDir`, `PlanHistoryEntry`.
   - The FR-022a branch in `planRule`. For the home store it is limited to state entries, to preserve spec 002 `--fix-links`.
3. `internal/layer/apply.go`: the new step kinds.
   - `moveNoOverwrite` (hard link, so it never overwrites).
   - Interrupted-move completion, which is guarded by `DistinctDirs`.
4. `internal/cli/history_groups.go` and `edit.go`: membership changes and their interplay with `--share`.
   - Confirmation is resolved before any write.
   - Membership is recorded before apply.
   - The applied steps are listed on failure.
5. `internal/harness/loader.go`: `validateHistoryRule` and `validateHistoryNesting` (FR-003).
6. Visibility and drift: `sync.go`, `doctor.go`, `list_show.go`, `group.go`, `wrapper/*`, `registry.go`.

Deviations to decide on via spec evolution:

- **US3 AS2:** Codex merges per file. Update the scenario text.
- **Store deleted by hand:** sync has no "history missing" message.
- **FR-013:** `changeGroup` does not check whether the profile is running when the plan has no filesystem steps, so only the profile record changes.

## Deep Review Report

**Gate: PASS** after 3 fix rounds plus a final gate check. Details are in `review-findings.md`.

| Agent | Found | Fixed | Remaining | Status |
|-------|-------|-------|-----------|--------|
| Correctness | 7 | 7 | 0 | completed |
| Architecture & Idioms | 9 | 5 | 2 Minor, 2 Notable | completed |
| Security | 3 | 2 | 0, 1 Notable | completed |
| Production Readiness | 6 | 4 | 2 Minor | completed |
| Test Quality | 9 | 4 | 4 Minor, 1 Notable | completed |
| Goal Alignment | - | - | - | skipped (no PR, no remote) |
| Re-review agents (rounds 1 to 3) | 9 | 6 | 1 Minor, 2 Notable | completed |
| CodeRabbit (external) | 4 | 4 | 0 | completed |
| Codex (external, 4 runs) | 10 | 8 | 2 regraded to Minor | completed |
| Copilot (external) | - | - | - | skipped (disabled in config) |
| Test Suite (regression) | 0 | - | 0 | passed in every round |

Several agents reported some of the same issues, so the per-agent counts overlap. The merged totals are 2 Critical (both fixed), 21 Important (19 fixed, 2 regraded to Minor with a mitigation), 30 Minor (11 fixed) and 5 Notable.

MVP: Correctness (7 findings).

### Key fixes applied

1. **Data loss closed.** A join whose store entry links back into the profile, including through a case variant or a firmlink, is refused with exit 2. Earlier, the "interrupted move" completion could delete the only copy (found and fixed in re-review rounds 1 and 2).
2. **FR-003:** a history entry may no longer contain another classified entry. This blocked sharing credentials through an override.
3. **Dry run matches the real run** for `--share` combined with `--group` (FR-013, SC-005). A refused join never leaves a half-applied share change.
4. **FR-022a for every category.** sync, leave and move detach links from entries that are no longer history.
5. **One planner** for member history entries, shared by sync and the join. Nested file entries get their store dir, and `sync --relink` never leaves a dangling link.
6. **Recoverable failures.** A failed join records the membership and lists what was applied. Set-aside runs never collide, and the directory search stops on errors.
7. **Merge safety.** Symlinks are never merged into a store, and nothing is written through a linked store dir.
8. **Guard and parsing fixes:** the adopted-dir guard also covers form input; `--no-group=false`; the duplicate doctor finding; the concurrent-use notice.
9. **New regression and effect tests:** SC-003, SC-005 (`assertPlanApplied`), FR-024 registry and wrapper values, and loader nesting cases.

### Remaining findings

No Critical or Important findings remain. Two Important findings were regraded to Minor with a mitigation:
- **Home member, history entry reclassified to a private category:** sync reports the link as a leak (`--fix-links`) instead of replacing it. This is a trade-off with spec 002, and `harness classify` now says so.
- **Combined edit with an entry reclassified to a borrowable category:** the entry needs one more `sync`.

Post-fix spec coverage: all requirements verified, and every FR still has an implementation after the fix rounds removed code.

Fix commits: 4c56b24, e18ea41, 29c4d1a, b978d4e.
