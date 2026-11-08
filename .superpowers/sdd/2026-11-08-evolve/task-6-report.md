# Task 6 report: sharing and history treatment

Status: DONE_WITH_CONCERNS

## Changes

- Updated `docs/guide/docs/user/share-levels.md`, preserving the existing headings and their order, including `### History and sessions`.
- Separated the share-level table from history-group membership and added the grouped/ungrouped history comparison across `all`, `content`, and `none`.
- Clarified that credentials stay private regardless of group membership and settings continue to follow the share-level table.
- Explained that groups select supported history entries among profiles of one harness, linked `history-groups.md`, summarized Claude Code, Codex CLI, Pi, and OpenCode support, and distinguished group sharing from cross-harness transcript conversion.
- Extended the page-specific rules in `docs/guide/guide-rules.yaml`.
- Marked only Task 6 checkboxes in the approved plan complete.

## Source claims

All ranges below refer to the verified target checkout `docs/guide/.guide/cache/aprofile-next/`.

- `aprofile:README.md:42-48` supports that share level governs use of the default setup, logins remain private, and the three levels have distinct setup behavior.
- `aprofile:README.md:62-73` supports the credentials, history, runtime state, owned data, content, settings, plugin-store, and hooks rows and their `all`/`content`/`none` behavior.
- `aprofile:README.md:83-102` supports concurrent settings/plugin-store behavior and settings replacement, backup, pull, and onboarding statements retained in the page.
- `aprofile:README.md:112-120` supports history groups as sets of profiles of one harness, isolated ungrouped history, membership independent of share level, and private logins/settings/runtime state.
- `aprofile:README.md:138-142` supports group join/leave behavior and the per-harness statement: OpenCode does not support history groups because sessions and logins share one database; `doctor` and `sync` behavior is unchanged in the source-backed page material.
- `aprofile:README.md:189` supports that copy never carries history and that cross-harness copy handles configuration such as MCP servers.
- `aprofile:internal/cli/copy.go:231-255` supports the distinction between share-level selection and target history-group selection on copy, including that copied history is not transferred.
- `aprofile:internal/harness/builtin/claude.yaml:232-249` supports which Claude Code history entries are marked for group sharing and which runtime entries remain private.
- `aprofile:internal/harness/builtin/codex.yaml:53-61` supports the Codex session and history entries marked for group sharing.
- `aprofile:internal/harness/builtin/opencode.yaml:51-61` supports that OpenCode's sessions database is runtime state rather than groupable history.
- `aprofile:internal/harness/builtin/pi.yaml:39-51` supports Pi's history note and session entries marked for group sharing.

## Checks

- Rule-first proof: `bash .superpowers/sdd/2026-11-08-evolve/validate-target.sh` failed before the page edit with four expected missing-rule violations: `ungrouped`, `grouped`, `history-groups.md`, and `within one harness`.
- Final target validation: `bash .superpowers/sdd/2026-11-08-evolve/validate-target.sh` passed, including guide configuration validation, rendering, rule checks, and leak check.
- `git diff --check` passed.
- cc-prose content-generator and pre-validator/check tools were not callable in this session; tutorial voice and the available guide style rules were applied manually.

## Findings and upstream candidates

- The previous page said aprofile provided no group command and characterized history as private at every share level. The pinned target README describes the group command and history groups; membership is separate from share level. The updated page follows the target source. The previous blanket statement about no history mirroring was imprecise: group members use selected shared history entries, though this is not cross-harness conversion or continuous transcript mirroring.
- No contradiction among the inspected pinned target sources was found, so there are no upstream candidates to report.

## Unverified and concerns

- No unsupported facts were added. Exact per-harness history contents are intentionally summarized instead of enumerated; the linked history-groups tutorial covers operation details.
- The cc-prose workflow limitation is the only process concern. The page's prose was checked against the tutorial voice, guide terms, and no-dash constraint manually.

Commit: pending.
