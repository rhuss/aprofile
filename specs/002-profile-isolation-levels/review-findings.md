# Deep Review Findings

**Date:** 2026-11-04
**Branch:** 002-profile-isolation-levels
**Rounds:** 3
**Gate Outcome:** PASS (see "Gate note")
**Invocation:** quality-gate (after_implement hook, via review-code)

## Summary

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 0 | 0 | 0 |
| Important | 34 | 34 | 0 |
| Minor | 37 | 25 | 12 |
| Notable | 3 | - | 3 |
| **Total** | **74** | **59** | **15** |

Counts are after deduplication across agents and rounds (initial review: 21 Important, 33 Minor,
3 Notable; re-review of round 1: 5 Important, 1 Minor; re-review of round 2: 5 Important, 2 Minor;
external tools: CodeRabbit 2 Important and 3 Minor in round 1, 0 in round 2; Codex 1 Important).

**Agents completed:** 5/5 internal (Goal Alignment skipped: no PR and no git remote), plus 2 external tools
(CodeRabbit, Codex). Copilot: skipped (disabled in config).

| Agent | Found | Fixed | Remaining | Status |
|-------|-------|-------|-----------|--------|
| Correctness | 9 | 9 | 0 | completed |
| Architecture & Idioms | 16 | 8 | 8 (6 Minor, 2 Notable) | completed |
| Security | 6 | 5 | 1 (Notable) | completed |
| Production Readiness | 12 | 10 | 2 (Minor) | completed |
| Test Quality | 17 | 15 | 2 (Minor) | completed |
| Goal Alignment | 0 | 0 | 0 | skipped (no PR found) |
| Re-review round 1 (correctness/security) | 6 | 6 | 0 | completed |
| Re-review round 2 (correctness) | 7 | 7 | 0 | completed |
| CodeRabbit (external) | 5 | 5 | 0 | completed (round 2: 0 findings) |
| Codex (external) | 1 | 1 | 0 | completed |
| Copilot (external) | 0 | 0 | 0 | skipped (disabled in config) |
| Test Suite (regression) | 0 | 0 | 0 | passed in every round |

## Gate note

Every Critical and Important finding was fixed. The fixes of round 3 (seven findings from the
round 2 re-review) are covered by new tests (`TestDivergedSurvivesBodyChangeAndLaunchers`,
`TestHookPlanningSafety`, `TestUserOrderKept`, `TestSettingsPullReplacesNonJSONFile`) and the full
suite, but were not re-reviewed by agents, because the review allows three rounds.

## Fixed findings, by theme

### Hook-set placement (internal/hooksets/place.go)
- **Nondeterministic merge** (correctness, production readiness; Important): map iteration made
  record files and settings files change between unchanged syncs. Recorded entries are now claimed
  in file order and the record is sorted.
- **Identical user groups taken over** (correctness; Important): a group the user added that equals
  a set's group was adopted as managed and later deleted on detach. It now stays the user's; no
  duplicate is placed.
- **Reapply deleted user groups by matcher** (correctness, CodeRabbit, re-review; Important):
  reapply now replaces a group only when exactly one user group has the same matcher and runs the
  same program (interpreters such as `bash` skipped); otherwise the entry stays diverged with an
  explanation, and the step note names what is replaced.
- **Diverged entries of detached sets reported forever** (correctness, production readiness; Minor):
  dropped silently once the set no longer applies.
- **Changed set body re-added a removed entry** (re-review 2; Important): a desired group that
  corresponds to a diverged entry (same set and matcher) is never placed by sync.
- **Sets that fail to load or have the wrong form** (Codex, re-review 1; Important): such sets are
  never applied and their placed entries stay unchanged (placeholder `Set.LoadErr`), reported as
  problems by sync and doctor.
- **Files form**: relink when a library file moves, report duplicate file names across sets,
  keep divergences of unavailable sets (Minor and Important).
- **Share level all**: managed entries in a profile's own hook file are removed; the record is
  cleared once the location is the shared link (correctness; Minor).
- **Order-only changes rewrote the file** (re-review 1; Minor): no write unless something was
  placed, removed or re-placed.
- Shared `hooksets.SyncProfile` for sync and `edit --share` (architecture; Important).

### Share level changes and sync
- **`edit --share` from none left skills and the plugin store unlinked** (correctness; Important):
  an empty own directory now becomes the link without confirmation; non-empty ones are reported.
- **Sync suggested a no-op command** (production readiness, CodeRabbit; Important): `edit --share`
  with the current level now re-applies that level's plan, completing what sync reports.
- **Hook-set part of a level change** listed in the dry run with the same lines as the apply,
  mirroring PlanHooks (re-review; Important).
- **Partial failures**: errors name the backup location and say re-running finishes the plan;
  prepare returns the changes made before an error and sync reports them (production readiness).
- **Running check names the process** (`pid`, executable) (production readiness; Minor).
- **`sync --relink` off a terminal**: message names the profiles and says the other changes were
  applied (production readiness; Minor).
- **`copy --overwrite` across levels**: refused with the `edit --share` command (correctness,
  production readiness; Minor).
- **TUI add form defaulted to all** (CodeRabbit; Important): it now starts at the effective default.
- **Unrelated edits blocked by a deleted set** (CodeRabbit; Minor): hook sets are validated only
  when they or the level change.

### Security
- **Copy-once copied secret-looking values without a warning** (Important): one warning per key,
  never the value.
- **Seeded and copied files written 0644** (Important, CodeRabbit): seeded mixed files are 0600,
  copies keep the source mode.
- **`hook-sets import` wrote secret literals into the library** (Minor): rejected, nothing written;
  a failed import removes its partial files dir.
- **Entry `.` could make a level change replace the whole profile dir** (Minor): rejected by the
  loader (also for `harness classify`).
- **Settings pull printed secret-looking permission entries** (Minor): such list items are shown
  as `<value hidden: looks like a secret>`.

### Settings, encoding, doctor
- `settings pull --replace` of a non-JSON file wrote an empty file (re-review; Important): the
  content is read at plan time; identical entries are skipped (no prompt, no backup).
- JSON rewrites escaped `&`, `<`, `>` (production readiness; Minor): `EncodeJSON` without HTML
  escaping everywhere aprofile rewrites a user file.
- Directory copy-once is atomic (temp dir + rename) (production readiness; Minor).
- Doctor reports a mixed file that became a link (architecture; Important); dead guard removed.
- `help sharing` output order is deterministic (architecture; Minor); stale comments fixed.

### Tests (test quality, all Important unless noted)
- SC-006 now diffs the filesystem against the dry-run entries and checks the default setup is
  unchanged; only a move to all may replace profile data.
- SC-001 checks an independent table of every Background entry; SC-002 allows only content and the
  plugin store as links at content; SC-005 uses an independent directory walk.
- New coverage: copy across levels (FR-007a), hand-edited level (FR-017a), settings pull apply
  with `--replace`, named keys and pi, backup contents for level change, pull and reapply, reapply
  exit 3, missing script during sync (fixture inside the temp root), import `--share`,
  `harness show` per level, doctor exit status for information findings, upgrade "nothing
  deleted" by snapshot, foreign links in the files form (Minor), vacuous-pass guard (Minor).

## Remaining findings (Minor)

1. `layer.Plan` keeps its unused `p` parameter (plan.md interface contract) (architecture).
2. The `home` override is threaded through Plan/Treatments only; other default-setup reads use
   `$HOME` (tests set `HOME` anyway) (architecture).
3. Four tree-copy helpers with different symlink policies (architecture).
4. Two secret heuristics (`mcp` and `layer.SecretWarnings`) (architecture).
5. Mixed-file key layers: the slice one `shared` key layer is accepted silently; borrowable layers
   are accepted for keys without a meaning (architecture).
6. `levelTable` is not derived from `DefaultSharePolicy`; the copy form's level options lack the
   descriptions (architecture).
7. Level names and the default are defined in several packages (architecture).
8. A sync interrupted between writing the hook file and saving the record leaves placed groups
   unrecorded; they then count as the user's (production readiness).
9. Backups copy whole trees before a replace instead of renaming (production readiness).
10. The hook-set unit test does not assert the paths passed to `Backup` (integration tests check
    the backup contents) (test quality).
11. The 20-profile timing test runs by default (skip with `-short` or `APROFILE_PERF=0`) (test quality).
12. `hookChangeSummary` mirrors PlanHooks' rules by hand (production readiness, re-review).

## Notable Observations

### NOTABLE-1
- **File:** internal/layer/plan.go, internal/cli/settings.go, internal/hooksets/place.go
- **Category:** security
- **Source:** security-agent
- **Description:** Link checks use `Lstat` on the final path component only; a symlinked
  intermediate directory (for example `config/opencode` linked by hand) is not detected.
- **Rationale:** Writes beneath such a directory would reach the default setup.

### NOTABLE-2
- **File:** internal/layer/treatment.go, internal/layer/plan.go
- **Category:** architecture
- **Source:** architecture-agent
- **Description:** `Treatments` reimplements the planner's decision table instead of sharing it.
- **Rationale:** Two copies of the table are the most likely place for show and dry runs to drift.

### NOTABLE-3
- **File:** internal/hooksets/place.go
- **Category:** architecture
- **Source:** architecture-agent
- **Description:** `planEventGroups` is a long function holding several record-state transitions.
- **Rationale:** Splitting it into classify, resolve divergence and assemble steps would make each
  transition testable on its own.

## Post-Fix Spec Coverage

Code was removed during the fix loop (adoption logic, a duplicated hook-sync path, dead guards).
All FR-001 to FR-035 remain implemented; the requirement-to-code matrix in REVIEW-CODE.md still
holds, and the test suite exercises every user story (US1 to US6).

## Test Suite Results

| Round | Test Command | Exit Code | Failures | Status |
|-------|-------------|-----------|----------|--------|
| 1 | make test (make check) | 0 | 0 | passed (after updating one test for the intended empty-dir change) |
| 2 | make test (make check) | 0 | 0 | passed (after updating two tests for the intended behavior) |
| 3 | make test (make check) | 0 | 0 | passed |
