# Deep Review Findings

**Date:** 2026-11-07
**Branch:** 003-history-groups
**Rounds:** 3 (plus a final gate check)
**Gate Outcome:** PASS
**Invocation:** quality-gate (ship pipeline)

## Summary

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 2 | 2 | 0 |
| Important | 21 | 19 | 0 (2 regraded to Minor, see below) |
| Minor | 30 | 11 | 19 |
| Notable | 5 | - | 5 |

**Agents completed:** 5/5 internal (Goal Alignment skipped: no PR, no remote), plus CodeRabbit and Codex. Copilot was disabled in config.
**Fix commits:** 4c56b24 (round 1), e18ea41 (round 2), 29c4d1a (round 3), b978d4e (follow-up minor).

## Findings fixed (Critical and Important)

| # | Sev | Source | Location | Finding | Resolution |
|---|-----|--------|----------|---------|------------|
| 1 | Important | correctness, architecture | internal/cli/edit.go, history_groups.go | `edit --share X --group G`: the share plan created history dirs, so the dry run and the real run listed different steps. A confirmation could also exit 3 after the share change had been applied (FR-013, SC-005). | Round 1: the share plan skips history entries when a group change is given. Round 3: links into the store from entries that are no longer history are left to the group change. |
| 2 | Important | correctness, architecture, Codex | internal/layer/plan.go planRule | FR-022a only handled entries that were still state. A history entry reclassified as content or credentials stayed linked into the group store. | Round 1: handled for every category. Round 2: PlanDetach on move and leave. |
| 3 | Important | security | internal/harness/loader.go | A history entry could contain a credentials entry (for example `agent/` around `agent/auth.json`), which shares logins with the group (FR-003). | `validateHistoryNesting` rejects it. |
| 4 | Important | architecture | history/join.go vs layer/plan.go | There were two join planners for entries without data (Stat vs Lstat had already drifted). | `layer.PlanHistoryEntry` and `HistoryStoreSteps` are now shared. |
| 5 | Important | Codex, correctness | internal/layer/plan.go | `sync --relink` replaced a real entry with a dangling link when the store dir was missing. | storeDir runs before the relink. |
| 6 | Important | Codex | internal/history/group.go | A nested history file had no store parent dir, so the first write failed. | Create and storeDir make the parent. |
| 7 | Important | Codex, correctness, CodeRabbit | internal/cli/add.go | `add --adopt` could pick a group in the form and bypass the adopted-dir guard. | Checked again in createProfileFromFlags. |
| 8 | Important | Codex, correctness, production | internal/history/join.go | Two joins in the same second shared a set-aside dir, overwrote the manifest and failed part way. | Run dirs get a counter suffix. |
| 9 | Important | production | internal/cli/history_groups.go | A failed join left an "isolated" profile with links into the store, which doctor called leaks. | Membership is recorded before apply, and the applied steps are listed on failure. |
| 10 | Important | test quality | tests/integration | The SC-005 test compared printed lists only. | `assertPlanApplied` checks the effects on disk. |
| 11 | Important | test quality | tests/integration | Registry and wrapper group values were not covered end to end (FR-024). | New test, including an inherited-value override. |
| 12 | Important | test quality | tests/integration | SC-003 was not measured for a seeded non-home store, a move out, or copy. | New test. |
| 13 | Critical | re-review r1 | join.go, apply.go | The round 1 "interrupted move" completion deleted the only copy when the store entry linked back into the profile. | Round 2 refuses with exit 2 (checkNotSelf). Round 3 compares file identity. |
| 14 | Important | re-review r1 | internal/layer/plan.go | Round 1 widened auto-fixing to private links of home members (spec 002 `--fix-links`). | Home is limited to state entries. |
| 15 | Important | re-review r1, Codex | internal/cli/history_groups.go | A move left links from no-longer-history entries in the old store. | PlanDetach on moves and re-joins. |
| 16 | Important | Codex r2 | internal/history/join.go | The set-aside dir search looped forever on permission errors. | The search stops on any error. |
| 17 | Critical | re-review r2 | join.go, apply.go | The data-loss guard compared path strings, so a case variant or firmlink deleted the only copy. | Compares `os.SameFile` identity, with a regression test. |
| 18 | Important | re-review r2 | internal/cli/history_groups.go | With `--share --yes`, a refused join failed after the share change was applied. | The join is planned before the share change, even with --yes. |
| 19 | Important | Codex r3 | internal/layer/plan.go, cli | A combined dry run listed a detach twice. | The share plan leaves those links to the group change. |

## Regraded to Minor (accepted, documented)

- **R1: Home member, history entry reclassified as credentials/owned/mixed** (Codex r3 P2, internal/layer/plan.go:273, history/join.go PlanDetach). Its link into `~/.claude` stays in place and is reported as a leaking link. Nothing records whether the link was once history, and auto-replacing private links into the default setup would break the spec 002 `--fix-links` contract (finding 14). `harness classify` now tells home members to run `aprofile sync --fix-links`. No data is lost.
- **R2: Combined `--share`/`--group` with an entry reclassified to a borrowable category** (Codex final P2, internal/layer/plan.go:274). The entry is missing until the next `sync`, which links it. `edit` already prints "run: aprofile sync". No data is lost.

## Minor findings fixed

These are fixed in rounds 1 to 3 or b978d4e:
- The share change is saved before the group change runs.
- `--no-group=false` is no longer a leave.
- doctor reports a wrong history link once.
- The concurrent-use notice prints on a join without steps.
- PlanMove is removed.
- NeedsConfirmation is reused.
- Level-table wording.
- Symlinks are never merged into a store.
- Interrupted hard-link moves complete.
- Blocked-store test assertions.
- classify note scope.

## Remaining Minor findings

- internal/list_show.go:352, doctor.go:205: show and doctor detect history links by a "history group " reason prefix (architecture).
- history/join.go, layer/plan.go: store mkdir steps carry an absolute Entry (architecture).
- internal/layer/apply.go copyExcl: an EXDEV copy writes straight to the final store path, so a crash mid-copy can leave a truncated file (production).
- internal/history/join.go ManifestWriter: the manifest is rewritten per item, which is O(n²) for very large set-asides (production).
- history/join.go planMerge: a symlink inside the store that points back into the profile (below the entry root) sets the file aside correctly but leaves a looping link in the default setup (final gate).
- Tests:
  - leave and move dry runs are not snapshotted
  - leave does not check every history entry
  - the Codex join output does not name the set-aside path
  - no all-`merge: none` harness test
  - the running refusal is checked by exit code only
  - SC-001 second-member checks for Codex and pi
  - integration wiring of the "default location is a profile dir" case
- Plus R1 and R2 above.

## Notable Observations

- **NOTABLE-1** (security): group stores and set-aside dirs are 0755 like profile dirs. Tighten them if profile dirs ever move to 0700.
- **NOTABLE-2** (architecture): the constitution's Principle II does not name the join's removal of an emptied entry tree explicitly.
- **NOTABLE-3** (test): the "default location inside the data dir / is a profile dir" refusal is unit-tested only.
- **NOTABLE-4** (re-review r2): for home members, a state entry that links into the default setup is detached without `--fix-links`. That is intended for former history, but a hand-made state link is detached as well. No data is lost (the target is never touched).
- **NOTABLE-5** (final gate): after a combined edit, borrowable reclassified entries need one more sync (see R2).

## Post-Fix Spec Coverage

The fix rounds removed code: PlanMove, storeMkdir, and the duplicated planner branches. Every FR-001 to FR-027 still has an implementation:
- FR-014 merge: history/join.go PlanJoin/planMerge
- FR-015 leave: PlanLeave
- FR-022 to FR-022c: layer/plan.go planHistory/historyLink, sync.go
- FR-013 plan, confirm, exit codes: cli/history_groups.go changeGroup

The full test suite passes. All spec requirements are verified after the fix loop.

## Test Suite Results

| Round | Test Command | Exit Code | Failures | Status |
|-------|-------------|-----------|----------|--------|
| 0 (baseline) | go test ./... (temp HOME) | 0 | 0 | passed |
| 1 | go test -count=1 ./... | 0 | 0 | passed (after fixing one expectation: home file entries link without a parent dir) |
| 2 | go test -count=1 ./... | 0 | 0 | passed |
| 3 | go test -count=1 ./... | 0 | 0 | passed (one assertion updated for the new refusal text) |

## Goal Alignment

Skipped: the repository has no remote and no PR.
