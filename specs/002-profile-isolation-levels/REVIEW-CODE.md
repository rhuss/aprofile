# Code Review: Profile Share Levels and Hook Sets

**Spec:** specs/002-profile-isolation-levels/spec.md
**Date:** 2026-11-04
**Reviewer:** Claude (speckit.spex-gates.review-code)

## Compliance Summary

**Overall Score: 100%** (after two fixes made during this review, listed below)

- Functional requirements: 36/36 (FR-001 to FR-035 plus FR-001a, FR-007a, FR-009a, FR-017a)
- Edge cases: 17/17
- Success criteria with automated checks: SC-001, SC-002, SC-003, SC-005 to SC-009 covered; SC-004 is a manual walkthrough (prepared in walkthrough.md, not run)
- Spec-declared constants: none (no `## Constants` section)

Test suite: 228 tests pass (`make check`: gofmt, vet, tests), up from 145.

## Functional Requirements

| FR | Implementation | Status |
|----|----------------|--------|
| FR-001 no catch-all, unclassified reported | `harness/loader.go` (built-in glob with borrowable layer rejected), `builtin/*.yaml`, `layer/plan.go` (report steps), `layer/treatment.go`, `doctor/doctor.go` | Compliant |
| FR-001a merge by entry, classify, show/doctor list | `harness/override.go` `mergeRulesByEntry`, `cli/harness.go` `harness classify`, `cli/list_show.go`, `doctor.definitionFindings` | Compliant |
| FR-002 Claude state list | `builtin/claude.yaml` | Compliant |
| FR-003 Codex state list, hooks.json | `builtin/codex.yaml` | Compliant |
| FR-004 database companions | `harness/loader.go` `expandCompanions` | Compliant |
| FR-005 verification evidence | research.md R1, R8 (planning) | Compliant |
| FR-006 one level per profile | `profile/profile.go` `Share` | Compliant |
| FR-007 --share on add/edit/copy/import, TUI, default | `cli/add.go`, `edit.go`, `copy.go`, `import.go`, `tui/forms.go`, `config.EffectiveDefaultShare` | Compliant |
| FR-007a copy level, settings, skills, hook sets | `cli/copy.go` `carrySettings`, `carrySkills`, `carryHookSets` | Compliant |
| FR-008 skills are content | built-ins, `layer/plan.go` (own dir at none) | Compliant |
| FR-009 four categories, data-driven | `harness/definition.go` `SharePolicy`, `Treatment` | Compliant |
| FR-009a mixed/owned settings never linked | planner categories; `help sharing`, `harness show` | Compliant |
| FR-010 copy once without hooks | `layer/plan.go` `copyOnce` | Compliant |
| FR-011 none seeds nothing | `layer/plan.go` `seedMixed`, minimal | Compliant |
| FR-012 README guide | README "Choosing a share level", `cli/readme_test.go` | Compliant |
| FR-013 help sharing | `cli/help_sharing.go` | Compliant |
| FR-014 descriptions, 80 chars | `cli/levels.go`, `levels_test.go` | Compliant |
| FR-015 show treatments, JSON | `layer/treatment.go`, `cli/list_show.go` | Compliant |
| FR-016 dry run lists, apply matches | `cli/edit.go` `changeShare`, `TestSC006DryRunMatchesApply` | Compliant (fixed, see below) |
| FR-017 backup, confirm, refuse while running | `cli/confirm.go`, `edit.go`, `profile/running_*.go` | Compliant |
| FR-017a edit applies; sync safe mode | `edit.go`, `sync/sync.go` (needs-confirm notes) | Compliant (fixed, see below) |
| FR-018 settings pull | `layer/settings.go`, `cli/settings.go` | Compliant |
| FR-019 registry share, APROFILE_SHARE | `registry.go`, `wrapper.go`, `login.go` | Compliant |
| FR-020 doctor checks and severities | `doctor/doctor.go` `shareFindings`, `definitionFindings` | Compliant |
| FR-021 no silent relink; --relink | `sync/sync.go`, `layer/prepare.go`, `cli/sync.go` | Compliant |
| FR-022 per-harness differences | `cli/harness.go`, `help_sharing.go` `harnessDifferences`, `show` | Compliant |
| FR-023 hook-set library | `hooksets/library.go` | Compliant |
| FR-024 hook location declared; else fail | built-ins `hooks:`, `cli/hooksets.go` `validateHookSets` | Compliant |
| FR-025 --hook-sets, no body fails | add/edit/copy flags, TUI multi-select | Compliant |
| FR-026 every-profile, exclusion | `hooksets.Applies`, flags, TUI | Compliant (fixed, see below) |
| FR-027 all profiles reject sets (exit 2) | `validateHookSets` | Compliant |
| FR-028 managed placement, reapply | `hooksets/place.go`, `record.go`, `cli/hooksets.go` reapply | Compliant |
| FR-029 scripts in place, files form links | `place.go` `planFiles` | Compliant |
| FR-030 no translation | bodies per harness only | Compliant |
| FR-031 hook-sets import | `hooksets/importer.go` | Compliant |
| FR-032 README alternatives | README "Hook sets" | Compliant |
| FR-033 missing share is all, no rewrite | `profile/store.go` | Compliant |
| FR-034 leaking links, fix-links message | `layer/plan.go` `leakingLink` | Compliant |
| FR-035 constitution amended | constitution 1.1.0 (commit f242a1d) | Compliant |

## Deviations found and fixed during this review

1. **FR-016/FR-017a**: `edit --share` left hook-set placement to the next `sync`. It now applies
   the hook-set part of the level change itself, and the dry run lists it with the same lines
   (`hookChangeSummary`). Test: `TestLevelChangePlacesHookSets`.
2. **FR-026**: the TUI copy form had no field for excluded hook sets. Added.

## Additions beyond the spec (documented)

- `default_locations` in definitions (contracts/harness-definition.md updated): OpenCode's
  default setup spans the XDG directories; without it no OpenCode profile could borrow content.
- A user catch-all rule with a borrowable layer actually links (spec Edge Cases: "allowed for
  user definitions"), and doctor reports it.
- Step kinds `create` and `fix-link`, status `not-synced` (data-model.md updated).
- Bug fix: terminal detection used "is a character device", so `/dev/null` counted as a terminal;
  now `isatty`. Affects `remove --purge` too (now exits 3 off a terminal without `--yes`).
- macOS running detection: Apple platform binaries hide their environment from
  `kern.procargs2`; harness binaries are detected (research.md, verification results).

## Open items (not code deviations)

- T058: R7 (`@` imports in a linked `CLAUDE.md`) and R8 (`/ide` from a profile) need a live
  harness session and were not run; R11 on Linux not run in this session.
- SC-004 walkthrough prepared, not run (manual by design).

## Code quality notes

- Pre-existing: doctor unit tests read the real `PATH` (wrapper shadowing check), so a developer
  machine with cc-deck wrappers on `PATH` adds findings; tests do not depend on them.
- `TestScenario9WrapperOverhead` measured 51 ms once under full parallel test load; it passes
  alone and the new benchmark measures about 31 ms.

## Deep review

The multi-perspective review (5 internal agents, CodeRabbit, Codex; 3 rounds) is documented in
[review-findings.md](review-findings.md): 0 Critical, 34 Important (all fixed), 37 Minor (25 fixed,
12 remaining and listed), 3 Notable (captured in brainstorm/idea-inbox.md). Behavior changes from
the fix rounds that touch the spec's wording:

- `edit --share <current level>` re-applies that level's plan; it is the command `sync` names to
  complete a hand-edited level (FR-017a).
- An empty own `skills/` or plugin store directory (from share level `none`) becomes the link
  without confirmation on a move to `content` or `all`; a non-empty one is kept and reported.
- A hook set whose library file fails to load, or whose body does not fit the harness, is never
  applied and its placed entries stay unchanged (like a deleted set, spec Edge Cases).
