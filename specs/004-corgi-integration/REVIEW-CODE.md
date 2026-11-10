# Code Review: Self-Contained Profile Folders (004-corgi-integration)

**Spec:** specs/004-corgi-integration/spec.md
**Date:** 2026-11-10
**Reviewer:** Claude (speckit-spex-gates-review-code + spex-deep-review)
**Base:** main...HEAD

## Compliance Summary

**Overall Score: 100%**

- Functional Requirements: 31/31 (100%) (FR-001 to FR-024 incl. lettered sub-requirements)
- Edge Cases: 22/22 (100%)
- Success Criteria with automated checks: SC-001 to SC-006 covered by tests (SC-001's real-harness run and SC-007 are manual walkthrough items by design)
- Spec-declared constants: none (no `## Constants` section)

## Functional Requirements

| Requirement | Implementation | Status |
|---|---|---|
| FR-001 one carrier per launch setting | internal/launch/carrier.go `DecideWith`; wrapper uses the same decision (internal/wrapper/env.go `newModel`) | Compliant |
| FR-002 folder locations, per-kind `folder_key`, `launch_clear` as definition data; harnesses.d may override | internal/harness/definition.go (`FolderLaunch`, `FolderKey`, `LaunchClear`), loader.go `validateLaunchFields`, builtin/claude.yaml, codex.yaml | Compliant |
| FR-002a model as launch argument is definition data, wrapper-only | `model_arg` in codex.yaml; carrier.go `ReasonHarnessRewrites` | Compliant |
| FR-003 sync writes folder-carried settings | internal/sync/sync.go `syncLaunch` -> `launch.Apply` | Compliant |
| FR-004 own only placed entries, record in data dir, no markers, TOML re-encode noted in dry run | launch/plan.go, record.go (`managed-launch/<harness>/<name>.json`), sync.go dry-run note | Compliant |
| FR-005 remove, restore, replace-user naming | plan.go `placeKind`, `planJSON`, `planTOML`; apply.go `ReadDocFor` | Compliant (fixed in review: record-only files are now read) |
| FR-006 key command, first line, quoted reference, no key value | launch/keycmd.go (`IFS= read -r`, `ShellQuote`, validated env names) | Compliant |
| FR-006a neutralizing empty auth entries | plan.go `desiredState` | Compliant (fixed in review: exec-hook variables are not neutralized) |
| FR-007 reference-valued extras wrapper-only | carrier.go `ReasonReferenceValue` | Compliant |
| FR-008 never write the linked default file | carrier.go `linked`, plan.go linked branch, apply.go link refusal | Compliant |
| FR-008a share change drops record, dry run lists effects | internal/cli/edit.go `launchLevelEffects`, `dropLaunchRecordFiles` | Compliant (fixed in review: record failure no longer aborts an applied change) |
| FR-009 Codex provider auth, env_key replaced, recorded, restored, provider switch | plan.go `planTOML`, `selectedProvider` | Compliant |
| FR-009a parse/shape errors leave files, wrapper, registry unchanged; non-zero exit; re-read and atomic write | apply.go re-plan before write, `config.WriteFileAtomic`; sync.go `preserve` | Compliant |
| FR-010 settings pull keeps launch entries | internal/cli/settings.go `launch.Apply` after the pull | Compliant |
| FR-011 idempotent sync | apply.go no-op path, record `Save` writes only on change | Compliant (fixed in review: duplicate variable names no longer alternate) |
| FR-012 wrapper clear-only for folder-carried settings | wrapper/env.go `m.clear`, wrapper.go `renderAuth` | Compliant |
| FR-013 wrapper-only settings set unconditionally | wrapper.go `renderLaunch` | Compliant |
| FR-014 clear launch_clear and inherited APROFILE_SET_VARS; folder carries SET_VARS | wrapper.go `renderCleanup`, env.go `Environ`, plan.go `desiredState` | Compliant (fixed in review: user APROFILE_* variables no longer leak) |
| FR-015 folder key checked, not exported | secret/render.go `CheckShell`, wrapper.go `renderAuth` | Compliant |
| FR-016 Codex model as quoted `-c model="..."` before caller args | definition.go `ExpandModelArg`, wrapper.go `renderGuardAndExec` | Compliant |
| FR-017 docs and help without env overrides | README.md, docs/README.md, add.go flag help | Compliant |
| FR-018 registry `model` field, format version 1 | internal/registry/registry.go, docs/README.md | Compliant |
| FR-019 registry model from profile store only | sync.go registry entry `Model: p.Model` | Compliant |
| FR-020 show/doctor self-containment and reasons, doctor as info | launch/report.go, cli/list_show.go `printFolder`, doctor.go `launchFindings` | Compliant |
| FR-020a stale folder in list/show/doctor | launch.Status `SyncStale`; list "folder stale", show, doctor problem | Compliant |
| FR-020b unplaced launch entries reported, names only | plan.go `unplaced`, report.go `UnplacedText` | Compliant (linked-file fix wording differs slightly, Minor M14) |
| FR-021 dry run lists entries, never values | sync.go `LaunchStepText` | Compliant |
| FR-022 Corgi documentation, aprofile never touches Corgi | docs/guide/docs/user/corgi.md | Compliant (table corrected in review) |
| FR-023 Codex accepts `--model` | definition.go `CarriesModel`, add.go, edit.go, fit.go | Compliant |
| FR-024 constitution 2.0.0 and superseding notes | .specify/memory/constitution.md; spec 001 and 002 notes; claude.yaml comment | Compliant |

## Edge Cases

All 22 edge-case bullets map to code and tests: cleared-variable key (carrier.go `ReasonClearedSource`), `env:` key env-dependent (report.go `FolderSelfContainedEnv`), missing key file (wrapper check, key command prints nothing), `oauth-token`, login kinds, reference extras, share `all`, user entry with same name (replace-user), share level change (FR-008a), unreadable file (FR-009a), user edits placed entry (restore), launch setting removed (remove), settings pull, Codex default provider, env_key replacement and restore, provider switch, unplaced entries, folder-only session SET_VARS, profile without model, harness without support.

## Extra Features (not in spec)

- `ReasonHookEnv`: exec-hook `env` variables are reported as wrapper-only with their own reason. Helpful; consistent with FR-020's intent. Candidate for a spec note.
- `harness show` prints the new definition fields. Helpful; consistent with Principle I.

## Deep Review Report

**Gate: PASS** (after fix round 2; the re-review of round 2 and CodeRabbit found no Critical or Important issues)

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 0 | 0 | 0 |
| Important | 13 | 13 | 0 |
| Minor | 31 | 6 | 25 |
| Notable | 0 | - | 0 |

| Agent | Found | Fixed | Remaining | Status |
|---|---|---|---|---|
| Correctness (incl. 2 re-reviews) | 9 | 8 (+1 merged) | 1 | completed |
| Architecture & Idioms | 12 | 3 | 9 | completed |
| Security | 2 | 0 | 2 | completed |
| Production Readiness | 6 | 2 | 4 | completed |
| Test Quality | 12 | 6 | 6 | completed |
| Goal Alignment | 0 | 0 | 0 | skipped (no PR found) |
| CodeRabbit (external) | 1 | 1 | 0 | completed (re-reviews: 0, 0) |
| Copilot (external) | 0 | 0 | 0 | skipped (disabled in config) |
| Codex (external) | 3 | 2 | 1 | completed |
| Test Suite (regression) | 0 | 0 | 0 | passed |

Fix commits: f2808ce, 4e2bfe6, b3cebe9, 5621836, 97b246f, 78ec0b9, 32076e7, 0a5dc37, 601f7e3.

Details, rationale and the remaining Minor findings with suggested fixes: [review-findings.md](review-findings.md).

Final checks: `go test -count=1 ./...` passed, `go vet ./...` clean, `gofmt -l .` empty.
